package media

import (
	"bytes"
	"context"
	"errors"
	"image"
	"image/color"
	"image/png"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

func frameJobs(t *testing.T, r *Runner, source, mode, resolution, quality string, every time.Duration) []PreviewJob {
	t.Helper()
	info := r.Probe(context.Background(), source, "")
	if info.Error != "" {
		t.Fatal(info.Error)
	}
	opts := DefaultPreviewOptions()
	opts.ImageMode, opts.ImageResolution, opts.ImageQuality, opts.ImageEvery = mode, resolution, quality, every
	plan, err := PlanPreviews(*info, source, opts, nil)
	if err != nil {
		t.Fatal(err)
	}
	return plan.Jobs
}

func runFrames(t *testing.T, r *Runner, source string, jobs []PreviewJob) []string {
	t.Helper()
	dir := t.TempDir()
	names := make([]string, 0, len(jobs))
	for _, job := range jobs {
		names = append(names, job.Output)
		job.Output = filepath.Join(dir, job.Output)
		if err := r.GenerateFrame(context.Background(), source, job); err != nil {
			t.Fatalf("generate %s at %.3fs: %v", job.Output, job.TimeS, err)
		}
		if err := validateFramePNG(job.Output, job.Size); err != nil {
			t.Fatal(err)
		}
		if _, err := os.Stat(PreviewPartPath(job.Output)); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("part remains: %v", err)
		}
	}
	entries, err := os.ReadDir(dir)
	if err != nil || len(entries) != len(jobs) {
		t.Fatalf("frame files = %v, %v", entries, err)
	}
	return names
}

func TestFrameArgsAndInvalidJobs(t *testing.T) {
	job := PreviewJob{Kind: "image", Output: "x-img01.png", TimeS: 1.25,
		Size: PreviewSize{320, 240}, ImageQuality: "low"}
	for quality, level := range map[string]string{"low": "9", "medium": "6", "high": "3"} {
		job.ImageQuality = quality
		want := []string{"-v", "error", "-ss", "1.250000", "-i", "source.mp4",
			"-map", "0:v:0", "-frames:v", "1", "-vf", "scale=320:240:flags=lanczos,setsar=1",
			"-compression_level", level}
		if got := frameArgs("source.mp4", job); !reflect.DeepEqual(got, want) {
			t.Fatalf("%s args = %v", quality, got)
		}
		if err := validateFrameJob("source.mp4", job); err != nil {
			t.Fatal(err)
		}
	}
	for _, invalid := range []PreviewJob{
		{Kind: "sample", Output: job.Output, Size: job.Size, ImageQuality: "high"},
		{Kind: "image", Output: "x.jpg", Size: job.Size, ImageQuality: "high"},
		{Kind: "image", Output: job.Output, Size: PreviewSize{319, 240}, ImageQuality: "high"},
		{Kind: "image", Output: job.Output, Size: job.Size, TimeS: -1, ImageQuality: "high"},
		{Kind: "image", Output: job.Output, Size: job.Size, TimeS: math.NaN(), ImageQuality: "high"},
		{Kind: "image", Output: job.Output, Size: job.Size, TimeS: math.Inf(1), ImageQuality: "high"},
		{Kind: "image", Output: job.Output, Size: job.Size, ImageQuality: "bogus"},
	} {
		if err := validateFrameJob("source.mp4", invalid); err == nil {
			t.Fatalf("accepted invalid job: %+v", invalid)
		}
	}
	job.ImageQuality = "high"
	for _, source := range []string{"x-img01.png", "x-img01.arxgo-part.png"} {
		if err := validateFrameJob(source, job); err == nil {
			t.Fatalf("accepted source/output conflict: %s", source)
		}
	}
}

func TestFramePNGValidationAndFailedPublish(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "frame.png")
	img := image.NewRGBA(image.Rect(0, 0, 4, 2))
	img.Set(0, 0, color.RGBA{R: 255, A: 255})
	f, err := os.Create(path)
	if err != nil {
		t.Fatal(err)
	}
	if err := png.Encode(f, img); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := validateFramePNG(path, PreviewSize{4, 2}); err != nil {
		t.Fatal(err)
	}
	if err := validateFramePNG(path, PreviewSize{2, 4}); err == nil || !strings.Contains(err.Error(), "dimensions") {
		t.Fatalf("wrong dimensions: %v", err)
	}
	if err := os.WriteFile(path, []byte("not a PNG"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := validateFramePNG(path, PreviewSize{4, 2}); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("invalid PNG: %v", err)
	}
	r := helperRunner(t, "success")
	job := PreviewJob{Kind: "image", Output: filepath.Join(dir, "new.png"),
		Size: PreviewSize{4, 2}, ImageQuality: "medium"}
	if err := r.GenerateFrame(context.Background(), "source.mp4", job); err == nil || !strings.Contains(err.Error(), "decode") {
		t.Fatalf("invalid ffmpeg output: %v", err)
	}
	for _, output := range []string{job.Output, PreviewPartPath(job.Output)} {
		if _, err := os.Stat(output); !errors.Is(err, os.ErrNotExist) {
			t.Fatalf("invalid frame published at %s: %v", output, err)
		}
	}
	if err := os.WriteFile(job.Output, []byte("user data"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := r.GenerateFrame(context.Background(), "source.mp4", job); !errors.Is(err, os.ErrExist) {
		t.Fatalf("occupied output: %v", err)
	}
	if got, err := os.ReadFile(job.Output); err != nil || string(got) != "user data" {
		t.Fatalf("occupied output changed: %q, %v", got, err)
	}
}

func TestGenerateFramesLiveModesAndNames(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "256x144", 3.2, true)
	for _, tc := range []struct {
		mode string
		want []string
	}{
		{"start", []string{"source-img01.png"}},
		{"middle", []string{"source-img01.png"}},
		{"end", []string{"source-img01.png"}},
		{"series", []string{"source-img01.png", "source-img02.png", "source-img03.png", "source-img04.png"}},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			jobs := frameJobs(t, r, source, tc.mode, "sd", "medium", time.Second)
			if len(jobs) != len(tc.want) {
				t.Fatalf("planned %d frames, want %d", len(jobs), len(tc.want))
			}
			if got := runFrames(t, r, source, jobs); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("names = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestGenerateFramesLiveClampRotationAndShortEnd(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	for _, tc := range []struct {
		size string
		want PreviewSize
	}{
		{"1280x720", PreviewSize{640, 360}},
		{"720x1280", PreviewSize{360, 640}},
	} {
		t.Run(tc.size, func(t *testing.T) {
			source := sampleSource(t, ffmpeg, ".mp4", tc.size, 1.3, false)
			jobs := frameJobs(t, r, source, "end", "sd", "high", time.Second)
			if len(jobs) != 1 || jobs[0].Size != tc.want || math.Abs(jobs[0].TimeS-0.3) > 0.01 {
				t.Fatalf("end job = %+v", jobs)
			}
			runFrames(t, r, source, jobs)
		})
	}
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 1.3, false)
	rotated := filepath.Join(t.TempDir(), "rotated.mp4")
	cmd := exec.CommandContext(context.Background(), ffmpeg, "-hide_banner", "-v", "error",
		"-display_rotation:v:0", "90", "-i", source, "-c", "copy", rotated)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("rotate source: %v: %s", err, out)
	}
	jobs := frameJobs(t, r, rotated, "end", "sd", "low", time.Second)
	if len(jobs) != 1 || jobs[0].Size != (PreviewSize{96, 128}) {
		t.Fatalf("rotated job = %+v", jobs)
	}
	if names := runFrames(t, r, rotated, jobs); !reflect.DeepEqual(names, []string{"rotated-img01.png"}) {
		t.Fatal(names)
	}
	// A final series seek at 1.2 seconds must decode a frame in a 1.3-second clip.
	jobs = frameJobs(t, r, source, "series", "sd", "medium", 1200*time.Millisecond)
	if len(jobs) != 2 || math.Abs(jobs[1].TimeS-1.2) > 0.01 {
		t.Fatalf("short series = %+v", jobs)
	}
	for i := range jobs {
		jobs[i].Output = filepath.Join(t.TempDir(), jobs[i].Output)
		if err := r.GenerateFrame(context.Background(), source, jobs[i]); err != nil {
			t.Fatalf("series frame %.1fs: %v", jobs[i].TimeS, err)
		}
		if err := validateFramePNG(jobs[i].Output, jobs[i].Size); err != nil {
			t.Fatal(err)
		}
	}
	first, err := os.ReadFile(jobs[0].Output)
	if err != nil {
		t.Fatal(err)
	}
	last, err := os.ReadFile(jobs[1].Output)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Equal(first, last) {
		t.Fatal("late frame is identical to the first frame")
	}
}

func TestGenerateFrameLiveUnknownDurationShortStart(t *testing.T) {
	ffmpeg, r := sampleTools(t)
	source := sampleSource(t, ffmpeg, ".mp4", "128x96", 0.8, false)
	info := r.Probe(context.Background(), source, "")
	if info.Error != "" {
		t.Fatal(info.Error)
	}
	info.DurationS = 0
	opts := DefaultPreviewOptions()
	opts.ImageMode = "start"
	plan, err := PlanPreviews(*info, source, opts, nil)
	if err != nil || len(plan.Jobs) != 1 {
		t.Fatalf("unknown-duration plan = %+v, %v", plan, err)
	}
	job := plan.Jobs[0]
	if job.DurationKnown || job.TimeS != 1 {
		t.Fatalf("unknown-duration job = %+v", job)
	}
	runFrames(t, r, source, plan.Jobs)
}
