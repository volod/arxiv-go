package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
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

// ReadRegistry parses a file registry from r.
func ReadRegistry(r io.Reader) ([]RegistryRow, error) {
	cr := csv.NewReader(r)
	cr.FieldsPerRecord = len(RegistryHeader)
	records, err := cr.ReadAll()
	if err != nil {
		return nil, err
	}
	if len(records) == 0 {
		return nil, fmt.Errorf("file registry: empty file")
	}
	if !equalStrings(records[0], RegistryHeader) {
		return nil, fmt.Errorf("file registry: unexpected header %q", records[0])
	}
	out := make([]RegistryRow, 0, len(records)-1)
	for i, rec := range records[1:] {
		row, err := parseRegistryRow(rec)
		if err != nil {
			return nil, fmt.Errorf("file registry: row %d: %w", i+2, err)
		}
		out = append(out, row)
	}
	return out, nil
}

func parseRegistryRow(rec []string) (RegistryRow, error) {
	size, err := strconv.ParseInt(rec[2], 10, 64)
	if err != nil {
		return RegistryRow{}, fmt.Errorf("file_size: %w", err)
	}
	var meta Metadata
	if rec[10] != "" {
		dec := json.NewDecoder(bytes.NewReader([]byte(rec[10])))
		if err := dec.Decode(&meta); err != nil {
			return RegistryRow{}, fmt.Errorf("metadata: %w", err)
		}
	}
	return RegistryRow{
		RelPath: rec[0], FileName: rec[1], FileSize: size, FileType: rec[3], FileMIME: rec[4],
		IsBinary: parseBool(rec[5]), IsMedia: parseBool(rec[6]), IsPicture: parseBool(rec[7]),
		IsVideo: parseBool(rec[8]), IsLarge: parseBool(rec[9]), Metadata: meta,
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
