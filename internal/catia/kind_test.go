package catia

import "testing"

func TestKindOfTable(t *testing.T) {
	cases := []struct {
		ext  string
		kind string
		ok   bool
	}{
		{"CATPart", KindCATPart, true},
		{".catpart", KindCATPart, true},
		{" CATProduct ", KindCATProduct, true},
		{"catdrawing", KindCATDrawing, true},
		{".CGR", KindCGR, true},
		{"3dxml", Kind3DXML, true},
		{".3DXML", Kind3DXML, true},
		{"mp4", "", false},
		{"catpart.bak", "", false},
		{"", "", false},
		{".", "", false},
	}
	for _, tc := range cases {
		got, ok := KindOf(tc.ext)
		if got != tc.kind || ok != tc.ok || IsExtension(tc.ext) != tc.ok {
			t.Errorf("KindOf(%q) = %q, %v; IsExtension=%v", tc.ext, got, ok, IsExtension(tc.ext))
		}
	}
}
