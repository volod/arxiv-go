package archive

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/state"
)

func TestInterruptWritesFinalCheckpointAndResumes(t *testing.T) {
	r, clock := newRoots(t), newClock()
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, testConfig(r, clock, 100))
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Phase("scan", Totals{}); err != nil {
		t.Fatal(err)
	}
	s.Stats.Files.Add(2)
	s.Update(func(cp *state.Checkpoint) { cp.ScanCursor = []string{"a", "b.mp4"}; cp.RegistryOffset = 123 })
	if err := s.Advance(2); err != nil { // below --checkpoint-every: nothing written yet
		t.Fatal(err)
	}
	if cp := readCheckpoint(t, s); cp.RegistryOffset != 0 {
		t.Fatalf("checkpoint written before it was due: %+v", cp)
	}
	clock.Advance(5 * time.Second)
	cancel()
	res := s.Finish(ctx, ctx.Err())
	if res.Status != StatusInterrupted {
		t.Fatalf("status = %v", res.Status)
	}
	cp := readCheckpoint(t, s)
	if cp.Phase != "scan" || cp.RegistryOffset != 123 || len(cp.ScanCursor) != 2 || cp.Counters.Files != 2 || cp.ElapsedS != 5 {
		t.Errorf("final checkpoint = %+v", cp)
	}
	if exists(s.Run.File(state.ReportFile)) {
		t.Error("interrupted run wrote a report, so it would not be resumed")
	}
	if exists(state.LockPath(r.archive)) || exists(state.LockPath(r.video)) {
		t.Error("lock kept after interrupt")
	}

	clock.Advance(time.Minute)
	resumed := start(t, testConfig(r, clock, 200))
	if !resumed.Resumed || resumed.Run.ID != s.Run.ID || resumed.Stats.Files.Load() != 2 {
		t.Fatalf("resume: resumed=%v id=%s files=%d", resumed.Resumed, resumed.Run.ID, resumed.Stats.Files.Load())
	}
	if lock, _ := state.ReadLock(state.LockPath(r.archive)); lock.RunID != s.Run.ID || lock.PID != 200 {
		t.Errorf("lock of resumed run = %+v", lock)
	}
	clock.Advance(2 * time.Second)
	if err := resumed.Checkpoint(); err != nil {
		t.Fatal(err)
	}
	if cp := readCheckpoint(t, resumed); cp.ElapsedS != 7 || cp.RegistryOffset != 123 {
		t.Errorf("resumed checkpoint = %+v (elapsed must accumulate across processes)", cp)
	}
	resumed.Finish(context.Background(), nil)
}

func TestResumeRequiresSameDefinition(t *testing.T) {
	r, clock := newRoots(t), newClock()
	interrupted := func(cfg Config) string {
		ctx, cancel := context.WithCancel(context.Background())
		s, err := Start(ctx, cfg)
		if err != nil {
			t.Fatal(err)
		}
		cancel()
		s.Finish(ctx, ctx.Err())
		return s.Run.ID
	}
	first := interrupted(testConfig(r, clock, 100))

	changed := testConfig(r, clock, 100)
	changed.Defining = testOptions{"hash"}
	clock.Advance(time.Second)
	second := interrupted(changed)
	if second == first {
		t.Fatal("run with different defining options resumed")
	}

	runtimeOnly := testConfig(r, clock, 100)
	runtimeOnly.Defining = testOptions{"hash"}
	runtimeOnly.Options = map[string]string{"LogLevel": "debug"}
	if s := start(t, runtimeOnly); !s.Resumed || s.Run.ID != second {
		t.Errorf("runtime-only option change did not resume: %v %s", s.Resumed, s.Run.ID)
	} else {
		s.Finish(context.Background(), context.Canceled) // stays incomplete
	}

	newRun := testConfig(r, clock, 100)
	newRun.NewRun = true
	clock.Advance(time.Second)
	if id := interrupted(newRun); id == second {
		t.Error("--new-run resumed the incomplete run")
	}
}

func TestDryRunNeverBecomesCurrent(t *testing.T) {
	r, clock := newRoots(t), newClock()
	ictx, cancel := context.WithCancel(context.Background())
	real, err := Start(ictx, testConfig(r, clock, 100))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	real.Finish(ictx, ictx.Err())

	cfg := testConfig(r, clock, 100)
	cfg.DryRun = true
	clock.Advance(time.Second)
	dry := start(t, cfg)
	if dry.Resumed || dry.Run.ID == real.Run.ID {
		t.Fatal("dry run resumed a real run")
	}
	if res := dry.Finish(context.Background(), nil); res.Status != StatusCompleted {
		t.Fatalf("dry run = %+v", res)
	}
	if cur, _ := state.ReadCurrent(r.archive); cur != real.Run.ID {
		t.Errorf("current = %s, want the incomplete real run %s", cur, real.Run.ID)
	}
	if !exists(dry.Run.File(state.ReportFile)) {
		t.Error("dry run has no report")
	}
}

func TestInterruptedDryRunWritesReport(t *testing.T) {
	r, clock := newRoots(t), newClock()
	cfg := testConfig(r, clock, 100)
	cfg.DryRun = true
	ctx, cancel := context.WithCancel(context.Background())
	s, err := Start(ctx, cfg)
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	res := s.Finish(ctx, ctx.Err())
	if res.Status != StatusInterrupted {
		t.Fatalf("status = %v", res.Status)
	}
	if !exists(s.Run.File(state.ReportFile)) {
		t.Error("interrupted dry run has no report")
	}
	if cur, _ := state.ReadCurrent(r.archive); cur != "" {
		t.Errorf("dry run became current: %s", cur)
	}
}

func TestCorruptStateNeedsOperator(t *testing.T) {
	r, clock := newRoots(t), newClock()
	ictx, cancel := context.WithCancel(context.Background())
	s, err := Start(ictx, testConfig(r, clock, 100))
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	s.Finish(ictx, ictx.Err())
	if err := os.WriteFile(s.Run.File(state.CheckpointFile), []byte("{torn"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, err = Start(context.Background(), testConfig(r, clock, 100))
	if !errors.Is(err, state.ErrStateCorrupt) {
		t.Fatalf("Start with corrupt checkpoint = %v", err)
	}
	if st := StartFailed(context.Background(), slog.New(slog.DiscardHandler), err); st != StatusNeedsOperator {
		t.Errorf("StartFailed = %v", st)
	}
	if exists(state.LockPath(r.archive)) {
		t.Error("lock kept after a failed start")
	}
	if _, err := os.Stat(s.Run.File(state.ReportFile)); !errors.Is(err, fs.ErrNotExist) {
		t.Error("corrupt run was changed")
	}
}
