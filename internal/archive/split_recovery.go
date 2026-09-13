package archive

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"strings"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// SplitResolver rolls an interrupted split transaction forward or lets state.Recover abort its
// unplaced part. Its stub writer is intentionally minimal until the report task adds full stubs.
type SplitResolver struct {
	FS     fsops.Ops
	Verify fsops.VerifyMode
	Crash  state.CrashHook
	Stubs  SplitStubWriter
}

// SplitStubWriter is the narrow seam for the later full Markdown renderer. Recovery and normal
// execution use the same writer, so its Path and Write must make the same collision decision.
type SplitStubWriter interface {
	Path(state.Tx) string
	Write(state.Tx) error
}

// PlaceholderStub writes only rel_path front matter until the full stub task is accepted.
type PlaceholderStub struct {
	FS    fsops.Ops
	Crash state.CrashHook
}

func NewSplitResolver(ops fsops.Ops, verify fsops.VerifyMode, crash state.CrashHook) SplitResolver {
	if ops == nil {
		ops = fsops.System{}
	}
	return SplitResolver{FS: ops, Verify: verify, Crash: crash,
		Stubs: PlaceholderStub{FS: ops, Crash: crash}}
}

func (r SplitResolver) Inspect(tx state.Tx) (state.Observation, error) {
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

func regularSize(path string) (bool, int64, error) {
	fi, err := os.Lstat(path)
	if errors.Is(err, fs.ErrNotExist) {
		return false, 0, nil
	}
	if err != nil {
		return false, 0, err
	}
	if !fi.Mode().IsRegular() {
		return false, 0, fmt.Errorf("%w: non-regular transaction path %s", state.ErrStateCorrupt, path)
	}
	return true, fi.Size(), nil
}

func (r SplitResolver) DeletePart(tx state.Tx) error {
	err := os.Remove(fsops.PartPath(tx.Begin.Dst))
	if err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return hitSplit(r.Crash, "fs:delete_part")
}

// StubPath chooses the owned primary path, or the collision fallback when the primary file is
// foreign. A foreign fallback is an error: split must not overwrite it.
func (r SplitResolver) StubPath(tx state.Tx) string {
	return r.Stubs.Path(tx)
}

func (p PlaceholderStub) Path(tx state.Tx) string {
	primary := tx.Begin.Src + ".md"
	if stubOwned(primary, tx.Begin.RelPath) {
		return primary
	}
	return tx.Begin.Src + ".arxgo.md"
}

func stubOwned(path, rel string) bool {
	b, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		return false
	}
	lines := strings.Split(string(b), "\n")
	if len(lines) < 3 || lines[0] != "---" {
		return false
	}
	for _, line := range lines[1:] {
		if line == "---" {
			return false
		}
		if line == "rel_path: "+rel {
			return true
		}
	}
	return false
}

func (r SplitResolver) WriteStub(tx state.Tx) error {
	return r.Stubs.Write(tx)
}

func (p PlaceholderStub) Write(tx state.Tx) error {
	path := p.Path(tx)
	if !stubOwned(path, tx.Begin.RelPath) {
		return fmt.Errorf("stub conflict at %s", path)
	}
	data := []byte("---\nrel_path: " + tx.Begin.RelPath + "\n---\n")
	if err := p.FS.AtomicWriteFile(path, data, 0o644); err != nil {
		return err
	}
	return hitSplit(p.Crash, "fs:stub")
}

func (r SplitResolver) RemoveSource(tx state.Tx) error {
	// Recovery removes a leftover source whenever dest verifies, including a
	// same-device rename that left both copies. The execute path only calls this
	// on the copy transfer.
	fi, err := os.Lstat(tx.Begin.Src)
	if errors.Is(err, fs.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Size() != tx.Begin.Size || !fi.ModTime().Equal(tx.Begin.Mtime) {
		return fmt.Errorf("%w: source changed before removal: %s", fsops.ErrSourceChanged, tx.Begin.Src)
	}
	dst, err := os.Lstat(tx.Begin.Dst)
	if err != nil || !dst.Mode().IsRegular() || dst.Size() != tx.Begin.Size {
		return fmt.Errorf("%w: destination changed before source removal: %s", fsops.ErrVerifyMismatch, tx.Begin.Dst)
	}
	if r.Verify == fsops.VerifyHash {
		ok, err := identicalFiles(tx.Begin.Src, tx.Begin.Dst)
		if err != nil {
			return err
		}
		if !ok {
			return fmt.Errorf("%w: source and destination differ before removal", fsops.ErrVerifyMismatch)
		}
	}
	if err := os.Remove(tx.Begin.Src); err != nil {
		return err
	}
	if err := fsops.SyncDir(filepath.Dir(tx.Begin.Src)); err != nil {
		return err
	}
	return hitSplit(r.Crash, "fs:source_removed")
}

// destinationStatus compares an already present destination without changing either file.
func destinationStatus(src, dst string, size int64, verify fsops.VerifyMode) (adopted, conflict bool, err error) {
	fi, err := os.Lstat(dst)
	if errors.Is(err, fs.ErrNotExist) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	if !fi.Mode().IsRegular() || fi.Size() != size {
		return false, true, nil
	}
	if verify == fsops.VerifySize {
		return true, false, nil
	}
	ok, err := identicalFiles(src, dst)
	return ok, !ok, err
}

func identicalFiles(a, b string) (bool, error) {
	ha, err := hashFile(a)
	if err != nil {
		return false, err
	}
	hb, err := hashFile(b)
	return ha == hb, err
}

func hashFile(path string) ([sha256.Size]byte, error) {
	var zero [sha256.Size]byte
	f, err := os.Open(path)
	if err != nil {
		return zero, err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return zero, err
	}
	var sum [sha256.Size]byte
	copy(sum[:], h.Sum(nil))
	return sum, nil
}

var _ state.Resolver = SplitResolver{}
