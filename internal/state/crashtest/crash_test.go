package crashtest

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/state"
)

func TestCrashEachConvergesToUninterruptedState(t *testing.T) {
	cases := []struct {
		name   string
		points []string
		setup  func(dir string, h *Hook) (*Op, error)
	}{
		{"split-copy", CopyPoints, func(dir string, h *Hook) (*Op, error) {
			return SplitOp(NewLayout(dir, "n/clip.mp4"), false, h)
		}},
		{"split-rename", RenamePoints, func(dir string, h *Hook) (*Op, error) {
			return SplitOp(NewLayout(dir, "n/clip.mp4"), true, h)
		}},
		{"restore-copy", RestoreCopyPoints, func(dir string, h *Hook) (*Op, error) {
			return RestoreOp(NewLayout(dir, "n/clip.mp4"), false, h)
		}},
		{"restore-rename", RestoreRenamePoints, func(dir string, h *Hook) (*Op, error) {
			return RestoreOp(NewLayout(dir, "n/clip.mp4"), true, h)
		}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			goldDir := t.TempDir()
			goldOp, err := tc.setup(goldDir, nil)
			if err != nil {
				t.Fatal(err)
			}
			goldWAL := mustWAL(t, goldDir, nil)
			if err := goldOp.Execute(context.Background(), goldWAL); err != nil {
				t.Fatal(err)
			}
			if err := goldOp.FinalOK(); err != nil {
				t.Fatalf("golden: %v", err)
			}
			_ = goldWAL.Close()

			for _, point := range tc.points {
				t.Run(point, func(t *testing.T) {
					dir := t.TempDir()
					h := &Hook{FailAt: point}
					op, err := tc.setup(dir, h)
					if err != nil {
						t.Fatal(err)
					}
					w := mustWAL(t, dir, h.Func())
					err = Catch(func() error { return op.Execute(context.Background(), w) })
					if !errors.Is(err, ErrCrash) {
						t.Fatalf("execute err = %v, want crash", err)
					}
					if err := w.Close(); err != nil {
						t.Fatal(err)
					}

					h.FailAt = ""
					w, err = state.OpenWAL(filepath.Join(dir, "wal.jsonl"), "20260913T101500Z-1a2b3c4d", state.WALOptions{
						Now: func() time.Time { return time.Date(2026, 9, 13, 7, 15, 0, 0, time.UTC) }, Crash: h.Func(),
					})
					if err != nil {
						t.Fatal(err)
					}
					if _, err := state.Recover(context.Background(), w, state.FSResolver{Crash: h.Func()}, nil); err != nil {
						t.Fatalf("recover: %v", err)
					}
					if err := op.Execute(context.Background(), w); err != nil {
						t.Fatalf("resume: %v", err)
					}
					if err := op.FinalOK(); err != nil {
						t.Fatalf("final: %v", err)
					}
					before, err := os.ReadFile(w.Path())
					if err != nil {
						t.Fatal(err)
					}
					if _, err := state.Recover(context.Background(), w, state.FSResolver{}, nil); err != nil {
						t.Fatal(err)
					}
					after, err := os.ReadFile(w.Path())
					if err != nil {
						t.Fatal(err)
					}
					if string(before) != string(after) {
						t.Fatal("recovery is not idempotent after resume")
					}
					_ = w.Close()
				})
			}
		})
	}
}

func TestCrashHookPanicIsCaught(t *testing.T) {
	dir := t.TempDir()
	h := &Hook{FailAt: "wal:begin", Panic: true}
	op, err := SplitOp(NewLayout(dir, "a.mp4"), true, h)
	if err != nil {
		t.Fatal(err)
	}
	w := mustWAL(t, dir, h.Func())
	err = Catch(func() error { return op.Execute(context.Background(), w) })
	if !errors.Is(err, ErrCrash) {
		t.Fatalf("err = %v", err)
	}
}

func mustWAL(t *testing.T, dir string, crash func(string) error) *state.WAL {
	t.Helper()
	w, err := state.OpenWAL(filepath.Join(dir, "wal.jsonl"), "20260913T101500Z-1a2b3c4d", state.WALOptions{
		Now:   func() time.Time { return time.Date(2026, 9, 13, 7, 15, 0, 0, time.UTC) },
		Crash: crash,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = w.Close() })
	return w
}
