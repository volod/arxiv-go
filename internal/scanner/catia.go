package scanner

import (
	"strings"

	"github.com/volod/arxiv-go/internal/catia"
)

// IsCatiaExtension reports whether ext (optional leading dot, any case) is a built-in CATIA
// extension. A CATIA extension in --video-extensions is a usage error so the two payloads cannot
// select the same file.
func IsCatiaExtension(ext string) bool {
	return catia.IsExtension(ext)
}

// markCatia sets is_catia from the last dotted suffix and clears is_video. AppleDouble sidecars
// never call this: they are classified from magic before extension rules run.
func markCatia(ft *FileType, name string) {
	if !catia.IsExtension(fileExt(name)) {
		return
	}
	ft.IsCatia = true
	ft.IsVideo = false
	ft.IsMedia = ft.IsPicture || strings.HasPrefix(ft.MIME, "audio/")
}

// IsCatiaName reports whether a base file name has a built-in CATIA extension, the is_catia rule.
func IsCatiaName(name string) bool {
	return catia.IsExtension(fileExt(name))
}
