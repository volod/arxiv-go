package archive

import (
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

// crashSplit runs a split that stops at point and returns the interrupted run id.
func crashSplit(t *testing.T, r roots, mode, point string) string {
	t.Helper()
	h := &crashtest.Hook{FailAt: point}
	cfg, c := splitConfig(r, mode)
	cfg.Crash = h.Func()
	attachRecoverer(&cfg, c, h.Func())
	if res := runSplit(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("crash at %s = %+v", point, res)
	}
	return currentRunID(t, r.archive)
}

// splitRecovererFor rebuilds a split resolver like the cli does from options.json.
func splitRecovererFor(t *testing.T, r roots) func(string, PayloadKind, json.RawMessage) (Resolver, error) {
	return func(op string, payload PayloadKind, _ json.RawMessage) (Resolver, error) {
		if op != opSplit || payload != PayloadVideo {
			t.Errorf("recovering op %q payload %q", op, payload)
		}
		_, c := splitConfig(r, "auto")
		rs := NewSplitResolver(nil, c.Verify, nil)
		rs.Descriptions = c.Descriptions
		return rs, nil
	}
}

func TestReplacedSplitRunIsRecoveredBeforeNewRun(t *testing.T) {
	for _, variant := range []string{"new-run", "other-options"} {
		t.Run(variant, func(t *testing.T) {
			r, src, dst := splitFixture(t)
			prev := crashSplit(t, r, "auto", "wal:placed")
			if exists(src + ".md") {
				t.Fatal("fixture: description written before the crash")
			}
			cfg, c := splitConfig(r, "auto")
			cfg.Lock.PID = 200
			cfg.RecovererFor = splitRecovererFor(t, r)
			if variant == "new-run" {
				cfg.NewRun = true
			} else {
				cfg.Defining = testOptions{"hash"}
			}
			if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("new run = %+v", res)
			}
			if id := currentRunID(t, r.archive); id == prev {
				t.Fatal("the interrupted run was resumed instead of replaced")
			}
			checkSplit(t, src, dst)
			rows := readVideoCSV(t, filepath.Join(r.archive, scanner.VideoRegistryName))
			if len(rows) != 2 || rows[1][0] != "nested/clip.mp4" || rows[1][2] != "moved" {
				t.Fatalf("registry rows = %q", rows)
			}
			log := string(mustRead(t, filepath.Join(state.StateDir(r.archive), "runs", prev, state.LogFile)))
			if !strings.Contains(log, "recovering the interrupted run") || !strings.Contains(log, "interrupted run recovered") {
				t.Fatalf("previous run log lacks the recovery:\n%s", log)
			}
		})
	}
}

func TestRestoreRecoversInterruptedSplitFirst(t *testing.T) {
	r, src, dst := splitFixture(t)
	crashSplit(t, r, "copy", "wal:copied")
	if !exists(fsops.PartPath(dst)) {
		t.Fatal("fixture: no part file at the crash point")
	}
	cfg, c := restoreConfig(r, "auto")
	cfg.Lock.PID = 200
	cfg.RecovererFor = splitRecovererFor(t, r)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if exists(fsops.PartPath(dst)) || exists(dst) || !exists(src) || exists(src+".md") {
		t.Fatalf("after recovery: part=%v dst=%v src=%v description=%v",
			exists(fsops.PartPath(dst)), exists(dst), exists(src), exists(src+".md"))
	}
}

func TestReplacedRunOutsideLockedRootsNeedsOperator(t *testing.T) {
	cases := map[string]func(r roots, cfg *Config){
		"scan": func(r roots, cfg *Config) {
			recoverer := cfg.RecovererFor
			*cfg = scanSessionConfig(r, newClock(), 5)
			cfg.RecovererFor = recoverer
		},
		"other-video-archive": func(r roots, cfg *Config) {
			other := filepath.Join(filepath.Dir(r.video), "video2")
			if err := os.Mkdir(other, 0o755); err != nil {
				t.Fatal(err)
			}
			cfg.Payload.Root = other
			cfg.NewRun = true
		},
		"no-recoverer": func(r roots, cfg *Config) {
			cfg.NewRun = true
			cfg.RecovererFor = nil
		},
	}
	for name, mutate := range cases {
		t.Run(name, func(t *testing.T) {
			r, src, dst := splitFixture(t)
			prev := crashSplit(t, r, "auto", "wal:placed")
			cfg, _ := splitConfig(r, "auto")
			cfg.RecovererFor = splitRecovererFor(t, r)
			mutate(r, &cfg)
			cfg.Lock.PID = 200
			rec := &recorder{}
			_, err := Start(context.Background(), cfg)
			if !errors.Is(err, ErrUnrecoveredRun) {
				t.Fatalf("Start = %v", err)
			}
			if st := StartFailed(context.Background(), slog.New(rec), err); st != StatusNeedsOperator {
				t.Fatalf("status = %v", st)
			}
			if id := currentRunID(t, r.archive); id != prev {
				t.Fatalf("current = %s, want %s", id, prev)
			}
			if exists(state.LockPath(r.archive)) {
				t.Fatal("lock kept after a refused start")
			}
			if exists(src+".md") || !exists(dst) {
				t.Fatal("refused start changed the archive")
			}
			if len(rec.messages("run state needs operator action")) != 1 {
				t.Fatalf("logs = %v", rec.records)
			}
		})
	}
}
