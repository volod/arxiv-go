package media

import (
	"math"
	"reflect"
	"slices"
	"strings"
	"testing"
	"time"
)

func planOptions(sample, image string) PreviewOptions {
	o := DefaultPreviewOptions()
	o.SampleMode, o.ImageMode = sample, image
	o.SampleEvery, o.ImageEvery = 5*time.Second, 5*time.Second
	return o
}

func video(duration float64, width, height int) MediaInfo {
	return MediaInfo{DurationS: duration, Width: width, Height: height, VideoStreams: 1}
}

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
		{"NaN skips", "end", "none", math.NaN(), 100, nil, nil, false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			o := planOptions(tc.sample, tc.image)
			o.MaxItems = tc.max
			p, err := PlanPreviews(video(tc.duration, 320, 240), "film.mp4", o, nil)
			if err != nil {
				t.Fatal(err)
			}
			var ranges []PreviewRange
			var times []float64
			for _, j := range p.Jobs {
				if j.Kind == PreviewSample {
					ranges = j.Ranges
				} else {
					times = append(times, j.TimeS)
				}
				if j.DurationKnown != (tc.duration > 0) {
					t.Errorf("%s DurationKnown = %v", j.Output, j.DurationKnown)
				}
			}
			if !reflect.DeepEqual(ranges, tc.wantRanges) || !reflect.DeepEqual(times, tc.wantTimes) {
				t.Errorf("ranges %v, times %v; want %v, %v", ranges, times, tc.wantRanges, tc.wantTimes)
			}
			warned := func(s string) bool {
				return slices.ContainsFunc(p.Warnings, func(w string) bool { return strings.Contains(w, s) })
			}
			if warned("capped") != tc.capped {
				t.Errorf("cap warnings = %v", p.Warnings)
			}
			if !(tc.duration > 0) && !warned("duration unknown") {
				t.Errorf("unknown duration warning missing: %v", p.Warnings)
			}
		})
	}
}

func TestPreviewClamp(t *testing.T) {
	cases := []struct {
		width, height int
		res           string
		want          PreviewSize
	}{
		{320, 240, "sd", PreviewSize{320, 240}},
		{1280, 720, "sd", PreviewSize{640, 360}},
		{4000, 2250, "hd", PreviewSize{1920, 1080}},
		{8000, 4500, "4k", PreviewSize{3840, 2160}},
		{240, 320, "sd", PreviewSize{240, 320}},
		{720, 1280, "sd", PreviewSize{360, 640}},
		{2250, 4000, "hd", PreviewSize{1080, 1920}},
		{4500, 8000, "4k", PreviewSize{2160, 3840}},
		{641, 359, "sd", PreviewSize{640, 358}},
		{319, 239, "sd", PreviewSize{318, 238}},
		{3, 1, "sd", PreviewSize{}},
	}
	for _, tc := range cases {
		if got := clampPreviewSize(MediaInfo{Width: tc.width, Height: tc.height}, tc.res); got != tc.want {
			t.Errorf("%dx%d %s: got %+v, want %+v", tc.width, tc.height, tc.res, got, tc.want)
		}
	}
}

func TestPreviewNamesAndCollisions(t *testing.T) {
	o := planOptions("series", "series")
	o.SampleEvery, o.ImageEvery, o.MaxItems = time.Second, time.Second, 101
	p, err := PlanPreviews(video(101, 1920, 1080), "dir/film.webm", o,
		func(s string) bool { return s == "film-smpl01.webm" || s == "film-img001.png" })
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Jobs) != 102 || p.Jobs[0].Output != "film-arxgo-smpl01.webm" {
		t.Fatalf("sample job: %+v", p.Jobs[0])
	}
	if p.Jobs[1].Output != "film-arxgo-img001.png" || p.Jobs[99].Output != "film-img099.png" || p.Jobs[101].Output != "film-img101.png" {
		t.Errorf("image names: %s %s %s", p.Jobs[1].Output, p.Jobs[99].Output, p.Jobs[101].Output)
	}
	if len(p.Warnings) != 2 {
		t.Errorf("collision warnings = %v", p.Warnings)
	}
	occupied := func(s string) bool { return strings.HasPrefix(s, "film") }
	if _, err := PlanPreviews(video(5, 320, 240), "film.mp4", o, occupied); err == nil {
		t.Error("occupied fallback was accepted")
	}
}

func TestPreviewSampleContainer(t *testing.T) {
	cases := []struct {
		source, output, video, audio string
		substituted                  bool
	}{
		{"a.mp4", "a-smpl01.mp4", "libx264", "aac", false},
		{"a.MOV", "a-smpl01.MOV", "libx264", "aac", false},
		{"a.mkv", "a-smpl01.mkv", "libx264", "aac", false},
		{"a.ts", "a-smpl01.ts", "libx264", "aac", false},
		{"a.webm", "a-smpl01.webm", "libvpx-vp9", "libopus", false},
		{"a.avi", "a-smpl01.avi", "mpeg4", "libmp3lame", false},
		{"a.mpg", "a-smpl01.mp4", "libx264", "aac", true},
		{"a.ogv", "a-smpl01.mp4", "libx264", "aac", true},
		{"a.bik", "a-smpl01.mp4", "libx264", "aac", true},
	}
	for _, tc := range cases {
		p, err := PlanPreviews(video(10, 320, 240), tc.source, planOptions("start", "none"), nil)
		if err != nil || len(p.Jobs) != 1 {
			t.Fatalf("%s: %+v, %v", tc.source, p, err)
		}
		j := p.Jobs[0]
		if j.Output != tc.output || j.VideoEncoder != tc.video || j.AudioEncoder != tc.audio {
			t.Errorf("%s: job %+v", tc.source, j)
		}
		if substituted := len(p.Warnings) == 1 && strings.Contains(p.Warnings[0], ".mp4"); substituted != tc.substituted {
			t.Errorf("%s: warnings %v", tc.source, p.Warnings)
		}
	}
}

func TestPreviewEstimate(t *testing.T) {
	o := planOptions("series", "series")
	o.SampleResolution, o.SampleQuality = "hd", "low"
	o.ImageResolution = "4k"
	p, err := PlanPreviews(video(12, 320, 240), "film.mp4", o, nil)
	if err != nil {
		t.Fatal(err)
	}
	// 12 sample seconds at 5 Mbit/s x 0.7, plus three 12 MiB 4k images.
	if want := int64(12*5e6*0.7/8) + 3*(12<<20); p.EstimateBytes != want {
		t.Errorf("estimate = %d, want %d", p.EstimateBytes, want)
	}
	if got := sampleEstimate(10, "4k", "high"); got != 37_500_000 {
		t.Errorf("high 4k estimate = %d", got)
	}
}

func TestPreviewSkipsAndInvalid(t *testing.T) {
	o := planOptions("middle", "start")
	for _, alter := range []func(*PreviewOptions){
		func(o *PreviewOptions) { o.MaxItems = 0 },
		func(o *PreviewOptions) { o.SampleEvery = 0 },
		func(o *PreviewOptions) { o.ImageMode = "bogus" },
		func(o *PreviewOptions) { o.SampleResolution = "8k" },
	} {
		invalid := o
		alter(&invalid)
		if _, err := PlanPreviews(video(5, 320, 240), "x.mp4", invalid, nil); err == nil {
			t.Errorf("invalid options accepted: %+v", invalid)
		}
	}
	if _, err := PlanPreviews(video(5, 320, 240), "noext", o, nil); err == nil {
		t.Error("source without extension accepted")
	}
	if _, err := PlanPreviews(video(5, 0, 0), "x.mp4", o, nil); err == nil {
		t.Error("missing dimensions accepted")
	}
	audioOnly := MediaInfo{DurationS: 5, HasAudio: true}
	if p, err := PlanPreviews(audioOnly, "x.mp4", o, nil); err != nil || len(p.Jobs) != 0 || len(p.Warnings) != 1 {
		t.Errorf("no video stream should skip with a warning: %+v, %v", p, err)
	}
	unknownNoSize := planOptions("middle", "end")
	if p, err := PlanPreviews(video(0, 0, 0), "x.mp4", unknownNoSize, nil); err != nil || len(p.Jobs) != 0 {
		t.Errorf("unknown duration without dimensions should skip: %+v, %v", p, err)
	}
}

func TestSampleEncodersAndArguments(t *testing.T) {
	job := PreviewJob{Kind: PreviewSample, Output: "clip.avi", Size: PreviewSize{640, 360},
		VideoEncoder: "libx264", AudioEncoder: "libmp3lame", HasAudio: true, Quality: "high"}
	video, audio, err := availableEncoders(job, map[string]bool{"mpeg4": true, "aac": true})
	if err != nil || video != "mpeg4" || audio != "aac" {
		t.Fatalf("fallback = %s, %s, %v", video, audio, err)
	}
	if _, _, err := availableEncoders(job, map[string]bool{"aac": true}); err == nil {
		t.Fatal("missing video encoder accepted")
	}
	vp9 := PreviewJob{VideoEncoder: "libvpx-vp9", AudioEncoder: "libopus", HasAudio: true}
	if _, _, err := availableEncoders(vp9, map[string]bool{"libvpx-vp9": true, "aac": true}); err == nil {
		t.Fatal("WebM accepted an audio encoder its container cannot hold")
	}
	joined := strings.Join(sampleEncodeArgs("source.avi", []PreviewRange{{0, 2}}, job, video, audio), " ")
	if !strings.Contains(joined, "-q:v 3") || !strings.Contains(joined, "-b:a 128k") ||
		!strings.Contains(joined, "-map 0:a:0") || strings.Contains(joined, "faststart") {
		t.Fatalf("args = %s", joined)
	}
	job.Output, job.AudioStream = "clip.MP4", 1
	joined = strings.Join(sampleEncodeArgs("s.mp4", []PreviewRange{{0, 1}, {2, 1}}, job, "libx264", "aac"), " ")
	if !strings.Contains(joined, "[0:a:1]asetpts") || !strings.Contains(joined, "[1:a:1]asetpts") ||
		!strings.Contains(joined, "concat=n=2:v=1:a=1") || !strings.Contains(joined, "-crf 20") ||
		!strings.Contains(joined, "-movflags +faststart") {
		t.Fatalf("series args = %s", joined)
	}
	if joined := strings.Join(sampleEncodeArgs("s.mp4", []PreviewRange{{0, 1}}, job, "libx264", "aac"), " "); !strings.Contains(joined, "-map 0:a:1") {
		t.Fatalf("single args = %s", joined)
	}
}
