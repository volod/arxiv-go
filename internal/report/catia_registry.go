package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"slices"
	"strconv"

	"github.com/volod/arxiv-go/internal/scanner"
)

// CatiaHeader is the fixed column order of arxgo-catia.csv: the payload columns, then the optional
// text sidecar path, CATIA summary and modification time.
var CatiaHeader = append(append([]string{}, PayloadHeader...),
	"text_rel_path", "catia_kind", "catia_format", "catia_release", "catia_components", "mtime")

// CatiaRow is one row of arxgo-catia.csv. Kind is empty when no described record of a CATIA split
// supplied a summary (a skipped or conflicting file); the summary cells are then empty.
type CatiaRow struct {
	PayloadRow
	TextRelPath string
	Kind        string
	Format      string
	Release     string // empty when unknown
	Components  int
	MTime       string
}

func (r CatiaRow) cells() []string {
	rec := make([]string, len(CatiaHeader))
	r.PayloadRow.cells(rec)
	rec[10] = r.TextRelPath
	if r.Kind != "" {
		rec[11], rec[12], rec[13], rec[14] = r.Kind, r.Format, r.Release, strconv.Itoa(r.Components)
	}
	rec[15] = r.MTime
	return rec
}

// WriteCatiaCSV writes the full header and rows to w. A column empty in every row is written with
// empty cells.
func WriteCatiaCSV(w io.Writer, rows []CatiaRow) error {
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		data = append(data, r.cells())
	}
	cw := csv.NewWriter(w)
	if err := cw.Write(CatiaHeader); err != nil {
		return err
	}
	return cw.WriteAll(data)
}

// LoadCatiaCSV reads a CATIA registry. The header must hold the payload columns followed by any
// subset of the optional columns in canonical order: a registry written by an earlier build omitted
// the columns empty in every row, and a missing column is read as empty.
func LoadCatiaCSV(r io.Reader) ([]CatiaRow, error) {
	records, err := csv.NewReader(r).ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("catia registry: empty file")
	}
	index, err := catiaColumns(records[0])
	if err != nil {
		return nil, fmt.Errorf("catia registry: %w", err)
	}
	out := make([]CatiaRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		if len(rec) != len(records[0]) {
			return nil, fmt.Errorf("catia registry: row %d: got %d fields, want %d", i+2, len(rec), len(records[0]))
		}
		row, err := parseCatiaRow(rec, index)
		if err != nil {
			return nil, fmt.Errorf("catia registry: row %d: %w", i+2, err)
		}
		out = append(out, row)
	}
	return out, nil
}

// catiaColumns maps each canonical column to its position in header, -1 when omitted.
func catiaColumns(header []string) ([]int, error) {
	if len(header) < PayloadRegistryKeep || !equalStrings(header[:PayloadRegistryKeep], CatiaHeader[:PayloadRegistryKeep]) {
		return nil, fmt.Errorf("unexpected header %q", header)
	}
	index := make([]int, len(CatiaHeader))
	for i := range index {
		index[i] = -1
		if i < PayloadRegistryKeep {
			index[i] = i
		}
	}
	next := PayloadRegistryKeep
	for pos, name := range header[PayloadRegistryKeep:] {
		i := slices.Index(CatiaHeader[next:], name)
		if i < 0 {
			return nil, fmt.Errorf("unexpected or out-of-order column %q", name)
		}
		index[next+i] = PayloadRegistryKeep + pos
		next += i + 1
	}
	return index, nil
}

func parseCatiaRow(rec []string, index []int) (CatiaRow, error) {
	p, err := parsePayloadCells(rec)
	if err != nil {
		return CatiaRow{}, err
	}
	cell := func(i int) string {
		if index[i] < 0 {
			return ""
		}
		return rec[index[i]]
	}
	row := CatiaRow{PayloadRow: p, TextRelPath: cell(10), Kind: cell(11), Format: cell(12),
		Release: cell(13), MTime: cell(15)}
	if n := cell(14); n != "" {
		if row.Components, err = strconv.Atoi(n); err != nil || row.Components < 0 {
			return CatiaRow{}, fmt.Errorf("catia_components: invalid count %q", n)
		}
	}
	return row, nil
}

// LoadCatiaFile opens path and parses it. A missing file yields a nil slice.
func LoadCatiaFile(path string) ([]CatiaRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return LoadCatiaCSV(f)
}

// MergeCatiaRows overlays incoming on existing by rel_path with the video registry rules: a moved
// row is not replaced by skipped or conflict, and empty incoming cells keep the old values. The
// result is sorted by the walk-order key.
func MergeCatiaRows(existing, incoming []CatiaRow) []CatiaRow {
	by := make(map[string]CatiaRow, len(existing)+len(incoming))
	for _, r := range existing {
		by[r.RelPath] = r
	}
	for _, r := range incoming {
		old, ok := by[r.RelPath]
		switch {
		case !ok:
			by[r.RelPath] = r
		case old.Status == StatusMoved && r.Status != StatusMoved:
		default:
			by[r.RelPath] = overlayCatiaRow(old, r)
		}
	}
	out := make([]CatiaRow, 0, len(by))
	for _, r := range by {
		out = append(out, r)
	}
	slices.SortFunc(out, func(a, b CatiaRow) int {
		return scanner.Compare(scanner.KeyOf(a.RelPath), scanner.KeyOf(b.RelPath))
	})
	return out
}

func overlayCatiaRow(old, in CatiaRow) CatiaRow {
	in.PayloadRow = OverlayPayload(old.PayloadRow, in.PayloadRow)
	if in.Kind == "" {
		in.Kind, in.Format, in.Release, in.Components = old.Kind, old.Format, old.Release, old.Components
	}
	if in.TextRelPath == "" {
		in.TextRelPath = old.TextRelPath
	}
	if in.MTime == "" {
		in.MTime = old.MTime
	}
	return in
}

// OverlayPayload keeps the old value of every empty incoming payload cell except status and run_id.
func OverlayPayload(old, in PayloadRow) PayloadRow {
	keep := func(dst *string, v string) {
		if *dst == "" {
			*dst = v
		}
	}
	keep(&in.SHA256, old.SHA256)
	keep(&in.FileMIME, old.FileMIME)
	keep(&in.DescriptionRelPath, old.DescriptionRelPath)
	keep(&in.URL, old.URL)
	keep(&in.FileName, old.FileName)
	keep(&in.Transfer, old.Transfer)
	if in.FileSize == 0 {
		in.FileSize = old.FileSize
	}
	return in
}

// MarkCatiaRestored sets status=restored and run_id on rows whose rel_path is in rels.
func MarkCatiaRestored(rows []CatiaRow, rels map[string]struct{}, runID string) []CatiaRow {
	out := append([]CatiaRow(nil), rows...)
	for i, r := range out {
		if _, hit := rels[r.RelPath]; !hit {
			continue
		}
		out[i].Status = StatusRestored
		if runID != "" {
			out[i].RunID = runID
		}
	}
	return out
}
