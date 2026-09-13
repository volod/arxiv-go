package fsops

import (
	"os"
	"path/filepath"
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
