//go:build unix

package fsops

import (
	"errors"
	"io/fs"
	"os"

	"golang.org/x/sys/unix"
)

var errCrossDevice error = unix.EXDEV

func renameNoReplace(oldpath, newpath string) error {
	supported, err := renameExclusive(oldpath, newpath)
	if supported {
		if err != nil {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
		}
		return nil
	}
	// Fallback for filesystems without RENAME_NOREPLACE. The run lock keeps
	// other arxgo processes away; the window only matters for concurrent
	// writers outside arxgo.
	if _, err := os.Lstat(newpath); err == nil {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: unix.EEXIST}
	} else if !errors.Is(err, fs.ErrNotExist) {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
	}
	return os.Rename(oldpath, newpath)
}

func replaceFile(oldpath, newpath string) error { return os.Rename(oldpath, newpath) }

// syncDir flushes directory entries. Filesystems that cannot fsync a
// directory (some FUSE and network mounts) report EINVAL or ENOTSUP; the
// rename is then as durable as that filesystem allows.
func syncDir(dir string) error {
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	err = f.Sync()
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if errors.Is(err, unix.EINVAL) || errors.Is(err, unix.ENOTSUP) || errors.Is(err, unix.EOPNOTSUPP) {
		return nil
	}
	return err
}
