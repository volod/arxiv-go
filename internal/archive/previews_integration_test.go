package archive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

func previewFixture(t *testing.T) (Config, SplitConfig, roots, string) {
	t.Helper()
	r := newRoots(t)
	ffmpeg := tooltest.LookPath(t, "ffmpeg")
	ffprobe := tooltest.LookPath(t, "ffprobe")
	src := filepath.Join(r.archive, "clip.mp4")
	cmd := exec.CommandContext(context.Background(), ffmpeg, "-hide_banner", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=2",
		"-f", "lavfi", "-i", "sine=frequency=440:duration=2",
		"-c:v", "mpeg4", "-q:v", "5", "-c:a", "aac", src)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate source: %v: %s", err, out)
	}
	cfg, c := splitConfig(r, "copy")
	c.Scan.LargeThreshold = 1 << 30
	c.Preview = media.DefaultPreviewOptions()
	c.Preview.SampleMode, c.Preview.ImageMode = "start", "start"
	c.Preview.SampleDuration = 1_000_000_000
	c.Tools = media.Toolset{media.FFmpeg: {Path: ffmpeg}, media.FFprobe: {Path: ffprobe}}
	return cfg, c, r, src
}

func TestSplitPreviewAndRestoreLive(t *testing.T) {
	cfg, c, r, src := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	rows := readVideoCSV(t, filepath.Join(r.archive, scanner.VideoRegistryName))
	if len(rows) != 2 || !strings.Contains(rows[1][11], "clip-smpl01.mp4") || !strings.Contains(rows[1][11], "clip-img01.png") {
		t.Fatalf("registry previews: %v", rows)
	}
	stub := filepath.Join(r.archive, "clip.mp4.md")
	data := mustRead(t, stub)
	if !bytes.Contains(data, []byte("[clip-smpl01.mp4]")) || !bytes.Contains(data, []byte("![clip-img01.png]")) {
		t.Fatalf("stub previews: %s", data)
	}
	sample := filepath.Join(r.archive, "clip-smpl01.mp4")
	image := filepath.Join(r.archive, "clip-img01.png")
	before, _ := os.Stat(sample)
	// A new split scan must exclude the recorded sample rather than moving it as a video.
	cfg2, c2 := splitConfig(r, "copy")
	c2.Preview, c2.Tools = c.Preview, c.Tools
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("rerun: %+v", got)
	}
	if !exists(sample) || !exists(image) || exists(filepath.Join(r.video, "clip-smpl01.mp4")) {
		t.Fatal("rerun moved or removed preview")
	}
	after, _ := os.Stat(sample)
	if !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("rerun regenerated existing preview")
	}
	if err := os.WriteFile(filepath.Join(r.archive, "clip-img99.png"), []byte("unowned"), 0o644); err != nil {
		t.Fatal(err)
	}
	rcfg, rc := restoreConfig(r, "copy")
	rc.DeletePreviews = true
	if got := runRestore(t, rcfg, rc); got.Status != StatusCompleted {
		t.Fatalf("restore: %+v", got)
	}
	if !exists(src) || exists(sample) || exists(image) || !exists(filepath.Join(r.archive, "clip-img99.png")) {
		t.Fatal("restore preview cleanup did not preserve unowned file")
	}
}

func TestPreviewFailureAndCrashResumeLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	c.Tools[media.FFmpeg] = media.Found{Path: "/bin/false"}
	if got := runSplit(t, cfg, c); got.Status != StatusPartial {
		t.Fatalf("ffmpeg failure: %+v", got)
	}
	if !exists(filepath.Join(r.video, "clip.mp4")) || exists(filepath.Join(r.archive, "clip-smpl01.mp4")) {
		t.Fatal("failed preview affected committed move")
	}
	if !bytes.Contains(mustRead(t, filepath.Join(r.archive, scanner.VideoSummaryName)), []byte("## Preview failures")) {
		t.Fatal("preview failure missing from summary")
	}
	// The next run catches up only the missing previews of the already moved video.
	cfg2, c2 := splitConfig(r, "copy")
	c2.Preview = c.Preview
	c2.Tools = media.Toolset{media.FFmpeg: {Path: tooltest.LookPath(t, "ffmpeg")}, media.FFprobe: {Path: tooltest.LookPath(t, "ffprobe")}}
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("catch-up: %+v", got)
	}
	if !exists(filepath.Join(r.archive, "clip-smpl01.mp4")) {
		idx, _ := readPreviewIndex(r.archive)
		t.Fatalf("missing catch-up preview; index=%+v rows=%v", idx, readVideoCSV(t, filepath.Join(r.archive, scanner.VideoRegistryName)))
	}

	cfg3, c3, r3, _ := previewFixture(t)
	crashed := false
	cfg3.Crash = func(point string) error {
		if point == "wal:preview_begin" && !crashed {
			crashed = true
			return errors.New("injected preview crash")
		}
		return nil
	}
	attachRecoverer(&cfg3, c3, cfg3.Crash)
	if got := runSplit(t, cfg3, c3); got.Status != StatusFailed {
		t.Fatalf("crash: %+v", got)
	}
	if !exists(filepath.Join(r3.video, "clip.mp4")) {
		t.Fatal("crash rolled back video")
	}
	cfg3.Crash = nil
	attachRecoverer(&cfg3, c3, nil)
	if got := runSplit(t, cfg3, c3); got.Status != StatusCompleted {
		t.Fatalf("resume: %+v", got)
	}
	if !exists(filepath.Join(r3.archive, "clip-img01.png")) {
		t.Fatal("resume missed image")
	}
	recs, err := state.ReadWALRecords(filepath.Join(r3.archive, ".arxgo", "runs", stateCurrentRun(t, r3.archive), "wal.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	var commits int
	for _, rec := range recs {
		if rec.Step == state.StepCommit {
			commits++
		}
	}
	if commits != 1 {
		t.Errorf("resumed move count = %d", commits)
	}
}

func TestRestoreKeepsChangedPreviewLive(t *testing.T) {
	cfg, c, r, src := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	preview := filepath.Join(r.archive, "clip-img01.png")
	before := mustRead(t, preview)
	if err := os.WriteFile(preview, append(before, 'x'), 0o644); err != nil {
		t.Fatal(err)
	}
	rcfg, rc := restoreConfig(r, "copy")
	rc.DeletePreviews = true
	if got := runRestore(t, rcfg, rc); got.Status != StatusPartial {
		t.Fatalf("changed preview restore: %+v", got)
	}
	if !exists(src) || !bytes.Equal(mustRead(t, preview), append(before, 'x')) {
		t.Fatal("restore deleted or altered changed preview")
	}
}

func TestCorruptVideoRegistryStopsBeforeMove(t *testing.T) {
	r, src, dst := splitFixture(t)
	if err := os.WriteFile(filepath.Join(r.archive, scanner.VideoRegistryName), []byte("broken\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "copy")
	got := runSplit(t, cfg, c)
	if got.Status != StatusNeedsOperator || !strings.Contains(got.Err.Error(), "restore a valid registry backup") {
		t.Fatalf("corrupt registry: %+v", got)
	}
	if !exists(src) || exists(dst) {
		t.Fatal("corrupt registry allowed a move")
	}
}

func TestPreviewNamesAcrossVideosLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	mov := filepath.Join(r.archive, "clip.mov")
	cmd := exec.CommandContext(context.Background(), c.Tools.Path(media.FFmpeg), "-hide_banner", "-v", "error", "-y",
		"-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=2",
		"-c:v", "mpeg4", "-q:v", "5", mov)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate MOV: %v: %s", err, out)
	}
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	if !exists(filepath.Join(r.archive, "clip-img01.png")) || !exists(filepath.Join(r.archive, "clip-arxgo-img01.png")) {
		t.Fatal("same-stem preview collision was not resolved")
	}
}

func TestPendingPreviewRejectsInvalidFinalLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	fired := false
	cfg.Crash = func(point string) error {
		if point == "wal:preview_begin" && !fired {
			fired = true
			return errors.New("injected crash")
		}
		return nil
	}
	attachRecoverer(&cfg, c, cfg.Crash)
	if got := runSplit(t, cfg, c); got.Status != StatusFailed {
		t.Fatalf("crash: %+v", got)
	}
	path := filepath.Join(r.archive, "clip-smpl01.mp4")
	if err := os.WriteFile(path, []byte("unrelated bytes"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg.Crash = nil
	attachRecoverer(&cfg, c, nil)
	if got := runSplit(t, cfg, c); got.Status != StatusPartial {
		t.Fatalf("invalid final: %+v", got)
	}
	if string(mustRead(t, path)) != "unrelated bytes" {
		t.Fatal("unrelated final file overwritten")
	}
}

func stateCurrentRun(t *testing.T, root string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(root, ".arxgo", "current"))
	if err != nil {
		t.Fatal(err)
	}
	return strings.TrimSpace(string(data))
}
