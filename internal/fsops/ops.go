package fsops

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
)

// PartSuffix is the reserved suffix of arxgo temporary files. Scans exclude
// it and recovery may delete such files.
const PartSuffix = ".arxgo-part"

// PartPath returns the temporary file name used while writing path.
func PartPath(path string) string { return path + PartSuffix }

// Ops is the set of filesystem operations mutating tasks depend on. System
// is the real implementation; tests substitute fakes (for example a device
// function that reports two roots on different devices, or a Rename that
// returns NewCrossDeviceError).
type Ops interface {
	SameDevice(a, b string) (bool, error)
	FreeSpace(path string) (Space, error)
	Rename(oldpath, newpath string) error
	Replace(oldpath, newpath string) error
	DurableCopy(ctx context.Context, src, dst string, opts CopyOptions) (CopyResult, error)
	AtomicWriteFile(path string, data []byte, perm fs.FileMode) error
}

// System implements Ops with the operating system's filesystem.
type System struct{}

var _ Ops = System{}

// SameDevice implements Ops.
func (System) SameDevice(a, b string) (bool, error) { return SameDevice(a, b) }

// FreeSpace implements Ops.
func (System) FreeSpace(path string) (Space, error) { return FreeSpace(path) }

// Rename implements Ops.
func (System) Rename(oldpath, newpath string) error { return Rename(oldpath, newpath) }

// Replace implements Ops.
func (System) Replace(oldpath, newpath string) error { return Replace(oldpath, newpath) }

// DurableCopy implements Ops.
func (System) DurableCopy(ctx context.Context, src, dst string, opts CopyOptions) (CopyResult, error) {
	return DurableCopy(ctx, src, dst, opts)
}

// AtomicWriteFile implements Ops.
func (System) AtomicWriteFile(path string, data []byte, perm fs.FileMode) error {
	return AtomicWriteFile(path, data, perm)
}

// Device identifies the filesystem volume holding a path. Two paths are on
// the same device when their IDs are equal.
type Device struct {
	ID uint64
	// Volume is a diagnostic label: the nearest existing path on Unix, the
	// volume mount point (for example `C:\` or `\\server\share\`) on Windows.
	Volume string
}

// DeviceOf returns the device holding path. A path that does not exist yet
// (for example a video archive root that split will create) resolves to its
// nearest existing ancestor.
func DeviceOf(path string) (Device, error) {
	existing, err := nearestExisting(path)
	if err != nil {
		return Device{}, err
	}
	return deviceOf(existing)
}

// SameDevice reports whether a and b are on the same filesystem volume, so
// that a rename between them can succeed. Missing paths resolve to their
// nearest existing ancestor.
func SameDevice(a, b string) (bool, error) {
	da, err := DeviceOf(a)
	if err != nil {
		return false, err
	}
	db, err := DeviceOf(b)
	if err != nil {
		return false, err
	}
	return da.ID == db.ID, nil
}

// Space describes a volume's capacity in bytes. Available is what an
// unprivileged caller may still write (the value preflight uses); Free
// includes reserved blocks. Network filesystems may report Total == 0 when
// the value is unknown.
type Space struct {
	Total     uint64
	Free      uint64
	Available uint64
}

// FreeSpace returns the capacity of the volume holding path, resolving a
// missing path to its nearest existing ancestor directory.
func FreeSpace(path string) (Space, error) {
	existing, err := nearestExisting(path)
	if err != nil {
		return Space{}, err
	}
	if fi, err := os.Stat(existing); err == nil && !fi.IsDir() {
		existing = filepath.Dir(existing)
	}
	return freeSpace(existing)
}

// nearestExisting returns the absolute form of path or of its closest
// ancestor that exists. Errors other than "not exist" (for example
// permission denied) are returned.
func nearestExisting(path string) (string, error) {
	abs, err := filepath.Abs(path)
	if err != nil {
		return "", err
	}
	for p := abs; ; {
		_, err := os.Stat(p)
		if err == nil {
			return p, nil
		}
		if !errors.Is(err, fs.ErrNotExist) {
			return "", err
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", err
		}
		p = parent
	}
}
