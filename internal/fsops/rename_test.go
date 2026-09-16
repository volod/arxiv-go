package fsops

import (
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestRenameMovesWithoutReplacing(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "src", "clip.mp4")
	dst := filepath.Join(dir, "dst", "clip.mp4")
	writeFile(t, src, []byte("video"))
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := Rename(src, dst); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, src)
	assertContent(t, dst, []byte("video"))

	other := filepath.Join(dir, "src", "other.mp4")
	writeFile(t, other, []byte("other"))
	err := Rename(other, dst)
	if !errors.Is(err, fs.ErrExist) {
		t.Fatalf("Rename onto existing file: got %v, want fs.ErrExist", err)
	}
	var le *os.LinkError
	if !errors.As(err, &le) || IsCrossDevice(err) {
		t.Fatalf("want a non-cross-device *os.LinkError, got %T %v", err, err)
	}
	assertContent(t, other, []byte("other"))
	assertContent(t, dst, []byte("video"))
}

func TestRenameMissingSource(t *testing.T) {
	dir := t.TempDir()
	err := Rename(filepath.Join(dir, "missing"), filepath.Join(dir, "dst"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("got %v, want fs.ErrNotExist", err)
	}
}

func TestReplaceOverwrites(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "new")
	dst := filepath.Join(dir, "old")
	writeFile(t, src, []byte("new"))
	writeFile(t, dst, []byte("old content"))
	if err := Replace(src, dst); err != nil {
		t.Fatal(err)
	}
	assertMissing(t, src)
	assertContent(t, dst, []byte("new"))
}

func TestRenameDeepPath(t *testing.T) {
	dir := t.TempDir()
	deep := dir
	for len(deep) < 300 {
		deep = filepath.Join(deep, strings.Repeat("d", 40))
	}
	src := filepath.Join(deep, "clip.mp4")
	dst := filepath.Join(deep, "moved.mp4")
	writeFile(t, src, []byte("deep"))
	if err := Rename(src, dst); err != nil {
		t.Fatal(err)
	}
	assertContent(t, dst, []byte("deep"))
}

func TestIsCrossDevice(t *testing.T) {
	cross := NewCrossDeviceError("a", "b")
	cases := []struct {
		name string
		err  error
		want bool
	}{
		{"nil", nil, false},
		{"constructed", cross, true},
		{"wrapped", fmt.Errorf("move: %w", cross), true},
		{"errno alone", errCrossDevice, true},
		{"exists", &os.LinkError{Op: "rename", Old: "a", New: "b", Err: fs.ErrExist}, false},
		{"not exist", &os.LinkError{Op: "rename", Old: "a", New: "b", Err: fs.ErrNotExist}, false},
		{"plain", errors.New("cross-device"), false},
	}
	for _, tc := range cases {
		if got := IsCrossDevice(tc.err); got != tc.want {
			t.Errorf("%s: IsCrossDevice(%v) = %v, want %v", tc.name, tc.err, got, tc.want)
		}
	}
}

func TestRenameAcrossDevicesIsClassified(t *testing.T) {
	dir := t.TempDir()
	shm := shmDir(t, dir)
	src := filepath.Join(dir, "clip.mp4")
	writeFile(t, src, []byte("video"))
	for name, rename := range map[string]func(string, string) error{"Rename": Rename, "Replace": Replace} {
		dst := filepath.Join(shm, name+".mp4")
		err := rename(src, dst)
		if !IsCrossDevice(err) {
			t.Fatalf("%s across devices: got %v, want cross-device error", name, err)
		}
		assertContent(t, src, []byte("video"))
		assertMissing(t, dst)
	}
}
