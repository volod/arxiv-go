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

// VideoHeader is the fixed column order of arxgo-videos.csv.
var VideoHeader = []string{
	"rel_path", "description_rel_path", "file_name", "file_size", "file_mime",
	"sha256", "transfer", "status", "run_id", "url", "previews",
}

func init() { VideoHeader = append(VideoHeader, MetadataHeader...) }

// Video row status values.
const (
	StatusMoved    = "moved"
	StatusRestored = "restored"
	StatusConflict = "conflict"
	StatusSkipped  = "skipped"
)

// VideoRow is one row of arxgo-videos.csv.
type VideoRow struct {
	RelPath            string
	DescriptionRelPath string
	FileName           string
	FileSize           int64
	FileMIME           string
	SHA256             string
	Transfer           string
	Status             string
	RunID              string
	URL                string
	Previews           string
	Metadata           Metadata
}

// WriteVideoCSV writes the header and rows in walk order to w. Metadata columns that are empty
// in every row are omitted.
func WriteVideoCSV(w io.Writer, rows []VideoRow) error {
	cw := csv.NewWriter(w)
	record := make([]string, len(VideoHeader))
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		record[0], record[1], record[2] = r.RelPath, r.DescriptionRelPath, r.FileName
		record[3] = strconv.FormatInt(r.FileSize, 10)
		record[4], record[5] = r.FileMIME, r.SHA256
		record[6], record[7], record[8] = r.Transfer, r.Status, r.RunID
		record[9], record[10] = r.URL, r.Previews
		copy(record[VideoRegistryKeep:], MetadataCells(r.Metadata))
		data = append(data, append([]string(nil), record...))
	}
	header, data := dropEmptyColumns(VideoHeader, VideoRegistryKeep, data)
	if err := cw.Write(header); err != nil {
		return err
	}
	return cw.WriteAll(data)
}

// LoadVideoCSV reads a video registry. A missing file returns (nil, nil). Omitted empty
// metadata columns are treated as empty.
func LoadVideoCSV(r io.Reader) ([]VideoRow, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("video registry: empty file")
	}
	keep, err := checkRequiredHeader(records[0], VideoHeader, VideoRegistryKeep)
	if err != nil {
		return nil, fmt.Errorf("video registry: %w", err)
	}
	out := make([]VideoRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		if len(rec) != len(records[0]) {
			return nil, fmt.Errorf("video registry: row %d: got %d fields, want %d", i+2, len(rec), len(records[0]))
		}
		row, err := parseVideoRow(rec, records[0], keep)
		if err != nil {
			return nil, fmt.Errorf("video registry: row %d: %w", i+2, err)
		}
		out = append(out, row)
	}
	return out, nil
}

func parseVideoRow(rec, header []string, keep int) (VideoRow, error) {
	size, err := strconv.ParseInt(rec[3], 10, 64)
	if err != nil {
		return VideoRow{}, fmt.Errorf("file_size: %w", err)
	}
	meta, err := parseMetadataHeader(header[keep:], rec[keep:])
	if err != nil {
		return VideoRow{}, err
	}
	return VideoRow{
		RelPath: rec[0], DescriptionRelPath: rec[1], FileName: rec[2],
		FileSize: size, FileMIME: rec[4], SHA256: rec[5], Transfer: rec[6], Status: rec[7],
		RunID: rec[8], URL: rec[9], Previews: rec[10], Metadata: meta,
	}, nil
}

// LoadVideoFile opens path and parses it. Missing files yield a nil slice.
func LoadVideoFile(path string) ([]VideoRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return LoadVideoCSV(f)
}

// MergeVideoRows overlays incoming on existing by rel_path. A moved row is not
// replaced by skipped or conflict. The result is sorted by the walk-order key.
func MergeVideoRows(existing, incoming []VideoRow) []VideoRow {
	by := make(map[string]VideoRow, len(existing)+len(incoming))
	for _, r := range existing {
		by[r.RelPath] = r
	}
	for _, r := range incoming {
		if old, ok := by[r.RelPath]; ok {
			if old.Status == StatusMoved && r.Status != StatusMoved {
				continue
			}
			by[r.RelPath] = overlayVideoRow(old, r)
			continue
		}
		by[r.RelPath] = r
	}
	out := make([]VideoRow, 0, len(by))
	for _, r := range by {
		out = append(out, r)
	}
	sortVideoRows(out)
	return out
}

func overlayVideoRow(old, in VideoRow) VideoRow {
	if !HasMetadata(in.Metadata) {
		in.Metadata = old.Metadata
	}
	if in.SHA256 == "" {
		in.SHA256 = old.SHA256
	}
	if in.FileMIME == "" {
		in.FileMIME = old.FileMIME
	}
	if in.DescriptionRelPath == "" {
		in.DescriptionRelPath = old.DescriptionRelPath
	}
	if in.URL == "" {
		in.URL = old.URL
	}
	if in.FileName == "" {
		in.FileName = old.FileName
	}
	if in.FileSize == 0 {
		in.FileSize = old.FileSize
	}
	if in.Transfer == "" {
		in.Transfer = old.Transfer
	}
	return in
}

// MarkRestored sets status=restored and run_id on rows whose rel_path is in rels.
func MarkRestored(rows []VideoRow, rels map[string]struct{}, runID string) []VideoRow {
	out := append([]VideoRow(nil), rows...)
	for i, r := range out {
		_, hit := rels[r.RelPath]
		if !hit {
			continue
		}
		out[i].Status = StatusRestored
		if runID != "" {
			out[i].RunID = runID
		}
	}
	return out
}

// HasMoved reports whether any row still has status moved.
func HasMoved(rows []VideoRow) bool {
	for _, r := range rows {
		if r.Status == StatusMoved {
			return true
		}
	}
	return false
}

func sortVideoRows(rows []VideoRow) {
	slices.SortFunc(rows, func(a, b VideoRow) int {
		return scanner.Compare(scanner.KeyOf(a.RelPath), scanner.KeyOf(b.RelPath))
	})
}

func equalStrings(a, b []string) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
