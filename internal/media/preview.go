package media

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

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
		SampleMode: "none", ImageMode: "none", SampleDuration: 5 * time.Second,
		SampleEvery: 5 * time.Minute, ImageEvery: 5 * time.Minute,
		SampleResolution: "sd", ImageResolution: "sd",
		SampleQuality: "medium", ImageQuality: "medium", MaxItems: 100,
	}
}

type PreviewRange struct{ StartS, DurationS float64 }
type PreviewSize struct{ Width, Height int }

// PreviewJob describes one output. A series sample has several ranges but one output.
type PreviewJob struct {
	Kind, Output                string
	Container                   string
	Ranges                      []PreviewRange
	TimeS                       float64 // image seek position
	Size                        PreviewSize
	VideoEncoder, AudioEncoder  string
	HasAudio                    bool
	DurationKnown               bool
	SampleQuality, ImageQuality string
	EstimateBytes               int64
}

type PreviewPlan struct {
	Jobs          []PreviewJob
	EstimateBytes int64
	SampleCapped  bool
	ImageCapped   bool
	Warnings      []string
}

// PlanPreviews uses only metadata, options and the caller's read-only occupancy predicate.
// Occupied must report every protected name, including unowned files and reserved outputs.
func PlanPreviews(info MediaInfo, source string, opts PreviewOptions, occupied func(string) bool) (PreviewPlan, error) {
	var plan PreviewPlan
	if err := ValidatePreviewOptions(opts); err != nil {
		return plan, err
	}
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	if base == "." || base == string(filepath.Separator) || ext == "" || len(base) == len(ext) {
		return plan, errors.New("preview source requires a file name with extension")
	}
	d := info.DurationS
	known := d > 0 && !math.IsNaN(d) && !math.IsInf(d, 0)
	needSample := opts.SampleMode != "none" && (known || opts.SampleMode == "start")
	needImage := opts.ImageMode != "none" && (known || opts.ImageMode == "start")
	if (needSample || needImage) && (info.Width < 2 || info.Height < 2) {
		return plan, errors.New("preview source dimensions are unavailable")
	}
	if needSample && ClampPreviewSize(info, opts.SampleResolution) == (PreviewSize{}) ||
		needImage && ClampPreviewSize(info, opts.ImageResolution) == (PreviewSize{}) {
		return plan, errors.New("preview source cannot fit even output dimensions")
	}
	stem := strings.TrimSuffix(base, ext)
	if !known && (opts.SampleMode != "none" || opts.ImageMode != "none") {
		plan.Warnings = append(plan.Warnings, "duration unknown; only start previews can be planned")
	}
	used := map[string]bool{}
	name := func(kind string, index, count int, extension string) (string, error) {
		width := 2
		if count > 99 {
			width = len(fmt.Sprint(count))
		}
		suffix := fmt.Sprintf("-%s%0*d%s", kind, width, index, extension)
		candidate := stem + suffix
		isUsed := func(s string) bool { return used[s] || occupied != nil && occupied(s) }
		if isUsed(candidate) {
			candidate = stem + "-arxgo" + suffix
			if isUsed(candidate) {
				return "", fmt.Errorf("preview output name occupied: %s", candidate)
			}
			plan.Warnings = append(plan.Warnings, "preview name collision: "+stem+suffix+" -> "+candidate)
		}
		used[candidate] = true
		return candidate, nil
	}
	if needSample {
		ranges, capped := sampleRanges(opts.SampleMode, d, known, opts.SampleDuration.Seconds(), opts.SampleEvery.Seconds(), opts.MaxItems)
		plan.SampleCapped = capped
		if capped {
			plan.Warnings = append(plan.Warnings, "sample series capped")
		}
		if len(ranges) > 0 {
			out, err := name("smpl", 1, 1, ext)
			if err != nil {
				return PreviewPlan{}, err
			}
			video, audio := PreviewEncoders(info.Container, ext)
			estimate := estimateSample(sumRanges(ranges), opts.SampleResolution, opts.SampleQuality)
			plan.Jobs = append(plan.Jobs, PreviewJob{Kind: "sample", Output: out, Ranges: ranges,
				Size: ClampPreviewSize(info, opts.SampleResolution), Container: info.Container,
				VideoEncoder: video, AudioEncoder: audio,
				HasAudio: info.HasAudio, DurationKnown: known, SampleQuality: opts.SampleQuality,
				EstimateBytes: estimate})
			plan.EstimateBytes += estimate
		}
	}
	if needImage {
		times, capped := imageTimes(opts.ImageMode, d, known, opts.ImageEvery.Seconds(), opts.MaxItems)
		plan.ImageCapped = capped
		if capped {
			plan.Warnings = append(plan.Warnings, "image series capped")
		}
		for i, at := range times {
			out, err := name("img", i+1, len(times), ".png")
			if err != nil {
				return PreviewPlan{}, err
			}
			estimate := imageEstimate(opts.ImageResolution)
			plan.Jobs = append(plan.Jobs, PreviewJob{Kind: "image", Output: out, TimeS: at,
				Size: ClampPreviewSize(info, opts.ImageResolution), ImageQuality: opts.ImageQuality,
				DurationKnown: known, EstimateBytes: estimate})
		}
		plan.EstimateBytes += int64(len(times)) * imageEstimate(opts.ImageResolution)
	}
	return plan, nil
}

func sumRanges(ranges []PreviewRange) float64 {
	var sum float64
	for _, r := range ranges {
		sum += r.DurationS
	}
	return sum
}

func ValidatePreviewOptions(o PreviewOptions) error {
	mode := func(s string) bool {
		return s == "none" || s == "start" || s == "middle" || s == "end" || s == "series"
	}
	res := func(s string) bool { return s == "sd" || s == "hd" || s == "4k" }
	quality := func(s string) bool { return s == "low" || s == "medium" || s == "high" }
	switch {
	case !mode(o.SampleMode) || !mode(o.ImageMode):
		return errors.New("invalid preview mode")
	case !res(o.SampleResolution) || !res(o.ImageResolution):
		return errors.New("invalid preview resolution")
	case !quality(o.SampleQuality) || !quality(o.ImageQuality):
		return errors.New("invalid preview quality")
	case o.SampleDuration <= 0 || o.SampleEvery <= 0 || o.ImageEvery <= 0:
		return errors.New("preview durations and spacing must be positive")
	case o.MaxItems < 1:
		return errors.New("preview max items must be at least 1")
	}
	return nil
}

func sampleRanges(mode string, d float64, known bool, length, every float64, cap int) ([]PreviewRange, bool) {
	if !known {
		return []PreviewRange{{0, length}}, false
	}
	if d <= length {
		return []PreviewRange{{0, d}}, false
	}
	switch mode {
	case "start":
		return []PreviewRange{{0, length}}, false
	case "middle":
		return []PreviewRange{{d/2 - length/2, length}}, false
	case "end":
		return []PreviewRange{{d - length, length}}, false
	case "series":
		var out []PreviewRange
		for i := 0; i < cap && float64(i)*every < d; i++ {
			start := float64(i) * every
			out = append(out, PreviewRange{start, math.Min(length, d-start)})
		}
		return out, float64(cap)*every < d
	}
	return nil, false
}

func imageTimes(mode string, d float64, known bool, every float64, cap int) ([]float64, bool) {
	if !known {
		return []float64{1}, false
	}
	switch mode {
	case "start":
		return []float64{math.Min(1, d/2)}, false
	case "middle":
		return []float64{d / 2}, false
	case "end":
		return []float64{math.Max(0, d-1)}, false
	case "series":
		var out []float64
		for i := 0; i < cap && float64(i)*every < d; i++ {
			at := float64(i) * every
			if i == 0 {
				at = math.Min(1, d/2)
			}
			out = append(out, at)
		}
		return out, float64(cap)*every < d
	}
	return nil, false
}

// PreviewEncoders uses the probed container when known. Availability and muxer fallback
// belong to the executor, which can inspect the installed ffmpeg encoders.
func PreviewEncoders(container, ext string) (video, audio string) {
	switch strings.ToLower(container) {
	case "webm":
		return "libvpx-vp9", "libopus"
	case "avi":
		return "mpeg4", "libmp3lame"
	case "mp4", "mov", "3gp", "matroska", "mkv":
		return "libx264", "aac"
	}
	switch strings.ToLower(ext) {
	case ".webm":
		return "libvpx-vp9", "libopus"
	case ".avi":
		return "mpeg4", "libmp3lame"
	default:
		return "libx264", "aac"
	}
}

func estimateSample(seconds float64, res, quality string) int64 {
	bitrate := map[string]float64{"sd": 1e6, "hd": 5e6, "4k": 20e6}[res]
	factor := map[string]float64{"low": 0.7, "medium": 1, "high": 1.5}[quality]
	return int64(math.Ceil(seconds * bitrate * factor / 8))
}

func imageEstimate(res string) int64 {
	return map[string]int64{"sd": 1 << 19, "hd": 3 << 20, "4k": 12 << 20}[res]
}
