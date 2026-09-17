package catia

import (
	"archive/zip"
	"bytes"
	"context"
	"io"
	"slices"
	"strings"
	"testing"
)

const sample3DXML = `<?xml version="1.0" encoding="utf-8"?>
<Model_3dxml>
  <Header>
    <SchemaVersion>4.3</SchemaVersion>
    <Title>fixture assembly</Title>
    <Author>fixture-author</Author>
    <Generator>3dxml fixture</Generator>
    <Created>2026-01-02</Created>
  </Header>
  <ProductStructure>
    <Reference3D name="FixtureRoot"/>
    <Instance3D name="FixtureInst"/>
    <ReferenceRep associatedFile="urn:3DXML:fixture-part.3dxml" format="TESSELLATED"/>
    <ReferenceRep associatedFile="http://example.invalid/skip.3dxml"/>
    <ReferenceRep associatedFile="https://example.invalid/skip2.3dxml"/>
    <ReferenceRep associatedFile="fixture-other.3dxml"/>
  </ProductStructure>
</Model_3dxml>`

func TestExtractXML3DXML(t *testing.T) {
	info := Extract(context.Background(), bytes.NewReader([]byte(sample3DXML)), "fixture.3dxml")
	if info.Err != nil || info.TextFailed {
		t.Fatalf("xml: %+v", info)
	}
	if info.Kind != Kind3DXML || info.Format != FormatXML || info.Release != "3DXML 4.3" {
		t.Fatalf("meta: %+v", info)
	}
	if info.Title != "fixture assembly" || info.Author != "fixture-author" || info.Generator != "3dxml fixture" || info.Created != "2026-01-02" {
		t.Fatalf("properties: %+v", info)
	}
	want := []string{"fixture-other.3dxml", "fixture-part.3dxml"}
	if !slices.Equal(info.Components, want) {
		t.Fatalf("components %q", info.Components)
	}
	if len(info.Notes) != 0 || info.Product != (Product{}) {
		t.Fatalf("3dxml names are not notes or properties: %+v", info)
	}
}

func TestExtractXMLBrokenRoot(t *testing.T) {
	info := Extract(context.Background(), bytes.NewReader([]byte("<<<")), "fixture.3dxml")
	if !info.TextFailed || info.ErrorKind != ErrorKindXML || info.Format != FormatXML {
		t.Fatalf("broken xml: %+v", info)
	}
	if len(info.Components) != 0 || len(info.Notes) != 0 || info.Release != ReleaseUnknown {
		t.Fatalf("cleared: %+v", info)
	}
}

func TestExtractZIP3DXMLLimits(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addZip(t, zw, "Manifest.xml", "<Manifest><Root>main.3dxml</Root></Manifest>")
	addZip(t, zw, "main.3dxml", sample3DXML)
	addZip(t, zw, "../secret.xml", "<Root/>")
	addZip(t, zw, `C:\volume.xml`, "<Root/>")
	h, err := zw.CreateHeader(&zip.FileHeader{Name: "huge.xml", Method: zip.Deflate})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.CopyN(h, zeroReader{}, zipMaxMember+1); err != nil {
		t.Fatal(err)
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	info := Extract(context.Background(), bytes.NewReader(buf.Bytes()), "fixture.3dxml")
	if info.Err != nil && !info.Truncated {
		t.Fatalf("zip: %+v", info)
	}
	if info.Format != FormatZIP || info.Kind != Kind3DXML {
		t.Fatalf("meta: %+v", info)
	}
	if !info.Truncated {
		t.Fatal("expected truncated from oversized member")
	}
	if slices.Contains(info.Components, "secret.xml") || slices.Contains(info.Components, "volume.xml") {
		t.Fatalf("unsafe members leaked: %q", info.Components)
	}
	if info.Release != "3DXML 4.3" {
		t.Fatalf("release %q", info.Release)
	}
	if slices.Contains(info.Components, "skip.3dxml") {
		t.Fatalf("external url: %q", info.Components)
	}
}

func TestExtractZIPBrokenRoot(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addZip(t, zw, "Manifest.xml", "<Manifest><Root>main.3dxml</Root></Manifest>")
	addZip(t, zw, "main.3dxml", "<<<not xml")
	addZip(t, zw, "ok.xml", sample3DXML)
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	info := Extract(context.Background(), bytes.NewReader(buf.Bytes()), "fixture.3dxml")
	if !info.TextFailed || info.ErrorKind != ErrorKindXML {
		t.Fatalf("broken root: %+v", info)
	}
	if len(info.Components) != 0 || info.Title != "" {
		t.Fatalf("no sidecar fields: %+v", info)
	}
}

func TestExtractZIPOtherMemberSkipped(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addZip(t, zw, "Manifest.xml", "<Manifest><Root>main.3dxml</Root></Manifest>")
	addZip(t, zw, "main.3dxml", sample3DXML)
	addZip(t, zw, "broken.xml", "<<<")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	info := Extract(context.Background(), bytes.NewReader(buf.Bytes()), "fixture.3dxml")
	if info.TextFailed || info.Err != nil {
		t.Fatalf("sibling error should skip: %+v", info)
	}
	if info.Release != "3DXML 4.3" || !slices.Contains(info.Components, "fixture-part.3dxml") {
		t.Fatalf("kept root: %+v", info)
	}
}

func addZip(t *testing.T, zw *zip.Writer, name, body string) {
	t.Helper()
	w, err := zw.CreateHeader(&zip.FileHeader{Name: name, Method: zip.Store})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(w, body); err != nil {
		t.Fatal(err)
	}
}

func TestExtractZIPMemberCap(t *testing.T) {
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	addZip(t, zw, "Manifest.xml", "<Manifest><Root>0.3dxml</Root></Manifest>")
	for i := 0; i < zipMaxMembers+2; i++ {
		name := strings.Repeat("x", 0) + itoa(i) + ".xml"
		addZip(t, zw, name, "<n>fixture-name-value</n>")
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	info := Extract(context.Background(), bytes.NewReader(buf.Bytes()), "fixture.3dxml")
	if !info.Truncated {
		t.Fatalf("member cap: truncated=%v err=%v", info.Truncated, info.Err)
	}
}
