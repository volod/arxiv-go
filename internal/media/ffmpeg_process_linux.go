package media

import (
	"errors"
	"os"
	"os/exec"
	"syscall"
)

type linuxProcessTree struct{ cmd *exec.Cmd }

func newManagedProcess(cmd *exec.Cmd) (managedProcess, error) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
		if errors.Is(err, syscall.ESRCH) {
			return os.ErrProcessDone
		}
		return err
	}
	return &linuxProcessTree{cmd: cmd}, nil
}

func (*linuxProcessTree) Attached(*exec.Cmd) error { return nil }

func (p *linuxProcessTree) Close(waitErr error) {
	// WaitDelay means a descendant still held a pipe. A normal exit needs no
	// post-Wait group kill, avoiding a race with reuse of a reaped group ID.
	if errors.Is(waitErr, exec.ErrWaitDelay) && p.cmd.Process != nil {
		_ = syscall.Kill(-p.cmd.Process.Pid, syscall.SIGKILL)
	}
}
