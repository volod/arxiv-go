//go:build unix

package fsops

import (
	"os"

	"golang.org/x/sys/unix"
)

// freeSpace queries statfs for an existing directory.
func freeSpace(dir string) (Space, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return Space{}, &os.PathError{Op: "statfs", Path: dir, Err: err}
	}
	size := blockSize(&st)
	return Space{
		Total:     uint64(st.Blocks) * size,
		Free:      uint64(st.Bfree) * size,
		Available: uint64(st.Bavail) * size,
	}, nil
}
