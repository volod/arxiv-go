package media

import (
	"reflect"
	"strings"
	"testing"
)

func TestRequirements(t *testing.T) {
	cases := []struct {
		name  string
		needs Needs
		want  []Requirement
	}{
		{"file metadata needs nothing", Needs{}, nil},
		{"none previews need nothing", Needs{Sample: "none", Image: "none"}, nil},
		{"media metadata", Needs{MetadataMedia: true}, []Requirement{{FFprobe, []string{"--metadata media"}}}},
		{"sample", Needs{Sample: "series"}, []Requirement{
			{FFprobe, []string{"--sample series"}}, {FFmpeg, []string{"--sample series"}}}},
		{"all", Needs{MetadataMedia: true, Sample: "start", Image: "end"}, []Requirement{
			{FFprobe, []string{"--metadata media", "--sample start", "--image end"}},
			{FFmpeg, []string{"--sample start", "--image end"}}}},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := Requirements(tc.needs); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("Requirements(%+v) = %+v, want %+v", tc.needs, got, tc.want)
			}
		})
	}
}

func TestGuidanceByPlatform(t *testing.T) {
	probe := []Requirement{{FFprobe, []string{"--metadata media"}}}
	cases := []struct {
		goos, goarch string
		want         []string
		absent       string
	}{
		{"linux", "amd64", []string{
			"for linux/amd64 (it includes ffprobe)",
			"  https://johnvansickle.com/ffmpeg/\n",
			"  https://ffmpeg.org/download.html#build-linux\n",
			"place ffprobe next to arxgo or add it to PATH",
		}, "gyan.dev"},
		{"windows", "amd64", []string{
			"for windows/amd64 (it includes ffprobe.exe)",
			"  https://www.gyan.dev/ffmpeg/builds/\n",
			"  https://github.com/BtbN/FFmpeg-Builds/releases\n",
			"place ffprobe.exe next to arxgo.exe or add it to PATH",
		}, "johnvansickle"},
		{"darwin", "arm64", []string{"  https://ffmpeg.org/download.html\n"}, "johnvansickle"},
		{"linux", "arm64", []string{"  https://ffmpeg.org/download.html\n"}, "johnvansickle"},
	}
	for _, tc := range cases {
		t.Run(tc.goos+"/"+tc.goarch, func(t *testing.T) {
			got := Guidance(probe, tc.goos, tc.goarch)
			for _, w := range tc.want {
				if !strings.Contains(got, w) {
					t.Errorf("guidance does not contain %q:\n%s", w, got)
				}
			}
			if strings.Contains(got, tc.absent) {
				t.Errorf("guidance contains %q:\n%s", tc.absent, got)
			}
		})
	}
	both := Guidance([]Requirement{{Tool: FFprobe}, {Tool: FFmpeg}}, "windows", "amd64")
	if !strings.Contains(both, "place ffprobe.exe and ffmpeg.exe next to arxgo.exe") {
		t.Errorf("two tools:\n%s", both)
	}
}
