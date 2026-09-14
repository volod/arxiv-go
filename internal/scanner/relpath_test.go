package scanner

import "testing"

func TestLocalRelPath(t *testing.T) {
	for rel, want := range map[string]bool{
		"clip.mp4":                     true,
		"a/b/clip.mp4":                 true,
		"a/.arxgo/clip.mp4":            true, // reserved only directly under the root
		"a/arxgo-videos.csv":           true,
		"":                             false,
		"/etc/clip.mp4":                false,
		"../clip.mp4":                  false,
		"a/../../clip.mp4":             false,
		"a/./clip.mp4":                 false,
		"a//clip.mp4":                  false,
		"a/":                           false,
		".arxgo/lock":                  false,
		"arxgo-registry.csv":           false,
		"arxgo-videos.md":              false,
		"a/clip.mp4.arxgo-part":        false,
		"a/clip-smpl01.arxgo-part.mp4": false, // preview part
		"a/clip.arxgo-part.x.mp4":      true,
		".arxgo":                       false,
	} {
		if got := LocalRelPath(rel); got != want {
			t.Errorf("LocalRelPath(%q) = %v, want %v", rel, got, want)
		}
	}
}
