package archive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/internal/state/crashtest"
)

func TestCorruptWALOnResumeNeedsOperator(t *testing.T) {
	r, clock := newRoots(t), newClock()
	s := start(t, testConfig(r, clock, 100))
	w, err := s.OpenWAL()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := w.Begin(state.Begin{Op: "split", RelPath: "a.mp4", Src: "s", Dst: "d", Size: 1}); err != nil {
		t.Fatal(err)
	}
	s.Finish(context.Background(), context.Canceled)

	path := s.Run.File(state.WALFile)
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, append(data, []byte("not-json\n{\"v\":1}\n")...), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Start(context.Background(), testConfig(r, clock, 200))
	if !errors.Is(err, state.ErrStateCorrupt) {
		t.Fatalf("Start = %v, want ErrStateCorrupt", err)
	}
	if st := StartFailed(context.Background(), s.Log, err); st != StatusNeedsOperator {
		t.Errorf("StartFailed = %v", st)
	}
}

func TestSessionRecoversInterruptedTransaction(t *testing.T) {
	r, clock := newRoots(t), newClock()
	layout := crashtest.NewLayout(filepath.Dir(r.archive), "n/clip.mp4")
	h := &crashtest.Hook{FailAt: "fs:place"}
	op, err := crashtest.SplitOp(layout, true, h)
	if err != nil {
		t.Fatal(err)
	}

	cfg := testConfig(r, clock, 100)
	cfg.Crash = h.Func()
	s := start(t, cfg)
	w, err := s.OpenWAL()
	if err != nil {
		t.Fatal(err)
	}
	err = crashtest.Catch(func() error { return op.Execute(context.Background(), w) })
	if !errors.Is(err, crashtest.ErrCrash) {
		t.Fatalf("execute = %v", err)
	}
	res := s.Finish(context.Background(), err)
	if res.Status != StatusFailed {
		t.Fatalf("status = %v", res.Status)
	}

	h.FailAt = ""
	cfg = testConfig(r, clock, 200)
	cfg.Recoverer = state.FSResolver{}
	cfg.Crash = h.Func()
	resumed := start(t, cfg)
	if !resumed.Resumed || resumed.Run.ID != s.Run.ID {
		t.Fatalf("resume id = %s", resumed.Run.ID)
	}
	w, err = resumed.OpenWAL()
	if err != nil {
		t.Fatal(err)
	}
	if err := op.Execute(context.Background(), w); err != nil {
		t.Fatal(err)
	}
	if err := op.FinalOK(); err != nil {
		t.Fatal(err)
	}
	if cp := readCheckpoint(t, resumed); cp.WALOffset <= 0 {
		t.Fatalf("wal_offset = %d", cp.WALOffset)
	}
	if res := resumed.Finish(context.Background(), nil); res.Status != StatusCompleted {
		t.Fatalf("finish = %+v", res)
	}
}
