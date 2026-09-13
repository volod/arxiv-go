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

// RestoreResolver rolls an interrupted restore transaction forward. WriteStub removes an
// owned stub (or is a no-op when --stubs keep). RemoveSource is a no-op when --transfer copy
// keeps the video archive file.
type RestoreResolver struct {
	FS         fsops.Ops
	Verify     fsops.VerifyMode
	Crash      state.CrashHook
	KeepStubs  bool
	KeepSource bool
	Archive    string

	hints *stubHints // stub_rel_path by rel_path, loaded once from the archive's video registry
}

// stubHints caches the stub paths of the video registry. The registry is rewritten only in the
// report phase, after every transaction, so one load serves recovery and execute.
type stubHints struct {
	once  sync.Once
	byRel map[string]string
}

func NewRestoreResolver(r RestoreResolver) RestoreResolver {
	if r.FS == nil {
		r.FS = fsops.System{}
	}
	r.hints = &stubHints{}
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
	return hitSplit(r.Crash, "fs:delete_part")
}

// StubPath is the owned stub that WriteStub removes (or keeps with --stubs keep), or "" when
// there is none. Callers record it before WriteStub runs.
func (r RestoreResolver) StubPath(tx state.Tx) string {
	return r.ownedStub(tx)
}

func (r RestoreResolver) WriteStub(tx state.Tx) error {
	if r.KeepStubs {
		return hitSplit(r.Crash, "fs:stub")
	}
	path := r.ownedStub(tx)
	if path == "" {
		return hitSplit(r.Crash, "fs:stub")
	}
	if err := os.Remove(path); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	if err := fsops.SyncDir(filepath.Dir(path)); err != nil {
		return err
	}
	return hitSplit(r.Crash, "fs:stub")
}

func (r RestoreResolver) RemoveSource(tx state.Tx) error {
	if r.KeepSource {
		return nil
	}
	split := SplitResolver{FS: r.FS, Verify: r.Verify, Crash: r.Crash}
	return split.RemoveSource(tx)
}

func (r RestoreResolver) ownedStub(tx state.Tx) string {
	rel := tx.Begin.RelPath
	for _, p := range r.stubCandidates(tx) {
		occ, err := report.InspectStub(p, rel)
		if err == nil && occ == report.StubOwned {
			return p
		}
	}
	return ""
}

func (r RestoreResolver) stubCandidates(tx state.Tx) []string {
	dst := tx.Begin.Dst
	out := []string{dst + ".md", dst + ".arxgo.md"}
	if hint := r.stubHint(tx.Begin.RelPath); hint != "" {
		out = append([]string{hint}, out...)
	}
	return out
}

func (r RestoreResolver) stubHint(rel string) string {
	if r.Archive == "" {
		return ""
	}
	h := r.hints
	if h == nil {
		h = &stubHints{}
	}
	h.once.Do(func() {
		h.byRel = map[string]string{}
		rows, err := report.LoadVideoFile(filepath.Join(r.Archive, scanner.VideoRegistryName))
		if err != nil {
			return
		}
		for _, row := range rows {
			if row.StubRelPath == "" || !scanner.LocalRelPath(row.StubRelPath) {
				continue
			}
			for _, key := range []string{row.RelPath, row.VideoRelPath} {
				if _, ok := h.byRel[key]; !ok && key != "" {
					h.byRel[key] = row.StubRelPath
				}
			}
		}
	})
	if stub := h.byRel[rel]; stub != "" {
		return filepath.Join(r.Archive, filepath.FromSlash(stub))
	}
	return ""
}

var _ state.Resolver = RestoreResolver{}
