package archive

import (
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// registryBase is the file registry at the scan's target path, opened before the scan replaces it.
// Its rows are read on demand: by rel_path for preserved rows, and as a stream in walk order next
// to the walk for present entries.
type registryBase struct {
	path string
	f    *os.File
	size int64
	id   state.FileID
	// stamped: the registry stamp names this path with this size and SHA-256, so the file is the
	// base of docs/openspec/stage-1-core/registry.md#incremental-update.
	stamped bool
	// reusable: stamped, detected with the run's settings and --redetect not given, so its rows are
	// the values detection would write for an unchanged file.
	reusable bool
	// scanStarted is the stamp's scan_started_at: only files last modified before it are reused.
	scanStarted time.Time
}

// openRegistryBase opens the registry at c.Registry, hashes it and checks it against the archive's
// stamp. A missing or unreadable registry is no base (nil).
func openRegistryBase(s *Session, c ScanConfig) *registryBase {
	f, err := os.Open(c.Registry)
	if err != nil {
		if !errors.Is(err, fs.ErrNotExist) {
			s.Log.Warn("existing file registry unreadable; not reusing its rows", "registry", c.Registry, "error", err)
		}
		return nil
	}
	h := sha256.New()
	n, err := io.Copy(h, f)
	if err != nil {
		_ = f.Close()
		s.Log.Warn("existing file registry unreadable; not reusing its rows", "registry", c.Registry, "error", err)
		return nil
	}
	b := &registryBase{path: c.Registry, f: f, size: n, id: state.FileID{Size: n, SHA256: hex.EncodeToString(h.Sum(nil))}}
	if st, ok := state.ReadRegistryStamp(s.cfg.Archive); ok {
		b.stamped = sameFilePath(st.Registry, c.Registry) && st.Size == b.id.Size && st.SHA256 == b.id.SHA256
		b.reusable = b.stamped && !c.Redetect && st.Detect.Equal(detectSettings(s, c))
		b.scanStarted = st.ScanStartedAt.UTC().Truncate(time.Second)
	}
	return b
}

// baseID is the identity a checkpoint records for the base: nil without a stamped base.
func (b *registryBase) baseID() *state.FileID {
	if b == nil || !b.stamped {
		return nil
	}
	id := b.id
	return &id
}

// reader streams the base rows from the start of the file.
func (b *registryBase) reader() (*report.RegistryReader, error) {
	return report.NewRegistryReader(bufio.NewReaderSize(io.NewSectionReader(b.f, 0, b.size), 1<<20))
}

// rowsFor reads the base rows whose rel_path want selects. A registry that cannot be parsed yields
// none, with a warning.
func (b *registryBase) rowsFor(s *Session, want func(rel string) bool) map[string]report.RegistryRow {
	out := map[string]report.RegistryRow{}
	rr, err := b.reader()
	for err == nil {
		var row report.RegistryRow
		if row, err = rr.Next(); err == nil && want(row.RelPath) {
			out[row.RelPath] = row
		}
	}
	if err != io.EOF {
		s.Log.Warn("existing file registry cannot be parsed; not reusing its rows", "registry", b.path, "error", err)
		return map[string]report.RegistryRow{}
	}
	return out
}

func (b *registryBase) close() error {
	if b == nil {
		return nil
	}
	return b.f.Close()
}

func sameFilePath(a, b string) bool { return filepath.Clean(a) == filepath.Clean(b) }

// detectSettings are the run's detection settings as the registry stamp records them.
func detectSettings(s *Session, c ScanConfig) state.DetectSettings {
	metadata := c.Metadata
	if metadata == "" {
		metadata = "file"
	}
	exts := slices.Clone(scanner.BuiltinVideoExtensions)
	for _, e := range c.VideoExtensions {
		if e = strings.ToLower(strings.TrimPrefix(e, ".")); e != "" {
			exts = append(exts, e)
		}
	}
	slices.Sort(exts)
	return state.DetectSettings{Version: s.cfg.Version, Metadata: metadata, VideoExtensions: slices.Compact(exts)}
}

// placedRegistry describes the registry file at its final path after placeRegistry.
type placedRegistry struct {
	outcome string // state.RegistryWritten or state.RegistryUnchanged
	size    int64
	sha256  string
}

// placeRegistry puts a completed part file at final: when final already holds the same bytes the
// part file is removed and final keeps its modification time, otherwise the part file replaces it.
func placeRegistry(part, final string) (placedRegistry, error) {
	p, equal, err := compareRegistry(part, final)
	if err != nil {
		return p, err
	}
	if equal {
		p.outcome = state.RegistryUnchanged
		if err := os.Remove(part); err != nil {
			return p, fmt.Errorf("remove unchanged registry part: %w", err)
		}
		return p, nil
	}
	p.outcome = state.RegistryWritten
	if err := fsops.Replace(part, final); err != nil {
		return p, fmt.Errorf("place registry: %w", err)
	}
	return p, nil
}

// compareRegistry hashes part and compares it with final in one pass.
func compareRegistry(part, final string) (placedRegistry, bool, error) {
	var p placedRegistry
	src, err := os.Open(part)
	if err != nil {
		return p, false, err
	}
	defer src.Close()
	old, err := os.Open(final)
	if err != nil {
		old = nil
	} else {
		defer old.Close()
	}
	h := sha256.New()
	a, b := make([]byte, 1<<20), make([]byte, 1<<20)
	equal := old != nil
	for {
		n, rerr := io.ReadFull(src, a)
		h.Write(a[:n])
		p.size += int64(n)
		if equal {
			m, _ := io.ReadFull(old, b[:n])
			equal = m == n && bytes.Equal(a[:n], b[:n])
		}
		if rerr == io.EOF || rerr == io.ErrUnexpectedEOF {
			break
		}
		if rerr != nil {
			return p, false, rerr
		}
	}
	if equal {
		// final must end where part ends.
		var one [1]byte
		n, _ := old.Read(one[:])
		equal = n == 0
	}
	p.sha256 = hex.EncodeToString(h.Sum(nil))
	return p, equal, nil
}

// writeRegistryStamp records the registry the run placed or found unchanged.
func writeRegistryStamp(s *Session, c ScanConfig, st *state.ScanStats, p placedRegistry) error {
	stamp := state.RegistryStamp{
		Registry: c.Registry, Size: p.size, SHA256: p.sha256, RunID: s.Run.ID, Op: s.cfg.Op,
		ScanStartedAt: st.StartedAt, Detect: detectSettings(s, c),
	}
	if err := state.WriteRegistryStamp(s.cfg.Archive, stamp); err != nil {
		return fmt.Errorf("write registry stamp: %w", err)
	}
	return nil
}

// writeFileIfChanged atomically writes data to path unless path already holds exactly data, so an
// unchanged registry keeps its modification time.
func writeFileIfChanged(fsys fsops.Ops, path string, data []byte) error {
	if old, err := os.ReadFile(path); err == nil && bytes.Equal(old, data) {
		return nil
	}
	return fsys.AtomicWriteFile(path, data, 0o644)
}
