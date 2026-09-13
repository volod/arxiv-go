package media

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

func TestRunnerTimeoutKillsProcessTree(t *testing.T) {
	r := helperRunner(t, "tree")
	r.Timeout = time.Second
	dir := t.TempDir()
	heartbeat := filepath.Join(dir, "heartbeat")
	pidPath := filepath.Join(dir, "child-pid")
	t.Setenv(ffmpegHeartbeatEnv, heartbeat)
	t.Setenv(ffmpegChildPIDEnv, pidPath)
	out := filepath.Join(dir, "preview.mp4")
	start := time.Now()
	err := r.Run(context.Background(), PreviewCommand{Output: out})
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("timeout = %v", err)
	}
	if time.Since(start) > 3*time.Second {
		t.Fatalf("process tree cleanup took %s", time.Since(start))
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatalf("child never started: %v", err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child, findErr := os.FindProcess(pid); findErr == nil {
			_ = child.Kill()
			_ = child.Release()
		}
	}()
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatalf("child never wrote heartbeat: %v", err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil || string(before) != string(after) {
		t.Fatalf("child survived timeout: before=%q after=%q err=%v", before, after, err)
	}
	for _, path := range []string{out, PreviewPartPath(out)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected file %s: %v", path, err)
		}
	}
}

func TestRunnerClosesOrphanChild(t *testing.T) {
	r := helperRunner(t, "orphan")
	dir := t.TempDir()
	heartbeat := filepath.Join(dir, "heartbeat")
	pidPath := filepath.Join(dir, "child-pid")
	t.Setenv(ffmpegHeartbeatEnv, heartbeat)
	t.Setenv(ffmpegChildPIDEnv, pidPath)
	out := filepath.Join(dir, "preview.mp4")
	err := r.Run(context.Background(), PreviewCommand{Output: out})
	if !errors.Is(err, exec.ErrWaitDelay) {
		t.Fatalf("orphan wait = %v", err)
	}
	pidBytes, err := os.ReadFile(pidPath)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.Atoi(string(pidBytes))
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		if child, findErr := os.FindProcess(pid); findErr == nil {
			_ = child.Kill()
			_ = child.Release()
		}
	}()
	before, err := os.ReadFile(heartbeat)
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(150 * time.Millisecond)
	after, err := os.ReadFile(heartbeat)
	if err != nil || string(before) != string(after) {
		t.Fatalf("orphan child survived: before=%q after=%q err=%v", before, after, err)
	}
	for _, path := range []string{out, PreviewPartPath(out)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("unexpected file %s: %v", path, err)
		}
	}
}
