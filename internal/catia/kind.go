package catia

import "strings"

// Kind tokens from the built-in extension table.
const (
	KindCATPart    = "CATPart"
	KindCATProduct = "CATProduct"
	KindCATDrawing = "CATDrawing"
	KindCGR        = "cgr"
	Kind3DXML      = "3dxml"
)

// kindByExt maps a lower-case extension without a leading dot to its kind token.
var kindByExt = map[string]string{
	"catpart":    KindCATPart,
	"catproduct": KindCATProduct,
	"catdrawing": KindCATDrawing,
	"cgr":        KindCGR,
	"3dxml":      Kind3DXML,
}

// KindOf returns the CATIA kind token for an extension. The extension may carry a leading
// dot and any case; comparison uses the last dotted suffix the same way as video extensions.
func KindOf(ext string) (string, bool) {
	k, ok := kindByExt[normExt(ext)]
	return k, ok
}

// IsExtension reports whether ext is a built-in CATIA extension.
func IsExtension(ext string) bool {
	_, ok := KindOf(ext)
	return ok
}

func normExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}
