package report

import (
	"bytes"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/catia"
)

func TestCatiaLine(t *testing.T) {
	if got := CatiaLine(catia.Info{Kind: "CATProduct", Format: catia.FormatV5, Release: "V5R30 SP5", Components: make([]string, 12)}); got != "CATProduct | V5_CFV2 | V5R30 SP5 | 12 components" {
		t.Fatalf("got %q", got)
	}
	if got := CatiaLine(catia.Info{Kind: "CATPart", Format: catia.FormatV5, Release: "V5R30", Components: []string{"a"}}); got != "CATPart | V5_CFV2 | V5R30 | 1 component" {
		t.Fatalf("got %q", got)
	}
	if got := CatiaLine(catia.Info{Kind: "cgr"}); got != "cgr | unknown | unknown | 0 components" {
		t.Fatalf("got %q", got)
	}
}

func TestRenderCatiaDescription(t *testing.T) {
	in := DescriptionInput{
		RelPath:  "cad/fixture-part.CATPart",
		FileSize: 4096,
		FileMIME: "application/octet-stream",
		Modified: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		MovedAt:  time.Date(2026, 9, 15, 12, 5, 0, 0, time.UTC),
		MovedTo:  "/mnt/nas/catia/cad/fixture-part.CATPart",
		Catia: &catia.Info{
			Kind: catia.KindCATPart, Format: catia.FormatV5, Release: "V5R30 SP5",
		},
	}
	got := string(RenderDescription(in))
	if strings.Contains(got, "video:") || strings.Contains(got, "created:") {
		t.Fatalf("video fields:\n%s", got)
	}
	if !strings.Contains(got, "catia: CATPart | V5_CFV2 | V5R30 SP5 | 0 components\n") {
		t.Fatalf("catia line:\n%s", got)
	}
	if !strings.HasPrefix(got, "arxgo: cad/fixture-part.CATPart\n") {
		t.Fatalf("marker:\n%s", got)
	}
	if bytes.Contains([]byte(got), []byte("\n\n")) {
		t.Fatalf("blank line:\n%s", got)
	}
}

func TestRenderCatiaText(t *testing.T) {
	in := CatiaTextInput{
		RelPath:     "cad/fixture-product.CATProduct",
		ExtractedAt: time.Date(2026, 9, 15, 12, 5, 1, 0, time.UTC),
		Info: catia.Info{
			Format:     catia.FormatV5,
			Release:    "V5R30 SP5",
			BuildLevel: "2026-01-01.00.00",
			Product: catia.Product{PartNumber: "FIXTURE-100", Revision: "B", Definition: "fixture assembly",
				Nomenclature: " spaced ", Source: "made", Description: "invented \"quoted\" description", Material: "fixture alloy"},
			Components: []string{"fixture-part.CATPart", "fixture-sub.CATProduct"},
			Notes:      []string{"1. Invented first requirement.\n2. Invented second requirement.", "fixture note"},
		},
	}
	got := string(RenderCatiaText(in))
	want := "arxgo-text: cad/fixture-product.CATProduct\nfile_name: fixture-product.CATProduct\nextracted_at: 2026-09-15T12:05:01Z\ntruncated: false\n" +
		"properties:\n- release: V5R30 SP5\n- build_level: 2026-01-01.00.00\n- part_number: FIXTURE-100\n- revision: B\n" +
		"- definition: fixture assembly\n- nomenclature: \" spaced \"\n- source: made\n- description: \"invented \\\"quoted\\\" description\"\n" +
		"- material: fixture alloy\n" +
		"components:\n- fixture-part.CATPart\n- fixture-sub.CATProduct\n" +
		"notes:\n- \"1. Invented first requirement.\\n2. Invented second requirement.\"\n- fixture note\n"
	if got != want {
		t.Fatalf("got\n%s\nwant\n%s", got, want)
	}
}

func TestRenderCatiaTextCap(t *testing.T) {
	long := strings.Repeat("component-name-", 40) + ".CATPart" // ~ 40*15+8
	var comps []string
	for i := 0; i < 20000; i++ {
		comps = append(comps, long+"-"+strings.Repeat("x", 20))
	}
	got := RenderCatiaText(CatiaTextInput{
		RelPath:     "cad/fixture-product.CATProduct",
		ExtractedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		Info:        catia.Info{Format: catia.FormatV5, Release: "V5R30", Components: comps},
	})
	if len(got) > catiaTextCap {
		t.Fatalf("len %d", len(got))
	}
	if !strings.Contains(string(got), "truncated: true") {
		t.Fatalf("not truncated:\n%s", got[:200])
	}
	if !strings.HasSuffix(string(got), "\n") {
		t.Fatal("missing newline")
	}
}

func TestRenderCatiaText3DXMLProperties(t *testing.T) {
	got := string(RenderCatiaText(CatiaTextInput{
		RelPath:     "cad/fixture.3dxml",
		ExtractedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		Info: catia.Info{
			Format:        catia.FormatXML,
			Release:       "3DXML 4.3",
			SchemaVersion: "4.3",
			Title:         "fixture assembly",
			Author:        "fixture-author",
		},
	}))
	if strings.Contains(got, "- release:") {
		t.Fatalf("3dxml should not use V5 release property:\n%s", got)
	}
	if !strings.Contains(got, "- schema_version: 4.3\n") || !strings.Contains(got, "- title: fixture assembly\n") {
		t.Fatalf("3dxml properties:\n%s", got)
	}
}
