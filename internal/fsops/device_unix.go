//go:build unix

package fsops

import (
	"os"

	"golang.org/x/sys/unix"
)

// deviceOf reads st_dev of an existing path. Symlinks are followed, so a
// root that is a symlink reports the device of its target.
func deviceOf(path string) (Device, error) {
	var st unix.Stat_t
	if err := unix.Stat(path, &st); err != nil {
		return Device{}, &os.PathError{Op: "stat", Path: path, Err: err}
	}
	return Device{ID: uint64(st.Dev), Volume: path}, nil
}

func devicesEqual(a, b Device) bool { return a.ID == b.ID }
