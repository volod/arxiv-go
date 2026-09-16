package report

import (
	"encoding/csv"
	"fmt"
	"io"
	"os"
	"strconv"
)

// LoadRegistry reads a file registry CSV. A missing file returns (nil, nil).
func LoadRegistry(path string) ([]RegistryRow, error) {
	f, err := os.Open(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, err
	}
	defer f.Close()
	return ReadRegistry(f)
}

// ReadRegistry parses a file registry from r. Metadata columns missing from a registry written by
// an earlier build that omitted them are treated as empty; a missing location is LocationArchive.
func ReadRegistry(r io.Reader) ([]RegistryRow, error) {
	rr, err := NewRegistryReader(r)
	if err != nil {
		return nil, err
	}
	out := []RegistryRow{} // non-nil: LoadRegistry returns nil only for a missing file
	for {
		row, err := rr.Next()
		if err == io.EOF {
			return out, nil
		}
		if err != nil {
			return nil, err
		}
		out = append(out, row)
	}
}

// RegistryReader streams the rows of a file registry in file order, so a scan can follow an earlier
// registry alongside its walk without holding every row.
type RegistryReader struct {
	cr     *csv.Reader
	header []string
	keep   int
	line   int
}

// NewRegistryReader reads and checks the header of the file registry in r.
func NewRegistryReader(r io.Reader) (*RegistryReader, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = -1
	cr.ReuseRecord = true
	header, err := cr.Read()
	if err == io.EOF {
		return nil, fmt.Errorf("file registry: empty file")
	}
	if err != nil {
		return nil, err
	}
	header = append([]string(nil), header...)
	keep, err := registryColumns(header)
	if err != nil {
		return nil, fmt.Errorf("file registry: %w", err)
	}
	return &RegistryReader{cr: cr, header: header, keep: keep, line: 1}, nil
}

// Next returns the next row, or io.EOF after the last one.
func (r *RegistryReader) Next() (RegistryRow, error) {
	rec, err := r.cr.Read()
	if err != nil {
		return RegistryRow{}, err
	}
	r.line++
	if len(rec) != len(r.header) {
		return RegistryRow{}, fmt.Errorf("file registry: row %d: got %d fields, want %d", r.line, len(rec), len(r.header))
	}
	row, err := parseRegistryRow(rec, r.header, r.keep)
	if err != nil {
		return RegistryRow{}, fmt.Errorf("file registry: row %d: %w", r.line, err)
	}
	return row, nil
}

// registryColumns checks a file-registry header and returns the index of its first metadata column:
// after location, or directly after the required columns in a registry from an earlier build.
func registryColumns(header []string) (int, error) {
	keep := FileRegistryRequired
	if len(header) > keep && header[keep] == LocationColumn {
		keep++
	}
	if len(header) < FileRegistryRequired || !equalStrings(header[:FileRegistryRequired], RegistryHeader[:FileRegistryRequired]) {
		return 0, fmt.Errorf("unexpected header %q", header)
	}
	if _, err := expandMetadata(header[keep:], make([]string, len(header)-keep)); err != nil {
		return 0, err
	}
	return keep, nil
}

func parseRegistryRow(rec, header []string, keep int) (RegistryRow, error) {
	size, err := strconv.ParseInt(rec[3], 10, 64)
	if err != nil {
		return RegistryRow{}, fmt.Errorf("file_size: %w", err)
	}
	meta, err := parseMetadataHeader(header[keep:], rec[keep:])
	if err != nil {
		return RegistryRow{}, err
	}
	location := LocationArchive
	if keep > FileRegistryRequired {
		switch location = rec[FileRegistryRequired]; location {
		case LocationArchive, LocationVideoArchive, LocationCatiaArchive:
		default:
			return RegistryRow{}, fmt.Errorf("location: unknown value %q", location)
		}
	}
	return RegistryRow{
		RelPath: rec[0], FileName: rec[1], FileType: rec[2], FileSize: size, IsLarge: parseBool(rec[4]),
		FileMIME: rec[5], IsBinary: parseBool(rec[6]), IsMedia: parseBool(rec[7]),
		IsPicture: parseBool(rec[8]), IsVideo: parseBool(rec[9]), IsCatia: parseBool(rec[10]),
		Location: location, Metadata: meta,
	}, nil
}

func parseBool(s string) bool { return s == "true" }

// RegistryByPath indexes rows by rel_path.
func RegistryByPath(rows []RegistryRow) map[string]RegistryRow {
	m := make(map[string]RegistryRow, len(rows))
	for _, r := range rows {
		m[r.RelPath] = r
	}
	return m
}
