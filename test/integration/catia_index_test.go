//go:build integration

package integration

import (
	"bytes"
	"path/filepath"
	"strconv"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
)

// checkCatiaIndex runs catia-index through the binary after the CATIA split: one section per moved
// row of arxgo-catia.csv, no missing text, a byte-identical rerun, section blocks equal to the
// sidecar blocks, and no --strings option.
func checkCatiaIndex(t *testing.T, bin *arxgoBin, archive, work string) {
	t.Helper()
	rows, err := report.LoadCatiaCSV(bytes.NewReader(readFile(t, filepath.Join(archive, "arxgo-catia.csv"))))
	if err != nil {
		t.Fatal(err)
	}
	moved := 0
	for _, row := range rows {
		if row.Status == report.StatusMoved {
			moved++
		}
	}
	args := []string{"catia-index", "--archive", archive, "--log-level", "warn"}
	if r := bin.run(t, args...); r.code != 0 {
		t.Fatalf("catia-index exit %d:\n%s", r.code, r.stderr)
	}
	out := filepath.Join(archive, "arxgo-catia-text.md")
	doc := readFile(t, out)
	sections := strings.Split(string(doc), "\n## ")[1:]
	if len(sections) != moved || !bytes.Contains(doc, []byte("\nfiles: "+strconv.Itoa(moved)+"\n")) ||
		!bytes.Contains(doc, []byte("\nmissing_text: 0\n")) {
		t.Errorf("catia-index: %d sections for %d moved rows:\n%.2000s", len(sections), moved, doc)
	}
	for _, section := range sections {
		_, text, ok := strings.Cut(section, "\ntext: ")
		if !ok {
			t.Errorf("section without text:\n%s", section)
			continue
		}
		sidecarRel, _, _ := strings.Cut(text, "\n")
		sidecar := string(readFile(t, filepath.Join(archive, filepath.FromSlash(sidecarRel))))
		_, blocks, _ := strings.Cut(sidecar, "\ntruncated: ")
		if _, sectionBlocks, _ := strings.Cut(section, "\ntruncated: "); strings.TrimSuffix(sectionBlocks, "\n") != strings.TrimSuffix(blocks, "\n") {
			t.Errorf("section blocks differ from %s:\n%s\nsidecar:\n%s", sidecarRel, section, sidecar)
		}
	}
	if r := bin.run(t, args...); r.code != 0 || !bytes.Equal(doc, readFile(t, out)) {
		t.Errorf("catia-index rerun exit %d, byte-identical %v", r.code, bytes.Equal(doc, readFile(t, out)))
	}
	if r := bin.run(t, append(args, "--strings", "--out", filepath.Join(work, "catia-index-strings.md"))...); r.code != 2 {
		t.Errorf("catia-index --strings exit %d, want 2", r.code)
	}
}
