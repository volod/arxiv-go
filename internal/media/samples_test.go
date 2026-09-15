package media

import (
	"bytes"
	"context"
	"errors"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

func sampleTools(t *testing.T) (string, *Runner) {
	t.Helper()
	ffmpeg := tooltest.LookPath(t, "ffmpeg")
	ffprobe := tooltest.LookPath(t, "ffprobe")
	return ffmpeg, &Runner{Tools: Toolset{FFmpeg: {Path: ffmpeg}, FFprobe: {Path: ffprobe}}}
}

// sampleSource generates a testsrc2 clip, with a sine tone when audio is set, in a codec the
// container of ext holds.
func sampleSource(t *testing.T, ffmpeg, ext, size string, duration float64, audio bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source"+ext)
	args := []string{"-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc2=size=" + size + ":rate=10:duration=" + formatSeconds(duration)}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration="+formatSeconds(duration))
	}
	videoCodec, audioCodec := "mpeg4", "aac"
	switch ext {
	case ".webm":
		videoCodec, audioCodec = "libvpx-vp9", "libopus"
		args = append(args, "-deadline", "realtime", "-cpu-used", "6")
	case ".avi":
		audioCodec = "libmp3lame"
	case ".mpg", ".vob":
		// Mono MP2: FFmpeg before 8.1 rejected it in a series concat filter after input seeking.
		videoCodec, audioCodec = "mpeg2video", "mp2"
	case ".ogv":
		videoCodec, audioCodec = "libtheora", "libvorbis"
	}
	args = append(args, "-c:v", videoCodec)
	if audio {
		args = append(args, "-c:a", audioCodec)
	}
	cmd := exec.CommandContext(context.Background(), ffmpeg, append(args, path)...)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("create %s source: %v: %s", ext, err, out)
	}
	return path
}

func sampleJob(t *testing.T, r *Runner, source, mode string, length, every time.Duration, resolution string) PreviewJob {
	t.Helper()
	info := r.Probe(context.Background(), source, "")
	if info.Error != "" {
		t.Fatal(info.Error)
	}
	opts := DefaultPreviewOptions()
	opts.SampleMode, opts.SampleDuration, opts.SampleEvery, opts.SampleResolution = mode, length, every, resolution
	plan, err := PlanPreviews(*info, source, opts, nil)
	if err != nil || len(plan.Jobs) != 1 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	job := plan.Jobs[0]
	job.Output = filepath.Join(t.TempDir(), job.Output)
	return job
}

func generateSample(t *testing.T, r *Runner, source string, job PreviewJob, tolerance float64) *MediaInfo {
	t.Helper()
	if err := r.GenerateSample(context.Background(), source, job); err != nil {
		t.Fatal(err)
	}
	info := r.Probe(context.Background(), job.Output, "")
	if info.Error != "" {
		t.Fatal(info.Error)
	}
	if want := job.totalSeconds(); math.Abs(info.DurationS-want) > tolerance {
		t.Fatalf("duration %.3f, want %.3f", info.DurationS, want)
	}
	if info.Width != job.Size.Width || info.Height != job.Size.Height || info.HasAudio != job.HasAudio || info.VideoStreams == 0 {
		t.Fatalf("sample = %+v, job = %+v", info, job)
	}
	if _, err := os.Stat(PreviewPartPath(job.Output)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part remains: %v", err)
	}
	return info
}

func TestGenerateSampleLiveModes(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "256x144", 3.2, true)
	for _, mode := range []string{"start", "middle", "end", "series"} {
		t.Run(mode, func(t *testing.T) {
			job := sampleJob(t, r, source, mode, time.Second, time.Second, "sd")
			generateSample(t, r, source, job, 0.5*float64(len(job.Ranges)))
		})
	}
}

// Each container gets a two-fragment series clip (concat filter) in the planned container.
func TestGenerateSampleLiveContainers(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, tc := range []struct{ ext, output, container string }{
		{".mov", "source-smpl01.mov", "mov"},
		{".mkv", "source-smpl01.mkv", "matroska"},
		{".webm", "source-smpl01.webm", "webm"},
		{".m4v", "source-smpl01.m4v", "mp4"},
		{".3gp", "source-smpl01.3gp", "3gp"},
		{".avi", "source-smpl01.avi", "avi"},
		{".mpg", "source-smpl01.mp4", "mp4"},
		{".vob", "source-smpl01.mp4", "mp4"},
		{".ogv", "source-smpl01.mp4", "mp4"},
	} {
		t.Run(tc.ext, func(t *testing.T) {
			source := sampleSource(t, ffmpeg, tc.ext, "256x144", 2.2, true)
			job := sampleJob(t, r, source, "series", 500*time.Millisecond, time.Second, "sd")
			if filepath.Base(job.Output) != tc.output || len(job.Ranges) != 3 {
				t.Fatalf("job = %+v", job)
			}
			if got := generateSample(t, r, source, job, 1); got.Container != tc.container {
				t.Fatalf("container = %s, want %s", got.Container, tc.container)
			}
		})
	}
}

func TestGenerateSampleLiveClampAndShortNoAudio(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, size := range []string{"1280x720", "144x256"} {
		t.Run(size, func(t *testing.T) {
			source := sampleSource(t, ffmpeg, ".mp4", size, 0.8, false)
			job := sampleJob(t, r, source, "end", 2*time.Second, time.Second, "sd")
			generateSample(t, r, source, job, 0.5)
		})
	}
}

func TestGenerateSampleLiveUnknownDurationStart(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 0.8, false)
	info := r.Probe(context.Background(), source, "")
	if info.Error != "" {
		t.Fatal(info.Error)
	}
	info.DurationS = 0
	opts := DefaultPreviewOptions()
	opts.SampleMode = "start"
	plan, err := PlanPreviews(*info, source, opts, nil)
	if err != nil || len(plan.Jobs) != 1 || plan.Jobs[0].DurationKnown {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	job := plan.Jobs[0]
	job.Output = filepath.Join(t.TempDir(), job.Output)
	if err := r.GenerateSample(context.Background(), source, job); err != nil {
		t.Fatal(err)
	}
	got := r.Probe(context.Background(), job.Output, "")
	if got.Error != "" || math.Abs(got.DurationS-0.8) > 0.5 || got.Width != job.Size.Width {
		t.Fatalf("unknown duration sample = %+v", got)
	}
}

func TestGenerateSampleLiveLongSeries(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, audio := range []bool{false, true} {
		t.Run(map[bool]string{false: "silent", true: "audio"}[audio], func(t *testing.T) {
			source := sampleSource(t, ffmpeg, ".mp4", "128x96", 5.2, audio)
			job := sampleJob(t, r, source, "series", 200*time.Millisecond, 100*time.Millisecond, "sd")
			if len(job.Ranges) <= sampleChunkLimit {
				t.Fatalf("expected chunked series, got %d ranges", len(job.Ranges))
			}
			generateSample(t, r, source, job, 0.5)
		})
	}
}

func TestGenerateSampleLiveRotatedSource(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 1.2, false)
	rotated := filepath.Join(t.TempDir(), "rotated.mp4")
	cmd := exec.CommandContext(context.Background(), ffmpeg, "-hide_banner", "-v", "error",
		"-display_rotation:v:0", "90", "-i", source, "-c", "copy", rotated)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rotate source: %v: %s", err, out)
	}
	job := sampleJob(t, r, rotated, "start", time.Second, time.Second, "sd")
	if job.Size != (PreviewSize{96, 128}) {
		t.Fatalf("rotated plan size = %+v", job.Size)
	}
	generateSample(t, r, rotated, job, 0.5)
}

func TestSampleEncoderFallbackAndValidation(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 1.2, true)
	job := sampleJob(t, r, source, "start", time.Second, time.Second, "sd")
	r.encoders = map[string]bool{"mpeg4": true, "aac": true}
	if got := generateSample(t, r, source, job, 0.5).VideoCodec; got != "mpeg4" {
		t.Fatalf("fallback codec = %s", got)
	}
	if err := r.ValidatePublishedPreview(context.Background(), source, job.Output, job); err != nil {
		t.Fatalf("published sample rejected: %v", err)
	}
	wrongSize := job
	wrongSize.Size = PreviewSize{64, 48}
	if err := r.ValidatePublishedPreview(context.Background(), source, job.Output, wrongSize); err == nil {
		t.Fatal("sample with other dimensions accepted")
	}
	job.Output = filepath.Join(t.TempDir(), "unpublished.mp4")
	r.Tools[FFprobe] = Found{}
	if err := r.GenerateSample(context.Background(), source, job); err == nil || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("expected probe failure, got %v", err)
	}
	for _, path := range []string{job.Output, PreviewPartPath(job.Output)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid sample published at %s: %v", path, err)
		}
	}
}

// undecodableAudioSource remuxes a generated MP4 into a MOV with copies of its AAC stream and
// renames the first audio sample entry to apac without its esds box: FFmpeg then has no decoder
// for that stream, as for Apple positional audio in iPhone recordings.
func undecodableAudioSource(t *testing.T, ffmpeg string, copies int) string {
	t.Helper()
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 1.2, true)
	mov := filepath.Join(t.TempDir(), "spatial.mov")
	args := []string{"-hide_banner", "-v", "error", "-y", "-i", source, "-map", "0:v"}
	for range copies {
		args = append(args, "-map", "0:a")
	}
	if out, err := exec.CommandContext(context.Background(), ffmpeg, append(args, "-c", "copy", mov)...).CombinedOutput(); err != nil {
		t.Fatalf("remux: %v: %s", err, out)
	}
	data, err := os.ReadFile(mov)
	if err != nil {
		t.Fatal(err)
	}
	entry := bytes.Index(data, []byte("mp4a"))
	esds := bytes.Index(data[max(entry, 0):], []byte("esds"))
	if entry < 0 || esds < 0 {
		t.Fatal("AAC sample entry not found")
	}
	copy(data[entry:], "apac")
	copy(data[entry+esds:], "free")
	if err := os.WriteFile(mov, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return mov
}

func TestGenerateSampleLiveUndecodableAudio(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, tc := range []struct {
		name     string
		copies   int
		hasAudio bool
	}{
		{"later stream decodes", 2, true},
		{"no stream decodes", 1, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			source := undecodableAudioSource(t, ffmpeg, tc.copies)
			job := sampleJob(t, r, source, "series", 400*time.Millisecond, 500*time.Millisecond, "sd")
			if !job.HasAudio || job.AudioStreams != tc.copies {
				t.Fatalf("planned job = %+v", job)
			}
			if err := r.GenerateSample(context.Background(), source, job); err != nil {
				t.Fatal(err)
			}
			if got := r.Probe(context.Background(), job.Output, ""); got.Error != "" || got.HasAudio != tc.hasAudio || got.VideoStreams != 1 {
				t.Fatalf("sample = %+v", got)
			}
			if err := r.ValidatePublishedPreview(context.Background(), source, job.Output, job); err != nil {
				t.Fatalf("published sample rejected: %v", err)
			}
		})
	}
}
