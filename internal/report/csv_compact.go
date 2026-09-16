package report

import (
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
)

const (
	// FileRegistryKeep is the number of file-registry columns that are always written.
	FileRegistryKeep = 11 // rel_path through is_catia
	// VideoRegistryKeep is the number of video-registry columns that are always written.
	VideoRegistryKeep = 11 // rel_path through previews
)

// DropEmptyCSVColumns rewrites path without optional columns that are empty in every data row.
// Columns before keep always remain. A file that already has nothing to drop is left unchanged.
// The scan part file keeps the full header until this runs, so resume offsets stay valid.
func DropEmptyCSVColumns(path string, keep int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	records, err := csv.NewReader(f).ReadAll()
	_ = f.Close()
	if err != nil {
		return err
	}
	if len(records) == 0 {
		return fmt.Errorf("csv: empty file")
	}
	header := records[0]
	if keep < 0 || keep > len(header) {
		return fmt.Errorf("csv: keep %d, header %d", keep, len(header))
	}
	newHeader, newRows := dropEmptyColumns(header, keep, records[1:])
	if equalStrings(newHeader, header) {
		return nil
	}
	return writeCSVFile(path, newHeader, newRows)
}

func dropEmptyColumns(header []string, keep int, rows [][]string) ([]string, [][]string) {
	used := make([]bool, len(header))
	for i := 0; i < keep; i++ {
		used[i] = true
	}
	for _, rec := range rows {
		for i := keep; i < len(header) && i < len(rec); i++ {
			if rec[i] != "" {
				used[i] = true
			}
		}
	}
	n := 0
	for _, ok := range used {
		if ok {
			n++
		}
	}
	if n == len(header) {
		return header, rows
	}
	idx := make([]int, 0, n)
	newHeader := make([]string, 0, n)
	for i, ok := range used {
		if !ok {
			continue
		}
		idx = append(idx, i)
		newHeader = append(newHeader, header[i])
	}
	out := make([][]string, len(rows))
	for r, rec := range rows {
		row := make([]string, len(idx))
		for j, i := range idx {
			if i < len(rec) {
				row[j] = rec[i]
			}
		}
		out[r] = row
	}
	return newHeader, out
}

func writeCSVFile(path string, header []string, rows [][]string) error {
	dir := filepath.Dir(path)
	tmp, err := os.CreateTemp(dir, filepath.Base(path)+".compact-")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	if info, statErr := os.Stat(path); statErr == nil {
		_ = tmp.Chmod(info.Mode().Perm())
	}
	cw := csv.NewWriter(tmp)
	err = cw.Write(header)
	if err == nil {
		err = cw.WriteAll(rows)
	}
	if err == nil {
		err = cw.Error()
	}
	if err == nil {
		err = tmp.Sync()
	}
	if cerr := tmp.Close(); err == nil {
		err = cerr
	}
	if err != nil {
		_ = os.Remove(tmpName)
		return err
	}
	if err := os.Rename(tmpName, path); err != nil {
		_ = os.Remove(path)
		if err2 := os.Rename(tmpName, path); err2 != nil {
			_ = os.Remove(tmpName)
			return err2
		}
	}
	return nil
}
