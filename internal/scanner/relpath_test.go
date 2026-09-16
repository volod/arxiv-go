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
		"arxgo-catia.csv":              false,
		"a/arxgo-catia.csv":            true,
		"a/clip.mp4.arxgo-part":        false,
		"a/clip-smpl01.arxgo-part.mp4": false, // preview part
		"a/clip.arxgo-part.x.mp4":      true,
		".arxgo":                       false,
		"arxgo-videos.restored-20260913T101500Z-1a2b3c4d.csv":  false, // retired by restore
		"arxgo-catia.restored-20260913T101500Z-1a2b3c4d.csv":   false,
		"a/arxgo-catia.restored-20260913T101500Z-1a2b3c4d.csv": true,
		"arxgo-videos.restored-notes.csv":                      true,
	} {
		if got := LocalRelPath(rel); got != want {
			t.Errorf("LocalRelPath(%q) = %v, want %v", rel, got, want)
		}
	}
}
