package report

import (
	"bytes"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/media"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func stubInput(url bool, mediaMode bool, rel string) StubInput {
	in := StubInput{
		RelPath:          rel,
		VideoArchivePath: "/mnt/nas/video/" + rel,
		FileSize:         734003200,
		FileMIME:         "video/mp4",
		RunID:            "20260913T101500Z-1a2b3c4d",
		MovedAt:          time.Date(2026, 9, 13, 10, 17, 42, 0, time.UTC),
		RelLink:          EscapePath("../../../../mnt/nas/video/" + rel),
	}
	if url {
		in.URL = ComposeURL("https://storage.example.com/video", rel)
	}
	if mediaMode {
		in.Media = &media.MediaInfo{
			DurationS: 1834.12, Width: 1920, Height: 1080,
			VideoCodec: "h264", AudioCodec: "aac", FrameRate: "25/1",
		}
	}
	return in
}

func TestRenderStubGoldens(t *testing.T) {
	cases := []struct {
		name, file string
		in         StubInput
	}{
		{"file-no-url", "stub-file.md", stubInput(false, false, "projects/2024/interview.mp4")},
		{"file-url", "stub-file-url.md", stubInput(true, false, "projects/2024/interview.mp4")},
		{"media-no-url", "stub-media.md", stubInput(false, true, "projects/2024/interview.mp4")},
		{"media-url", "stub-media-url.md", stubInput(true, true, "projects/2024/interview.mp4")},
		{"unicode-spaces", "stub-unicode.md", stubInput(true, false, "deep/clip-\u03b1 and space.mp4")},
	}
	dir := filepath.Join("..", "..", "test", "testdata", "report")
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got, err := RenderStub(tc.in)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(got, []byte("---\narxgo_stub: 1\n")) {
				t.Fatalf("missing marker:\n%s", got)
			}
			path := filepath.Join(dir, tc.file)
			if *updateGolden {
				if err := os.MkdirAll(dir, 0o755); err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, got, 0o644); err != nil {
					t.Fatal(err)
				}
			}
			want, err := os.ReadFile(path)
			if err != nil {
				t.Fatalf("golden %s: %v (run go test -update)", path, err)
			}
			if !bytes.Equal(got, want) {
				t.Errorf("golden %s\n--- got\n%s\n--- want\n%s", path, got, want)
			}
			fm, err := ParseFrontMatter(bytes.NewReader(got))
			if err != nil {
				t.Fatal(err)
			}
			if fm["rel_path"] != tc.in.RelPath || fm[StubMarker] != StubMarkerValue {
				t.Errorf("front matter = %v", fm)
			}
		})
	}
}

func TestChooseStubPathCollisionAndTruncation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	p, err := ChooseStubPath(src, "clip.mp4")
	if err != nil || p != src+".md" {
		t.Fatalf("primary: %s, %v", p, err)
	}
	if err := os.WriteFile(src+".md", []byte("human"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = ChooseStubPath(src, "clip.mp4")
	if err != nil || p != src+".arxgo.md" {
		t.Fatalf("arxgo fallback: %s, %v", p, err)
	}
	if err := os.WriteFile(src+".arxgo.md", []byte("also human"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = ChooseStubPath(src, "clip.mp4")
	if err != nil || filepath.Base(p) != "clip-1.mp4.md" {
		t.Fatalf("indexed: %s, %v", p, err)
	}
	owned := []byte("---\narxgo_stub: 1\nrel_path: clip.mp4\n---\n")
	if err := os.WriteFile(p, owned, 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := ChooseStubPath(src, "clip.mp4")
	if err != nil || again != p {
		t.Fatalf("reuse owned indexed: %s, %v", again, err)
	}

	oldName, oldPath := nameMax, pathMax
	nameMax, pathMax = 20, 4096
	t.Cleanup(func() { nameMax, pathMax = oldName, oldPath })
	long := filepath.Join(dir, "verylongvideoname.mp4")
	p, err = ChooseStubPath(long, "verylongvideoname.mp4")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(p)
	if pathUnitLen(base) > 20 || !strings.HasSuffix(base, ".mp4.md") {
		t.Fatalf("truncated name %q len %d", base, pathUnitLen(base))
	}
}

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

func TestSummaryBandsAndTop100(t *testing.T) {
	rows := make([]VideoRow, 0, 120)
	for i := 0; i < 105; i++ {
		w, h := 1920, 1080
		if i < 3 {
			w, h = 320, 240
		} else if i < 6 {
			w, h = 640, 480
		} else if i >= 100 {
			w, h = 3840, 2160
		}
		rows = append(rows, VideoRow{
			RelPath:  fmt.Sprintf("v/%03d.mp4", i),
			FileName: "c.mp4", FileSize: int64(1000-i) * 1000, Status: StatusMoved,
			Metadata: Metadata{V: 1, Media: &media.MediaInfo{
				Container: "mp4", VideoCodec: "h264", Width: w, Height: h, DurationS: 10,
			}},
		})
	}
	rows = append(rows,
		VideoRow{RelPath: "skip.mp4", Status: StatusSkipped, FileName: "skip.mp4"},
		VideoRow{RelPath: "boom.mp4", Status: StatusConflict, FileName: "boom.mp4"},
	)
	b, err := RenderSummary(SummaryInput{
		Generated: time.Date(2026, 9, 13, 12, 0, 0, 0, time.UTC),
		Version:   "test", RunIDs: []string{"run-a"},
		Archive: "/archive", VideoArchive: "/video", Rows: rows,
	})
	if err != nil {
		t.Fatal(err)
	}
	s := string(b)
	for _, want := range []string{
		"Generated: 2026-09-13T12:00:00Z",
		"| <SD | 3 |",
		"| SD | 3 |",
		"| HD | 94 |",
		"| 4K+ | 5 |",
		"- skipped: skip.mp4",
		"- conflict: boom.mp4",
		"Videos: 105",
	} {
		if !strings.Contains(s, want) {
			t.Errorf("summary missing %q\n%s", want, s)
		}
	}
	if n := strings.Count(s, "\n| v/"); n != 100 {
		t.Errorf("top table rows = %d, want 100", n)
	}
}

func TestFrontMatterRoundTripQuoted(t *testing.T) {
	raw := FormatFrontMatter([][2]string{
		{"rel_path", `dir/a#b`},
		{"note", `say "hi"`},
	})
	fm, err := ParseFrontMatter(strings.NewReader(raw + "\nbody\n"))
	if err != nil {
		t.Fatal(err)
	}
	if fm["rel_path"] != `dir/a#b` || fm["note"] != `say "hi"` {
		t.Fatalf("%v", fm)
	}
}
