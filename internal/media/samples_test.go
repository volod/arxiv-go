package media

import (
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

func sampleSource(t *testing.T, ffmpeg, ext, size string, duration float64, audio bool) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "source"+ext)
	args := []string{"-hide_banner", "-v", "error", "-y", "-f", "lavfi", "-i",
		"testsrc2=size=" + size + ":rate=10:duration=" + sampleSeconds(duration)}
	if audio {
		args = append(args, "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=48000:duration="+sampleSeconds(duration))
	}
	if ext == ".webm" {
		args = append(args, "-c:v", "libvpx-vp9", "-deadline", "realtime", "-cpu-used", "6")
		if audio {
			args = append(args, "-c:a", "libopus")
		}
	} else {
		args = append(args, "-c:v", "mpeg4", "-q:v", "5")
		if audio {
			audioCodec := "aac"
			if ext == ".avi" {
				audioCodec = "libmp3lame"
			}
			args = append(args, "-c:a", audioCodec)
		}
	}
	args = append(args, path)
	cmd := exec.CommandContext(context.Background(), ffmpeg, args...)
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

func checkSample(t *testing.T, r *Runner, output string, job PreviewJob, tolerance float64) {
	t.Helper()
	info := r.Probe(context.Background(), output, "")
	if info.Error != "" {
		t.Fatal(info.Error)
	}
	var want float64
	for _, segment := range job.Ranges {
		want += segment.DurationS
	}
	if math.Abs(info.DurationS-want) > tolerance {
		t.Fatalf("duration %.3f, want %.3f", info.DurationS, want)
	}
	if info.Width != job.Size.Width || info.Height != job.Size.Height || info.Container != job.Container ||
		info.HasAudio != job.HasAudio || info.VideoStreams == 0 {
		t.Fatalf("sample = %+v, job = %+v", info, job)
	}
	if _, err := os.Stat(PreviewPartPath(output)); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("part remains: %v", err)
	}
}

func TestGenerateSamplesLiveContainersAndModes(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, ext := range []string{".mp4", ".mov", ".mkv", ".webm", ".m4v", ".3gp", ".avi"} {
		t.Run(ext, func(t *testing.T) {
			source := sampleSource(t, ffmpeg, ext, "256x144", 3.2, true)
			for _, mode := range []string{"start", "middle", "end", "series"} {
				t.Run(mode, func(t *testing.T) {
					job := sampleJob(t, r, source, mode, time.Second, time.Second, "sd")
					output, err := r.GenerateSample(context.Background(), source, job)
					if err != nil {
						t.Fatal(err)
					}
					if output != job.Output || filepath.Base(output) != "source-smpl01"+ext {
						t.Fatalf("output = %s", output)
					}
					checkSample(t, r, output, job, 0.5*float64(len(job.Ranges)))
				})
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
			output, err := r.GenerateSample(context.Background(), source, job)
			if err != nil {
				t.Fatal(err)
			}
			checkSample(t, r, output, job, 0.5)
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
	if err != nil || len(plan.Jobs) != 1 {
		t.Fatalf("plan = %+v, %v", plan, err)
	}
	job := plan.Jobs[0]
	if job.DurationKnown {
		t.Fatal("unknown duration marked known")
	}
	job.Output = filepath.Join(t.TempDir(), job.Output)
	output, err := r.GenerateSample(context.Background(), source, job)
	if err != nil {
		t.Fatal(err)
	}
	got := r.Probe(context.Background(), output, "")
	if got.Error != "" || math.Abs(got.DurationS-0.8) > 0.5 || got.Width != job.Size.Width {
		t.Fatalf("unknown duration sample = %+v", got)
	}
}

func TestGenerateSampleLiveLongSeries(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, audio := range []bool{false, true} {
		name := "silent"
		if audio {
			name = "audio"
		}
		t.Run(name, func(t *testing.T) {
			source := sampleSource(t, ffmpeg, ".mp4", "128x96", 5.2, audio)
			job := sampleJob(t, r, source, "series", 200*time.Millisecond, 100*time.Millisecond, "sd")
			if len(job.Ranges) <= sampleChunkLimit {
				t.Fatalf("expected chunked series, got %d ranges", len(job.Ranges))
			}
			output, err := r.GenerateSample(context.Background(), source, job)
			if err != nil {
				t.Fatal(err)
			}
			checkSample(t, r, output, job, 0.5)
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
	output, err := r.GenerateSample(context.Background(), rotated, job)
	if err != nil {
		t.Fatal(err)
	}
	checkSample(t, r, output, job, 0.5)
}

func TestGenerateSampleLiveUnknownExtensionFallback(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 1.2, false)
	unknown := filepath.Join(t.TempDir(), "source.unknown")
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(unknown, data, 0o600); err != nil {
		t.Fatal(err)
	}
	job := sampleJob(t, r, unknown, "start", time.Second, time.Second, "sd")
	output, err := r.GenerateSample(context.Background(), unknown, job)
	if err != nil {
		t.Fatal(err)
	}
	if output != strings.TrimSuffix(job.Output, ".unknown")+".mp4" {
		t.Fatalf("fallback output = %s", output)
	}
	checkSample(t, r, output, job, 0.5)
	if _, err := os.Stat(job.Output); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected original output: %v", err)
	}
	job.Output = filepath.Join(t.TempDir(), "occupied-smpl01.unknown")
	if err := os.WriteFile(job.Output, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := r.GenerateSample(context.Background(), unknown, job); !errors.Is(err, os.ErrExist) {
		t.Fatalf("occupied output: %v", err)
	}
	if data, err := os.ReadFile(job.Output); err != nil || string(data) != "user data" {
		t.Fatalf("occupied output changed: %q, %v", data, err)
	}
	if _, err := os.Stat(strings.TrimSuffix(job.Output, ".unknown") + ".mp4"); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("unexpected fallback after output conflict: %v", err)
	}
}

func TestSampleEncoderFallbackAndValidation(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 1.2, true)
	job := sampleJob(t, r, source, "start", time.Second, time.Second, "sd")
	r.encoders = map[string]bool{"mpeg4": true, "aac": true}
	output, err := r.GenerateSample(context.Background(), source, job)
	if err != nil {
		t.Fatal(err)
	}
	checkSample(t, r, output, job, 0.5)
	if got := r.Probe(context.Background(), output, "").VideoCodec; got != "mpeg4" {
		t.Fatalf("fallback codec = %s", got)
	}
	job.Output = filepath.Join(t.TempDir(), "unpublished.mp4")
	r.Tools[FFprobe] = Found{}
	if _, err := r.GenerateSample(context.Background(), source, job); err == nil || !strings.Contains(err.Error(), "ffprobe") {
		t.Fatalf("expected probe failure, got %v", err)
	}
	for _, path := range []string{job.Output, PreviewPartPath(job.Output)} {
		if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid sample published at %s: %v", path, err)
		}
	}
}

func TestSampleEncodersAndArguments(t *testing.T) {
	job := PreviewJob{Kind: "sample", Output: "clip.avi", Size: PreviewSize{640, 360},
		VideoEncoder: "libx264", AudioEncoder: "libmp3lame", HasAudio: true, SampleQuality: "high"}
	video, audio, err := sampleEncoders(job, map[string]bool{"mpeg4": true, "aac": true})
	if err != nil || video != "mpeg4" || audio != "aac" {
		t.Fatalf("fallback = %s, %s, %v", video, audio, err)
	}
	if _, _, err := sampleEncoders(job, map[string]bool{"aac": true}); err == nil {
		t.Fatal("missing video encoder accepted")
	}
	args := sampleEncodeArgs("source.avi", []PreviewRange{{0, 2}}, job, video, audio)
	joined := strings.Join(args, " ")
	if !strings.Contains(joined, "-q:v 3") || !strings.Contains(joined, "-b:a 128k") ||
		!strings.Contains(joined, "-map 0:a:0") {
		t.Fatalf("args = %s", joined)
	}
}
