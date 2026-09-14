package report

import (
	"path/filepath"
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
