package integration

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

func TestCrashEachConvergesToUninterruptedState(t *testing.T) {
	cases := []struct {
		name   string
		points []string
		setup  func(dir string, h *crashtest.Hook) (*crashtest.Op, error)
	}{
		{"split-copy", crashtest.CopyPoints, func(dir string, h *crashtest.Hook) (*crashtest.Op, error) {
			return crashtest.SplitOp(crashtest.NewLayout(dir, "n/clip.mp4"), false, h)
		}},
		{"split-rename", crashtest.RenamePoints, func(dir string, h *crashtest.Hook) (*crashtest.Op, error) {
			return crashtest.SplitOp(crashtest.NewLayout(dir, "n/clip.mp4"), true, h)
		}},
		{"restore-copy", crashtest.RestoreCopyPoints, func(dir string, h *crashtest.Hook) (*crashtest.Op, error) {
			return crashtest.RestoreOp(crashtest.NewLayout(dir, "n/clip.mp4"), false, h)
		}},
		{"restore-rename", crashtest.RestoreRenamePoints, func(dir string, h *crashtest.Hook) (*crashtest.Op, error) {
			return crashtest.RestoreOp(crashtest.NewLayout(dir, "n/clip.mp4"), true, h)
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
					h := &crashtest.Hook{FailAt: point}
					op, err := tc.setup(dir, h)
					if err != nil {
						t.Fatal(err)
					}
					w := mustWAL(t, dir, h.Func())
					err = crashtest.Catch(func() error { return op.Execute(context.Background(), w) })
					if !errors.Is(err, crashtest.ErrCrash) {
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
					if _, err := state.Recover(context.Background(), w, crashtest.Resolver{Crash: h.Func()}, nil); err != nil {
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
					if _, err := state.Recover(context.Background(), w, crashtest.Resolver{}, nil); err != nil {
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
	h := &crashtest.Hook{FailAt: "wal:begin", Panic: true}
	op, err := crashtest.SplitOp(crashtest.NewLayout(dir, "a.mp4"), true, h)
	if err != nil {
		t.Fatal(err)
	}
	w := mustWAL(t, dir, h.Func())
	err = crashtest.Catch(func() error { return op.Execute(context.Background(), w) })
	if !errors.Is(err, crashtest.ErrCrash) {
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
