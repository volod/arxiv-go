package archive

import (
	"bufio"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/volod/arxiv-go/internal/report"
)

// CandidateVersion is the "v" field of candidates.jsonl records.
const CandidateVersion = 1

// Candidate is one video found by the scan, in walk order. Split and restore read the list from
// the run directory instead of scanning again when they resume.
type Candidate struct {
	V        int       `json:"v"`
	RelPath  string    `json:"rel_path"`
	Size     int64     `json:"size"`
	MTime    time.Time `json:"mtime"`
	MIME     string    `json:"mime"`
	FileType string    `json:"file_type,omitempty"`
}

// candidateWriter appends candidates to candidates.jsonl and tracks the durable offset.
type candidateWriter struct {
	f      *os.File
	buf    *bufio.Writer
	n      int64 // bytes written, including buffered ones
	synced int64 // bytes made durable by the last sync
}

func createCandidates(path string) (*candidateWriter, error) {
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_TRUNC, 0o644)
	if err != nil {
		return nil, err
	}
	return &candidateWriter{f: f, buf: bufio.NewWriterSize(f, 64<<10)}, nil
}

func resumeCandidates(path string, offset int64) (*candidateWriter, error) {
	f, err := os.OpenFile(path, os.O_RDWR, 0)
	if err != nil {
		return nil, err
	}
	info, err := f.Stat()
	if err == nil && info.Size() < offset {
		err = fmt.Errorf("%w: %s has %d bytes, checkpoint %d", report.ErrPartTooShort, path, info.Size(), offset)
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
	return &candidateWriter{f: f, buf: bufio.NewWriterSize(f, 64<<10), n: offset, synced: offset}, nil
}

func (w *candidateWriter) write(c Candidate) error {
	c.V = CandidateVersion
	line, err := json.Marshal(c)
	if err != nil {
		return err
	}
	line = append(line, '\n')
	n, err := w.buf.Write(line)
	w.n += int64(n)
	return err
}

// sync flushes and fsyncs new records; with nothing new since the last sync it does no I/O.
func (w *candidateWriter) sync() (int64, error) {
	if w.n == w.synced {
		return w.n, nil
	}
	if err := w.buf.Flush(); err != nil {
		return 0, err
	}
	if err := w.f.Sync(); err != nil {
		return 0, err
	}
	w.synced = w.n
	return w.n, nil
}

func (w *candidateWriter) close() error {
	_, err := w.sync()
	return errors.Join(err, w.f.Close())
}

// ReadCandidates calls fn for every candidate in the list at path, in walk order.
func ReadCandidates(path string, fn func(Candidate) error) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64<<10), 16<<20)
	for line := 1; sc.Scan(); line++ {
		var c Candidate
		if err := json.Unmarshal(sc.Bytes(), &c); err != nil {
			return fmt.Errorf("%s:%d: %w", path, line, err)
		}
		if c.V != CandidateVersion {
			return fmt.Errorf("%s:%d: unsupported candidate version %d", path, line, c.V)
		}
		if err := fn(c); err != nil {
			return err
		}
	}
	return sc.Err()
}
