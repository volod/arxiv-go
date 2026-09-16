package report

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/catia"
)

func writeTemp(t *testing.T, name, body string) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestReadCatiaTextRoundTripsRenderedSidecar(t *testing.T) {
	rel := "cad/\"odd\" name.CATProduct"
	body := RenderCatiaText(CatiaTextInput{
		RelPath: rel, Archive: "/data/archive", ExtractedAt: time.Date(2026, 9, 16, 10, 0, 0, 0, time.UTC),
		Identity: TextIdentity{FileSize: "8 (8 B)", Catia: "CATProduct | V5_CFV2 | V5R30 | 2 components", Description: "cad/x.md"},
		Info: catia.Info{Format: catia.FormatV5, Release: "V5R30", BuildLevel: " spaced ",
			Components: []string{"a.CATPart", "b\\c.CATPart"}, Strings: []string{"assembly note", "line\nbreak"}},
	})
	p := writeTemp(t, "s.text.md", string(body)+"operator appendix\n- not an item of ours\n")
	text, err := ReadCatiaText(p, rel, true)
	if err != nil {
		t.Fatal(err)
	}
	if text.Fields[TextSidecarMarker] != rel || text.Fields["archive"] != "/data/archive" || text.Fields["truncated"] != "false" ||
		text.Fields["description"] != "cad/x.md" || text.Fields["catia"] != "CATProduct | V5_CFV2 | V5R30 | 2 components" {
		t.Fatalf("fields %v", text.Fields)
	}
	if strings.Join(text.Properties, "|") != `release: V5R30|build_level: " spaced "` ||
		strings.Join(text.Components, "|") != `a.CATPart|"b\\c.CATPart"` ||
		strings.Join(text.Strings, "|") != `assembly note|"line\nbreak"` {
		t.Fatalf("blocks %q %q %q", text.Properties, text.Components, text.Strings)
	}
	if id := TextIdentityOfSidecar(text); id.Description != "" || id.FileSize != "8 (8 B)" {
		t.Fatalf("identity %+v", id)
	}
	noStrings, err := ReadCatiaText(p, rel, false)
	if err != nil || noStrings.Strings != nil || len(noStrings.Components) != 2 {
		t.Fatalf("without strings: %+v %v", noStrings, err)
	}
}

func TestReadCatiaTextRejectsForeignFiles(t *testing.T) {
	for name, body := range map[string]string{
		"empty":         "",
		"other file":    "arxgo-text: other.CATPart\n",
		"description":   "arxgo: fixture.CATPart\n",
		"operator note": "notes about fixture.CATPart\n",
	} {
		p := writeTemp(t, "s.text.md", body)
		if _, err := ReadCatiaText(p, "fixture.CATPart", false); !errors.Is(err, ErrNotDescription) {
			t.Errorf("%s: err %v", name, err)
		}
	}
	bom := writeTemp(t, "bom.text.md", "\uFEFFarxgo-text: fixture.CATPart\ntruncated: true\n")
	if text, err := ReadCatiaText(bom, "fixture.CATPart", false); err != nil || text.Fields["truncated"] != "true" {
		t.Fatalf("BOM sidecar: %+v %v", text, err)
	}
	long := writeTemp(t, "long.text.md", "arxgo-text: fixture.CATPart\nstrings:\n- "+strings.Repeat("x", catiaTextLineLimit)+"\n")
	if _, err := ReadCatiaText(long, "fixture.CATPart", true); err == nil || errors.Is(err, ErrNotDescription) {
		t.Fatalf("over-long line: %v", err)
	}
	if _, err := ReadCatiaText(filepath.Join(t.TempDir(), "absent"), "fixture.CATPart", false); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("absent: %v", err)
	}
}

func TestWriteCatiaIndexDocument(t *testing.T) {
	var b bytes.Buffer
	at := time.Date(2026, 9, 16, 10, 0, 0, 0, time.FixedZone("x", 3600))
	must := func(err error) {
		if err != nil {
			t.Fatal(err)
		}
	}
	must(WriteCatiaIndexHeader(&b, CatiaIndexHeader{Archive: "/data/archive", CatiaArchive: "/mnt/catia", HistoryAt: at,
		Files: 2, Components: 1, MissingText: 1}))
	must(WriteCatiaIndexSection(&b, CatiaIndexSection{RelPath: "cad/a.CATProduct", Text: "cad/a.CATProduct.text.md", Truncated: "false",
		Identity:  TextIdentity{Catia: "CATProduct | V5_CFV2 | unknown | 1 component", Description: "cad/a.CATProduct.md"},
		CatiaText: CatiaText{Properties: []string{"release: V5R30"}, Components: []string{"b.CATPart"}, Strings: []string{"note text"}}}))
	must(WriteCatiaIndexSection(&b, CatiaIndexSection{RelPath: "new\nline.CATPart"}))
	must(WriteMissingText(&b, []MissingText{{RelPath: "new\nline.CATPart", Reason: MissingNotRecorded}}))
	want := "# arxgo CATIA text index\n\narchive: /data/archive\ncatia_archive: /mnt/catia\nhistory_at: 2026-09-16T09:00:00Z\n" +
		"files: 2\ncomponents: 1\nmissing_text: 1\n" +
		"\n## cad/a.CATProduct\n\nfile_name: a.CATProduct\ncatia: CATProduct | V5_CFV2 | unknown | 1 component\n" +
		"description: cad/a.CATProduct.md\ntext: cad/a.CATProduct.text.md\ntruncated: false\n" +
		"properties:\n- release: V5R30\ncomponents:\n- b.CATPart\nstrings:\n- note text\n" +
		"\n## \"new\\nline.CATPart\"\n\nfile_name: \"new\\nline.CATPart\"\n" +
		"\n## Missing text\n\n- not_recorded: \"new\\nline.CATPart\"\n"
	if b.String() != want {
		t.Fatalf("index:\n%s\nwant:\n%s", b.String(), want)
	}
	var empty bytes.Buffer
	must(WriteMissingText(&empty, nil))
	if empty.Len() != 0 {
		t.Fatalf("empty Missing text section: %q", empty.String())
	}
}

func TestHasArxgoMarker(t *testing.T) {
	for body, want := range map[string]bool{
		"arxgo: a.CATPart\n": true, "arxgo-text: a.CATPart\n": true, "\uFEFFarxgo: a\n": true,
		"# arxgo CATIA text index\n": false, "arxgo:a\n": false, "": false,
	} {
		if got, err := HasArxgoMarker(writeTemp(t, "f.md", body)); err != nil || got != want {
			t.Errorf("%q: %v %v", body, got, err)
		}
	}
	if got, err := HasArxgoMarker(filepath.Join(t.TempDir(), "absent.md")); err != nil || got {
		t.Errorf("absent: %v %v", got, err)
	}
}
