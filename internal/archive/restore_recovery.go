package archive

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"

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
}

func NewRestoreResolver(r RestoreResolver) RestoreResolver {
	if r.FS == nil {
		r.FS = fsops.System{}
	}
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

func (r RestoreResolver) StubPath(tx state.Tx) string {
	if p := r.ownedStub(tx); p != "" {
		return p
	}
	return tx.Begin.Dst + ".md"
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
	rows, err := report.LoadVideoFile(filepath.Join(r.Archive, scanner.VideoRegistryName))
	if err != nil || rows == nil {
		return ""
	}
	for _, row := range rows {
		if (row.RelPath == rel || row.VideoRelPath == rel) && row.StubRelPath != "" {
			return filepath.Join(r.Archive, filepath.FromSlash(row.StubRelPath))
		}
	}
	return ""
}

var _ state.Resolver = RestoreResolver{}
