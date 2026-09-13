package archive

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

type exdevFS struct {
	*fakeFS
	archive, video string
}

func (f exdevFS) Rename(old, new string) error {
	if strings.HasPrefix(old, f.archive) && strings.HasPrefix(new, f.video) {
		return fsops.NewCrossDeviceError(old, new)
	}
	return fsops.Rename(old, new)
}

func TestSplitRenameEXDEVFallbackRechecksSpace(t *testing.T) {
	for _, enough := range []bool{false, true} {
		t.Run(map[bool]string{false: "short", true: "enough"}[enough], func(t *testing.T) {
			r, src, dst := splitFixture(t)
			available := uint64(9000)
			if enough {
				available = 1 << 30
			} else if err := os.WriteFile(src, append(append([]byte{}, videoFixture...), make([]byte, 20000)...), 0o640); err != nil {
				t.Fatal(err)
			}
			cfg, c := splitConfig(r, "auto")
			cfg.FS = exdevFS{fakeFS: &fakeFS{
				device: func(string) string { return "disk" },
				space:  map[string]fsops.Space{"disk": {Total: 1 << 40, Available: available}},
			}, archive: r.archive, video: r.video}
			res := runSplit(t, cfg, c)
			if !enough {
				if res.Status != StatusInsufficientSpace || !exists(src) || exists(dst) {
					t.Fatalf("shortfall = %+v", res)
				}
				return
			}
			if res.Status != StatusCompleted {
				t.Fatalf("fallback = %+v", res)
			}
			checkSplit(t, src, dst)
		})
	}
}

func TestSplitChangedSourceRetries(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "copy")
	var calls int
	c.StageCopy = func(ctx context.Context, from, to string, o fsops.CopyOptions) (fsops.CopyResult, error) {
		calls++
		if calls == 1 {
			if err := os.WriteFile(fsops.PartPath(to), []byte("incomplete"), 0o600); err != nil {
				return fsops.CopyResult{}, err
			}
			if err := os.WriteFile(from, videoFixture, 0o640); err != nil {
				return fsops.CopyResult{}, err
			}
			return fsops.CopyResult{}, fsops.ErrSourceChanged
		}
		return fsops.StageCopy(ctx, from, to, o)
	}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted || calls != 2 {
		t.Fatalf("retry = %+v, calls %d", res, calls)
	}
	checkSplit(t, src, dst)
}

func TestSplitChangedSourceTwiceSkipsAndRemovesPart(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "copy")
	c.StageCopy = func(_ context.Context, _, to string, _ fsops.CopyOptions) (fsops.CopyResult, error) {
		if err := os.WriteFile(fsops.PartPath(to), []byte("incomplete"), 0o600); err != nil {
			return fsops.CopyResult{}, err
		}
		return fsops.CopyResult{}, fsops.ErrSourceChanged
	}
	if res := runSplit(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("twice changed = %+v", res)
	}
	if !exists(src) || exists(dst) || exists(fsops.PartPath(dst)) {
		t.Error("changed source or part was not preserved correctly")
	}
}

func TestSplitDestinationAppearedDuringCopyIsSkipped(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "copy")
	c.StageCopy = func(ctx context.Context, from, to string, o fsops.CopyOptions) (fsops.CopyResult, error) {
		if err := os.MkdirAll(filepath.Dir(to), 0o755); err != nil {
			return fsops.CopyResult{}, err
		}
		if err := os.WriteFile(to, videoFixture, 0o644); err != nil {
			return fsops.CopyResult{}, err
		}
		return fsops.StageCopy(ctx, from, to, o)
	}
	if res := runSplit(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("appeared = %+v", res)
	}
	if !exists(src) || exists(src+".md") || exists(fsops.PartPath(dst)) {
		t.Error("conflict during copy did not leave source and drop the part")
	}
	if got := mustRead(t, dst); !bytes.Equal(got, videoFixture) {
		t.Error("destination that appeared mid-copy was overwritten")
	}
}

func TestSplitSkipsSourceRemovedAfterScan(t *testing.T) {
	r, src, dst := splitFixture(t)
	second := filepath.Join(r.archive, "other.mp4")
	if err := os.WriteFile(second, videoFixture, 0o640); err != nil {
		t.Fatal(err)
	}
	h := &crashtest.Hook{FailAt: "wal:commit"}
	cfg, c := splitConfig(r, "auto")
	cfg.Crash = h.Func()
	cfg.Recoverer = NewSplitResolver(nil, c.Verify, h.Func())
	res := runSplit(t, cfg, c)
	if !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("first commit was not reached: %+v", res)
	}
	if err := os.Remove(second); err != nil {
		t.Fatal(err)
	}
	h.FailAt = ""
	cfg, c = splitConfig(r, "auto")
	cfg.Lock.PID = 200
	res = runSplit(t, cfg, c)
	if res.Status != StatusPartial {
		t.Fatalf("resume = %+v", res)
	}
	checkSplit(t, src, dst)
	if exists(filepath.Join(r.video, "other.mp4")) {
		t.Error("missing source was still transferred")
	}
}

type flushFailFS struct {
	fsops.System
	failDst string
}

func (f flushFailFS) Rename(old, new string) error {
	if err := fsops.Rename(old, new); err != nil {
		return err
	}
	if new == f.failDst {
		return fmt.Errorf("directory flush failed after rename")
	}
	return nil
}

func TestSplitFlushErrorAfterRenameContinuesAsPlaced(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	cfg.FS = flushFailFS{failDst: dst}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("flush = %+v", res)
	}
	checkSplit(t, src, dst)
}

func TestSplitCopyRecoversNanosecondMtime(t *testing.T) {
	r, src, dst := splitFixture(t)
	want := time.Date(2024, 5, 1, 10, 22, 3, 123456789, time.Local)
	if err := os.Chtimes(src, time.Time{}, want); err != nil {
		t.Fatal(err)
	}
	fi, err := os.Lstat(src)
	if err != nil {
		t.Fatal(err)
	}
	if fi.ModTime().Nanosecond() == 0 {
		t.Skip("filesystem stores mtime at second resolution")
	}
	h := &crashtest.Hook{FailAt: "wal:placed"}
	cfg, c := splitConfig(r, "copy")
	cfg.Crash = h.Func()
	cfg.Recoverer = NewSplitResolver(nil, c.Verify, h.Func())
	res := runSplit(t, cfg, c)
	if !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("placed was not reached: %+v", res)
	}
	h.FailAt = ""
	cfg, c = splitConfig(r, "copy")
	cfg.Lock.PID = 200
	res = runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("resume = %+v", res)
	}
	checkSplit(t, src, dst)
}

func TestSplitMirrorsNestedAndUnicodePaths(t *testing.T) {
	r := newRoots(t)
	rel := "deep/a/b/clip-\u03b1.mp4"
	src := filepath.Join(r.archive, filepath.FromSlash(rel))
	dst := filepath.Join(r.video, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(src), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, videoFixture, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if exists(src) {
		t.Error("source still exists")
	}
	if got := mustRead(t, dst); !bytes.Equal(got, videoFixture) {
		t.Error("destination bytes differ")
	}
	if !strings.Contains(string(mustRead(t, src+".md")), "rel_path: "+rel) {
		t.Error("placeholder stub missing")
	}
	for _, dir := range []string{"deep", "deep/a", "deep/a/b"} {
		fi, err := os.Stat(filepath.Join(r.video, filepath.FromSlash(dir)))
		if err != nil || !fi.IsDir() {
			t.Fatalf("mirror %s: %v", dir, err)
		}
		srcDir, err := os.Stat(filepath.Join(r.archive, filepath.FromSlash(dir)))
		if err != nil {
			t.Fatal(err)
		}
		if fi.Mode().Perm() != srcDir.Mode().Perm() {
			t.Errorf("mirror %s mode = %v, want %v", dir, fi.Mode().Perm(), srcDir.Mode().Perm())
		}
	}
}

func TestSplitAutoCopiesWhenRootsOnDifferentFilesystems(t *testing.T) {
	shm, err := os.MkdirTemp("/dev/shm", "arxgo-split-")
	if err != nil {
		t.Skip("cannot create temp dir on /dev/shm: ", err)
	}
	t.Cleanup(func() { _ = os.RemoveAll(shm) })
	r := newRoots(t)
	same, err := fsops.SameDevice(r.archive, shm)
	if err != nil || same {
		t.Skip("archive temp dir shares a device with /dev/shm")
	}
	src := filepath.Join(r.archive, "nested", "clip.mp4")
	if err := os.Mkdir(filepath.Dir(src), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, videoFixture, 0o640); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "auto")
	cfg.VideoArchive = shm
	c.Scan.SkipPaths = []string{shm}
	dst := filepath.Join(shm, "nested", "clip.mp4")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("tmpfs split = %+v", res)
	}
	checkSplit(t, src, dst)
	wal, err := os.ReadFile(filepath.Join(state.StateDir(r.archive), "runs", currentRunID(t, r.archive), state.WALFile))
	if err != nil || !strings.Contains(string(wal), `"transfer":"copy"`) {
		t.Fatalf("expected copy across devices: %v, %s", err, wal)
	}
}
