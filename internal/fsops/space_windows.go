//go:build windows

package fsops

import (
	"os"
	"strings"

	"golang.org/x/sys/windows"
)

// freeSpace queries GetDiskFreeSpaceEx for an existing directory. Available
// honors per-user quotas.
func freeSpace(dir string) (Space, error) {
	// A UNC directory name must end with a backslash; the separator is
	// harmless for drive paths.
	if !strings.HasSuffix(dir, `\`) {
		dir += `\`
	}
	p, err := windows.UTF16PtrFromString(longPath(dir))
	if err != nil {
		return Space{}, &os.PathError{Op: "GetDiskFreeSpaceEx", Path: dir, Err: err}
	}
	var s Space
	if err := windows.GetDiskFreeSpaceEx(p, &s.Available, &s.Total, &s.Free); err != nil {
		return Space{}, &os.PathError{Op: "GetDiskFreeSpaceEx", Path: dir, Err: err}
	}
	return s, nil
}
