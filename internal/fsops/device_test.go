package fsops

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestSameDeviceSiblings(t *testing.T) {
	root := t.TempDir()
	a := filepath.Join(root, "a")
	b := filepath.Join(root, "b")
	writeFile(t, filepath.Join(a, "clip.mp4"), []byte("x"))
	if err := os.Mkdir(b, 0o755); err != nil {
		t.Fatal(err)
	}
	cases := []struct{ name, x, y string }{
		{"sibling directories", a, b},
		{"file and directory", filepath.Join(a, "clip.mp4"), b},
		{"missing path resolves to ancestor", a, filepath.Join(b, "not", "yet", "created")},
		{"relative and absolute", root, relTo(t, root)},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			same, err := SameDevice(tc.x, tc.y)
			if err != nil {
				t.Fatal(err)
			}
			if !same {
				t.Fatalf("SameDevice(%q, %q) = false, want true", tc.x, tc.y)
			}
		})
	}
}

func TestSameDeviceFalseAcrossShmAndWorkspace(t *testing.T) {
	if fi, err := os.Stat("/dev/shm"); err != nil || !fi.IsDir() {
		t.Skip("/dev/shm is not available on this host")
	}
	workspace, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	dShm, err := DeviceOf("/dev/shm")
	if err != nil {
		t.Fatal(err)
	}
	dWork, err := DeviceOf(workspace)
	if err != nil {
		t.Fatal(err)
	}
	if dShm.ID == dWork.ID {
		t.Skipf("workspace %s is on the /dev/shm device on this host", workspace)
	}
	same, err := SameDevice("/dev/shm", workspace)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Fatal("SameDevice(/dev/shm, workspace) = true, want false")
	}
}

func TestDeviceOfMissingPathMatchesAncestor(t *testing.T) {
	root := t.TempDir()
	want, err := DeviceOf(root)
	if err != nil {
		t.Fatal(err)
	}
	got, err := DeviceOf(filepath.Join(root, "missing", "video-archive"))
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != want.ID {
		t.Fatalf("device of missing path = %d, want %d", got.ID, want.ID)
	}
	if got.Volume == "" {
		t.Fatal("Volume label is empty")
	}
}

func TestDevicesEqual(t *testing.T) {
	if !devicesEqual(Device{ID: 7, Volume: "a"}, Device{ID: 7, Volume: "b"}) {
		t.Fatal("same ID must match when Volume differs")
	}
	if devicesEqual(Device{ID: 1, Volume: "x"}, Device{ID: 2, Volume: "x"}) {
		t.Fatal("different IDs must not match")
	}
	zeroSame := devicesEqual(Device{ID: 0, Volume: `C:\`}, Device{ID: 0, Volume: `c:\`})
	zeroDiff := devicesEqual(Device{ID: 0, Volume: `C:\`}, Device{ID: 0, Volume: `D:\`})
	if runtime.GOOS == "windows" {
		if !zeroSame {
			t.Fatal("Windows: serial 0 must match equal mount points ignoring case")
		}
		if zeroDiff {
			t.Fatal("Windows: serial 0 must not treat distinct mount points as one device")
		}
		return
	}
	if !devicesEqual(Device{ID: 0, Volume: "/tmp/a"}, Device{ID: 0, Volume: "/tmp/b"}) {
		t.Fatal("Unix: st_dev 0 matches regardless of path")
	}
}

// relTo returns path relative to the working directory, or skips when the
// temporary directory is on another Windows drive.
func relTo(t *testing.T, path string) string {
	t.Helper()
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	rel, err := filepath.Rel(wd, path)
	if err != nil {
		t.Skipf("no relative path from %s to %s: %v", wd, path, err)
	}
	return rel
}
