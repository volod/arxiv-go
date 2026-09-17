package report

import (
	"strings"
	"testing"
	"time"
)

// Regression: the edge test read single bytes, so a value ending in a Cyrillic letter whose last
// UTF-8 byte is 0x85 or 0xA0 (U+0445, U+0420) was taken for trailing U+0085 or U+00A0 and quoted.
func TestQuoteValueDecodesEdgeRunes(t *testing.T) {
	cases := []struct {
		value  string
		quoted bool
	}{
		{"fixture-\u0445", false},         // ends in U+0445 (D1 85)
		{"fixture \u0420", false},         // ends in U+0420 (D0 A0)
		{"\u0405-fixture.CATPart", false}, // starts with U+0405 (D0 85)
		{"\u0105", false},                 // U+0105 (C4 85)
		{"plain value", false},
		{"no-break\u00a0", true},
		{"\u2003em space first", true},
		{"tab\t", true},
		{`back\slash`, true},
	}
	for _, tc := range cases {
		got := quoteValue(tc.value)
		if quoted := strings.HasPrefix(got, `"`); quoted != tc.quoted {
			t.Errorf("quoteValue(%q) = %q, quoted %v, want %v", tc.value, got, quoted, tc.quoted)
		}
		back, err := unquoteValue(got)
		if err != nil || back != tc.value {
			t.Errorf("unquoteValue(%q) = %q, %v", got, back, err)
		}
	}
	rel := "cad/fixture-dir-\u0420/fixture-part.\u0445"
	got := RenderDescription(DescriptionInput{RelPath: rel, Archive: "/data/fixture-archive-\u0445",
		FileSize: 1, Modified: time.Unix(0, 0), MovedAt: time.Unix(0, 0)})
	lines := strings.Split(string(got), "\n")
	if lines[0] != DescriptionMarker+": "+rel || !strings.HasPrefix(lines[1], "archive: /data/") || strings.Contains(lines[1], `"`) {
		t.Errorf("marker and archive quoted:\n%s", got)
	}
}

// Regression: file_size was omitted for an empty file, although the contract writes it always. A
// CATIA file is selected by its name, so an empty .CATPart is moved and described.
func TestRenderDescriptionEmptyFileHasSize(t *testing.T) {
	got := string(RenderDescription(DescriptionInput{RelPath: "cad/empty.CATPart", Archive: "/data/archive",
		Modified: time.Unix(0, 0), MovedAt: time.Unix(0, 0), MovedTo: "/mnt/catia/cad/empty.CATPart"}))
	if lines := strings.Split(got, "\n"); len(lines) < 3 || lines[2] != "file_size: 0 (0 B)" {
		t.Errorf("empty file description:\n%s", got)
	}
	identity := TextIdentityOf(mustParseDescription(t, got), "cad/empty.CATPart.md")
	if identity.FileSize != "0 (0 B)" {
		t.Errorf("sidecar identity file_size = %q", identity.FileSize)
	}
}

func mustParseDescription(t *testing.T, s string) Description {
	t.Helper()
	d, err := ParseDescription(strings.NewReader(s))
	if err != nil {
		t.Fatal(err)
	}
	return d
}
