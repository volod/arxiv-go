package media

import (
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
)

// previewBoxes are the bounding boxes of the resolution settings, landscape orientation.
var previewBoxes = map[string]PreviewSize{"sd": {640, 360}, "hd": {1920, 1080}, "4k": {3840, 2160}}

// PlanPreviews turns metadata and options into jobs without running a tool. occupied reports every
// name in the target directory that the plan must not take, including files of other videos.
func PlanPreviews(info MediaInfo, source string, opts PreviewOptions, occupied func(string) bool) (PreviewPlan, error) {
	if err := opts.validate(); err != nil {
		return PreviewPlan{}, err
	}
	base := filepath.Base(source)
	ext := filepath.Ext(base)
	if base == "." || base == string(filepath.Separator) || ext == "" || len(base) == len(ext) {
		return PreviewPlan{}, errors.New("preview source requires a file name with extension")
	}
	p := previewPlanner{info: info, opts: opts, stem: strings.TrimSuffix(base, ext), ext: ext,
		occupied: occupied, used: map[string]bool{}}
	if err := p.plan(); err != nil {
		return PreviewPlan{}, err
	}
	return p.out, nil
}

type previewPlanner struct {
	info      MediaInfo
	opts      PreviewOptions
	stem, ext string
	occupied  func(string) bool
	used      map[string]bool
	out       PreviewPlan
}

func (p *previewPlanner) plan() error {
	if !p.opts.Enabled() {
		return nil
	}
	if p.info.VideoStreams == 0 {
		p.warn("no video stream; previews skipped")
		return nil
	}
	known := p.durationKnown()
	if !known {
		p.warn("duration unknown; only start previews can be planned")
	}
	sample := modeRequested(p.opts.SampleMode) && (known || p.opts.SampleMode == "start")
	image := modeRequested(p.opts.ImageMode) && (known || p.opts.ImageMode == "start")
	if !sample && !image {
		return nil
	}
	if p.info.Width < 2 || p.info.Height < 2 {
		return errors.New("preview source dimensions are unavailable")
	}
	if sample {
		if err := p.planSample(known); err != nil {
			return err
		}
	}
	if image {
		return p.planImages(known)
	}
	return nil
}

func (p *previewPlanner) durationKnown() bool {
	d := p.info.DurationS
	return d > 0 && !math.IsNaN(d) && !math.IsInf(d, 0)
}

func (p *previewPlanner) planSample(known bool) error {
	size := clampPreviewSize(p.info, p.opts.SampleResolution)
	if size == (PreviewSize{}) {
		return errors.New("preview source cannot fit even output dimensions")
	}
	ranges, capped := sampleRanges(p.opts.SampleMode, p.info.DurationS, known,
		p.opts.SampleDuration.Seconds(), p.opts.SampleEvery.Seconds(), p.opts.MaxItems)
	if capped {
		p.warn("sample series capped")
	}
	codec, ext, substituted := sampleContainer(p.ext)
	if substituted {
		p.warn(fmt.Sprintf("no sample container for %s; writing %s", p.ext, ext))
	}
	name, err := p.name("smpl", 1, 1, ext)
	if err != nil {
		return err
	}
	job := PreviewJob{Kind: PreviewSample, Output: name, Ranges: ranges, Size: size,
		Quality: p.opts.SampleQuality, VideoEncoder: codec.video, AudioEncoder: codec.audio,
		HasAudio: p.info.HasAudio, AudioStreams: max(p.info.AudioStreams, 1), DurationKnown: known}
	job.EstimateBytes = sampleEstimate(job.totalSeconds(), p.opts.SampleResolution, p.opts.SampleQuality)
	p.add(job)
	return nil
}

func (p *previewPlanner) planImages(known bool) error {
	size := clampPreviewSize(p.info, p.opts.ImageResolution)
	if size == (PreviewSize{}) {
		return errors.New("preview source cannot fit even output dimensions")
	}
	times, capped := imageTimes(p.opts.ImageMode, p.info.DurationS, known, p.opts.ImageEvery.Seconds(), p.opts.MaxItems)
	if capped {
		p.warn("image series capped")
	}
	for i, at := range times {
		name, err := p.name("img", i+1, len(times), ".png")
		if err != nil {
			return err
		}
		p.add(PreviewJob{Kind: PreviewImage, Output: name, TimeS: at, Size: size,
			Quality: p.opts.ImageQuality, DurationKnown: known,
			EstimateBytes: previewBoxBytes[p.opts.ImageResolution]})
	}
	return nil
}

func (p *previewPlanner) add(job PreviewJob) {
	p.out.Jobs = append(p.out.Jobs, job)
	p.out.EstimateBytes += job.EstimateBytes
}

func (p *previewPlanner) warn(msg string) { p.out.Warnings = append(p.out.Warnings, msg) }

// name returns <stem>-<kind><NN><ext>, or <stem>-arxgo-<kind><NN><ext> when that name is taken.
func (p *previewPlanner) name(kind string, index, count int, ext string) (string, error) {
	width := 2
	if count > 99 {
		width = len(fmt.Sprint(count))
	}
	suffix := fmt.Sprintf("-%s%0*d%s", kind, width, index, ext)
	taken := func(s string) bool { return p.used[s] || p.occupied != nil && p.occupied(s) }
	name := p.stem + suffix
	if taken(name) {
		fallback := p.stem + "-arxgo" + suffix
		if taken(fallback) {
			return "", fmt.Errorf("preview output name occupied: %s", fallback)
		}
		p.warn("preview name collision: " + name + " -> " + fallback)
		name = fallback
	}
	p.used[name] = true
	return name, nil
}

func sampleRanges(mode string, d float64, known bool, length, every float64, limit int) ([]PreviewRange, bool) {
	switch {
	case !known:
		return []PreviewRange{{0, length}}, false
	case d <= length:
		return []PreviewRange{{0, d}}, false
	}
	switch mode {
	case "start":
		return []PreviewRange{{0, length}}, false
	case "middle":
		return []PreviewRange{{d/2 - length/2, length}}, false
	case "end":
		return []PreviewRange{{d - length, length}}, false
	}
	var out []PreviewRange
	for i := 0; i < limit && float64(i)*every < d; i++ {
		start := float64(i) * every
		out = append(out, PreviewRange{start, math.Min(length, d-start)})
	}
	return out, float64(limit)*every < d
}

func imageTimes(mode string, d float64, known bool, every float64, limit int) ([]float64, bool) {
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
	}
	out := []float64{math.Min(1, d/2)}
	for i := 1; i < limit && float64(i)*every < d; i++ {
		out = append(out, float64(i)*every)
	}
	return out, float64(limit)*every < d
}

// clampPreviewSize fits the display-oriented source in the requested box, without upscaling, then
// rounds both dimensions down to even encoder-compatible values. Both metadata readers report
// display-oriented dimensions and ffmpeg applies the rotation while decoding, so no swap is needed.
func clampPreviewSize(info MediaInfo, resolution string) PreviewSize {
	w, h := info.Width, info.Height
	box, ok := previewBoxes[resolution]
	if w <= 0 || h <= 0 || !ok {
		return PreviewSize{}
	}
	if h > w {
		box.Width, box.Height = box.Height, box.Width
	}
	scale := math.Min(1, math.Min(float64(box.Width)/float64(w), float64(box.Height)/float64(h)))
	width, height := int(math.Floor(float64(w)*scale)), int(math.Floor(float64(h)*scale))
	width -= width % 2
	height -= height % 2
	if width < 2 || height < 2 {
		return PreviewSize{}
	}
	return PreviewSize{width, height}
}

// Space estimates from the specification: bitrate by resolution times a quality factor for
// samples, a fixed size by resolution for images.
var (
	sampleBitrates  = map[string]float64{"sd": 1e6, "hd": 5e6, "4k": 20e6}
	qualityFactors  = map[string]float64{"low": 0.7, "medium": 1, "high": 1.5}
	previewBoxBytes = map[string]int64{"sd": 1 << 19, "hd": 3 << 20, "4k": 12 << 20}
)

func sampleEstimate(seconds float64, resolution, quality string) int64 {
	return int64(math.Ceil(seconds * sampleBitrates[resolution] * qualityFactors[quality] / 8))
}
