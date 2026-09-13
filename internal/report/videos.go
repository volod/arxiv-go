package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"slices"
	"sort"
	"strconv"

	"github.com/volod/arxiv-go/internal/scanner"
)

// VideoHeader is the fixed column order of arxgo-videos.csv.
var VideoHeader = []string{
	"rel_path", "video_rel_path", "stub_rel_path", "file_name", "file_size", "file_mime",
	"sha256", "transfer", "status", "run_id", "url", "previews", "metadata",
}

// Video row status values.
const (
	StatusMoved    = "moved"
	StatusRestored = "restored"
	StatusConflict = "conflict"
	StatusSkipped  = "skipped"
)

// VideoRow is one row of arxgo-videos.csv.
type VideoRow struct {
	RelPath      string
	VideoRelPath string
	StubRelPath  string
	FileName     string
	FileSize     int64
	FileMIME     string
	SHA256       string
	Transfer     string
	Status       string
	RunID        string
	URL          string
	Previews     string
	Metadata     Metadata
}

// WriteVideoCSV writes the header and rows in walk order to w.
func WriteVideoCSV(w io.Writer, rows []VideoRow) error {
	cw := csv.NewWriter(w)
	if err := cw.Write(VideoHeader); err != nil {
		return err
	}
	record := make([]string, len(VideoHeader))
	for _, r := range rows {
		meta, err := marshalMetadata(r.Metadata)
		if err != nil {
			return err
		}
		record[0], record[1], record[2] = r.RelPath, r.VideoRelPath, r.StubRelPath
		record[3] = r.FileName
		record[4] = strconv.FormatInt(r.FileSize, 10)
		record[5], record[6] = r.FileMIME, r.SHA256
		record[7], record[8], record[9] = r.Transfer, r.Status, r.RunID
		record[10], record[11], record[12] = r.URL, r.Previews, meta
		if err := cw.Write(record); err != nil {
			return err
		}
	}
	cw.Flush()
	return cw.Error()
}

func marshalMetadata(m Metadata) (string, error) {
	if m.V == 0 {
		m.V = MetadataVersion
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(m); err != nil {
		return "", err
	}
	return string(bytes.TrimSuffix(buf.Bytes(), []byte("\n"))), nil
}

// LoadVideoCSV reads a video registry. A missing file returns (nil, nil).
func LoadVideoCSV(r io.Reader) ([]VideoRow, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = len(VideoHeader)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("video registry: empty file")
	}
	if !equalStrings(records[0], VideoHeader) {
		return nil, fmt.Errorf("video registry: unexpected header %q", records[0])
	}
	out := make([]VideoRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		row, err := parseVideoRow(rec)
		if err != nil {
			return nil, fmt.Errorf("video registry: row %d: %w", i+2, err)
		}
		out = append(out, row)
	}
	return out, nil
}

func parseVideoRow(rec []string) (VideoRow, error) {
	size, err := strconv.ParseInt(rec[4], 10, 64)
	if err != nil {
		return VideoRow{}, fmt.Errorf("file_size: %w", err)
	}
	var meta Metadata
	if rec[12] != "" {
		if err := json.Unmarshal([]byte(rec[12]), &meta); err != nil {
			return VideoRow{}, fmt.Errorf("metadata: %w", err)
		}
	}
	return VideoRow{
		RelPath: rec[0], VideoRelPath: rec[1], StubRelPath: rec[2], FileName: rec[3],
		FileSize: size, FileMIME: rec[5], SHA256: rec[6], Transfer: rec[7], Status: rec[8],
		RunID: rec[9], URL: rec[10], Previews: rec[11], Metadata: meta,
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
	if in.Metadata.V == 0 {
		in.Metadata = old.Metadata
	}
	if in.SHA256 == "" {
		in.SHA256 = old.SHA256
	}
	if in.FileMIME == "" {
		in.FileMIME = old.FileMIME
	}
	if in.StubRelPath == "" {
		in.StubRelPath = old.StubRelPath
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

func sortVideoRows(rows []VideoRow) {
	slices.SortFunc(rows, func(a, b VideoRow) int {
		return scanner.Compare(scanner.KeyOf(a.RelPath), scanner.KeyOf(b.RelPath))
	})
}

// UniqueRunIDs returns sorted unique run ids from rows.
func UniqueRunIDs(rows []VideoRow) []string {
	seen := map[string]struct{}{}
	var out []string
	for _, r := range rows {
		if r.RunID == "" {
			continue
		}
		if _, ok := seen[r.RunID]; ok {
			continue
		}
		seen[r.RunID] = struct{}{}
		out = append(out, r.RunID)
	}
	sort.Strings(out)
	return out
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
