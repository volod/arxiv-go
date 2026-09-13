package media

import (
	"math"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestPreviewPositions(t *testing.T) {
	cases := []struct {
		name, sample, image string
		duration            float64
		max                 int
		wantRanges          []PreviewRange
		wantTimes           []float64
		capped              bool
	}{
		{"start", "start", "start", 20, 100, []PreviewRange{{0, 5}}, []float64{1}, false},
		{"middle", "middle", "middle", 20, 100, []PreviewRange{{7.5, 5}}, []float64{10}, false},
		{"end", "end", "end", 20, 100, []PreviewRange{{15, 5}}, []float64{19}, false},
		{"short", "end", "start", 3, 100, []PreviewRange{{0, 3}}, []float64{1}, false},
		{"tiny", "middle", "end", 0.5, 100, []PreviewRange{{0, 0.5}}, []float64{0}, false},
		{"series", "series", "series", 12, 100, []PreviewRange{{0, 5}, {5, 5}, {10, 2}}, []float64{1, 5, 10}, false},
		{"capped", "series", "series", 20, 2, []PreviewRange{{0, 5}, {5, 5}}, []float64{1, 5}, true},
		{"unknown start", "start", "start", 0, 100, []PreviewRange{{0, 5}}, []float64{1}, false},
		{"unknown skips", "middle", "series", 0, 100, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := DefaultPreviewOptions()
			o.SampleMode, o.ImageMode = tc.sample, tc.image
			o.SampleEvery, o.ImageEvery = 5*time.Second, 5*time.Second
			o.MaxItems = tc.max
			p, err := PlanPreviews(MediaInfo{DurationS: tc.duration, Width: 320, Height: 240}, "film.mp4", o, nil)
			if err != nil {
				t.Fatal(err)
			}
			var ranges []PreviewRange
			var times []float64
			for _, j := range p.Jobs {
				if j.Kind == "sample" {
					ranges = j.Ranges
				} else {
					times = append(times, j.TimeS)
				}
			}
			if !reflect.DeepEqual(ranges, tc.wantRanges) || !reflect.DeepEqual(times, tc.wantTimes) {
				t.Errorf("ranges %v, times %v; want %v, %v", ranges, times, tc.wantRanges, tc.wantTimes)
			}
			if p.SampleCapped != tc.capped || p.ImageCapped != tc.capped {
				t.Errorf("caps: %+v", p)
			}
			if tc.duration == 0 && len(p.Warnings) == 0 {
				t.Error("unknown duration warning missing")
			}
		})
	}
}

func TestPreviewClamp(t *testing.T) {
	cases := []struct {
		width, height, rotation int
		res                     string
		want                    PreviewSize
	}{
		{320, 240, 0, "sd", PreviewSize{320, 240}},
		{1280, 720, 0, "sd", PreviewSize{640, 360}},
		{4000, 2250, 0, "hd", PreviewSize{1920, 1080}},
		{8000, 4500, 0, "4k", PreviewSize{3840, 2160}},
		{240, 320, 0, "sd", PreviewSize{240, 320}},
		{720, 1280, 0, "sd", PreviewSize{360, 640}},
		{2250, 4000, 0, "hd", PreviewSize{1080, 1920}},
		{4500, 8000, 0, "4k", PreviewSize{2160, 3840}},
		{1280, 720, 90, "sd", PreviewSize{360, 640}},
		{641, 359, 0, "sd", PreviewSize{640, 358}},
		{319, 239, 0, "sd", PreviewSize{318, 238}},
	}
	for _, tc := range cases {
		got := ClampPreviewSize(MediaInfo{Width: tc.width, Height: tc.height, Rotation: tc.rotation}, tc.res)
		if got != tc.want {
			t.Errorf("%+v %s: got %+v, want %+v", tc, tc.res, got, tc.want)
		}
	}
}

func TestPreviewNamesEncodersAndEstimate(t *testing.T) {
	o := DefaultPreviewOptions()
	o.SampleMode, o.ImageMode = "series", "series"
	o.SampleEvery, o.ImageEvery, o.MaxItems = time.Second, time.Second, 101
	p, err := PlanPreviews(MediaInfo{DurationS: 101, Width: 1920, Height: 1080, Container: "webm"}, "dir/film.webm", o,
		func(s string) bool { return s == "film-smpl01.webm" || s == "film-img001.png" })
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Jobs) != 102 || p.Jobs[0].Output != "film-arxgo-smpl01.webm" || p.Jobs[0].VideoEncoder != "libvpx-vp9" || p.Jobs[0].AudioEncoder != "libopus" {
		t.Fatalf("sample job: %+v", p.Jobs[0])
	}
	if p.Jobs[1].Output != "film-arxgo-img001.png" || p.Jobs[99].Output != "film-img099.png" || p.Jobs[101].Output != "film-img101.png" {
		t.Errorf("image names: %s %s %s", p.Jobs[1].Output, p.Jobs[99].Output, p.Jobs[101].Output)
	}
	if p.EstimateBytes != 495*125000+101*(1<<19) {
		t.Errorf("estimate = %d", p.EstimateBytes)
	}
	if len(p.Warnings) != 2 {
		t.Errorf("collision warnings = %v", p.Warnings)
	}
	if video, audio := PreviewEncoders("webm", ".mp4"); video != "libvpx-vp9" || audio != "libopus" {
		t.Errorf("container codec precedence = %s, %s", video, audio)
	}
	_, err = PlanPreviews(MediaInfo{DurationS: 5, Width: 320, Height: 240}, "film.mp4", o, func(s string) bool { return strings.HasPrefix(s, "film") })
	if err == nil {
		t.Error("occupied fallback was accepted")
	}
}

func TestPreviewEstimateFactorsAndInvalid(t *testing.T) {
	if got := estimateSample(10, "hd", "low"); got != 4_375_000 {
		t.Errorf("low hd estimate = %d", got)
	}
	if got := estimateSample(10, "4k", "high"); got != 37_500_000 {
		t.Errorf("high 4k estimate = %d", got)
	}
	if got := imageEstimate("hd"); got != 3<<20 {
		t.Errorf("hd image = %d", got)
	}
	o := DefaultPreviewOptions()
	for _, alter := range []func(*PreviewOptions){func(o *PreviewOptions) { o.MaxItems = 0 }, func(o *PreviewOptions) { o.SampleEvery = 0 }, func(o *PreviewOptions) { o.ImageMode = "bogus" }} {
		invalid := o
		alter(&invalid)
		if _, err := PlanPreviews(MediaInfo{}, "x.mp4", invalid, nil); err == nil {
			t.Error("invalid options accepted")
		}
	}
	o.SampleMode = "middle"
	p, err := PlanPreviews(MediaInfo{DurationS: math.NaN(), Width: 320, Height: 240}, "x.mp4", o, nil)
	if err != nil || len(p.Jobs) != 0 {
		t.Errorf("NaN duration: %+v %v", p, err)
	}
	if _, err := PlanPreviews(MediaInfo{DurationS: 5}, "x.mp4", o, nil); err == nil {
		t.Error("missing dimensions accepted")
	}
	p, err = PlanPreviews(MediaInfo{}, "x.mp4", o, nil)
	if err != nil || len(p.Jobs) != 0 || len(p.Warnings) == 0 {
		t.Errorf("unknown duration without dimensions should skip: %+v, %v", p, err)
	}
}
