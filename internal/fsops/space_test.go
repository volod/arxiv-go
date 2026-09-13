package fsops

import (
	"path/filepath"
	"testing"
)

func TestFreeSpaceTempDir(t *testing.T) {
	dir := t.TempDir()
	s, err := FreeSpace(dir)
	if err != nil {
		t.Fatal(err)
	}
	if s.Total == 0 || s.Available == 0 {
		t.Fatalf("FreeSpace(%s) = %+v, want non-zero total and available", dir, s)
	}
	if s.Available > s.Free || s.Free > s.Total {
		t.Fatalf("FreeSpace(%s) = %+v, want available <= free <= total", dir, s)
	}
}

func TestFreeSpaceFileAndMissingPathUseTheirVolume(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "registry.csv")
	writeFile(t, file, []byte("rel_path\n"))
	want, err := FreeSpace(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{file, filepath.Join(dir, "missing", "video-archive")} {
		got, err := FreeSpace(p)
		if err != nil {
			t.Fatalf("FreeSpace(%s): %v", p, err)
		}
		if got.Total != want.Total {
			t.Fatalf("FreeSpace(%s).Total = %d, want %d", p, got.Total, want.Total)
		}
	}
}
