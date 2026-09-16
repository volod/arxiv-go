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
	want := []string{row.RelPath, row.FileName, "mp4", "42", "false", "video/mp4", "true", "true", "false", "true", "false", "archive"}
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
	if int64(len(data)) != end || strings.Contains(string(data), "lost") || !strings.Contains(string(data), "\nb,b,,0,false,,false,false,false,false,false,") {
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

func TestFileRegistryHeaderOrder(t *testing.T) {
	want := []string{
		"rel_path", "file_name", "file_type", "file_size", "is_large", "file_mime",
		"is_binary", "is_media", "is_picture", "is_video", "is_catia",
	}
	if got := RegistryHeader[:FileRegistryRequired]; !reflect.DeepEqual(got, want) {
		t.Fatalf("required header = %q, want %q", got, want)
	}
	if got := RegistryHeader[FileRegistryRequired]; got != "location" {
		t.Fatalf("column 12 = %q, want location", got)
	}
	if got := RegistryHeader[FileRegistryRequired+1:]; !reflect.DeepEqual(got, MetadataHeader) {
		t.Fatalf("metadata columns = %q", got)
	}
}

func TestRegistryLocationRoundTripAndValidation(t *testing.T) {
	var b strings.Builder
	path := filepath.Join(t.TempDir(), "reg.csv")
	w, err := CreateRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, loc := range []string{LocationArchive, LocationVideoArchive, LocationCatiaArchive} {
		if err := w.Write(RegistryRow{RelPath: loc + ".bin", FileName: loc + ".bin", Location: loc}); err != nil {
			t.Fatal(err)
		}
	}
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	rows, err := LoadRegistry(path)
	if err != nil || len(rows) != 3 {
		t.Fatalf("rows = %+v (%v)", rows, err)
	}
	for _, row := range rows {
		if row.RelPath != row.Location+".bin" {
			t.Errorf("row %s: location %q", row.RelPath, row.Location)
		}
	}
	b.WriteString(strings.Join(RegistryHeader, ","))
	b.WriteString("\na,a,,0,false,,false,false,false,false,false,mirror" + strings.Repeat(",", len(MetadataHeader)) + "\n")
	if _, err := ReadRegistry(strings.NewReader(b.String())); err == nil || !strings.Contains(err.Error(), "location") {
		t.Fatalf("unknown location accepted: %v", err)
	}
}

func TestLoadRegistryRejectsPreviousColumnOrder(t *testing.T) {
	old := "rel_path,file_name,file_size,file_type,file_mime,is_binary,is_media,is_picture,is_video,is_large\na,a,1,txt,text/plain,false,false,false,false,false\n"
	if _, err := ReadRegistry(strings.NewReader(old)); err == nil || !strings.Contains(err.Error(), "unexpected header") {
		t.Fatalf("old header: %v", err)
	}
}

func TestRegistryRoundTripFlagsAndMetadata(t *testing.T) {
	path := filepath.Join(t.TempDir(), "reg.csv")
	w, err := CreateRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	meta := FileMetadata(time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC))
	meta.LinkTarget = "fixture-target"
	row := RegistryRow{
		RelPath: "cad/fixture-part.CATPart", FileName: "fixture-part.CATPart", FileSize: 12,
		FileType: "catpart", FileMIME: "application/octet-stream",
		IsBinary: true, IsCatia: true, IsLarge: true, Location: LocationCatiaArchive, Metadata: meta,
	}
	if err := w.Write(row); err != nil || w.Close() != nil {
		t.Fatal(err)
	}
	got, err := LoadRegistry(path)
	if err != nil || len(got) != 1 {
		t.Fatalf("loaded = %+v (%v)", got, err)
	}
	got[0].Metadata.Media = nil
	if !reflect.DeepEqual(got[0], row) {
		t.Fatalf("round trip\n got %+v\nwant %+v", got[0], row)
	}
}
