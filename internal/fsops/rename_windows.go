//go:build windows

package fsops

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"
)

var errCrossDevice error = windows.ERROR_NOT_SAME_DEVICE

// renameNoReplace uses MoveFileEx without MOVEFILE_REPLACE_EXISTING, which
// fails atomically when newpath exists. MOVEFILE_WRITE_THROUGH waits for the
// move to reach the disk.
func renameNoReplace(oldpath, newpath string) error {
	return moveFile(oldpath, newpath, windows.MOVEFILE_WRITE_THROUGH)
}

func replaceFile(oldpath, newpath string) error {
	return moveFile(oldpath, newpath, windows.MOVEFILE_REPLACE_EXISTING|windows.MOVEFILE_WRITE_THROUGH)
}

func moveFile(oldpath, newpath string, flags uint32) error {
	from, err := windows.UTF16PtrFromString(longPath(oldpath))
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
	}
	to, err := windows.UTF16PtrFromString(longPath(newpath))
	if err != nil {
		return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
	}
	// Another process (a reader, an indexer or antivirus) holding the file
	// without delete sharing makes the move fail transiently; retry briefly.
	delay := 5 * time.Millisecond
	for attempt := 0; ; attempt++ {
		err = windows.MoveFileEx(from, to, flags)
		if err == nil {
			return nil
		}
		if attempt == 8 || !(errors.Is(err, windows.ERROR_ACCESS_DENIED) || errors.Is(err, windows.ERROR_SHARING_VIOLATION)) {
			return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: err}
		}
		time.Sleep(delay)
		delay *= 2
	}
}

// syncDir is a no-op: Windows cannot flush a directory handle, and
// MoveFileEx with MOVEFILE_WRITE_THROUGH already waits for the move.
func syncDir(string) error { return nil }

// longPath adds the `\\?\` prefix to absolute paths near MAX_PATH so deep
// archive trees work without the system-wide long path setting.
func longPath(path string) string {
	abs, err := filepath.Abs(path)
	if err != nil || len(abs) < 248 || strings.HasPrefix(abs, `\\?\`) {
		return path
	}
	if strings.HasPrefix(abs, `\\`) {
		return `\\?\UNC\` + abs[2:]
	}
	return `\\?\` + abs
}
