//go:build unix && !linux

package fsops

import "golang.org/x/sys/unix"

// blockSize returns the unit of the statfs block counts.
func blockSize(st *unix.Statfs_t) uint64 { return uint64(st.Bsize) }

// renameExclusive reports that no atomic no-replace rename is available.
func renameExclusive(oldpath, newpath string) (supported bool, err error) { return false, nil }
