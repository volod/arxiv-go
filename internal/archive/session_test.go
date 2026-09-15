package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/state"
)

type roots struct{ archive, video string }

func newRoots(t *testing.T) roots {
	t.Helper()
	dir := t.TempDir()
	r := roots{filepath.Join(dir, "archive"), filepath.Join(dir, "video")}
	for _, d := range []string{r.archive, r.video} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return r
}

type testOptions struct{ Verify string }

func testConfig(r roots, clock *fakeClock, pid int) Config {
	return Config{
		Op: "split", Version: "test", Archive: r.archive, Payload: Payload{Kind: PayloadVideo, Root: r.video},
		Options: testOptions{"size"}, Defining: testOptions{"size"},
		LogLevel: slog.LevelInfo, ProgressInterval: 10 * time.Second,
		CheckpointEvery: 3, CheckpointInterval: time.Minute,
		Now:   clock.Now,
		Lock:  state.LockOptions{Host: "test-host", PID: pid, Alive: func(p int) bool { return p != deadTestPID }},
		Ticks: func(time.Duration) (<-chan time.Time, func()) { return nil, func() {} },
	}
}

const deadTestPID = 13

func start(t *testing.T, cfg Config) *Session {
	t.Helper()
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatalf("Start: %v", err)
	}
	t.Cleanup(func() {
		if s.stopProgress != nil {
			s.stopProgress()
		}
	})
	return s
}

func readCheckpoint(t *testing.T, s *Session) state.Checkpoint {
	t.Helper()
	cp, err := state.ReadCheckpoint(s.Run.File(state.CheckpointFile))
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

func exists(path string) bool {
	_, err := os.Stat(path)
	return err == nil
}

func TestSessionLeavesRunFilesAndReleasesLocks(t *testing.T) {
	r, clock := newRoots(t), newClock()
	s := start(t, testConfig(r, clock, 100))
	for _, root := range []string{r.archive, r.video} {
		if !exists(state.LockPath(root)) {
			t.Fatalf("no lock in %s while running", root)
		}
	}
	if mirror, _ := state.ReadLock(state.LockPath(r.video)); mirror.Role != state.RoleMirror || mirror.Peer != r.archive || mirror.RunID != s.Run.ID {
		t.Errorf("mirror lock = %+v", mirror)
	}
	if cur, _ := state.ReadCurrent(r.archive); cur != s.Run.ID {
		t.Errorf("current = %q, want %q", cur, s.Run.ID)
	}
	if err := s.Phase("scan", Totals{}); err != nil {
		t.Fatal(err)
	}
	s.Stats.Files.Add(7)
	clock.Advance(3 * time.Second)
	res := s.Finish(context.Background(), nil)
	if res.Status != StatusCompleted || res.RunID != s.Run.ID {
		t.Fatalf("result = %+v", res)
	}
	for _, name := range []string{state.OptionsFile, state.CheckpointFile, state.LogFile, state.ReportFile} {
		if !exists(s.Run.File(name)) {
			t.Errorf("%s missing", name)
		}
	}
	for _, root := range []string{r.archive, r.video} {
		if exists(state.LockPath(root)) {
			t.Errorf("lock left in %s", root)
		}
	}
	var rep state.Report
	if err := state.ReadJSON(s.Run.File(state.ReportFile), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Status != "completed" || rep.Counters.Files != 7 || len(rep.Phases) != 1 || rep.Phases[0].WallS != 3 ||
		rep.Phases[0].Counters.Files != 7 || len(rep.Roots) != 2 || rep.WallS != 3 {
		t.Errorf("report = %+v", rep)
	}
	var opts state.RunOptions
	if err := state.ReadJSON(s.Run.File(state.OptionsFile), &opts); err != nil {
		t.Fatal(err)
	}
	var stored testOptions
	if err := json.Unmarshal(opts.Options, &stored); err != nil || stored.Verify != "size" ||
		opts.RunID != s.Run.ID || !opts.SameDefinition("split", []byte(`{"Verify":"size"}`)) {
		t.Errorf("options = %+v", opts)
	}
}

func TestSecondSessionOnSameRootsIsLocked(t *testing.T) {
	r, clock := newRoots(t), newClock()
	first := start(t, testConfig(r, clock, 100))
	defer first.Finish(context.Background(), nil)

	_, err := Start(context.Background(), testConfig(r, clock, 200))
	var locked *state.LockedError
	if !errors.As(err, &locked) || locked.State != state.LockHeld || locked.Owner.PID != 100 || locked.Owner.RunID != first.Run.ID {
		t.Fatalf("second Start = %v", err)
	}

	// Another archive sharing the video archive is refused by the mirror lock, and its own
	// archive lock is released again.
	other := roots{filepath.Join(filepath.Dir(r.archive), "archive2"), r.video}
	if err := os.Mkdir(other.archive, 0o755); err != nil {
		t.Fatal(err)
	}
	_, err = Start(context.Background(), testConfig(other, clock, 300))
	if !errors.As(err, &locked) || locked.Path != state.LockPath(r.video) {
		t.Fatalf("shared video archive Start = %v", err)
	}
	if exists(state.LockPath(other.archive)) {
		t.Error("archive lock kept after the mirror lock failed")
	}
	if exists(filepath.Join(state.StateDir(other.archive), "runs")) {
		t.Error("run directory created although the lock was not acquired")
	}

	var rec recorder
	if st := StartFailed(context.Background(), slog.New(&rec), err); st != StatusLocked {
		t.Errorf("StartFailed = %v", st)
	}
	if m := rec.messages(err.Error()); len(m) != 1 || m[0]["owner_run"] != first.Run.ID || m[0]["owner_pid"] != "100" {
		t.Errorf("start failure log = %v", rec.records)
	}
}

func TestScanTakesOnlyArchiveLock(t *testing.T) {
	r, clock := newRoots(t), newClock()
	cfg := testConfig(r, clock, 100)
	cfg.Op, cfg.Payload = "scan", Payload{}
	s := start(t, cfg)
	if exists(state.LockPath(r.video)) {
		t.Error("scan took the video archive lock")
	}
	if !exists(state.LockPath(r.archive)) {
		t.Error("scan did not take the archive lock")
	}
	if peer := s.locks[0].Info().Peer; peer != "" {
		t.Errorf("scan lock peer = %q, want empty", peer)
	}
	s.Finish(context.Background(), nil)
}

func TestRunLogKeepsInfoWhenConsoleIsWarn(t *testing.T) {
	r, clock := newRoots(t), newClock()
	cfg := testConfig(r, clock, 100)
	cfg.LogLevel = slog.LevelWarn
	var console bytes.Buffer
	cfg.Console = slog.NewTextHandler(&console, &slog.HandlerOptions{Level: slog.LevelWarn})
	s := start(t, cfg)
	s.Log.Info("file only")
	s.Log.Warn("both")
	path := s.Run.File(state.LogFile)
	s.Finish(context.Background(), nil)

	if out := console.String(); strings.Contains(out, "file only") || strings.Contains(out, "run started") ||
		!strings.Contains(out, "msg=both") {
		t.Errorf("console = %q", out)
	}
	text := string(mustRead(t, path))
	if !strings.Contains(text, `"msg":"file only"`) || !strings.Contains(text, `"msg":"run started"`) ||
		!strings.Contains(text, `"msg":"both"`) {
		t.Errorf("run log missing info records: %s", text)
	}
}

func TestStaleLockNeedsForceUnlockInSession(t *testing.T) {
	r, clock := newRoots(t), newClock()
	crashed := start(t, testConfig(r, clock, deadTestPID))
	_ = crashed // the process "dies" without Finish; its locks stay behind

	_, err := Start(context.Background(), testConfig(r, clock, 200))
	if !errors.Is(err, state.ErrLocked) {
		t.Fatalf("Start over stale lock = %v", err)
	}
	cfg := testConfig(r, clock, 200)
	cfg.ForceUnlock = true
	s := start(t, cfg)
	if !s.Resumed || s.Run.ID != crashed.Run.ID {
		t.Errorf("forced start did not resume the crashed run: resumed=%v id=%s", s.Resumed, s.Run.ID)
	}
	if res := s.Finish(context.Background(), nil); res.Status != StatusCompleted {
		t.Fatalf("result = %+v", res)
	}
}

func TestDryRunDoesNotCreateVideoArchive(t *testing.T) {
	r, clock := newRoots(t), newClock()
	r.video = filepath.Join(filepath.Dir(r.video), "new-video")
	cfg := testConfig(r, clock, 100)
	cfg.CreateMirror, cfg.DryRun = true, true
	start(t, cfg).Finish(context.Background(), nil)
	if exists(r.video) {
		t.Error("dry run created the video archive root")
	}
	cfg.DryRun = false
	s := start(t, cfg)
	if !exists(state.LockPath(r.video)) {
		t.Error("split did not create the video archive root and its mirror lock")
	}
	s.Finish(context.Background(), nil)
}

func TestCheckpointCadence(t *testing.T) {
	r, clock := newRoots(t), newClock()
	s := start(t, testConfig(r, clock, 100))
	defer s.Finish(context.Background(), nil)
	written := func() time.Time { return readCheckpoint(t, s).WrittenAt }
	base := written()

	clock.Advance(time.Second)
	for i := range 2 {
		s.Stats.Files.Add(1)
		if err := s.Advance(1); err != nil {
			t.Fatal(err)
		}
		if !written().Equal(base) {
			t.Fatalf("checkpoint after %d files, want after 3", i+1)
		}
	}
	s.Stats.Files.Add(1)
	if err := s.Advance(1); err != nil {
		t.Fatal(err)
	}
	if cp := readCheckpoint(t, s); cp.WrittenAt.Equal(base) || cp.Counters.Files != 3 {
		t.Fatalf("no checkpoint after --checkpoint-every files: %+v", cp)
	}
	base = written()
	clock.Advance(time.Minute)
	if err := s.Advance(1); err != nil {
		t.Fatal(err)
	}
	if written().Equal(base) {
		t.Error("no checkpoint after --checkpoint-interval")
	}
}

func TestLostLockStopsForOperator(t *testing.T) {
	r, clock := newRoots(t), newClock()
	s := start(t, testConfig(r, clock, 100))
	if err := os.Remove(state.LockPath(r.archive)); err != nil {
		t.Fatal(err)
	}
	err := s.Checkpoint()
	if !errors.Is(err, state.ErrLockLost) {
		t.Fatalf("Checkpoint = %v, want ErrLockLost", err)
	}
	before := readCheckpoint(t, s)
	logPath := s.Run.File(state.LogFile)
	beforeLog, statErr := os.Stat(logPath)
	if statErr != nil {
		t.Fatal(statErr)
	}
	if res := s.Finish(context.Background(), err); res.Status != StatusNeedsOperator {
		t.Fatalf("status = %v", res.Status)
	}
	if after := readCheckpoint(t, s); !after.WrittenAt.Equal(before.WrittenAt) || exists(s.Run.File(state.ReportFile)) {
		t.Error("run state written after the lock was lost")
	}
	if fi, err := os.Stat(logPath); err != nil || fi.Size() != beforeLog.Size() {
		t.Error("run log written after the lock was lost")
	}
	if !exists(state.LockPath(r.video)) {
		t.Error("mirror lock released although operator action is needed")
	}
}

func TestFailedAndPartialOutcomes(t *testing.T) {
	r, clock := newRoots(t), newClock()
	s := start(t, testConfig(r, clock, 100))
	res := s.Finish(context.Background(), errors.New("disk on fire"))
	if res.Status != StatusFailed || exists(s.Run.File(state.ReportFile)) || exists(state.LockPath(r.archive)) {
		t.Fatalf("failed run: status=%v report=%v lock=%v", res.Status, exists(s.Run.File(state.ReportFile)), exists(state.LockPath(r.archive)))
	}

	s = start(t, testConfig(r, clock, 100))
	if !s.Resumed {
		t.Error("failed run was not resumed")
	}
	s.Stats.VideosSkipped.Add(1)
	s.Issue(state.IssueSkipped, "a/b.mp4", "destination exists")
	if res := s.Finish(context.Background(), nil); res.Status != StatusPartial {
		t.Fatalf("status = %v, want partial", res.Status)
	}
	var rep state.Report
	if err := state.ReadJSON(s.Run.File(state.ReportFile), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Status != "partial" || !rep.Resumed || len(rep.Issues) != 1 || rep.Issues[0].RelPath != "a/b.mp4" {
		t.Errorf("report = %+v", rep)
	}

	s = start(t, testConfig(r, clock, 100))
	if s.Resumed {
		t.Error("completed run was resumed")
	}
	s.Finish(context.Background(), nil)
}

func TestCanceledBeforeStart(t *testing.T) {
	r, clock := newRoots(t), newClock()
	c, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Start(c, testConfig(r, clock, 100)); !errors.Is(err, context.Canceled) {
		t.Fatalf("Start = %v", err)
	}
	if exists(state.StateDir(r.archive)) {
		t.Error("state written for a run canceled before it started")
	}
}
