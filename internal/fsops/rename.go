package fsops

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// Rename moves oldpath to newpath without ever replacing an existing
// newpath, then flushes the affected directories so the rename survives a
// crash. Errors are *os.LinkError values: errors.Is(err, fs.ErrExist) when
// newpath exists and IsCrossDevice(err) when the paths are on different
// volumes (callers then fall back to DurableCopy).
//
// A non-LinkError result means the rename happened but a directory flush
// failed; the move is visible but may not be durable.
func Rename(oldpath, newpath string) error {
	if err := renameNoReplace(oldpath, newpath); err != nil {
		return err
	}
	return syncParents(oldpath, newpath)
}

// Replace atomically moves oldpath to newpath, replacing an existing file,
// and flushes the affected directories. Errors are classified as for Rename.
func Replace(oldpath, newpath string) error {
	if err := replaceFile(oldpath, newpath); err != nil {
		return err
	}
	return syncParents(oldpath, newpath)
}

// IsCrossDevice reports whether err is a rename failure caused by the two
// paths being on different volumes (EXDEV on Unix, ERROR_NOT_SAME_DEVICE on
// Windows), usually wrapped in an *os.LinkError.
func IsCrossDevice(err error) bool {
	return err != nil && errors.Is(err, errCrossDevice)
}

// NewCrossDeviceError returns the error a rename between volumes produces
// on this platform, for fakes that simulate a cross-device layout.
func NewCrossDeviceError(oldpath, newpath string) error {
	return &os.LinkError{Op: "rename", Old: oldpath, New: newpath, Err: errCrossDevice}
}

func syncParents(oldpath, newpath string) error {
	newDir := filepath.Dir(newpath)
	if err := syncDir(newDir); err != nil {
		return fmt.Errorf("fsops: renamed %s but flushing %s failed: %w", oldpath, newDir, err)
	}
	if oldDir := filepath.Dir(oldpath); filepath.Clean(oldDir) != filepath.Clean(newDir) {
		if err := syncDir(oldDir); err != nil {
			return fmt.Errorf("fsops: renamed %s but flushing %s failed: %w", oldpath, oldDir, err)
		}
	}
	return nil
}
