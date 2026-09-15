package report

import (
	"bytes"
	"flag"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/media"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

func descriptionInput(url bool, mediaMode bool, rel string) DescriptionInput {
	in := DescriptionInput{
		RelPath:  rel,
		FileSize: 734003200,
		FileMIME: "video/mp4",
		Modified: time.Date(2024, 5, 1, 10, 22, 3, 0, time.UTC),
		MovedAt:  time.Date(2026, 9, 13, 10, 17, 42, 0, time.UTC),
		MovedTo:  "/mnt/nas/video/" + rel,
	}
	if url {
		in.URL = ComposeURL("https://storage.example.com/video", rel)
	}
	if mediaMode {
		in.SHA256 = strings.Repeat("ab", 32)
		in.Media = &media.MediaInfo{
			DurationS: 1834.12, Width: 1920, Height: 1080, CreationTime: "2024-05-01T09:51:00Z",
			VideoCodec: "h264", AudioCodec: "aac", FrameRate: "25/1",
		}
	}
	return in
}

func TestRenderDescriptionGoldens(t *testing.T) {
	cases := []struct {
		file string
		in   DescriptionInput
	}{
		{"description-file.md", descriptionInput(false, false, "projects/2024/interview.mp4")},
		{"description-media-url.md", descriptionInput(true, true, "projects/2024/interview.mp4")},
		{"description-unicode.md", descriptionInput(true, false, " deep/clip-\u03b1 \"and\" space.mp4")},
	}
	dir := filepath.Join("..", "..", "test", "testdata", "report")
	for _, tc := range cases {
		t.Run(tc.file, func(t *testing.T) {
			got := RenderDescription(tc.in)
			path := filepath.Join(dir, tc.file)
			if *updateGolden {
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
			if bytes.Contains(got, []byte("\n\n")) {
				t.Errorf("blank line in description:\n%s", got)
			}
			description, err := ParseDescription(bytes.NewReader(got))
			if err != nil || description[DescriptionMarker] != tc.in.RelPath || description["file_size"] != "734003200 (700.0 MiB)" {
				t.Fatalf("parsed = %v, %v", description, err)
			}
			if occ, err := inspectBytes(t, got, tc.in.RelPath); err != nil || occ != DescriptionOwned {
				t.Errorf("rendered description not owned: %v, %v", occ, err)
			}
		})
	}
}

func inspectBytes(t *testing.T, data []byte, rel string) (DescriptionOccupancy, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "description.md")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return InspectDescription(p, rel)
}

func TestParseDescriptionStopsAtFieldBlock(t *testing.T) {
	description, err := ParseDescription(strings.NewReader("arxgo: a.mp4\nvideo: 0:05 | 2x2\n- [a-smpl01.mp4](a-smpl01.mp4)\nnote: not a field\n"))
	if err != nil || len(description) != 2 || description["video"] != "0:05 | 2x2" {
		t.Fatalf("description = %v, %v", description, err)
	}
	for _, bad := range []string{"", "# a.mp4\n", "file_size: 1\narxgo: a.mp4\n", "arxgo: \"unterminated\n"} {
		if _, err := ParseDescription(strings.NewReader(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestChooseDescriptionPathCollisionAndTruncation(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	p, err := ChooseDescriptionPath(src, "clip.mp4")
	if err != nil || p != src+".md" {
		t.Fatalf("primary: %s, %v", p, err)
	}
	if err := os.WriteFile(src+".md", []byte("human"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = ChooseDescriptionPath(src, "clip.mp4")
	if err != nil || p != src+".arxgo.md" {
		t.Fatalf("arxgo fallback: %s, %v", p, err)
	}
	if err := os.WriteFile(src+".arxgo.md", []byte("also human"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = ChooseDescriptionPath(src, "clip.mp4")
	if err != nil || filepath.Base(p) != "clip-1.mp4.md" {
		t.Fatalf("indexed: %s, %v", p, err)
	}
	owned := []byte("arxgo: clip.mp4\nfile_size: 1 (1 B)\n")
	if err := os.WriteFile(p, owned, 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := ChooseDescriptionPath(src, "clip.mp4")
	if err != nil || again != p {
		t.Fatalf("reuse owned indexed: %s, %v", again, err)
	}

	oldName, oldPath := nameMax, pathMax
	nameMax, pathMax = 20, 4096
	t.Cleanup(func() { nameMax, pathMax = oldName, oldPath })
	long := filepath.Join(dir, "verylongvideoname.mp4")
	p, err = ChooseDescriptionPath(long, "verylongvideoname.mp4")
	if err != nil {
		t.Fatal(err)
	}
	base := filepath.Base(p)
	if pathUnitLen(base) > 20 || !strings.HasSuffix(base, ".mp4.md") {
		t.Fatalf("truncated name %q len %d", base, pathUnitLen(base))
	}
}

func TestInspectDescriptionTreatsNonFilesAsForeign(t *testing.T) {
	dir := t.TempDir()
	owned := []byte("arxgo: clip.mp4\n")
	ownedPath := filepath.Join(dir, "owned.md")
	if err := os.WriteFile(ownedPath, owned, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(dir, "dir.md"), 0o755); err != nil {
		t.Fatal(err)
	}
	long := append(bytes.Repeat([]byte("x"), descriptionHeaderLimit), "\narxgo: clip.mp4\n"...)
	if err := os.WriteFile(filepath.Join(dir, "long.md"), long, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "other.md"), []byte("arxgo: other.mp4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "notes.md"), []byte("# notes\narxgo: clip.mp4\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cases := map[string]DescriptionOccupancy{"missing.md": DescriptionAbsent, "owned.md": DescriptionOwned, "dir.md": DescriptionForeign,
		"long.md": DescriptionForeign, "other.md": DescriptionForeign, "notes.md": DescriptionForeign}
	if err := os.Symlink(ownedPath, filepath.Join(dir, "link.md")); err == nil {
		cases["link.md"] = DescriptionForeign
	}
	if err := os.WriteFile(filepath.Join(dir, "locked.md"), owned, 0o000); err != nil {
		t.Fatal(err)
	}
	if f, err := os.Open(filepath.Join(dir, "locked.md")); err != nil {
		cases["locked.md"] = DescriptionForeign // unreadable for this user (not root)
	} else {
		f.Close()
	}
	for name, want := range cases {
		got, err := InspectDescription(filepath.Join(dir, name), "clip.mp4")
		if err != nil || got != want {
			t.Errorf("%s: occupancy %v, %v; want %v", name, got, err, want)
		}
	}
	p, err := ChooseDescriptionPath(filepath.Join(dir, "dir"), "clip.mp4")
	if err != nil || filepath.Base(p) != "dir.arxgo.md" {
		t.Fatalf("directory at the primary description path: %s, %v", p, err)
	}
}

func TestMediaLine(t *testing.T) {
	for rate, want := range map[string]string{
		"25/1": "1:05 | 1920x1080 | h264 + aac | 25 fps", "30000/1001": "1:05 | 1920x1080 | h264 + aac | 29.97 fps",
		"12690000/422899": "1:05 | 1920x1080 | h264 + aac | 30.01 fps", "": "1:05 | 1920x1080 | h264 + aac",
	} {
		m := &media.MediaInfo{DurationS: 65.4, Width: 1920, Height: 1080, VideoCodec: "h264", AudioCodec: "aac", FrameRate: rate}
		if got := MediaLine(m); got != want {
			t.Errorf("%q: %q, want %q", rate, got, want)
		}
	}
}

func TestReplacePreviewLinks(t *testing.T) {
	description := RenderDescription(descriptionInput(false, false, "clip.mp4"))
	withNotes := append(append([]byte(nil), description...), "Operator note.\n"...)
	frame := []PreviewLink{{Name: "clip-img01.png", URL: "clip-img01.png"}}
	sample := []PreviewLink{{Name: "clip-smpl01.mp4", URL: "clip-smpl01.mp4"}}

	first := ReplacePreviewLinks(withNotes, frame)
	if want := string(description) + "- ![clip-img01.png](clip-img01.png)\nOperator note.\n"; string(first) != want {
		t.Fatalf("added links:\n%s\nwant\n%s", first, want)
	}
	second := ReplacePreviewLinks(first, sample)
	if want := string(description) + "- [clip-smpl01.mp4](clip-smpl01.mp4)\nOperator note.\n"; string(second) != want {
		t.Fatalf("replaced links:\n%s", second)
	}
	if again := ReplacePreviewLinks(second, sample); !bytes.Equal(again, second) {
		t.Fatal("unchanged links rewrote the description")
	}
	if removed := ReplacePreviewLinks(second, nil); !bytes.Equal(removed, withNotes) {
		t.Fatalf("removing links changed other text:\n%q", removed)
	}
	if st, err := ParseDescription(bytes.NewReader(second)); err != nil || st[DescriptionMarker] != "clip.mp4" {
		t.Fatalf("description with links = %v, %v", st, err)
	}
}

func TestChooseTextSidecarPathCollisionAndReuse(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fixture.CATPart")
	rel := "fixture.CATPart"
	p, err := ChooseTextSidecarPath(src, rel)
	if err != nil || p != src+".text.md" {
		t.Fatalf("primary: %s, %v", p, err)
	}
	if err := os.WriteFile(src+".text.md", []byte("operator notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = ChooseTextSidecarPath(src, rel)
	if err != nil || p != src+".arxgo.text.md" {
		t.Fatalf("arxgo fallback: %s, %v", p, err)
	}
	if err := os.WriteFile(src+".arxgo.text.md", []byte("also human\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	p, err = ChooseTextSidecarPath(src, rel)
	if err != nil || filepath.Base(p) != "fixture-1.CATPart.text.md" {
		t.Fatalf("indexed: %s, %v", p, err)
	}
	owned := []byte("arxgo-text: fixture.CATPart\nextracted_at: 2026-09-15T12:00:00Z\ntruncated: false\n")
	if err := os.WriteFile(p, owned, 0o644); err != nil {
		t.Fatal(err)
	}
	again, err := ChooseTextSidecarPath(src, rel)
	if err != nil || again != p {
		t.Fatalf("reuse owned indexed: %s, %v", again, err)
	}
	if occ, err := InspectTextSidecar(p, rel); err != nil || occ != DescriptionOwned {
		t.Fatalf("inspect owned = %v, %v", occ, err)
	}
	if occ, err := InspectTextSidecar(src+".text.md", rel); err != nil || occ != DescriptionForeign {
		t.Fatalf("inspect foreign = %v, %v", occ, err)
	}
}
