//go:build linux

package fsops

import (
	"errors"

	"golang.org/x/sys/unix"
)

// blockSize returns the unit of the statfs block counts: f_frsize, which the
// kernel sets to f_bsize unless the filesystem reports its own value.
func blockSize(st *unix.Statfs_t) uint64 {
	if st.Frsize > 0 {
		return uint64(st.Frsize)
	}
	return uint64(st.Bsize)
}

// renameExclusive renames with renameat2(RENAME_NOREPLACE), which fails
// atomically with EEXIST when newpath exists. supported is false when the
// kernel or filesystem lacks the flag (EINVAL, ENOSYS), so the caller falls
// back to a check followed by rename.
func renameExclusive(oldpath, newpath string) (supported bool, err error) {
	err = unix.Renameat2(unix.AT_FDCWD, oldpath, unix.AT_FDCWD, newpath, unix.RENAME_NOREPLACE)
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOSYS) {
		return false, nil
	}
	return true, err
}
