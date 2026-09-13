package archive

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// resumeFixture is the edge-case tree (with the unreadable file when not root) and the outputs of
// one uninterrupted scan of it.
type resumeFixture struct {
	r          roots
	registry   string
	original   []byte // registry content before any scan
	want       []byte // registry after an uninterrupted scan
	wantCands  []byte
	wantStatus Status
	wantSum    *state.ScanSummary
	entries    int64
}

func newResumeFixture(t *testing.T) resumeFixture {
	t.Helper()
	r := newRoots(t)
	buildEdgeCaseTree(t, r.archive)
	if os.Geteuid() != 0 {
		writeScanFile(t, r.archive, "deep/l1/locked.txt", []byte("secret\n"))
		locked := filepath.Join(r.archive, "deep", "l1", "locked.txt")
		if err := os.Chmod(locked, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	}
	f := resumeFixture{r: r, registry: filepath.Join(r.archive, "arxgo-registry.csv")}
	f.original = mustRead(t, f.registry)
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 1000), testScanConfig(r))
	f.want, f.wantStatus = mustRead(t, f.registry), res.Status
	f.wantCands = mustRead(t, candidatesPath(r, res.RunID))
	f.wantSum = readReport(t, r.archive, res.RunID).Scan
	f.entries = f.wantSum.Files + f.wantSum.Symlinks + f.wantSum.Dirs + f.wantSum.Skipped["unreadable"]
	f.reset(t)
	return f
}

// reset restores the original registry and removes run state, so the next scan starts fresh.
func (f resumeFixture) reset(t *testing.T) {
	t.Helper()
	if err := os.WriteFile(f.registry, f.original, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.RemoveAll(state.StateDir(f.r.archive)); err != nil {
		t.Fatal(err)
	}
	_ = os.Remove(fsops.PartPath(f.registry))
}

func candidatesPath(r roots, runID string) string {
	return filepath.Join(state.StateDir(r.archive), "runs", runID, state.CandidatesFile)
}

// finishResume resumes the interrupted run and checks it equals the uninterrupted scan. The resume
// continues from the checkpoint when one recorded durable output, otherwise it starts over.
func (f resumeFixture) finishResume(t *testing.T, cfg Config, interrupted Result) {
	t.Helper()
	fromCheckpoint := readRunCheckpoint(t, f.r, interrupted.RunID).RegistryOffset > 0
	if got := mustRead(t, f.registry); !bytes.Equal(got, f.original) {
		t.Fatal("existing registry replaced before the scan completed")
	}
	res, rec := runScan(t, context.Background(), cfg, testScanConfig(f.r))
	if res.RunID != interrupted.RunID || res.Status != f.wantStatus {
		t.Fatalf("resume: run %s status %v (%v), want run %s status %v", res.RunID, res.Status, res.Err, interrupted.RunID, f.wantStatus)
	}
	if resumed := len(rec.messages("resuming scan")) == 1; resumed != fromCheckpoint {
		t.Errorf("resumed from checkpoint = %v, want %v", resumed, fromCheckpoint)
	}
	if got := mustRead(t, f.registry); !bytes.Equal(got, f.want) {
		t.Fatalf("resumed registry differs\n--- got\n%s\n--- want\n%s", got, f.want)
	}
	if got := mustRead(t, candidatesPath(f.r, res.RunID)); !bytes.Equal(got, f.wantCands) {
		t.Fatalf("resumed candidates differ\n--- got\n%s\n--- want\n%s", got, f.wantCands)
	}
	sum := readReport(t, f.r.archive, res.RunID).Scan
	sum.ElapsedS = f.wantSum.ElapsedS
	if !reflect.DeepEqual(sum, f.wantSum) {
		t.Fatalf("resumed summary\n got %+v\nwant %+v", sum, f.wantSum)
	}
}

// An interrupt always makes the output durable before the final checkpoint, so the cadence does
// not change what is resumed; crashes are tested with two cadences below.
func TestScanInterruptedAfterEveryEntryResumesByteIdentical(t *testing.T) {
	t.Parallel()
	f := newResumeFixture(t)
	for n := int64(1); n < f.entries; n++ {
		cfg := scanSessionConfig(f.r, newClock(), 4)
		ctx, cancel := context.WithCancel(context.Background())
		sc := testScanConfig(f.r)
		sc.afterEntry = func(written int64) error {
			if written == n {
				cancel()
			}
			return nil
		}
		res, _ := runScan(t, ctx, cfg, sc)
		cancel()
		if res.Status != StatusInterrupted {
			t.Fatalf("n %d: status %v (%v)", n, res.Status, res.Err)
		}
		f.finishResume(t, cfg, res)
		f.reset(t)
	}
}

func TestScanCrashAfterEveryEntryResumesByteIdentical(t *testing.T) {
	for _, every := range []int{1, 3} {
		t.Run(fmt.Sprintf("checkpoint-every-%d", every), func(t *testing.T) {
			t.Parallel()
			crashAfterEveryEntry(t, every)
		})
	}
}

func crashAfterEveryEntry(t *testing.T, every int) {
	f := newResumeFixture(t)
	for n := int64(1); n < f.entries; n++ {
		cfg := scanSessionConfig(f.r, newClock(), every)
		sc := testScanConfig(f.r)
		sc.afterEntry = func(written int64) error {
			if written == n {
				return errTestCrash
			}
			return nil
		}
		res, _ := runScan(t, context.Background(), cfg, sc)
		if res.Status != StatusFailed {
			t.Fatalf("n %d: status %v (%v)", n, res.Status, res.Err)
		}
		// Bytes past the checkpoint, as a crash may leave them, are discarded on resume. The tail is
		// longer than the rest of the registry, so rows written after the offset cannot hide it.
		torn := bytes.Repeat([]byte("torn,row\n{\"v\":"), 1024)
		for _, p := range []string{fsops.PartPath(f.registry), candidatesPath(f.r, res.RunID)} {
			appendBytes(t, p, torn)
		}
		f.finishResume(t, cfg, res)
		f.reset(t)
	}
}

func TestScanRestartsWhenPartFileIsShorterThanCheckpoint(t *testing.T) {
	f := newResumeFixture(t)
	cfg := scanSessionConfig(f.r, newClock(), 2)
	sc := testScanConfig(f.r)
	sc.afterEntry = func(written int64) error {
		if written == f.entries/2 {
			return errTestCrash
		}
		return nil
	}
	res, _ := runScan(t, context.Background(), cfg, sc)
	if cp := readRunCheckpoint(t, f.r, res.RunID); cp.RegistryOffset < 20 {
		t.Fatalf("checkpoint offset %d too small for the test", cp.RegistryOffset)
	}
	if err := os.Truncate(fsops.PartPath(f.registry), 10); err != nil {
		t.Fatal(err)
	}
	res2, rec := runScan(t, context.Background(), cfg, testScanConfig(f.r))
	if res2.RunID != res.RunID || res2.Status != f.wantStatus {
		t.Fatalf("status %v (%v)", res2.Status, res2.Err)
	}
	if len(rec.messages("scan output does not match the checkpoint; scanning again from the start")) != 1 {
		t.Error("restart not logged")
	}
	if got := mustRead(t, f.registry); !bytes.Equal(got, f.want) {
		t.Errorf("restarted registry differs\n--- got\n%s\n--- want\n%s", got, f.want)
	}
	if sum := readReport(t, f.r.archive, res2.RunID).Scan; sum.Files != f.wantSum.Files || sum.Dirs != f.wantSum.Dirs {
		t.Errorf("restarted summary counts twice: %+v", sum)
	}
}

func TestScanCompletedBeforeReportIsNotRepeated(t *testing.T) {
	f := newResumeFixture(t)
	cfg := scanSessionConfig(f.r, newClock(), 4)
	cfg.Lock.PID = deadTestPID // the first process "dies" after the scan, before Finish
	s := start(t, cfg)
	if _, err := Scan(context.Background(), s, testScanConfig(f.r)); err != nil {
		t.Fatal(err)
	}
	s.runLog.Close()
	cfg.Lock.PID, cfg.ForceUnlock = 100, true
	writeScanFile(t, f.r.archive, "added-later.txt", []byte("new\n"))
	res, rec := runScan(t, context.Background(), cfg, testScanConfig(f.r))
	if res.RunID != s.Run.ID || res.Status != f.wantStatus {
		t.Fatalf("status %v (%v)", res.Status, res.Err)
	}
	if len(rec.messages("scan already completed in this run")) != 1 {
		t.Error("completed scan was repeated")
	}
	if got := mustRead(t, f.registry); !bytes.Equal(got, f.want) {
		t.Error("registry changed by the resumed, already completed scan")
	}
}

func appendBytes(t *testing.T, path string, data []byte) {
	t.Helper()
	fh, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := fh.Write(data); err != nil {
		t.Fatal(err)
	}
	if err := fh.Close(); err != nil {
		t.Fatal(err)
	}
}

func readRunCheckpoint(t *testing.T, r roots, runID string) state.Checkpoint {
	t.Helper()
	cp, err := state.ReadCheckpoint(filepath.Join(state.StateDir(r.archive), "runs", runID, state.CheckpointFile))
	if err != nil {
		t.Fatal(err)
	}
	return cp
}

// A directory on the resume cursor's path that became unreadable is reported again although its key
// precedes the cursor; the checkpointed cursor must not move back to it.
func TestScanCursorNeverMovesBackwards(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
	f := newResumeFixture(t)
	cfg := scanSessionConfig(f.r, newClock(), 1)
	ctx, cancel := context.WithCancel(context.Background())
	sc := testScanConfig(f.r)
	var cursor []string
	sc.afterEntry = func(int64) error {
		cp := readCheckpointFile(t, f.r)
		if len(cp.ScanCursor) == 6 && cp.ScanCursor[5] == "deep.mp4" {
			cursor = cp.ScanCursor
			cancel()
		}
		return nil
	}
	res, _ := runScan(t, ctx, cfg, sc)
	cancel()
	if res.Status != StatusInterrupted || cursor == nil {
		t.Fatalf("status %v (%v), cursor %q", res.Status, res.Err, cursor)
	}
	l2 := filepath.Join(f.r.archive, "deep", "l1", "l2")
	if err := os.Chmod(l2, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(l2, 0o755) })

	ctx2, cancel2 := context.WithCancel(context.Background())
	defer cancel2()
	sc.afterEntry = func(int64) error { cancel2(); return nil }
	res2, _ := runScan(t, ctx2, cfg, sc)
	if res2.RunID != res.RunID || res2.Status != StatusInterrupted {
		t.Fatalf("second run %s status %v (%v)", res2.RunID, res2.Status, res2.Err)
	}
	got := readRunCheckpoint(t, f.r, res.RunID)
	if !reflect.DeepEqual(got.ScanCursor, cursor) || got.Scan.Skipped["unreadable"] < 1 {
		t.Errorf("cursor after re-reported directory = %q (skipped %v), want %q", got.ScanCursor, got.Scan.Skipped, cursor)
	}
}

func readCheckpointFile(t *testing.T, r roots) state.Checkpoint {
	t.Helper()
	id, err := state.ReadCurrent(r.archive)
	if err != nil {
		t.Fatal(err)
	}
	return readRunCheckpoint(t, r, id)
}
