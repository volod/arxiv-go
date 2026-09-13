package fsops

import (
	"bytes"
	"errors"
	"io/fs"
	"math/rand/v2"
	"os"
	"path/filepath"
	"testing"
)

// payload returns n deterministic pseudo-random bytes.
func payload(seed uint64, n int) []byte {
	r := rand.New(rand.NewChaCha8([32]byte{byte(seed), byte(seed >> 8), 42}))
	b := make([]byte, n)
	for i := range b {
		b[i] = byte(r.Uint32())
	}
	return b
}

func writeFile(t *testing.T, path string, data []byte) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, data, 0o644); err != nil {
		t.Fatal(err)
	}
}

func assertContent(t *testing.T, path string, want []byte) {
	t.Helper()
	got, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read %s: %v", path, err)
	}
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: content differs (got %d bytes, want %d)", path, len(got), len(want))
	}
}

func assertMissing(t *testing.T, path string) {
	t.Helper()
	if _, err := os.Lstat(path); !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("%s: want not exist, got %v", path, err)
	}
}

// shmDir returns a fresh directory under /dev/shm that is on a different
// device than dir, or skips the test with the reason.
func shmDir(t *testing.T, dir string) string {
	t.Helper()
	if fi, err := os.Stat("/dev/shm"); err != nil || !fi.IsDir() {
		t.Skip("/dev/shm is not available on this host")
	}
	shm, err := os.MkdirTemp("/dev/shm", "arxgo-fsops-")
	if err != nil {
		t.Skipf("/dev/shm is not writable: %v", err)
	}
	t.Cleanup(func() { os.RemoveAll(shm) })
	same, err := SameDevice(shm, dir)
	if err != nil {
		t.Fatal(err)
	}
	if same {
		t.Skipf("/dev/shm and %s share a device on this host", dir)
	}
	return shm
}
