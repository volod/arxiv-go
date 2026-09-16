package fsops

import (
	"os"
	"os/exec"
	"runtime"
	"testing"
)

func TestProcessAlive(t *testing.T) {
	if !ProcessAlive(os.Getpid()) {
		t.Error("own process reported dead")
	}
	for _, pid := range []int{0, -1} {
		if ProcessAlive(pid) {
			t.Errorf("pid %d reported alive", pid)
		}
	}
	if ProcessAlive(deadPID(t)) {
		t.Error("exited child reported alive")
	}
}

// deadPID returns the pid of a child process that has exited and been reaped.
func deadPID(t *testing.T) int {
	t.Helper()
	name, args := "true", []string{}
	if runtime.GOOS == "windows" {
		name, args = "cmd", []string{"/c", "exit", "0"}
	}
	cmd := exec.Command(name, args...)
	if err := cmd.Run(); err != nil {
		t.Skipf("cannot start a child process: %v", err)
	}
	return cmd.Process.Pid
}
