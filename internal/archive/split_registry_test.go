package archive

import (
	"encoding/csv"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/test/fixtures/testmp4"
)

func TestSplitWritesVideoRegistryAndSummaryToBothRoots(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	c.BaseURL = "https://cdn.example.com/v"
	if ms, ok := c.Stubs.(*MarkdownStub); ok {
		ms.cfg.BaseURL = c.BaseURL
	}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	checkSplit(t, src, dst)
	stub := string(mustRead(t, src+".md"))
	if !strings.Contains(stub, "arxgo_stub: 1") || !strings.Contains(stub, "https://cdn.example.com/v/nested/clip.mp4") {
		t.Fatalf("stub:\n%s", stub)
	}
	for _, root := range []string{r.archive, r.video} {
		csvPath := filepath.Join(root, scanner.VideoRegistryName)
		mdPath := filepath.Join(root, scanner.VideoSummaryName)
		rows := readVideoCSV(t, csvPath)
		if len(rows) != 2 || rows[0][0] != "rel_path" || rows[1][0] != "nested/clip.mp4" {
			t.Fatalf("%s rows = %q", csvPath, rows)
		}
		if rows[1][8] != "moved" || rows[1][10] != "https://cdn.example.com/v/nested/clip.mp4" {
			t.Fatalf("row = %q", rows[1])
		}
		sum := string(mustRead(t, mdPath))
		if !strings.Contains(sum, "# Video archive summary") || !strings.Contains(sum, "nested/clip.mp4") {
			t.Fatalf("summary:\n%s", sum)
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
	if rows[1][8] != "moved" || rows[2][8] != "moved" {
		t.Fatalf("status = %q", rows)
	}
}

func TestSplitMediaModeStubHasDuration(t *testing.T) {
	// A QuickTime movie carries a second (data) handler box; it must still be a moved video.
	for name, quickTime := range map[string]bool{"interview.mp4": false, "camera.mov": true} {
		t.Run(name, func(t *testing.T) {
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
			c.Scan.Metadata = "media"
			if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("split = %+v", res)
			}
			if exists(src) || !exists(filepath.Join(r.video, name)) {
				t.Fatal("video not moved")
			}
			stub := string(mustRead(t, src+".md"))
			if !strings.Contains(stub, "0:05 | 1920x1080 | h264 + aac | 25 fps") {
				t.Fatalf("media line:\n%s", stub)
			}
		})
	}
}

func TestSplitForeignThenIndexedStub(t *testing.T) {
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
	if !strings.Contains(string(mustRead(t, indexed)), "arxgo_stub: 1") {
		t.Fatal("indexed stub missing")
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
