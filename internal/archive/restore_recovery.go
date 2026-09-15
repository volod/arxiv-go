package archive

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"sync"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// RestoreResolver rolls an interrupted restore transaction forward. WriteDescription removes an
// owned description (or is a no-op when --descriptions keep). RemoveSource is a no-op when --transfer copy
// keeps the mirror file.
type RestoreResolver struct {
	FS               fsops.Ops
	Verify           fsops.VerifyMode
	Crash            state.CrashHook
	KeepDescriptions bool
	KeepSource       bool
	Archive          string
	Payload          PayloadKind // selects the payload registry that holds description paths

	hints *descriptionHints // description_rel_path by rel_path, loaded once from the archive's payload registry
}

// descriptionHints caches the description paths of the payload registry. The registry is rewritten only in the
// report phase, after every transaction, so one load serves recovery and execute.
type descriptionHints struct {
	once  sync.Once
	byRel map[string]string
}

func NewRestoreResolver(r RestoreResolver) RestoreResolver {
	if r.FS == nil {
		r.FS = fsops.System{}
	}
	r.hints = &descriptionHints{}
	return r
}

func (r RestoreResolver) Inspect(tx state.Tx) (state.Observation, error) {
	var o state.Observation
	var err error
	if o.SrcExists, o.SrcSize, err = regularSize(tx.Begin.Src); err != nil {
		return o, err
	}
	if o.DstExists, o.DstSize, err = regularSize(tx.Begin.Dst); err != nil {
		return o, err
	}
	if o.PartExists, _, err = regularSize(fsops.PartPath(tx.Begin.Dst)); err != nil {
		return o, err
	}
	return o, nil
}

func (r RestoreResolver) DeletePart(tx state.Tx) error {
	err := os.Remove(fsops.PartPath(tx.Begin.Dst))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return hitCrash(r.Crash, "fs:delete_part")
}

// DescriptionPath is the owned description that WriteDescription removes (or keeps with --descriptions keep), or "" when
// there is none. Callers record it before WriteDescription runs.
func (r RestoreResolver) DescriptionPath(tx state.Tx) string {
	return r.ownedDescription(tx)
}

func (r RestoreResolver) WriteDescription(tx state.Tx) error {
	if r.KeepDescriptions {
		return hitCrash(r.Crash, "fs:description")
	}
	path := r.ownedDescription(tx)
	if path == "" {
		return hitCrash(r.Crash, "fs:description")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := fsops.SyncDir(filepath.Dir(path)); err != nil {
		return err
	}
	return hitCrash(r.Crash, "fs:description")
}

func (r RestoreResolver) RemoveSource(tx state.Tx) error {
	if r.KeepSource {
		return nil
	}
	split := SplitResolver{FS: r.FS, Verify: r.Verify, Crash: r.Crash}
	return split.RemoveSource(tx)
}

func (r RestoreResolver) ownedDescription(tx state.Tx) string {
	rel := tx.Begin.RelPath
	for _, p := range r.descriptionCandidates(tx) {
		occ, err := report.InspectDescription(p, rel)
		if err == nil && occ == report.DescriptionOwned {
			return p
		}
	}
	return ""
}

func (r RestoreResolver) descriptionCandidates(tx state.Tx) []string {
	dst := tx.Begin.Dst
	out := []string{dst + ".md", dst + ".arxgo.md"}
	if hint := r.descriptionHint(tx.Begin.RelPath); hint != "" {
		out = append([]string{hint}, out...)
	}
	return out
}

func (r RestoreResolver) descriptionHint(rel string) string {
	if r.Archive == "" {
		return ""
	}
	h := r.hints
	if h == nil {
		h = &descriptionHints{}
	}
	h.once.Do(func() {
		h.byRel = map[string]string{}
		spec, err := specOf(r.Payload)
		if err != nil {
			return
		}
		rows, err := spec.loadRows(filepath.Join(r.Archive, spec.registry))
		if err != nil {
			return
		}
		for _, row := range rows {
			if row.DescriptionRelPath == "" || !scanner.LocalRelPath(row.DescriptionRelPath) {
				continue
			}
			for _, key := range []string{row.RelPath} {
				if _, ok := h.byRel[key]; !ok && key != "" {
					h.byRel[key] = row.DescriptionRelPath
				}
			}
		}
	})
	if description := h.byRel[rel]; description != "" {
		return filepath.Join(r.Archive, filepath.FromSlash(description))
	}
	return ""
}

var _ state.Resolver = RestoreResolver{}
