package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

func TestScanClassifiesCatiaAndNotVideo(t *testing.T) {
	r := newRoots(t)
	appleDouble := append([]byte{0x00, 0x05, 0x16, 0x07, 0x00, 0x02, 0x00, 0x00}, "Mac OS X        "...)
	files := map[string][]byte{
		"cad/fixture-part.CATPart":       {0x56, 0x35, 0x5F, 0x43, 0x46, 0x56, 0x32, 0x00}, // V5_CFV2 NUL, invented fixture
		"cad/fixture-product.CATProduct": ftypHead("isom", "isom", "mp41"),
		"cad/fixture-drawing.CATDrawing": []byte("plain text drawing placeholder\n"),
		"cad/fixture-shape.cgr":          nil,
		"cad/fixture-xml.3dxml":          []byte("<?xml version=\"1.0\"?><root/>"),
		"cad/fixture-zip.3dxml":          []byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"),
		"cad/._fixture.CATPart":          appleDouble,
		"media/clip.mp4":                 ftypHead("isom", "isom", "mp41"),
	}
	for rel, data := range files {
		writeScanFile(t, r.archive, rel, data)
	}

	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), testScanConfig(r))
	if res.Status != StatusCompleted {
		t.Fatalf("status = %v (%v)", res.Status, res.Err)
	}
	path := filepath.Join(r.archive, "arxgo-registry.csv")
	raw := mustRead(t, path)
	header := strings.Split(strings.TrimSuffix(string(raw[:bytes.IndexByte(raw, '\n')]), "\r"), ",")
	if want := report.RegistryHeader[:report.FileRegistryKeep]; len(header) < len(want) || strings.Join(header[:len(want)], ",") != strings.Join(want, ",") {
		t.Fatalf("header = %q, want prefix %q", header, want)
	}
	rows, err := report.LoadRegistry(path)
	if err != nil {
		t.Fatal(err)
	}
	byPath := report.RegistryByPath(rows)

	catiaRels := []string{
		"cad/fixture-part.CATPart", "cad/fixture-product.CATProduct", "cad/fixture-drawing.CATDrawing",
		"cad/fixture-shape.cgr", "cad/fixture-xml.3dxml", "cad/fixture-zip.3dxml",
	}
	for _, rel := range catiaRels {
		row, ok := byPath[rel]
		if !ok {
			t.Errorf("missing row %s", rel)
			continue
		}
		if !row.IsCatia || row.IsVideo {
			t.Errorf("%s: is_catia=%v is_video=%v mime=%s", rel, row.IsCatia, row.IsVideo, row.FileMIME)
		}
	}
	prod := byPath["cad/fixture-product.CATProduct"]
	if prod.IsMedia || prod.FileMIME != "video/mp4" {
		t.Errorf("video-shaped CATProduct should keep MIME but not be media: %+v", prod)
	}
	ad := byPath["cad/._fixture.CATPart"]
	if ad.IsCatia || ad.IsVideo || ad.FileMIME != "multipart/appledouble" {
		t.Errorf("AppleDouble = %+v", ad)
	}
	clip := byPath["media/clip.mp4"]
	if !clip.IsVideo || clip.IsCatia {
		t.Errorf("clip = %+v", clip)
	}

	var cands []string
	if err := ReadCandidates(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.CandidatesFile), func(c Candidate) error {
		cands = append(cands, c.RelPath)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if strings.Join(cands, ",") != "media/clip.mp4" {
		t.Errorf("candidates = %q, want only the video", cands)
	}

	sum := readReport(t, r.archive, res.RunID).Scan
	if sum == nil || sum.Catia.Count != int64(len(catiaRels)) {
		t.Errorf("catia stats = %+v, want count %d", sum, len(catiaRels))
	}
	repRaw, err := os.ReadFile(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.ReportFile))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Contains(repRaw, []byte(`"catia"`)) {
		t.Fatalf("report omitted non-zero catia: %s", repRaw)
	}
	var payload map[string]any
	if err := json.Unmarshal(repRaw, &payload); err != nil {
		t.Fatal(err)
	}
	scanObj, _ := payload["scan"].(map[string]any)
	if _, ok := scanObj["catia"]; !ok {
		t.Error("scan.catia missing from report JSON")
	}
}
