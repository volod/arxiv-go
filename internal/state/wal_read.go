package state

import (
	"bufio"
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"sort"
	"strconv"
	"strings"
)

// load reads the WAL from the start, truncates a torn final line, and rebuilds indexes.
func (w *WAL) load() error {
	fi, err := w.f.Stat()
	if err != nil {
		return err
	}
	size := fi.Size()
	if size == 0 {
		return nil
	}
	r := bufio.NewReader(io.NewSectionReader(w.f, 0, size))
	var offset, lastGood int64
	var n int
	for {
		line, err := r.ReadBytes('\n')
		n++
		got := int64(len(line))
		atEOF := err == io.EOF
		if err != nil && !atEOF {
			return err
		}
		if got == 0 && atEOF {
			break
		}
		last := atEOF || offset+got == size
		payload := line
		if line[len(line)-1] == '\n' {
			payload = line[:len(line)-1]
		}
		var rec Record
		if err := json.Unmarshal(payload, &rec); err != nil {
			if last {
				return w.truncate(lastGood)
			}
			return fmt.Errorf("%w: %s: corrupt WAL line %d: %v", ErrStateCorrupt, w.path, n, err)
		}
		if rec.V != WALVersion || rec.TxID == "" || rec.Step == "" || rec.Seq <= 0 {
			return fmt.Errorf("%w: %s: invalid WAL record at line %d", ErrStateCorrupt, w.path, n)
		}
		if err := w.ingest(rec); err != nil {
			return err
		}
		offset += got
		lastGood = offset
		if atEOF {
			break
		}
	}
	return nil
}

func (w *WAL) ingest(rec Record) error {
	if rec.Seq <= w.seq {
		return fmt.Errorf("%w: %s: non-increasing seq %d after %d", ErrStateCorrupt, w.path, rec.Seq, w.seq)
	}
	if rec.Step == StepBegin {
		if _, ok := w.begin[rec.TxID]; ok {
			return fmt.Errorf("%w: %s: duplicate begin for %s", ErrStateCorrupt, w.path, rec.TxID)
		}
	} else if _, ok := w.begin[rec.TxID]; !ok {
		return fmt.Errorf("%w: %s: step %s before begin for %s", ErrStateCorrupt, w.path, rec.Step, rec.TxID)
	}
	w.seq = rec.Seq
	if n, err := txNumber(rec.TxID, w.runID); err == nil && n > w.nextTx {
		w.nextTx = n
	}
	w.note(rec)
	return nil
}

func (w *WAL) truncate(to int64) error {
	if err := w.f.Truncate(to); err != nil {
		return err
	}
	return w.f.Sync()
}

func (w *WAL) openLocked() []Tx {
	out := make([]Tx, 0, len(w.begin))
	for id, b := range w.begin {
		if _, ok := w.done[id]; ok {
			continue
		}
		out = append(out, Tx{Begin: b, Last: w.last[id]})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Begin.Seq < out[j].Begin.Seq })
	return out
}

func txNumber(txid, runID string) (int64, error) {
	prefix := runID + "-"
	s := txid
	if strings.HasPrefix(txid, prefix) {
		s = txid[len(prefix):]
	} else if i := strings.LastIndex(txid, "-"); i >= 0 {
		s = txid[i+1:]
	}
	return strconv.ParseInt(s, 10, 64)
}

// ReadWALRecords reads path as JSON Lines without opening it for append. A missing file
// returns (nil, nil). A torn last line without a newline is ignored.
func ReadWALRecords(path string) ([]Record, error) {
	data, err := os.ReadFile(path)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	if len(data) == 0 {
		return nil, nil
	}
	lines := bytes.Split(data, []byte("\n"))
	var out []Record
	for i, line := range lines {
		if len(line) == 0 {
			continue
		}
		if i == len(lines)-1 && data[len(data)-1] != '\n' {
			break // torn tail
		}
		var rec Record
		if err := json.Unmarshal(line, &rec); err != nil {
			return nil, fmt.Errorf("%w: %s: corrupt WAL line %d: %v", ErrStateCorrupt, path, i+1, err)
		}
		out = append(out, rec)
	}
	return out, nil
}
