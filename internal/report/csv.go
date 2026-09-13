package report

import (
	"bytes"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strconv"
	"time"
)

// RegistryHeader is the fixed column order of the file registry; see
// docs/openspec/stage-1-core/contracts.md#file-registry-csv.
var RegistryHeader = []string{
	"rel_path", "file_name", "file_size", "file_type", "file_mime",
	"is_binary", "is_media", "is_picture", "is_video", "is_large", "metadata",
}

// MetadataVersion is the "v" field of the metadata JSON.
const MetadataVersion = 1

// Metadata is the metadata column. Keys with zero values are omitted.
type Metadata struct {
	V          int    `json:"v"`
	MTime      string `json:"mtime,omitempty"`
	Mode       string `json:"mode,omitempty"`
	LinkTarget string `json:"link_target,omitempty"`
}

// FileMetadata returns the file-mode metadata for a modification time and mode.
func FileMetadata(mtime time.Time, mode fs.FileMode) Metadata {
	m := Metadata{V: MetadataVersion, Mode: fmt.Sprintf("%04o", mode.Perm())}
	if !mtime.IsZero() {
		m.MTime = mtime.UTC().Format(time.RFC3339)
	}
	return m
}

// RegistryRow is one row of the file registry.
type RegistryRow struct {
	RelPath   string
	FileName  string
	FileSize  int64
	FileType  string
	FileMIME  string
	IsBinary  bool
	IsMedia   bool
	IsPicture bool
	IsVideo   bool
	IsLarge   bool
	Metadata  Metadata
}

// RegistryWriter writes registry rows to a part file and tracks the byte offset of the data
// written so far, which the scan checkpoints after Sync.
type RegistryWriter struct {
	f      *os.File // nil for a discarding writer (dry run)
	count  *countingWriter
	csv    *csv.Writer
	json   bytes.Buffer
	enc    *json.Encoder
	record []string
	synced int64 // offset made durable by the last Sync; -1 before the first
}

type countingWriter struct {
	w io.Writer
	n int64
}

func (c *countingWriter) Write(p []byte) (int, error) {
	n, err := c.w.Write(p)
	c.n += int64(n)
	return n, err
}

// ErrPartTooShort reports a part file shorter than the checkpointed offset.
var ErrPartTooShort = errors.New("registry part file is shorter than the checkpoint")

// CreateRegistry creates or truncates the part file at path and writes the header.
func CreateRegistry(path string) (*RegistryWriter, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	w := newRegistryWriter(f, f, 0)
	if err := w.csv.Write(RegistryHeader); err != nil {
		_ = f.Close()
		return nil, err
	}
	return w, nil
}

// ResumeRegistry opens the part file at path, truncates it to offset and appends after it. A
// missing file wraps fs.ErrNotExist; a file shorter than offset wraps ErrPartTooShort.
func ResumeRegistry(path string, offset int64) (*RegistryWriter, error) {
	f, err := openTruncated(path, offset)
	if err != nil {
		return nil, err
	}
	return newRegistryWriter(f, f, offset), nil
}

// DiscardRegistry returns a writer that formats rows without storing them, for dry runs.
func DiscardRegistry() *RegistryWriter {
	w := newRegistryWriter(nil, io.Discard, 0)
	_ = w.csv.Write(RegistryHeader)
	return w
}

func newRegistryWriter(f *os.File, dst io.Writer, offset int64) *RegistryWriter {
	w := &RegistryWriter{f: f, count: &countingWriter{w: dst, n: offset}, record: make([]string, len(RegistryHeader)), synced: -1}
	w.csv = csv.NewWriter(w.count)
	w.enc = json.NewEncoder(&w.json)
	w.enc.SetEscapeHTML(false)
	return w
}

// openTruncated opens an existing file for appending after truncating it to offset.
func openTruncated(path string, offset int64) (*os.File, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && info.Size() < offset {
		err = fmt.Errorf("%w: %s has %d bytes, checkpoint %d", ErrPartTooShort, path, info.Size(), offset)
	}
	if err == nil {
		err = f.Truncate(offset)
	}
	if err == nil {
		_, err = f.Seek(offset, io.SeekStart)
	}
	if err != nil {
		_ = f.Close()
		return nil, err
	}
	return f, nil
}

// Write buffers one row.
func (w *RegistryWriter) Write(r RegistryRow) error {
	w.json.Reset()
	if err := w.enc.Encode(r.Metadata); err != nil {
		return err
	}
	meta := bytes.TrimSuffix(w.json.Bytes(), []byte("\n"))
	w.record[0], w.record[1] = r.RelPath, r.FileName
	w.record[2] = strconv.FormatInt(r.FileSize, 10)
	w.record[3], w.record[4] = r.FileType, r.FileMIME
	w.record[5], w.record[6] = strconv.FormatBool(r.IsBinary), strconv.FormatBool(r.IsMedia)
	w.record[7], w.record[8] = strconv.FormatBool(r.IsPicture), strconv.FormatBool(r.IsVideo)
	w.record[9], w.record[10] = strconv.FormatBool(r.IsLarge), string(meta)
	return w.csv.Write(w.record)
}

// Sync flushes buffered rows, fsyncs the part file and returns the durable byte offset. When no
// row was written since the last Sync it does no I/O.
func (w *RegistryWriter) Sync() (int64, error) {
	w.csv.Flush()
	if err := w.csv.Error(); err != nil {
		return 0, err
	}
	if w.f != nil && w.count.n != w.synced {
		if err := w.f.Sync(); err != nil {
			return 0, err
		}
	}
	w.synced = w.count.n
	return w.count.n, nil
}

// Close syncs and closes the part file.
func (w *RegistryWriter) Close() error {
	_, err := w.Sync()
	if w.f != nil {
		err = errors.Join(err, w.f.Close())
		w.f = nil
	}
	return err
}
