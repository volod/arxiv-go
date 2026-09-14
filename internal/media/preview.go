package media

import (
	"context"
	"errors"
	"fmt"
	"path/filepath"
	"time"
)

// Preview kinds.
const (
	PreviewSample = "sample"
	PreviewImage  = "image"
)

const previewModeNone = "none"

// PreviewOptions are validated by the CLI and checked again for direct callers.
type PreviewOptions struct {
	SampleMode, ImageMode             string
	SampleDuration, SampleEvery       time.Duration
	ImageEvery                        time.Duration
	SampleResolution, ImageResolution string
	SampleQuality, ImageQuality       string
	MaxItems                          int
}

func DefaultPreviewOptions() PreviewOptions {
	return PreviewOptions{
		SampleMode: previewModeNone, ImageMode: previewModeNone, SampleDuration: 5 * time.Second,
		SampleEvery: 5 * time.Minute, ImageEvery: 5 * time.Minute,
		SampleResolution: "sd", ImageResolution: "sd",
		SampleQuality: "medium", ImageQuality: "medium", MaxItems: 100,
	}
}

// Enabled reports whether any preview is requested. An empty mode means none.
func (o PreviewOptions) Enabled() bool {
	return modeRequested(o.SampleMode) || modeRequested(o.ImageMode)
}

func modeRequested(mode string) bool { return mode != "" && mode != previewModeNone }

func (o PreviewOptions) validate() error {
	mode := func(s string) bool {
		return s == previewModeNone || s == "start" || s == "middle" || s == "end" || s == "series"
	}
	res := func(s string) bool { _, ok := previewBoxes[s]; return ok }
	switch {
	case !mode(o.SampleMode) || !mode(o.ImageMode):
		return errors.New("invalid preview mode")
	case !res(o.SampleResolution) || !res(o.ImageResolution):
		return errors.New("invalid preview resolution")
	case !qualityValid(o.SampleQuality) || !qualityValid(o.ImageQuality):
		return errors.New("invalid preview quality")
	case o.SampleDuration <= 0 || o.SampleEvery <= 0 || o.ImageEvery <= 0:
		return errors.New("preview durations and spacing must be positive")
	case o.MaxItems < 1:
		return errors.New("preview max items must be at least 1")
	}
	return nil
}

func qualityValid(q string) bool { return q == "low" || q == "medium" || q == "high" }

type PreviewRange struct{ StartS, DurationS float64 }
type PreviewSize struct{ Width, Height int }

// PreviewJob describes one output. A series sample has several ranges but one output. The planner
// sets Output to a file name; the caller joins it with the target directory before execution.
type PreviewJob struct {
	Kind          string
	Output        string
	Ranges        []PreviewRange // sample time ranges
	TimeS         float64        // image seek position
	Size          PreviewSize    // display-oriented, even dimensions
	Quality       string
	VideoEncoder  string // preferred sample encoders for the output container
	AudioEncoder  string
	HasAudio      bool
	AudioStreams  int // audio streams of the source
	AudioStream   int // index among them of the encoded stream, chosen by the executor
	DurationKnown bool
	EstimateBytes int64
}

func (j PreviewJob) totalSeconds() float64 {
	var sum float64
	for _, r := range j.Ranges {
		sum += r.DurationS
	}
	return sum
}

// validate checks the fields both executors rely on, and that the output never names the source.
func (j PreviewJob) validate(source string) error {
	if source == "" || j.Output == "" || filepath.Ext(j.Output) == "" ||
		j.Size.Width < 2 || j.Size.Height < 2 || j.Size.Width%2 != 0 || j.Size.Height%2 != 0 ||
		!qualityValid(j.Quality) {
		return fmt.Errorf("invalid %s job", j.Kind)
	}
	src := filepath.Clean(source)
	if src == filepath.Clean(j.Output) || src == filepath.Clean(PreviewPartPath(j.Output)) {
		return fmt.Errorf("%s output conflicts with source", j.Kind)
	}
	return nil
}

type PreviewPlan struct {
	Jobs          []PreviewJob
	EstimateBytes int64
	Warnings      []string
}

// Generate runs the executor for the job kind. The caller must own the preview WAL event before
// passing an output in the archive.
func (r *Runner) Generate(ctx context.Context, source string, job PreviewJob) error {
	switch job.Kind {
	case PreviewSample:
		return r.GenerateSample(ctx, source, job)
	case PreviewImage:
		return r.GenerateFrame(ctx, source, job)
	default:
		return fmt.Errorf("unknown preview kind %q", job.Kind)
	}
}

// ValidatePublishedPreview checks a final file left after a crash between publication and the
// preview_done event. It applies the same content checks used before publication.
func (r *Runner) ValidatePublishedPreview(ctx context.Context, source, path string, job PreviewJob) error {
	switch job.Kind {
	case PreviewSample:
		job, err := r.sampleAudio(ctx, source, job)
		if err != nil {
			return err
		}
		return r.validateSample(ctx, path, job)
	case PreviewImage:
		return validateFramePNG(path, job.Size)
	default:
		return fmt.Errorf("unknown preview kind %q", job.Kind)
	}
}
