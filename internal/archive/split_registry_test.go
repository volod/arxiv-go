package archive

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/test/fixtures/testmp4"
)

func TestSplitWritesVideoRegistryToBothRoots(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	c.BaseURL = "https://cdn.example.com/v"
	if ms, ok := c.Descriptions.(*MarkdownDescription); ok {
		ms.cfg.BaseURL = c.BaseURL
	}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	checkSplit(t, src, dst)
	description := string(mustRead(t, src+".md"))
	if !strings.HasPrefix(description, "arxgo: nested/clip.mp4\n") || !strings.Contains(description, "url: https://cdn.example.com/v/nested/clip.mp4") {
		t.Fatalf("description:\n%s", description)
	}
	for _, root := range []string{r.archive, r.video} {
		csvPath := filepath.Join(root, scanner.VideoRegistryName)
		rows := readVideoCSV(t, csvPath)
		if len(rows) != 2 || rows[0][0] != "rel_path" || rows[1][0] != "nested/clip.mp4" {
			t.Fatalf("%s rows = %q", csvPath, rows)
		}
		if rows[1][7] != "moved" || rows[1][9] != "https://cdn.example.com/v/nested/clip.mp4" {
			t.Fatalf("row = %q", rows[1])
		}
		if exists(filepath.Join(root, "arxgo-videos.md")) {
			t.Fatal("unexpected Markdown summary")
		}
	}
}

func TestSplitRegistryMergesTwoRuns(t *testing.T) {
	r := newRoots(t)
	writeVideo(t, r.archive, "a/one.mp4")
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("run1 = %+v", res)
	}
	writeVideo(t, r.archive, "b/two.mp4")
	cfg, c = splitConfig(r, "auto")
	cfg.NewRun = true
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("run2 = %+v", res)
	}
	rows := readVideoCSV(t, filepath.Join(r.archive, scanner.VideoRegistryName))
	if len(rows) != 3 {
		t.Fatalf("rows = %q", rows)
	}
	if rows[1][0] != "a/one.mp4" || rows[2][0] != "b/two.mp4" {
		t.Fatalf("walk order = %q", rows)
	}
	if rows[1][7] != "moved" || rows[2][7] != "moved" {
		t.Fatalf("status = %q", rows)
	}
	for _, row := range rows[1:] {
		want := report.FileURL(filepath.ToSlash(filepath.Join(r.video, filepath.FromSlash(row[0]))))
		if row[9] != want {
			t.Errorf("%s local URL = %q, want %q", row[0], row[9], want)
		}
	}
}

func TestSplitMediaModeDescriptionHasDuration(t *testing.T) {
	// A QuickTime movie carries a second (data) handler box; it must still be a moved video.
	// File mode collects ISO BMFF fields without ffprobe; media mode does the same for these
	// containers and would additionally use ffprobe for other formats.
	for _, mode := range []string{"file", "media"} {
		for name, quickTime := range map[string]bool{"interview.mp4": false, "camera.mov": true} {
			t.Run(mode+"/"+name, func(t *testing.T) {
				r := newRoots(t)
				body := testmp4.File(testmp4.Options{
					Tracks:    []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 1920, Height: 1080}, {Kind: "soun", Codec: "mp4a"}},
					Title:     "Interview",
					QuickTime: quickTime,
				})
				src := filepath.Join(r.archive, name)
				if err := os.WriteFile(src, body, 0o644); err != nil {
					t.Fatal(err)
				}
				cfg, c := splitConfig(r, "auto")
				c.Scan.Metadata = mode
				if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
					t.Fatalf("split = %+v", res)
				}
				if exists(src) || !exists(filepath.Join(r.video, name)) {
					t.Fatal("video not moved")
				}
				description := string(mustRead(t, src+".md"))
				if !strings.Contains(description, "0:05 | 1920x1080 | h264 + aac | 25 fps") {
					t.Fatalf("media line:\n%s", description)
				}
				rows, err := report.LoadVideoFile(filepath.Join(r.archive, scanner.VideoRegistryName))
				if err != nil || len(rows) != 1 || rows[0].Metadata.Media == nil {
					t.Fatalf("video rows = %+v (%v)", rows, err)
				}
				m := rows[0].Metadata.Media
				if m.Source != "go-mp4" || m.DurationS != 5 || m.Width != 1920 || m.Height != 1080 || m.VideoCodec != "h264" || m.AudioCodec != "aac" || m.FrameRate != "25/1" || m.Tags["title"] != "Interview" {
					t.Fatalf("video CSV media = %+v", m)
				}
			})
		}
	}
}

func TestSplitForeignThenIndexedDescription(t *testing.T) {
	r, src, dst := splitFixture(t)
	if err := os.WriteFile(src+".md", []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+".arxgo.md", []byte("other"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if string(mustRead(t, src+".md")) != "notes" || string(mustRead(t, src+".arxgo.md")) != "other" {
		t.Fatal("foreign files changed")
	}
	indexed := filepath.Join(filepath.Dir(src), "clip-1.mp4.md")
	if !strings.HasPrefix(string(mustRead(t, indexed)), "arxgo: nested/clip.mp4\n") {
		t.Fatal("indexed description missing")
	}
	if !exists(dst) {
		t.Fatal("video missing")
	}
}

func writeVideo(t *testing.T, root, rel string) {
	t.Helper()
	p := filepath.Join(root, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, videoFixture, 0o644); err != nil {
		t.Fatal(err)
	}
}

func readVideoCSV(t *testing.T, path string) [][]string {
	t.Helper()
	f, err := os.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	return rows
}
