package archive

import (
	"bytes"
	"context"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

// headerLine returns the first line of the CSV at path.
func headerLine(t *testing.T, path string) string {
	t.Helper()
	raw := mustRead(t, path)
	i := bytes.IndexByte(raw, '\n')
	if i < 0 {
		t.Fatalf("%s has no header line", path)
	}
	return string(raw[:i])
}

func scanArchive(t *testing.T, r roots) string {
	t.Helper()
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), testScanConfig(r))
	if res.Status != StatusCompleted {
		t.Fatalf("scan = %v (%v)", res.Status, res.Err)
	}
	return headerLine(t, filepath.Join(r.archive, "arxgo-registry.csv"))
}

// Every registry is written with its full contract header, whatever the tree holds and whichever
// command wrote it, so a downstream loader sees one schema.
func TestRegistryHeadersAreStableAcrossCommands(t *testing.T) {
	fileHeader := strings.Join(report.RegistryHeader, ",")
	videoHeader := strings.Join(report.VideoHeader, ",")
	catiaHeader := strings.Join(report.CatiaHeader, ",")
	fileHeaders := map[string]string{}

	fileHeaders["empty archive"] = scanArchive(t, newRoots(t))

	for _, verify := range []fsops.VerifyMode{fsops.VerifySize, fsops.VerifyHash} {
		r, _, _ := splitFixture(t)
		fileHeaders["video tree/"+verify.String()] = scanArchive(t, r)
		cfg, c := splitConfig(r, "auto")
		c.Verify = verify
		c.Descriptions = NewMarkdownDescription(DescriptionConfig{
			Archive: r.archive, Mirror: r.video, Registry: c.Scan.Registry, Version: "test", Verify: verify,
		})
		attachRecoverer(&cfg, c, nil)
		if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
			t.Fatalf("video split %s = %+v", verify.String(), res)
		}
		fileHeaders["video split/"+verify.String()] = headerLine(t, c.Scan.Registry)
		for _, root := range []string{r.archive, r.video} {
			if got := headerLine(t, filepath.Join(root, scanner.VideoRegistryName)); got != videoHeader {
				t.Errorf("video registry %s in %s: header = %s", verify.String(), root, got)
			}
		}
		fileHeaders["video tree after split/"+verify.String()] = scanArchive(t, r)
	}

	for _, text := range []bool{false, true} {
		r, _ := catiaFixture(t)
		name := "catia"
		if text {
			name = "catia-text"
		}
		fileHeaders[name+" tree"] = scanArchive(t, r.roots)
		cfg, c := catiaSplitConfig(r, "auto")
		c.CatiaText = text
		if !text {
			c.Verify = fsops.VerifySize
			c.Descriptions = NewMarkdownDescription(DescriptionConfig{
				Archive: r.archive, Mirror: r.catia, Registry: c.Scan.Registry,
				Version: "test", Payload: PayloadCatia, Verify: c.Verify,
			})
			attachRecoverer(&cfg, c, nil)
		}
		if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
			t.Fatalf("%s split = %+v", name, res)
		}
		fileHeaders[name+" split"] = headerLine(t, c.Scan.Registry)
		for _, root := range []string{r.archive, r.catia} {
			if got := headerLine(t, filepath.Join(root, scanner.CatiaRegistryName)); got != catiaHeader {
				t.Errorf("%s registry in %s: header = %s", name, root, got)
			}
		}
		fileHeaders[name+" tree after split"] = scanArchive(t, r.roots)
	}

	for name, got := range fileHeaders {
		if got != fileHeader {
			t.Errorf("%s: file registry header = %s", name, got)
		}
	}
}

// Previews fill the previews column; without them the column is still written.
func TestVideoRegistryHeaderWithPreviewsLive(t *testing.T) {
	cfg, c, r, _ := previewFixture(t)
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted {
		t.Fatalf("split: %+v", got)
	}
	want := strings.Join(report.VideoHeader, ",")
	if got := headerLine(t, filepath.Join(r.archive, scanner.VideoRegistryName)); got != want {
		t.Fatalf("video registry header with previews = %s", got)
	}
	if got := headerLine(t, c.Scan.Registry); got != strings.Join(report.RegistryHeader, ",") {
		t.Fatalf("file registry header with previews = %s", got)
	}
}
