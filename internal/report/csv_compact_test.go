package report

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDropEmptyCSVColumnsOmitsUnusedMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.csv")
	w, err := CreateRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	row := RegistryRow{
		RelPath: "notes.txt", FileName: "notes.txt", FileSize: 4, FileType: "txt", FileMIME: "text/plain",
		Metadata: FileMetadata(time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC)),
	}
	if err := w.Write(row); err != nil || w.Close() != nil {
		t.Fatal(err)
	}
	if err := DropEmptyCSVColumns(path, FileRegistryKeep); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil || len(records) != 2 {
		t.Fatalf("records: %v %v", records, err)
	}
	if strings.Contains(string(data), "media_source") || strings.Contains(string(data), "link_target") {
		t.Fatalf("unused columns kept: %q", records[0])
	}
	if records[0][len(records[0])-1] != "mtime" {
		t.Fatalf("header = %q", records[0])
	}
	got, err := LoadRegistry(path)
	if err != nil || len(got) != 1 || got[0].RelPath != "notes.txt" || got[0].Metadata.MTime == "" || got[0].Metadata.Media != nil {
		t.Fatalf("loaded = %+v (%v)", got, err)
	}
}

func TestLoadRegistryAcceptsFullWidthHeader(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.csv")
	w, err := CreateRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	row := RegistryRow{
		RelPath: "notes.txt", FileName: "notes.txt", FileSize: 4, FileType: "txt", FileMIME: "text/plain",
		Metadata: FileMetadata(time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC)),
	}
	if err := w.Write(row); err != nil || w.Close() != nil {
		t.Fatal(err)
	}
	got, err := LoadRegistry(path)
	if err != nil || len(got) != 1 || got[0].RelPath != "notes.txt" || got[0].Metadata.MTime == "" {
		t.Fatalf("loaded = %+v (%v)", got, err)
	}
}

func TestWriteVideoCSVOmitsUnusedMetadata(t *testing.T) {
	var b strings.Builder
	if err := WriteVideoCSV(&b, []VideoRow{{
		RelPath: "a.mp4", FileName: "a.mp4", FileSize: 1, Status: StatusMoved, RunID: "r1",
		Metadata: FileMetadata(time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC)),
	}}); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(b.String(), "media_source") {
		t.Fatalf("unused media columns: %s", b.String())
	}
	rows, err := LoadVideoCSV(strings.NewReader(b.String()))
	if err != nil || len(rows) != 1 || rows[0].RelPath != "a.mp4" || rows[0].Metadata.MTime == "" {
		t.Fatalf("loaded = %+v (%v)", rows, err)
	}
}
