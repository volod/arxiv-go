package state

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"
)

const testHost = "host-a"

func lockInfo(runID string) LockInfo {
	return LockInfo{RunID: runID, StartedAt: time.Date(2026, 9, 13, 10, 0, 0, 0, time.UTC), Op: "split", Role: RoleArchive}
}

func alwaysAlive(int) bool { return true }
func neverAlive(int) bool  { return false }

func mustAcquire(t *testing.T, root string, info LockInfo, opts LockOptions) *Lock {
	t.Helper()
	l, err := AcquireLock(root, info, opts)
	if err != nil {
		t.Fatalf("AcquireLock: %v", err)
	}
	return l
}

func lockedState(t *testing.T, err error) *LockedError {
	t.Helper()
	var locked *LockedError
	if !errors.As(err, &locked) || !errors.Is(err, ErrLocked) {
		t.Fatalf("error = %v, want *LockedError", err)
	}
	return locked
}

func TestAcquireAndReleaseLock(t *testing.T) {
	root := t.TempDir()
	l := mustAcquire(t, root, lockInfo("20260913T100000Z-00000001"), LockOptions{Host: testHost, PID: 10, Alive: alwaysAlive})
	got, err := ReadLock(LockPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if got.V != 1 || got.PID != 10 || got.Host != testHost || got.Root != root || got.RunID != l.Info().RunID {
		t.Errorf("lock content = %+v", got)
	}
	if err := l.Verify(); err != nil {
		t.Errorf("Verify: %v", err)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(LockPath(root)); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("lock still present after release: %v", err)
	}
	// Released: another process can take it.
	mustAcquire(t, root, lockInfo("20260913T100000Z-00000002"), LockOptions{Host: testHost, PID: 11, Alive: alwaysAlive})
}

func TestConcurrentLockAttemptsHaveOneWinner(t *testing.T) {
	root := t.TempDir()
	const n = 32
	var wg sync.WaitGroup
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, errs[i] = AcquireLock(root, lockInfo(fmt.Sprintf("20260913T100000Z-%08x", i)),
				LockOptions{Host: testHost, PID: 1000 + i, Alive: alwaysAlive})
		}()
	}
	wg.Wait()
	winners := 0
	for _, err := range errs {
		if err == nil {
			winners++
			continue
		}
		if st := lockedState(t, err).State; st != LockHeld {
			t.Errorf("loser state = %s, want held", st)
		}
	}
	if winners != 1 {
		t.Fatalf("winners = %d, want 1", winners)
	}
}

// TestLockHeldByLiveProcess uses a real child process as the owner, so liveness is checked with
// the operating system rather than an injected function.
func TestLockHeldByLiveProcess(t *testing.T) {
	root := t.TempDir()
	child := startHolder(t)
	info := lockInfo("20260913T100000Z-0000beef")
	if _, err := AcquireLock(root, info, LockOptions{PID: child}); err != nil {
		t.Fatal(err)
	}
	_, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"), LockOptions{ForceUnlock: true})
	locked := lockedState(t, err)
	if locked.State != LockHeld || locked.Owner.PID != child || locked.Owner.RunID != info.RunID {
		t.Fatalf("locked = %+v", locked)
	}
	for _, want := range []string{"is held by run " + info.RunID, "pid " + strconv.Itoa(child), "op split"} {
		if !strings.Contains(err.Error(), want) {
			t.Errorf("message %q does not name %q", err.Error(), want)
		}
	}
}

func TestStaleLockNeedsForceUnlock(t *testing.T) {
	root := t.TempDir()
	dead := deadPID(t)
	stale := lockInfo("20260913T090000Z-0000dead")
	if _, err := AcquireLock(root, stale, LockOptions{PID: dead}); err != nil {
		t.Fatal(err)
	}
	_, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"), LockOptions{})
	if locked := lockedState(t, err); locked.State != LockStale || !strings.Contains(err.Error(), "--force-unlock") {
		t.Fatalf("without --force-unlock: %v", err)
	}
	if got, _ := ReadLock(LockPath(root)); got.RunID != stale.RunID {
		t.Fatalf("refused attempt changed the lock: %+v", got)
	}

	l, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"), LockOptions{ForceUnlock: true})
	if err != nil {
		t.Fatalf("with --force-unlock: %v", err)
	}
	if l.TookOver == nil || l.TookOver.RunID != stale.RunID {
		t.Errorf("TookOver = %+v", l.TookOver)
	}
	if got, _ := ReadLock(LockPath(root)); got.RunID != "20260913T100000Z-00000002" || got.PID != os.Getpid() {
		t.Errorf("lock after takeover = %+v", got)
	}
	assertNoPartFiles(t, StateDir(root))
}

func TestLockWithOwnPIDFromEarlierProcessIsStale(t *testing.T) {
	root := t.TempDir()
	opts := LockOptions{Host: testHost, PID: 1, Alive: alwaysAlive}
	mustAcquire(t, root, lockInfo("20260913T090000Z-00000001"), opts)
	_, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"), opts)
	if st := lockedState(t, err).State; st != LockStale {
		t.Fatalf("state = %s, want stale (pid reused by this process)", st)
	}
}

func TestRemoteHostLockIsNeverTakenOver(t *testing.T) {
	root := t.TempDir()
	remote := lockInfo("20260913T090000Z-00000001")
	mustAcquire(t, root, remote, LockOptions{Host: "nas-box", PID: 77, Alive: neverAlive})
	_, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"),
		LockOptions{Host: testHost, PID: 78, Alive: neverAlive, ForceUnlock: true})
	locked := lockedState(t, err)
	if locked.State != LockRemote || !strings.Contains(err.Error(), "nas-box") {
		t.Fatalf("locked = %+v (%v)", locked, err)
	}
	if got, _ := ReadLock(LockPath(root)); got.RunID != remote.RunID {
		t.Fatalf("remote lock was replaced: %+v", got)
	}
	// Host names compare case-insensitively.
	_, err = AcquireLock(root, lockInfo("20260913T100000Z-00000003"),
		LockOptions{Host: "NAS-BOX", PID: 78, Alive: alwaysAlive})
	if st := lockedState(t, err).State; st != LockHeld {
		t.Errorf("same host in other case: state = %s, want held", st)
	}
}

func TestUnreadableLock(t *testing.T) {
	orig := unreadableRetries
	unreadableRetries = nil
	t.Cleanup(func() { unreadableRetries = orig })
	for name, content := range map[string]string{"empty": "", "torn": `{"v":1,"run_id":"2026`, "no pid": `{"v":1,"run_id":"x","host":"h"}`} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			writeLockFile(t, root, content)
			_, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"), LockOptions{Host: testHost, PID: 5, Alive: alwaysAlive})
			if st := lockedState(t, err).State; st != LockUnreadable {
				t.Fatalf("state = %s, want unreadable", st)
			}
			l, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"), LockOptions{Host: testHost, PID: 5, Alive: alwaysAlive, ForceUnlock: true})
			if err != nil {
				t.Fatal(err)
			}
			if l.TookOver == nil || l.TookOver.RunID != "" {
				t.Errorf("TookOver = %+v, want zero owner", l.TookOver)
			}
		})
	}
}

func TestUnreadableLockBecomesReadableDuringRetry(t *testing.T) {
	orig := unreadableRetries
	unreadableRetries = []time.Duration{200 * time.Millisecond}
	t.Cleanup(func() { unreadableRetries = orig })

	root := t.TempDir()
	writeLockFile(t, root, "")
	go func() {
		time.Sleep(20 * time.Millisecond)
		_ = os.WriteFile(LockPath(root), []byte(`{"v":1,"run_id":"20260913T100000Z-0000beef","pid":10,"host":"host-a",`+
			`"started_at":"2026-09-13T10:00:00Z","op":"split","role":"archive"}`+"\n"), 0o644)
	}()
	_, err := AcquireLock(root, lockInfo("20260913T100000Z-00000002"),
		LockOptions{Host: testHost, PID: 11, Alive: alwaysAlive})
	locked := lockedState(t, err)
	if locked.State != LockHeld || locked.Owner.RunID != "20260913T100000Z-0000beef" {
		t.Fatalf("locked = %+v (%v), want held by the lock that finished writing", locked, err)
	}
}

func TestConcurrentForceUnlockHasOneWinner(t *testing.T) {
	root := t.TempDir()
	const stalePID = 99
	mustAcquire(t, root, lockInfo("20260913T090000Z-00000001"), LockOptions{Host: testHost, PID: stalePID, Alive: alwaysAlive})
	alive := func(pid int) bool { return pid != stalePID }
	const n = 8
	var wg sync.WaitGroup
	locks := make([]*Lock, n)
	errs := make([]error, n)
	for i := range n {
		wg.Add(1)
		go func() {
			defer wg.Done()
			locks[i], errs[i] = AcquireLock(root, lockInfo(fmt.Sprintf("20260913T100000Z-%08x", i+2)),
				LockOptions{Host: testHost, PID: 100 + i, Alive: alive, ForceUnlock: true})
		}()
	}
	wg.Wait()
	valid := 0
	for i := range n {
		if errs[i] == nil && locks[i].Verify() == nil {
			valid++
		}
	}
	if valid != 1 {
		t.Fatalf("processes holding a verified lock = %d, want 1 (errors %v)", valid, errs)
	}
	assertNoPartFiles(t, StateDir(root))
}

func TestVerifyAndReleaseDetectLostLock(t *testing.T) {
	root := t.TempDir()
	opts := LockOptions{Host: testHost, PID: 10, Alive: neverAlive}
	l := mustAcquire(t, root, lockInfo("20260913T100000Z-00000001"), opts)

	other := mustAcquire(t, root, lockInfo("20260913T100000Z-00000002"),
		LockOptions{Host: testHost, PID: 11, Alive: neverAlive, ForceUnlock: true})
	if err := l.Verify(); !errors.Is(err, ErrLockLost) {
		t.Fatalf("Verify after takeover = %v, want ErrLockLost", err)
	}
	if err := l.Release(); !errors.Is(err, ErrLockLost) {
		t.Fatalf("Release after takeover = %v, want ErrLockLost", err)
	}
	if err := other.Verify(); err != nil {
		t.Fatalf("release of a lost lock removed the new owner's lock: %v", err)
	}
	if err := os.Remove(LockPath(root)); err != nil {
		t.Fatal(err)
	}
	if err := other.Verify(); !errors.Is(err, ErrLockLost) {
		t.Fatalf("Verify after removal = %v, want ErrLockLost", err)
	}
}

func TestSetRunID(t *testing.T) {
	root := t.TempDir()
	l := mustAcquire(t, root, lockInfo("20260913T100000Z-00000001"), LockOptions{Host: testHost, PID: 10, Alive: alwaysAlive})
	if err := l.SetRunID("20260912T100000Z-00000009"); err != nil {
		t.Fatal(err)
	}
	if got, _ := ReadLock(LockPath(root)); got.RunID != "20260912T100000Z-00000009" {
		t.Errorf("lock run id = %q", got.RunID)
	}
	if err := l.Release(); err != nil {
		t.Fatal(err)
	}
}

func writeLockFile(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(StateDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(LockPath(root), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertNoPartFiles(t *testing.T, dir string) {
	t.Helper()
	matches, _ := filepath.Glob(filepath.Join(dir, "*.arxgo-part"))
	if len(matches) > 0 {
		t.Errorf("leftover part files: %v", matches)
	}
}

// deadPID returns the pid of a child process that has exited and been reaped.
func deadPID(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^$")
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot run a child process: %v", err)
	}
	return cmd.Process.Pid
}

// startHolder starts a child process that stays alive until the test ends and returns its pid.
func startHolder(t *testing.T) int {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^TestHelperSleep$")
	cmd.Env = append(os.Environ(), "ARXGO_TEST_SLEEP=1")
	stdin, err := cmd.StdinPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Skipf("cannot start a child process: %v", err)
	}
	t.Cleanup(func() {
		stdin.Close()
		if runtime.GOOS == "windows" {
			cmd.Process.Kill()
		}
		cmd.Wait()
	})
	return cmd.Process.Pid
}

// TestHelperSleep is the child process body for startHolder: it blocks until stdin closes.
func TestHelperSleep(t *testing.T) {
	if os.Getenv("ARXGO_TEST_SLEEP") != "1" {
		t.Skip("helper process")
	}
	buf := make([]byte, 1)
	os.Stdin.Read(buf)
}
