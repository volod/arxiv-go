//go:build integration

package integration

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

// TestStage2Previews extends the binary-level split/restore proof with real generated media.
func TestStage2Previews(t *testing.T) {
	ffmpeg := tooltest.LookPath(t, "ffmpeg")
	_ = tooltest.LookPath(t, "ffprobe")
	bin := buildArxgo(t)
	work := t.TempDir()
	bin.work = work
	archive, video := filepath.Join(work, "archive"), filepath.Join(work, "video")
	if err := os.MkdirAll(archive, 0o755); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(archive, "clip.mp4")
	cmd := exec.CommandContext(context.Background(), ffmpeg, "-hide_banner", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=2",
		"-c:v", "mpeg4", "-q:v", "5", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate clip: %v: %s", err, out)
	}
	base := []string{"--archive", archive, "--video-archive", video, "--min-free", "0", "--log-level", "warn"}
	split := append([]string{"split", "--sample", "start", "--image", "start", "--sample-duration", "1s"}, base...)
	if r := bin.run(t, split...); r.code != 0 {
		t.Fatalf("split exit %d: %s", r.code, r.stderr)
	}
	for _, p := range []string{"clip-smpl01.mp4", "clip-img01.png"} {
		if _, err := os.Stat(filepath.Join(archive, p)); err != nil {
			t.Fatal(err)
		}
	}
	stub, err := os.ReadFile(src + ".md")
	if err != nil || !bytes.Contains(stub, []byte("![clip-img01.png]")) {
		t.Fatalf("stub: %v: %s", err, stub)
	}
	rows, err := report.LoadVideoFile(filepath.Join(archive, "arxgo-videos.csv"))
	if err != nil || len(rows) != 1 || !strings.Contains(rows[0].Previews, "clip-smpl01.mp4") {
		t.Fatalf("registry rows = %+v, %v", rows, err)
	}
	if r := bin.run(t, split...); r.code != 0 {
		t.Fatalf("rerun exit %d: %s", r.code, r.stderr)
	}
	if _, err := os.Stat(filepath.Join(video, "clip-smpl01.mp4")); !os.IsNotExist(err) {
		t.Fatalf("preview became split candidate: %v", err)
	}
	restore := append([]string{"restore", "--previews", "delete"}, base...)
	if r := bin.run(t, restore...); r.code != 0 {
		t.Fatalf("restore exit %d: %s", r.code, r.stderr)
	}
	if _, err := os.Stat(src); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{"clip-smpl01.mp4", "clip-img01.png"} {
		if _, err := os.Stat(filepath.Join(archive, p)); !os.IsNotExist(err) {
			t.Fatalf("preview retained: %s: %v", p, err)
		}
	}
}
