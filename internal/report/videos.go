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

// PayloadHeader is columns 1-10 of every payload registry (arxgo-videos.csv, arxgo-catia.csv): what
// the file is and its state, where to find it and its description, then transfer evidence.
var PayloadHeader = []string{
	"rel_path", "file_name", "status", "url", "description_rel_path",
	"file_size", "sha256", "transfer", "run_id", "file_mime",
}

// PayloadRegistryKeep is the number of payload registry columns shared by every payload.
const PayloadRegistryKeep = 10

// VideoRegistryRequired is the number of video-registry columns every reader requires (rel_path
// through previews). Writers always write the full VideoHeader.
const VideoRegistryRequired = 11

// VideoHeader is the fixed column order of arxgo-videos.csv: the payload columns, previews, then the
// flat metadata columns.
var VideoHeader = []string{}

func init() {
	VideoHeader = append(append(append(VideoHeader, PayloadHeader...), "previews"), MetadataHeader...)
}

// Video row status values.
const (
	StatusMoved    = "moved"
	StatusRestored = "restored"
	StatusConflict = "conflict"
	StatusSkipped  = "skipped"
)

// PayloadRow holds the payload registry columns 1-10 of one moved, restored, skipped or conflicting
// file.
type PayloadRow struct {
	RelPath            string
	FileName           string
	Status             string
	URL                string
	DescriptionRelPath string
	FileSize           int64
	SHA256             string
	Transfer           string
	RunID              string
	FileMIME           string
}

// cells writes the payload columns into record[:PayloadRegistryKeep].
func (r PayloadRow) cells(record []string) {
	record[0], record[1], record[2], record[3] = r.RelPath, r.FileName, r.Status, r.URL
	record[4], record[5] = r.DescriptionRelPath, strconv.FormatInt(r.FileSize, 10)
	record[6], record[7], record[8], record[9] = r.SHA256, r.Transfer, r.RunID, r.FileMIME
}

// parsePayloadCells reads the payload columns from rec[:PayloadRegistryKeep].
func parsePayloadCells(rec []string) (PayloadRow, error) {
	size, err := strconv.ParseInt(rec[5], 10, 64)
	if err != nil {
		return PayloadRow{}, fmt.Errorf("file_size: %w", err)
	}
	return PayloadRow{
		RelPath: rec[0], FileName: rec[1], Status: rec[2], URL: rec[3], DescriptionRelPath: rec[4],
		FileSize: size, SHA256: rec[6], Transfer: rec[7], RunID: rec[8], FileMIME: rec[9],
	}, nil
}

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

// Payload returns the payload registry columns of the row.
func (r VideoRow) Payload() PayloadRow {
	return PayloadRow{
		RelPath: r.RelPath, FileName: r.FileName, Status: r.Status, URL: r.URL,
		DescriptionRelPath: r.DescriptionRelPath, FileSize: r.FileSize, SHA256: r.SHA256,
		Transfer: r.Transfer, RunID: r.RunID, FileMIME: r.FileMIME,
	}
}

// videoRowOf combines payload columns with the video-specific columns.
func videoRowOf(p PayloadRow, previews string, meta Metadata) VideoRow {
	return VideoRow{
		RelPath: p.RelPath, DescriptionRelPath: p.DescriptionRelPath, FileName: p.FileName,
		FileSize: p.FileSize, FileMIME: p.FileMIME, SHA256: p.SHA256, Transfer: p.Transfer,
		Status: p.Status, RunID: p.RunID, URL: p.URL, Previews: previews, Metadata: meta,
	}
}

// WriteVideoCSV writes the full header and rows in walk order to w. A column empty in every row is
// written with empty cells.
func WriteVideoCSV(w io.Writer, rows []VideoRow) error {
	cw := csv.NewWriter(w)
	record := make([]string, len(VideoHeader))
	data := make([][]string, 0, len(rows))
	for _, r := range rows {
		r.Payload().cells(record)
		record[PayloadRegistryKeep] = r.Previews
		copy(record[VideoRegistryRequired:], MetadataCells(r.Metadata))
		data = append(data, append([]string(nil), record...))
	}
	if err := cw.Write(VideoHeader); err != nil {
		return err
	}
	return cw.WriteAll(data)
}

// LoadVideoCSV reads a video registry. Metadata columns missing from a registry written by an
// earlier build that omitted them are treated as empty.
func LoadVideoCSV(r io.Reader) ([]VideoRow, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("video registry: empty file")
	}
	keep, err := checkRequiredHeader(records[0], VideoHeader, VideoRegistryRequired)
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
	p, err := parsePayloadCells(rec)
	if err != nil {
		return VideoRow{}, err
	}
	meta, err := parseMetadataHeader(header[keep:], rec[keep:])
	if err != nil {
		return VideoRow{}, err
	}
	return videoRowOf(p, rec[PayloadRegistryKeep], meta), nil
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
