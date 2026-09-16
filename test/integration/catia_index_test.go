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
// row of arxgo-catia.csv, no missing text, a byte-identical rerun, and a --strings document that
// differs only by its strings: blocks.
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
	sections := 0
	for _, line := range strings.Split(string(doc), "\n") {
		if strings.HasPrefix(line, "## ") && line != "## Missing text" {
			sections++
		}
	}
	if sections != moved || !bytes.Contains(doc, []byte("\nfiles: "+strconv.Itoa(moved)+"\n")) ||
		!bytes.Contains(doc, []byte("\nmissing_text: 0\n")) {
		t.Errorf("catia-index: %d sections for %d moved rows:\n%.2000s", sections, moved, doc)
	}
	if r := bin.run(t, args...); r.code != 0 || !bytes.Equal(doc, readFile(t, out)) {
		t.Errorf("catia-index rerun exit %d, byte-identical %v", r.code, bytes.Equal(doc, readFile(t, out)))
	}
	withStrings := filepath.Join(work, "catia-index-strings.md")
	if r := bin.run(t, append(args, "--strings", "--out", withStrings)...); r.code != 0 {
		t.Fatalf("catia-index --strings exit %d:\n%s", r.code, r.stderr)
	}
	var stripped strings.Builder
	inStrings := false
	for _, line := range strings.SplitAfter(string(readFile(t, withStrings)), "\n") {
		if line == "strings:\n" || inStrings && strings.HasPrefix(line, "- ") {
			inStrings = true
			continue
		}
		inStrings = false
		stripped.WriteString(line)
	}
	if stripped.String() != string(doc) {
		t.Error("catia-index --strings differs from the default index beyond strings: blocks")
	}
}
