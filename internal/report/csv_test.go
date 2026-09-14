package report

import (
	"encoding/csv"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestRegistryWriterQuotingAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.csv.arxgo-part")
	w, err := CreateRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := FileMetadata(time.Date(2024, 5, 1, 12, 22, 3, 999, time.FixedZone("EEST", 3*3600)))
	meta.LinkTarget = "../a&b<c>.txt"
	row := RegistryRow{
		RelPath: "dir/new\nline, \"quoted\".mp4", FileName: "new\nline, \"quoted\".mp4", FileSize: 42,
		FileType: "mp4", FileMIME: "video/mp4", IsBinary: true, IsMedia: true, IsVideo: true, Metadata: meta,
	}
	if err := w.Write(row); err != nil {
		t.Fatal(err)
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(string(data), "\r") {
		t.Error("registry contains CR")
	}
	records, err := csv.NewReader(strings.NewReader(string(data))).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	want := []string{row.RelPath, row.FileName, "42", "mp4", "video/mp4", "true", "true", "false", "true", "false"}
	want = append(want, MetadataCells(meta)...)
	if len(records) != 2 || !reflect.DeepEqual(records[0], RegistryHeader) || !reflect.DeepEqual(records[1], want) {
		t.Errorf("records = %q", records)
	}
}

func TestRegistryResumeTruncatesToOffset(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.part")
	w, err := CreateRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Write(RegistryRow{RelPath: "a", FileName: "a"})
	offset, err := w.Sync()
	if err != nil {
		t.Fatal(err)
	}
	_ = w.Write(RegistryRow{RelPath: "lost", FileName: "lost"})
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	r, err := ResumeRegistry(path, offset)
	if err != nil {
		t.Fatal(err)
	}
	_ = r.Write(RegistryRow{RelPath: "b", FileName: "b"})
	end, err := r.Sync()
	if err != nil || r.Close() != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	if int64(len(data)) != end || strings.Contains(string(data), "lost") || !strings.Contains(string(data), "\nb,b,0,,,false,false,false,false,false,") {
		t.Errorf("resumed registry %q (end %d)", data, end)
	}
	if _, err := ResumeRegistry(path, end+1); !errors.Is(err, ErrPartTooShort) {
		t.Errorf("offset past the end: %v", err)
	}
	if _, err := ResumeRegistry(path+".missing", 1); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing part: %v", err)
	}
}

func TestDiscardRegistryCountsWithoutFile(t *testing.T) {
	w := DiscardRegistry()
	_ = w.Write(RegistryRow{RelPath: "a", FileName: "a"})
	n, err := w.Sync()
	if err != nil || n == 0 || w.Close() != nil {
		t.Fatalf("discard writer: %d, %v", n, err)
	}
}

func TestFileMetadataOmitsZeroTime(t *testing.T) {
	if m := FileMetadata(time.Time{}); m.MTime != "" || HasMetadata(m) {
		t.Errorf("metadata = %+v", m)
	}
}

func TestMarkRestoredAndHasMoved(t *testing.T) {
	rows := []VideoRow{
		{RelPath: "a.mp4", Status: StatusMoved, RunID: "old"},
		{RelPath: "b.mp4", Status: StatusMoved},
		{RelPath: "c.mp4", Status: StatusConflict},
	}
	got := MarkRestored(rows, map[string]struct{}{"a.mp4": {}, "b.mp4": {}}, "new")
	if got[0].Status != StatusRestored || got[0].RunID != "new" {
		t.Fatalf("a = %+v", got[0])
	}
	if got[1].Status != StatusRestored {
		t.Fatalf("b = %+v", got[1])
	}
	if got[2].Status != StatusConflict {
		t.Fatalf("c = %+v", got[2])
	}
	if HasMoved(got) {
		t.Fatal("HasMoved after marking a and b")
	}
	if !HasMoved(rows) {
		t.Fatal("original still has moved")
	}
}
