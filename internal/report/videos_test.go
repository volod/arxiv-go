package report

import (
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestComposeURLAndRelativeLink(t *testing.T) {
	u := ComposeURL("https://cdn.example.com/v", "dir/my file.mp4")
	if u != "https://cdn.example.com/v/dir/my%20file.mp4" {
		t.Fatalf("url = %s", u)
	}
	dir := t.TempDir()
	from := filepath.Join(dir, "a", "b")
	to := filepath.Join(dir, "video", "a", "b", "clip.mp4")
	rel := RelativeLink(from, to)
	if rel != "../../video/a/b/clip.mp4" {
		t.Fatalf("rel = %s", rel)
	}
	for in, want := range map[string]string{
		"/mnt/nas/video/a b/clip#1.MP4": "file:///mnt/nas/video/a%20b/clip%231.MP4",
		"D:/video/a b/clip.mp4":         "file:///D:/video/a%20b/clip.mp4",
		"//nas/video/clip.mp4":          "file://nas/video/clip.mp4",
		"video/clip.mp4":                "",
	} {
		if got := FileURL(in); got != want {
			t.Errorf("FileURL(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestMergeVideoRowsWalkOrderAndMovedWins(t *testing.T) {
	existing := []VideoRow{
		{RelPath: "z.mp4", Status: StatusMoved, FileSize: 1, RunID: "run1"},
		{RelPath: "a/b.mp4", Status: StatusMoved, FileSize: 2, RunID: "run1", SHA256: "abc"},
	}
	incoming := []VideoRow{
		{RelPath: "m.mp4", Status: StatusMoved, FileSize: 3, RunID: "run2"},
		{RelPath: "a/b.mp4", Status: StatusSkipped, FileSize: 2, RunID: "run2"},
		{RelPath: "z.mp4", Status: StatusMoved, FileSize: 1, RunID: "run2", FileMIME: "video/mp4"},
	}
	got := MergeVideoRows(existing, incoming)
	if len(got) != 3 || got[0].RelPath != "a/b.mp4" || got[1].RelPath != "m.mp4" || got[2].RelPath != "z.mp4" {
		t.Fatalf("order = %v", got)
	}
	if got[0].Status != StatusMoved || got[0].SHA256 != "abc" {
		t.Fatalf("moved must win and keep sha: %+v", got[0])
	}
	if got[2].FileMIME != "video/mp4" || got[2].RunID != "run2" {
		t.Fatalf("overlay: %+v", got[2])
	}
}

func TestPayloadRegistryColumnOrder(t *testing.T) {
	want := []string{"rel_path", "file_name", "status", "url", "description_rel_path",
		"file_size", "sha256", "transfer", "run_id", "file_mime"}
	if !slices.Equal(PayloadHeader, want) || PayloadRegistryKeep != len(want) {
		t.Fatalf("payload header = %q", PayloadHeader)
	}
	if !slices.Equal(VideoHeader[:VideoRegistryRequired], append(slices.Clone(want), "previews")) ||
		!slices.Equal(VideoHeader[VideoRegistryRequired:], MetadataHeader) {
		t.Fatalf("video header = %q", VideoHeader)
	}
	row := VideoRow{RelPath: "a/b.mp4", FileName: "b.mp4", Status: StatusMoved, URL: "file:///v/a/b.mp4",
		DescriptionRelPath: "a/b.mp4.md", FileSize: 7, SHA256: "abc", Transfer: "copy", RunID: "r1",
		FileMIME: "video/mp4", Previews: "a/b-img01.png"}
	var b strings.Builder
	if err := WriteVideoCSV(&b, []VideoRow{row}); err != nil {
		t.Fatal(err)
	}
	wantCSV := strings.Join(VideoHeader, ",") + "\n" +
		"a/b.mp4,b.mp4,moved,file:///v/a/b.mp4,a/b.mp4.md,7,abc,copy,r1,video/mp4,a/b-img01.png" +
		strings.Repeat(",", len(MetadataHeader)) + "\n"
	if b.String() != wantCSV {
		t.Fatalf("csv =\n%s\nwant\n%s", b.String(), wantCSV)
	}
	got, err := LoadVideoCSV(strings.NewReader(b.String()))
	if err != nil || len(got) != 1 || got[0] != row || got[0].Payload() != (PayloadRow{RelPath: "a/b.mp4", FileName: "b.mp4",
		Status: StatusMoved, URL: "file:///v/a/b.mp4", DescriptionRelPath: "a/b.mp4.md", FileSize: 7, SHA256: "abc",
		Transfer: "copy", RunID: "r1", FileMIME: "video/mp4"}) {
		t.Fatalf("round trip = %+v, %v", got, err)
	}
	previous := "rel_path,description_rel_path,file_name,file_size,file_mime,sha256,transfer,status,run_id,url,previews\n"
	if _, err := LoadVideoCSV(strings.NewReader(previous)); err == nil {
		t.Fatal("video registry in the previous column order loaded")
	}
}
