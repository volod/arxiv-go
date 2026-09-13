//go:build windows

package fsops

import (
	"os"

	"golang.org/x/sys/windows"
)

// deviceOf maps an existing path to its volume mount point with
// GetVolumePathName and identifies the volume by the serial number from
// GetVolumeInformation. Mapped drives and UNC paths to the same share report
// the same serial.
func deviceOf(path string) (Device, error) {
	volume, err := volumePath(path)
	if err != nil {
		return Device{}, err
	}
	root, err := windows.UTF16PtrFromString(volume)
	if err != nil {
		return Device{}, &os.PathError{Op: "GetVolumeInformation", Path: volume, Err: err}
	}
	var serial uint32
	if err := windows.GetVolumeInformation(root, nil, 0, &serial, nil, nil, nil, 0); err != nil {
		return Device{}, &os.PathError{Op: "GetVolumeInformation", Path: volume, Err: err}
	}
	return Device{ID: uint64(serial), Volume: volume}, nil
}

// volumePath returns the mount point of the volume holding path, with a
// trailing backslash (for example `C:\` or `\\server\share\`).
func volumePath(path string) (string, error) {
	p, err := windows.UTF16PtrFromString(longPath(path))
	if err != nil {
		return "", &os.PathError{Op: "GetVolumePathName", Path: path, Err: err}
	}
	buf := make([]uint16, windows.MAX_LONG_PATH)
	if err := windows.GetVolumePathName(p, &buf[0], uint32(len(buf))); err != nil {
		return "", &os.PathError{Op: "GetVolumePathName", Path: path, Err: err}
	}
	return windows.UTF16ToString(buf), nil
}
