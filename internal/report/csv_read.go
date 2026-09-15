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

// ReadRegistry parses a file registry from r. Metadata columns that were omitted because they
// were empty in every row are treated as empty.
func ReadRegistry(r io.Reader) ([]RegistryRow, error) {
	cr := csv.NewReader(r)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("file registry: empty file")
	}
	keep, err := checkRequiredHeader(records[0], RegistryHeader, FileRegistryKeep)
	if err != nil {
		return nil, fmt.Errorf("file registry: %w", err)
	}
	out := make([]RegistryRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		if len(rec) != len(records[0]) {
			return nil, fmt.Errorf("file registry: row %d: got %d fields, want %d", i+2, len(rec), len(records[0]))
		}
		row, err := parseRegistryRow(rec, records[0], keep)
		if err != nil {
			return nil, fmt.Errorf("file registry: row %d: %w", i+2, err)
		}
		out = append(out, row)
	}
	return out, nil
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
	return RegistryRow{
		RelPath: rec[0], FileName: rec[1], FileType: rec[2], FileSize: size, IsLarge: parseBool(rec[4]),
		FileMIME: rec[5], IsBinary: parseBool(rec[6]), IsMedia: parseBool(rec[7]),
		IsPicture: parseBool(rec[8]), IsVideo: parseBool(rec[9]), IsCatia: parseBool(rec[10]),
		Metadata: meta,
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
