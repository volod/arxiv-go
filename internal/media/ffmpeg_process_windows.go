package media

import (
	"os"
	"os/exec"
	"sync/atomic"
	"unsafe"

	"golang.org/x/sys/windows"
)

type windowsProcessTree struct {
	job      windows.Handle
	assigned atomic.Bool
}

func newManagedProcess(cmd *exec.Cmd) (managedProcess, error) {
	job, err := windows.CreateJobObject(nil, nil)
	if err != nil {
		return nil, err
	}
	info := windows.JOBOBJECT_EXTENDED_LIMIT_INFORMATION{}
	info.BasicLimitInformation.LimitFlags = windows.JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE
	_, err = windows.SetInformationJobObject(job, windows.JobObjectExtendedLimitInformation,
		uintptr(unsafe.Pointer(&info)), uint32(unsafe.Sizeof(info)))
	if err != nil {
		_ = windows.CloseHandle(job)
		return nil, err
	}
	p := &windowsProcessTree{job: job}
	cmd.Cancel = func() error {
		if !p.assigned.Load() {
			if cmd.Process == nil {
				return os.ErrProcessDone
			}
			return cmd.Process.Kill()
		}
		return windows.TerminateJobObject(job, 1)
	}
	return p, nil
}

func (p *windowsProcessTree) Attached(cmd *exec.Cmd) error {
	handle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE,
		false, uint32(cmd.Process.Pid))
	if err != nil {
		return err
	}
	defer windows.CloseHandle(handle)
	if err := windows.AssignProcessToJobObject(p.job, handle); err != nil {
		return err
	}
	p.assigned.Store(true)
	return nil
}

func (p *windowsProcessTree) Close(error) { _ = windows.CloseHandle(p.job) }
