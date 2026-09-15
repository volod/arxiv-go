package catia

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
)

func TestDetectFormat(t *testing.T) {
	cases := []struct {
		head []byte
		want string
	}{
		{[]byte("V5_CFV2\x00rest"), FormatV5},
		{[]byte{'P', 'K', 0x03, 0x04, 'x'}, FormatZIP},
		{[]byte("<?xml version"), FormatXML},
		{append(append([]byte{}, utf8BOM...), []byte("\n  <Model")...), FormatXML},
		{[]byte("  \t\r\n<Root"), FormatXML},
		{[]byte("not a catia file"), FormatUnknown},
		{nil, FormatUnknown},
	}
	for _, tc := range cases {
		if got := DetectFormat(tc.head); got != tc.want {
			t.Errorf("DetectFormat(%q) = %s, want %s", tc.head, got, tc.want)
		}
	}
}

func TestExtractV5ComponentsAndRelease(t *testing.T) {
	self := "fixture-product.CATProduct"
	data := v5File(
		v5LastSave(30, 5),
		v5Prop("MinimalVersionToRead", "CATIAV5R30"),
		v5Prop("CATBuildLevel", "2026-01-01.00.00"),
		v5Window(
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			componentChunk("fixture-product.CATProduct"),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			componentChunk("fixture-part.CATPart"),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			componentChunk("fixture-sub.CATProduct"),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			componentChunk("fixture-part.CATPart"),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			[]byte("CATOctetArray ignored"),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			[]byte("xfeat ignored\x00nameZ\""),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			[]byte("\x00missing-quote"),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			[]byte("quote-before-nul\"\x00nopeZ\""),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			[]byte("\x00bad/slash.CATPartZ\""),
			[]byte{0x01, ';', 0x01, 0x04, 'F', 'i', 'l', 'e'},
			componentChunk("fixture-extra.CATPart"),
		),
	)
	p := writeTemp(t, self, data)
	info := ExtractPath(context.Background(), p)
	if info.Err != nil || info.TextFailed {
		t.Fatalf("extract: %+v", info)
	}
	if info.Kind != KindCATProduct || info.Format != FormatV5 || info.Release != "V5R30 SP5" || info.BuildLevel != "2026-01-01.00.00" {
		t.Fatalf("meta: kind=%s format=%s release=%s build=%s", info.Kind, info.Format, info.Release, info.BuildLevel)
	}
	want := []string{"fixture-extra.CATPart", "fixture-part.CATPart", "fixture-sub.CATProduct"}
	if !slices.Equal(info.Components, want) {
		t.Fatalf("components %q, want %q", info.Components, want)
	}
}

func TestExtractV5MinimalVersionFallbackAndNoMarkers(t *testing.T) {
	withMin := v5File(v5Prop("MinimalVersionToRead", "CATIAV5R30"))
	info := Extract(context.Background(), bytes.NewReader(withMin), "fixture-part.CATPart")
	if info.Release != "V5R30" || info.Format != FormatV5 || info.Kind != KindCATPart {
		t.Fatalf("fallback: %+v", info)
	}
	if len(info.Components) != 0 {
		t.Fatalf("no markers yielded %q", info.Components)
	}

	bare := append([]byte("V5_CFV2\x00"), bytes.Repeat([]byte{0}, 64)...)
	info = Extract(context.Background(), bytes.NewReader(bare), "fixture-drawing.CATDrawing")
	if info.Release != ReleaseUnknown || info.Kind != KindCATDrawing || len(info.Components) != 0 {
		t.Fatalf("unknown release: %+v", info)
	}
}

func TestExtractV5OversizeWindowEmpty(t *testing.T) {
	var b bytes.Buffer
	b.Write(magicV5)
	b.Write(octetStart)
	b.Write(bytes.Repeat([]byte{'A'}, windowCap+1))
	b.Write(octetEnd)
	info := Extract(context.Background(), bytes.NewReader(b.Bytes()), "fixture-product.CATProduct")
	if info.Format != FormatV5 || len(info.Components) != 0 {
		t.Fatalf("oversize window: %+v", info)
	}
}

func TestExtractLargeStreamBounded(t *testing.T) {
	const n = 32 << 20
	r := io.MultiReader(bytes.NewReader(magicV5), io.LimitReader(zeroReader{}, n), bytes.NewReader(v5Prop("LastSaveVersion", lastSaveXML(30, 5))))
	info := Extract(context.Background(), r, "fixture-part.CATPart")
	if info.Err != nil {
		t.Fatal(info.Err)
	}
	if info.Release != "V5R30 SP5" {
		t.Fatalf("release %q", info.Release)
	}
	total := 0
	for _, s := range info.Strings {
		total += len(s)
	}
	if total > sidecarCap {
		t.Fatalf("strings collected %d bytes", total)
	}
}

func TestExtractCGRStrings(t *testing.T) {
	body := []byte("xxxx searchable text here yyyy")
	utf16 := utf16LE("utf16 searchable run")
	data := append(append([]byte("CGR!"), body...), utf16...)
	info := Extract(context.Background(), bytes.NewReader(data), "fixture.cgr")
	if info.Kind != KindCGR || info.Format != FormatUnknown {
		t.Fatalf("cgr meta: %+v", info)
	}
	if !contains(info.Strings, "searchable text here") && !containsPrefix(info.Strings, "searchable") {
		t.Fatalf("ascii harvest missing: %q", info.Strings)
	}
	if !contains(info.Strings, "utf16 searchable run") && !containsPrefix(info.Strings, "utf16 searchable") {
		t.Fatalf("utf16 harvest missing: %q", info.Strings)
	}
}

func TestExtractV5FirstPropertyWins(t *testing.T) {
	data := v5File(v5LastSave(30, 5), v5LastSave(28, 0), v5Prop("CATBuildLevel", "first"), v5Prop("CATBuildLevel", "second"))
	info := Extract(context.Background(), bytes.NewReader(data), "fixture-part.CATPart")
	if info.Release != "V5R30 SP5" || info.BuildLevel != "first" {
		t.Fatalf("first wins: %+v", info)
	}
}

func TestZipNameUnsafe(t *testing.T) {
	for _, name := range []string{"../secret.xml", "/abs.xml", `\abs.xml`, `C:\vol.xml`, `foo/../../x.xml`} {
		if !zipNameUnsafe(name) {
			t.Errorf("expected unsafe %q", name)
		}
	}
	for _, name := range []string{"Manifest.xml", "dir/part.3dxml", "foo.bar.xml"} {
		if zipNameUnsafe(name) {
			t.Errorf("expected safe %q", name)
		}
	}
}

func TestExtractEmptyAndUnreadable(t *testing.T) {
	info := Extract(context.Background(), bytes.NewReader(nil), "fixture-part.CATPart")
	if info.Kind != KindCATPart || info.Format != FormatUnknown || info.Release != ReleaseUnknown {
		t.Fatalf("empty: %+v", info)
	}
	dir := t.TempDir()
	p := filepath.Join(dir, "missing.CATPart")
	info = ExtractPath(context.Background(), p)
	if info.ErrorKind != ErrorKindRead || info.Err == nil {
		t.Fatalf("missing: %+v", info)
	}
}

func writeTemp(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func v5File(parts ...[]byte) []byte {
	var b bytes.Buffer
	b.Write(magicV5)
	b.Write(bytes.Repeat([]byte{0}, 32))
	for _, p := range parts {
		b.Write(p)
	}
	return b.Bytes()
}

func v5LastSave(release, sp int) []byte {
	return v5Prop("LastSaveVersion", lastSaveXML(release, sp))
}

func lastSaveXML(release, sp int) string {
	return "<Version>5/<Version><Release>" + itoa(release) + "/<Release><ServicePack>" + itoa(sp) + "/<ServicePack>"
}

func v5Prop(key, val string) []byte {
	var b []byte
	b = binary.LittleEndian.AppendUint16(b, uint16(len(key)))
	b = append(b, key...)
	b = binary.LittleEndian.AppendUint32(b, v5TypeString)
	b = binary.LittleEndian.AppendUint32(b, uint32(len(val)))
	b = append(b, val...)
	return b
}

func v5Window(parts ...[]byte) []byte {
	var b bytes.Buffer
	b.Write(octetStart)
	for _, p := range parts {
		b.Write(p)
	}
	b.Write(octetEnd)
	return b.Bytes()
}

func componentChunk(name string) []byte {
	var b []byte
	b = append(b, 0)
	b = append(b, `C:\cad\`...)
	b = append(b, name...)
	b = append(b, 'Z', '"')
	return b
}

func utf16LE(s string) []byte {
	out := make([]byte, 0, len(s)*2)
	for i := 0; i < len(s); i++ {
		out = append(out, s[i], 0)
	}
	return out
}

func contains(list []string, want string) bool {
	return slices.Contains(list, want)
}

func containsPrefix(list []string, prefix string) bool {
	for _, s := range list {
		if strings.Contains(s, prefix) {
			return true
		}
	}
	return false
}

func itoa(n int) string {
	if n == 0 {
		return "0"
	}
	var d [8]byte
	i := len(d)
	for n > 0 {
		i--
		d[i] = byte('0' + n%10)
		n /= 10
	}
	return string(d[i:])
}

type zeroReader struct{}

func (zeroReader) Read(p []byte) (int, error) {
	for i := range p {
		p[i] = 0
	}
	return len(p), nil
}
