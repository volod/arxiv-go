package archive

import (
	"context"
	"fmt"
	"path/filepath"
	"sync/atomic"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// PayloadKind names what split and restore move. It is stored as "payload" in options.json.
type PayloadKind string

// Payload kinds. Both split; only video restores in this build. Each kind replays only its own
// runs from the state directory.
const (
	PayloadVideo PayloadKind = "video"
	PayloadCatia PayloadKind = "catia"
)

// Payload is the kind a split or restore run moves and its mirror root: the video archive for
// video. Scan runs have none.
type Payload struct {
	Kind PayloadKind
	Root string
}

// MirrorFlag is the command-line flag of the kind's mirror root, for operator messages.
func (k PayloadKind) MirrorFlag() string { return "--" + string(k) + "-archive" }

// payloadCounters are the live counters a payload's split and restore update.
type payloadCounters struct {
	done, skipped, failed, bytes *atomic.Int64
	mirrorWritten, mirrorFreed   *atomic.Int64
}

// payloadTotals are a payload's counters read from a snapshot.
type payloadTotals struct {
	done, skipped, failed, bytes int64
	mirrorWritten, mirrorFreed   int64
}

// splitHooks are the payload-specific steps of the shared split executor.
type splitHooks interface {
	// prepare runs before the scan: it reads history and adds the payload's own outputs to the
	// scan's skip paths. It must not mutate the archive in a dry run.
	prepare(ctx context.Context, c *SplitConfig) error
	// plan adds the payload's work beyond the candidates to the preflight input.
	plan(ctx context.Context, c *SplitConfig, remaining *Candidates) error
	// start begins post-commit sidecar work, catching up files committed earlier.
	start(ctx context.Context, w *state.WAL) (postCommit, error)
	// writeRegistry writes the payload registry after the execute phase.
	writeRegistry(c SplitConfig) error
}

// postCommit is the optional sidecar generated after a file's move commits (video previews).
type postCommit interface {
	committed(rel string) error
	// stop waits for queued work and joins its fatal error with err.
	stop(err error) error
}

// restoreHooks are the payload-specific steps of the shared restore executor.
type restoreHooks interface {
	// rows are the payload registry rows by rel_path; include reports whether a mirror file is a
	// candidate although detection does not select it.
	rows() map[string]report.PayloadRow
	include(rel string) bool
	// beforeExecute finishes interrupted sidecar work of earlier processes.
	beforeExecute(w *state.WAL) error
	// committed runs after the restore of rel commits.
	committed(w *state.WAL, rel string) error
	// updateRegistry marks restored rows in the payload registry.
	updateRegistry(c RestoreConfig) error
}

// payloadSpec is what a payload kind selects in the shared split and restore executor.
type payloadSpec struct {
	kind     PayloadKind
	noun     string // log and preflight wording: "video"
	plural   string // preflight need of the payload bytes: "videos"
	role     Role   // preflight role of the mirror root
	registry string // payload registry file name in the archive and mirror roots
	// candidate selects split candidates from the scan classification.
	candidate  func(scanner.FileType) bool
	counters   func(*Stats) payloadCounters
	totals     func(state.Counters) payloadTotals
	loadRows   func(path string) ([]report.PayloadRow, error)
	newSplit   func(s *Session) splitHooks
	newRestore func(ctx context.Context, s *Session, c *RestoreConfig) (restoreHooks, error) // nil: no restore
}

// payloadSpecs lists the payload kinds this build can split and restore.
var payloadSpecs = map[PayloadKind]*payloadSpec{PayloadVideo: videoPayload, PayloadCatia: catiaPayload}

// specOf returns the executor spec of a kind.
func specOf(kind PayloadKind) (*payloadSpec, error) {
	if spec := payloadSpecs[kind]; spec != nil {
		return spec, nil
	}
	if kind == "" {
		return nil, fmt.Errorf("split and restore need a payload kind")
	}
	return nil, fmt.Errorf("payload %q: not available in this build", kind)
}

// sessionPayload validates the payload of a run configuration: split and restore need a known kind
// and its mirror root, scan has neither.
func sessionPayload(cfg Config) (*payloadSpec, error) {
	switch cfg.Op {
	case opSplit, opRestore:
		spec, err := specOf(cfg.Payload.Kind)
		if err != nil {
			return nil, err
		}
		if cfg.Op == opRestore && spec.newRestore == nil {
			return nil, fmt.Errorf("restore of payload %q: not available in this build", spec.kind)
		}
		if cfg.Payload.Root == "" {
			return nil, fmt.Errorf("%s: %s mirror root is required", cfg.Op, spec.noun)
		}
		return spec, nil
	default:
		if cfg.Payload != (Payload{}) {
			return nil, fmt.Errorf("%s takes no payload", cfg.Op)
		}
		return nil, nil
	}
}

// runPayload returns the payload kind and mirror root an earlier run recorded in options.json.
func runPayload(o state.RunOptions) Payload {
	p := Payload{Kind: PayloadKind(o.Payload)}
	switch p.Kind {
	case PayloadVideo:
		p.Root = o.VideoArchive
	case PayloadCatia:
		p.Root = o.CatiaArchive
	}
	return p
}

// setRunPayload stores the payload and its mirror root in options.json fields.
func setRunPayload(o *state.RunOptions, p Payload) {
	o.Payload = string(p.Kind)
	switch p.Kind {
	case PayloadVideo:
		o.VideoArchive = p.Root
	case PayloadCatia:
		o.CatiaArchive = p.Root
	}
}

// registryPaths are the copies of the payload registry name in the archive and the mirror root.
func registryPaths(s *Session, name string) []string {
	return []string{filepath.Join(s.cfg.Archive, name), filepath.Join(s.cfg.Payload.Root, name)}
}

// corruptRegistry stops a run whose operator registry cannot be parsed.
func corruptRegistry(path string, err error) error {
	return fmt.Errorf("%w: cannot parse %s: %v; restore a valid registry backup or repair the CSV, then rerun", state.ErrStateCorrupt, path, err)
}

// payloadRowsByPath indexes payload registry rows by rel_path. The registry lives in a root that
// may be shared, so a row whose paths would leave a root or name a reserved path is ignored and
// its file, if any, is restored as unregistered to its own relative path.
func payloadRowsByPath(s *Session, rows []report.PayloadRow) map[string]report.PayloadRow {
	out := make(map[string]report.PayloadRow, len(rows))
	for _, r := range rows {
		if !scanner.LocalRelPath(r.RelPath) || (r.DescriptionRelPath != "" && !scanner.LocalRelPath(r.DescriptionRelPath)) {
			s.Log.Warn(s.payload.noun+" registry row ignored: a path is not a local path below its root", "rel_path", r.RelPath, "description_rel_path", r.DescriptionRelPath)
			continue
		}
		out[r.RelPath] = r
	}
	return out
}

// writeRegistryCopies atomically writes data as the payload registry in both roots.
func (sp *payloadSpec) writeRegistryCopies(s *Session, data []byte) error {
	for _, path := range registryPaths(s, sp.registry) {
		if err := s.cfg.FS.AtomicWriteFile(path, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}
