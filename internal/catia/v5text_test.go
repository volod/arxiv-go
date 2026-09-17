package catia

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"reflect"
	"slices"
	"strings"
	"testing"
	"testing/iotest"
)

// dict encodes a V5 dictionary string in the short form up to 48 bytes and the long form above.
func dict(s string) []byte {
	if len(s) < dictShortMax {
		return append([]byte{byte(len(s) + 1)}, s...)
	}
	b := binary.LittleEndian.AppendUint32([]byte{0}, uint32(len(s)))
	return append(b, s...)
}

func dictRun(strs ...string) []byte {
	var b []byte
	for _, s := range strs {
		b = append(b, dict(s)...)
	}
	return b
}

// rtf wraps invented text runs in the RTF header CATIA writes.
func rtf(body string) string {
	return `{{\fonttbl{\f1 FixtureFont;}{\f2 Fixture Letter;}}{\colortbl\red0\green0\blue0;}` + body + `}`
}

func extractBytes(data []byte, name string) Info {
	return Extract(context.Background(), bytes.NewReader(data), name)
}

func TestExtractV5ProductProperties(t *testing.T) {
	description := "invented description long enough for the long form of a dictionary string"
	data := v5File(
		[]byte("\x0bASMPRODUC noise"),
		dictRun("ASMPRODUCT", "FIXTURE-100", "CATMechProdCont", "_ViewsList", "_Connectors", "_Revision", "B",
			"_RepresentedBy", "_Component", "_Source", "made", "_PartNumber", "_Definition", "fixture assembly",
			"_Nomenclature", "fixture nomenclature", "_DescriptionRef", description, "_BagRepsList",
			"_Revision", "value of a child"),
		dictRun("String", "Material", "fixture alloy αβ"),
		dictRun("String", "Material", "second material"),
	)
	info := extractBytes(data, "fixture-product.CATProduct")
	want := Product{PartNumber: "FIXTURE-100", Revision: "B", Definition: "fixture assembly",
		Nomenclature: "fixture nomenclature", Source: "made", Description: description, Material: "fixture alloy αβ"}
	if info.Product != want {
		t.Fatalf("product %+v, want %+v", info.Product, want)
	}
}

func TestExtractV5ProductPropertiesLeftOut(t *testing.T) {
	cases := []struct {
		name string
		data []byte
	}{
		{"parameter names, unknown source, attributes without values", v5File(
			dictRun("ASMPRODUCT", "_ViewsList", "_Revision", "Revision", "_Source", "Unknown", "_PartNumber",
				"Part Number", "CATCkeValby", "_Definition", "Definition", "_Nomenclature", "Nomenclature",
				"_DescriptionRef", "Product Description", "_BagRepsList"),
			dictRun("String", "Material", "None"))},
		{"run ends at an invalid string", v5File(
			dictRun("ASMPRODUCT", "_Revision"), []byte{0xFF, 'x'}, dictRun("C", "_Definition", "lost"),
			dictRun("String", "Material", "_Attribute"))},
		{"run cut by the end of the file", v5File(dictRun("ASMPRODUCT"), []byte{40, 'p', 'a', 'r'})},
		{"string with a control character", v5File(dictRun("ASMPRODUCT", "bad\x01value", "_Revision", "B"))},
		{"no marker", v5File(dictRun("ASMPRODUCTX", "FIXTURE-100", "_Revision", "B"))},
	}
	for _, tc := range cases {
		info := extractBytes(tc.data, "fixture-part.CATPart")
		if info.Product != (Product{}) {
			t.Errorf("%s: product %+v", tc.name, info.Product)
		}
	}
}

func TestExtractV5ProductRunLimits(t *testing.T) {
	strs := []string{"ASMPRODUCT", "FIXTURE-1"}
	for i := 0; i < dictMaxStrings; i++ {
		strs = append(strs, "_Filler")
	}
	info := extractBytes(v5File(dictRun(append(strs, "_Revision", "B")...)), "fixture-part.CATPart")
	if info.Product.PartNumber != "FIXTURE-1" || info.Product.Revision != "" {
		t.Fatalf("string limit: %+v", info.Product)
	}
	long := strings.Repeat("d", dictWindow)
	info = extractBytes(v5File(dictRun("ASMPRODUCT", "FIXTURE-2", "_DescriptionRef", long)), "fixture-part.CATPart")
	if info.Product.PartNumber != "FIXTURE-2" || info.Product.Description != "" {
		t.Fatalf("window limit: %+v", info.Product)
	}
}

func TestExtractV5Notes(t *testing.T) {
	requirement := rtf(`{\ql\fs5000\f2\cf1\i\expnd0 1. Invented first requirement.\par}{\ql\fs5000\f2\cf1\i\expnd0   2. Invented second requirement. }`)
	field := rtf(`{\field{\*\dsindex1}{\fldrslt {\ql\fs5000\f2\cf1\i\expnd0 Fixture field}}}{\ql\fs5000\f2\cf1\i\expnd0 \symb <DEGREE> \{kept\} a\\b}`)
	reformatted := rtf(`{\qc\fs3500\f2\cf1\b\expnd0 1. Invented first requirement.\par}{\qc 2. Invented second requirement.}`)
	unicodeNote := rtf(`{\ql fixture \u945?\u946? note\tab end}`)
	data := v5File(
		dictRun("DrwText", "RTFString", requirement),
		dict(`{\rtf1 fixture short}`),
		dictRun(rtf(`{\ql 12}`), rtf(`{\ql A}`), rtf(`{\ql XXX}`), rtf(`{\ql 0<DEGREE>}`), rtf(`{\ql Ab 1,2}`)),
		dict(field),
		dict(reformatted),
		[]byte{0, 0xFF, 0xFF, 0, 0}, []byte(rtf(`{\ql invented unprefixed}`)),
		dict(`{\rtf1 invented unterminated`),
		dict(rtf("{\\ql invented \xff invalid}")),
		dict(unicodeNote),
	)
	info := extractBytes(data, "fixture-drawing.CATDrawing")
	want := []string{
		"1. Invented first requirement.\n2. Invented second requirement.",
		"fixture short",
		"Fixture field<DEGREE> {kept} a\\b",
		"fixture αβ note end",
	}
	if !slices.Equal(info.Notes, want) {
		t.Fatalf("notes %q\nwant %q", info.Notes, want)
	}
	if info.Truncated {
		t.Fatal("truncated without reaching the cap")
	}
}

func TestExtractV5TextAtEveryChunkBoundary(t *testing.T) {
	data := v5File(
		v5LastSave(30, 5),
		dictRun("String", "Material", "fixture alloy"),
		dictRun("ASMPRODUCT", "FIXTURE-100", "_Revision", "B", "_DescriptionRef", strings.Repeat("fixture description ", 4), "_BagRepsList"),
		dict(rtf(`{\ql invented note one}`)),
		dict(`{\rtf1 note two}`),
		dict(rtf(`{\ql invented note three\par second line}`)),
		v5Window([]byte{0x01, 0x04, 'F', 'i', 'l', 'e'}, componentChunk("fixture-part.CATPart")),
	)
	whole := extractBytes(data, "fixture-product.CATProduct")
	if len(whole.Notes) != 3 || whole.Product.Description == "" || whole.Product.Material == "" || len(whole.Components) != 1 {
		t.Fatalf("whole read: %+v", whole)
	}
	readers := map[string]func(io.Reader) io.Reader{"one byte": iotest.OneByteReader, "half": iotest.HalfReader}
	for size := 2; size <= 17; size++ {
		readers["chunks of "+itoa(size)] = func(r io.Reader) io.Reader { return &chunkReader{r: r, n: size} }
	}
	for name, wrap := range readers {
		got := Extract(context.Background(), wrap(bytes.NewReader(data)), "fixture-product.CATProduct")
		if !reflect.DeepEqual(got, whole) {
			t.Errorf("%s: %+v\nwant %+v", name, got, whole)
		}
	}
}

func TestExtractV5NotesCap(t *testing.T) {
	var b bytes.Buffer
	b.Write(v5File())
	for i := 0; b.Len() < 2*sidecarCap; i++ {
		b.Write(dict(rtf(`{\ql invented note ` + itoa(i) + " " + strings.Repeat("text ", 200) + `}`)))
	}
	info := extractBytes(b.Bytes(), "fixture-drawing.CATDrawing")
	total := 0
	for _, n := range info.Notes {
		total += len(n)
	}
	if !info.Truncated || total > sidecarCap || total < sidecarCap-2<<10 {
		t.Fatalf("truncated=%v total=%d", info.Truncated, total)
	}
}

func TestExtractOtherFormatsHaveNoText(t *testing.T) {
	planted := slices.Concat([]byte("CGR invented geometry "), dictRun("ASMPRODUCT", "FIXTURE-100", "String", "Material", "fixture alloy"),
		dict(rtf(`{\ql invented note}`)))
	for _, name := range []string{"fixture.cgr", "fixture-part.CATPart"} {
		info := extractBytes(planted, name)
		if info.Format != FormatUnknown || info.Product != (Product{}) || len(info.Notes) != 0 || len(info.Components) != 0 {
			t.Errorf("%s: %+v", name, info)
		}
	}
}

func TestRTFPlain(t *testing.T) {
	cases := []struct{ in, want string }{
		{rtf(`{\ql one\par two\line three}`), "one\ntwo\nthree"},
		{`{\rtf1{\info{\title hidden}}{\stylesheet{\s0 hidden;}}{\*\generator hidden}shown}`, "shown"},
		{`{\rtf1 \infoword kept}`, "kept"},
		{"{\\rtf1 raw\r\nbreak}", "rawbreak"},
		{`{\rtf1 a\'41\'e9b}`, "aAb"},
		{`{\rtf1 \u-3913?x\u8364\'80y}`, "\uf0b7x\u20acy"},
		{`{\rtf1 minus\- \~ \fs-20 x}`, "minus  x"},
		{`{\rtf1 {}\par\par   \par end\`, "end"},
	}
	for _, tc := range cases {
		if got := rtfPlain(tc.in); got != tc.want {
			t.Errorf("rtfPlain(%q) = %q, want %q", tc.in, got, tc.want)
		}
	}
}

func TestNoteText(t *testing.T) {
	for text, want := range map[string]bool{
		"Fix.": true, "αβγ": true, "1. ab c": true, "<DIAMETER>10 hole": true,
		"12": false, "A": false, "XXX": false, "xXx x": false, "0<DEGREE>": false, "Ab 1,2": false,
		"<SYMBOL28>": false, "<ABC": true, "": false,
	} {
		if got := noteText(text); got != want {
			t.Errorf("noteText(%q) = %v, want %v", text, got, want)
		}
	}
}

// chunkReader returns at most n bytes per Read.
type chunkReader struct {
	r io.Reader
	n int
}

func (c *chunkReader) Read(p []byte) (int, error) {
	return c.r.Read(p[:min(len(p), c.n)])
}
