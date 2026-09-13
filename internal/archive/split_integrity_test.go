package archive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

func TestSplitDirectoryAtStubPathUsesFallback(t *testing.T) {
	r, src, dst := splitFixture(t)
	if err := os.Mkdir(src+".md", 0o755); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if fi, err := os.Stat(src + ".md"); err != nil || !fi.IsDir() {
		t.Fatalf("directory at the stub path changed: %v", err)
	}
	if got := string(mustRead(t, src+".arxgo.md")); !strings.Contains(got, "rel_path: nested/clip.mp4") {
		t.Fatalf("fallback stub:\n%s", got)
	}
	if exists(src) || !exists(dst) {
		t.Fatal("video not moved")
	}
}

func TestConflictAbortIsDurableBeforePartRemoval(t *testing.T) {
	r, src, dst := splitFixture(t)
	foreign := bytes.Repeat([]byte{'x'}, len(videoFixture)) // same size, other bytes
	h := &crashtest.Hook{FailAt: "wal:aborted"}
	cfg, c := splitConfig(r, "copy")
	cfg.Crash = h.Func()
	attachRecoverer(&cfg, c, h.Func())
	c.StageCopy = func(ctx context.Context, from, to string, o fsops.CopyOptions) (fsops.CopyResult, error) {
		res, err := fsops.StageCopy(ctx, from, to, o)
		if err == nil {
			err = os.WriteFile(to, foreign, 0o644) // appears after the copy, before placement
		}
		return res, err
	}
	if res := runSplit(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("crash = %+v", res)
	}
	if !exists(fsops.PartPath(dst)) {
		t.Fatal("part file removed before the abort was logged")
	}
	cfg, c = splitConfig(r, "copy")
	cfg.Lock.PID = 200
	if res := runSplit(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("rerun = %+v", res)
	}
	if !bytes.Equal(mustRead(t, src), videoFixture) || !bytes.Equal(mustRead(t, dst), foreign) {
		t.Fatal("source or foreign destination changed")
	}
	if exists(src+".md") || exists(fsops.PartPath(dst)) {
		t.Fatalf("stub %v or stale part %v remains", exists(src+".md"), exists(fsops.PartPath(dst)))
	}
}

func TestSplitReportsFileNamesThatAreNotUTF8(t *testing.T) {
	r := newRoots(t)
	name := "clip-\xff.mp4"
	if err := os.WriteFile(filepath.Join(r.archive, name), videoFixture, 0o644); err != nil {
		t.Skipf("filesystem rejects a name that is not valid UTF-8: %v", err)
	}
	cfg, c := splitConfig(r, "auto")
	rec := &recorder{}
	cfg.Console = rec
	if res := runSplit(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("split = %+v", res)
	}
	if !exists(filepath.Join(r.archive, name)) {
		t.Fatal("video moved or lost")
	}
	var found bool
	for _, m := range rec.messages("item skipped") {
		found = found || strings.Contains(m["reason"], "not valid UTF-8")
	}
	if !found {
		t.Fatalf("skip reason does not name the UTF-8 limit: %v", rec.messages("item skipped"))
	}
}
