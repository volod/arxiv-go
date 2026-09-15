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
	"time"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

// previewFixture generates a two-second clip.mp4 in a new archive and returns a split
// configuration with start sample and image previews.
func previewFixture(t *testing.T) (Config, SplitConfig, roots, string) {
	t.Helper()
	r := newRoots(t)
	ffmpeg := tooltest.LookPath(t, "ffmpeg")
	ffprobe := tooltest.LookPath(t, "ffprobe")
	src := filepath.Join(r.archive, "clip.mp4")
	generateClip(t, ffmpeg, src, "-f", "lavfi", "-i", "sine=frequency=440:duration=2", "-c:v", "mpeg4", "-q:v", "5", "-c:a", "aac")
	cfg, c := splitConfig(r, "copy")
	c.Scan.LargeThreshold = 1 << 30
	c.Preview = media.DefaultPreviewOptions()
	c.Preview.SampleMode, c.Preview.ImageMode = "start", "start"
	c.Preview.SampleDuration = time.Second
	c.Tools = media.Toolset{media.FFmpeg: {Path: ffmpeg}, media.FFprobe: {Path: ffprobe}}
	return cfg, c, r, src
}

func generateClip(t *testing.T, ffmpeg, path string, args ...string) {
	t.Helper()
	full := append([]string{"-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=10:duration=2"}, args...)
	cmd := exec.CommandContext(context.Background(), ffmpeg, append(full, path)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("generate %s: %v: %s", path, err, out)
	}
}

// nextSplit is a fresh process configuration for the same roots and preview settings.
func nextSplit(r roots, c SplitConfig) (Config, SplitConfig) {
	cfg, next := splitConfig(r, "copy")
	next.Preview, next.Tools, next.Scan.VideoExtensions = c.Preview, c.Tools, c.Scan.VideoExtensions
	next.Scan.LargeThreshold = c.Scan.LargeThreshold
	return cfg, next
}

func TestSplitPreviewsRegistryDescriptionAndRerunLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	rep := readReport(t, r.archive, currentRunID(t, r.archive))
	if rep.Counters.PreviewsDone != 2 || rep.Counters.VideosDone != 1 {
		t.Fatalf("counters = %+v", rep.Counters)
	}
	rows := readVideoCSV(t, filepath.Join(r.archive, scanner.VideoRegistryName))
	if len(rows) != 2 || rows[1][10] != "clip-img01.png;clip-smpl01.mp4" {
		t.Fatalf("registry previews: %v", rows)
	}
	description := mustRead(t, filepath.Join(r.archive, "clip.mp4.md"))
	if !bytes.Contains(description, []byte("- [clip-smpl01.mp4](clip-smpl01.mp4)")) || !bytes.Contains(description, []byte("- ![clip-img01.png](clip-img01.png)")) {
		t.Fatalf("description previews: %s", description)
	}
	sample := filepath.Join(r.archive, "clip-smpl01.mp4")
	before, _ := os.Stat(sample)
	// A new split scan excludes the recorded sample instead of moving it as a video, and a rerun
	// regenerates nothing.
	cfg2, c2 := nextSplit(r, c)
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("rerun: %+v", got)
	}
	after, _ := os.Stat(sample)
	if exists(filepath.Join(r.video, "clip-smpl01.mp4")) || !before.ModTime().Equal(after.ModTime()) {
		t.Fatal("rerun moved or regenerated the preview")
	}
	if !bytes.Equal(description, mustRead(t, filepath.Join(r.archive, "clip.mp4.md"))) {
		t.Fatal("rerun rewrote the description")
	}
}

// Containers that cannot hold the sample codecs get an .mp4 sample chosen at planning, so a rerun
// finds it (regression: the fallback name was unknown to the next run and failed it).
func TestSubstitutedSampleContainerRerunLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	ffmpeg := c.Tools.Path(media.FFmpeg)
	generateClip(t, ffmpeg, filepath.Join(r.archive, "dvd.mpg"),
		"-f", "lavfi", "-i", "sine=duration=2", "-ac", "2", "-c:v", "mpeg2video", "-c:a", "mp2")
	generateClip(t, ffmpeg, filepath.Join(r.archive, "game.bik"), "-c:v", "mpeg4", "-f", "mp4")
	c.Scan.VideoExtensions = []string{"bik"}
	c.Preview.ImageMode = "none"
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v %v", got, readReport(t, r.archive, got.RunID).Issues)
	}
	for _, name := range []string{"dvd-smpl01.mp4", "game-smpl01.mp4"} {
		if !exists(filepath.Join(r.archive, name)) {
			t.Fatalf("missing %s", name)
		}
	}
	cfg2, c2 := nextSplit(r, c)
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("rerun: %+v %v", got, readReport(t, r.archive, got.RunID).Issues)
	}
}

func TestPreviewFailureIsCountedAndCaughtUpLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	ffmpeg := c.Tools[media.FFmpeg]
	c.Tools[media.FFmpeg] = media.Found{Path: "/bin/false"}
	got := runSplit(t, cfg, c)
	if got.Status != StatusPartial {
		t.Fatalf("ffmpeg failure: %+v", got)
	}
	if rep := readReport(t, r.archive, got.RunID); rep.Counters.PreviewsFailed == 0 || rep.Counters.VideosFailed != 0 || rep.Counters.VideosDone != 1 {
		t.Fatalf("counters = %+v", rep.Counters)
	}
	if !exists(filepath.Join(r.video, "clip.mp4")) || exists(filepath.Join(r.archive, "clip-smpl01.mp4")) {
		t.Fatal("failed preview affected the committed move")
	}
	if exists(filepath.Join(r.archive, "arxgo-videos.md")) {
		t.Fatal("unexpected Markdown summary")
	}
	cfg2, c2 := nextSplit(r, c)
	c2.Tools = media.Toolset{media.FFmpeg: ffmpeg, media.FFprobe: c.Tools[media.FFprobe]}
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("catch-up: %+v", got)
	}
	if !exists(filepath.Join(r.archive, "clip-smpl01.mp4")) || !exists(filepath.Join(r.archive, "clip-img01.png")) {
		t.Fatal("catch-up did not generate the missing previews")
	}
}

func TestPreviewCrashResumesOnlyMissingPreviewsLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	crashed := false
	crash := func(point string) error {
		if point == "wal:preview_begin" && !crashed {
			crashed = true
			return errors.New("injected preview crash")
		}
		return nil
	}
	cfg.Crash = crash
	attachRecoverer(&cfg, c, crash)
	if got := runSplit(t, cfg, c); got.Status != StatusFailed {
		t.Fatalf("crash: %+v", got)
	}
	if !exists(filepath.Join(r.video, "clip.mp4")) {
		t.Fatal("crash rolled back the video")
	}
	cfg.Crash = nil
	attachRecoverer(&cfg, c, nil)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("resume: %+v", got)
	}
	if !exists(filepath.Join(r.archive, "clip-img01.png")) || !exists(filepath.Join(r.archive, "clip-smpl01.mp4")) {
		t.Fatal("resume missed a preview")
	}
	recs, err := state.ReadWALRecords(filepath.Join(state.StateDir(r.archive), "runs", currentRunID(t, r.archive), state.WALFile))
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

// Cancellation during a preview leaves its event unfinished instead of recording a failure.
func TestCanceledPreviewIsRetriedNotFailedLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cfg.Crash = func(point string) error {
		if point == "wal:preview_begin" {
			cancel()
		}
		return nil
	}
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	res := s.Finish(ctx, Split(ctx, s, c))
	if res.Status != StatusInterrupted {
		t.Fatalf("canceled split: %+v", res)
	}
	recs, err := state.ReadWALRecords(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.WALFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if rec.Step == state.StepPreviewFailed {
			t.Fatalf("cancellation recorded a failed preview: %+v", rec)
		}
	}
	cfg.Crash = nil
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted || !exists(filepath.Join(r.archive, "clip-smpl01.mp4")) {
		t.Fatalf("resume after cancel: %+v", got)
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

func TestPreviewNamesAcrossVideosLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	generateClip(t, c.Tools.Path(media.FFmpeg), filepath.Join(r.archive, "clip.mov"), "-c:v", "mpeg4", "-q:v", "5")
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	if !exists(filepath.Join(r.archive, "clip-img01.png")) || !exists(filepath.Join(r.archive, "clip-arxgo-img01.png")) {
		t.Fatal("same-stem preview collision was not resolved")
	}
}

// Registry paths come from the WAL relative to the root each run recorded, so renaming the archive
// root keeps preview and description paths local (regression: exit 5 on every later run).
func TestRelocatedArchiveKeepsRegistryPathsLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	moved := r.archive + "-moved"
	if err := os.Rename(r.archive, moved); err != nil {
		t.Fatal(err)
	}
	r.archive = moved
	cfg2, c2 := nextSplit(r, c)
	if got := runSplit(t, cfg2, c2); got.Status != StatusCompleted {
		t.Fatalf("split after relocation: %+v", got)
	}
	rows, err := report.LoadVideoFile(filepath.Join(r.archive, scanner.VideoRegistryName))
	if err != nil || len(rows) != 1 || rows[0].DescriptionRelPath != "clip.mp4.md" || !strings.Contains(rows[0].Previews, "clip-img01.png") {
		t.Fatalf("registry after relocation: %+v, %v", rows, err)
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
