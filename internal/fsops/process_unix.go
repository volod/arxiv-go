//go:build unix

package fsops

import (
	"errors"
	"os"
	"strconv"

	"golang.org/x/sys/unix"
)

// ProcessAlive reports whether a process with pid exists on this host. It
// sends signal 0, which checks existence without affecting the process; a
// permission error still proves the process exists. When kill(2) is denied,
// /proc/<pid> is used as a fallback on Linux.
func ProcessAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := unix.Kill(pid, 0)
	switch {
	case err == nil, errors.Is(err, unix.EPERM):
		return true
	case errors.Is(err, unix.ESRCH):
		return false
	default:
		_, statErr := os.Stat("/proc/" + strconv.Itoa(pid))
		return statErr == nil
	}
}
