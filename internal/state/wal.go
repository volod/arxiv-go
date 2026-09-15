package state

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
)

// WALVersion is the video transfer transaction record format version. Preview events use version 2.
const WALVersion = 1
const PreviewWALVersion = 2

// Step names a WAL record. Restore uses description_removed in place of described. Preview events are
// independent sub-records after the commit of the video they serve.
type Step string

// Transaction steps recorded in the WAL.
const (
	StepBegin              Step = "begin"
	StepCopied             Step = "copied"
	StepVerified           Step = "verified"
	StepPlaced             Step = "placed"
	StepDescribed          Step = "described"
	StepDescriptionRemoved Step = "description_removed"
	StepSourceRemoved      Step = "source_removed"
	StepCommit             Step = "commit"
	StepAborted            Step = "aborted"
	StepPreviewBegin       Step = "preview_begin"
	StepPreviewDone        Step = "preview_done"
	StepPreviewFailed      Step = "preview_failed"
	StepPreviewDelete      Step = "preview_delete"
	StepPreviewDeleted     Step = "preview_deleted"
)

// Transfer recorded on begin: copy path vs same-device rename.
const (
	TransferCopy   = "copy"
	TransferRename = "rename"
)

// CrashHook is called after a WAL record is on disk (and fsynced when the step requires it).
// Tests inject a failure; production passes nil.
type CrashHook func(point string) error

// Record is one JSON Lines WAL record. Begin carries the full payload; later steps carry step
// data only (sha256, description, reason).
type Record struct {
	V           int       `json:"v"`
	TxID        string    `json:"txid"`
	Seq         int64     `json:"seq"`
	Step        Step      `json:"step"`
	TS          time.Time `json:"ts"`
	Op          string    `json:"op,omitempty"`
	RelPath     string    `json:"rel_path,omitempty"`
	Src         string    `json:"src,omitempty"`
	Dst         string    `json:"dst,omitempty"`
	Size        int64     `json:"size,omitempty"`
	Mtime       time.Time `json:"mtime,omitempty"`
	Transfer    string    `json:"transfer,omitempty"`
	SHA256      string    `json:"sha256,omitempty"`
	Description string    `json:"description,omitempty"`
	Reason      string    `json:"reason,omitempty"`
}

// Begin is the payload of a begin record.
type Begin struct {
	Op       string
	RelPath  string
	Src      string
	Dst      string
	Size     int64
	Mtime    time.Time
	Transfer string
}

// Tx is one transaction reconstructed from the WAL.
type Tx struct {
	Begin Record
	Last  Record
}

// WALOptions are optional WAL open settings.
type WALOptions struct {
	Now   func() time.Time
	Crash CrashHook
}

// WAL is the append-only transaction log at runs/<id>/wal.jsonl.
type WAL struct {
	path     string
	runID    string
	f        *os.File
	now      func() time.Time
	crash    CrashHook
	mu       sync.Mutex
	seq      int64
	nextTx   int64
	begin    map[string]Record
	last     map[string]Record
	done     map[string]Step
	commits  *CommittedSet
	previews map[string]Record
}

// OpenWAL opens or creates path. A torn final line is truncated; a decode or version failure
// on any earlier line wraps ErrStateCorrupt.
func OpenWAL(path, runID string, opts WALOptions) (*WAL, error) {
	if runID == "" {
		return nil, fmt.Errorf("wal: empty run id")
	}
	if opts.Now == nil {
		opts.Now = time.Now
	}
	_, statErr := os.Stat(path)
	created := errors.Is(statErr, fs.ErrNotExist)
	if statErr != nil && !created {
		return nil, statErr
	}
	f, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE|os.O_APPEND, 0o644)
	if err != nil {
		return nil, err
	}
	w := &WAL{
		path: path, runID: runID, f: f, now: opts.Now, crash: opts.Crash,
		begin: make(map[string]Record), last: make(map[string]Record), done: make(map[string]Step),
		commits:  NewCommittedSet(),
		previews: make(map[string]Record),
	}
	if err := w.load(); err != nil {
		f.Close()
		return nil, err
	}
	if created {
		if err := fsops.SyncDir(filepath.Dir(path)); err != nil {
			f.Close()
			return nil, err
		}
	}
	return w, nil
}

// Path returns the WAL file path.
func (w *WAL) Path() string { return w.path }

// Offset returns the current file size, which checkpoints store as wal_offset.
func (w *WAL) Offset() int64 {
	w.mu.Lock()
	defer w.mu.Unlock()
	fi, err := w.f.Stat()
	if err != nil {
		return 0
	}
	return fi.Size()
}

// Committed returns the set of rel_path values whose transactions committed.
func (w *WAL) Committed() *CommittedSet { return w.commits }

// OpenTransactions returns incomplete transactions (no commit or aborted), ordered by begin seq.
func (w *WAL) OpenTransactions() []Tx {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.openLocked()
}

// Begin appends a durable begin record and returns it.
func (w *WAL) Begin(b Begin) (Record, error) {
	w.mu.Lock()
	w.nextTx++
	rec := Record{
		TxID: formatTxID(w.runID, w.nextTx), Step: StepBegin,
		Op: b.Op, RelPath: b.RelPath, Src: b.Src, Dst: b.Dst,
		Size: b.Size, Mtime: b.Mtime.UTC(), Transfer: b.Transfer,
	}
	rec, err := w.appendLocked(rec)
	w.mu.Unlock()
	if err != nil {
		return rec, err
	}
	return rec, w.hit("wal:" + string(rec.Step))
}

// Append writes a later step for txid. Fields on extra other than step data are ignored.
func (w *WAL) Append(txid string, step Step, extra Record) (Record, error) {
	w.mu.Lock()
	if _, ok := w.begin[txid]; !ok {
		w.mu.Unlock()
		return Record{}, fmt.Errorf("wal: unknown txid %s", txid)
	}
	if done, ok := w.done[txid]; ok {
		w.mu.Unlock()
		return Record{}, fmt.Errorf("wal: transaction %s already %s", txid, done)
	}
	rec := Record{TxID: txid, Step: step, SHA256: extra.SHA256, Description: extra.Description, Reason: extra.Reason}
	rec, err := w.appendLocked(rec)
	w.mu.Unlock()
	if err != nil {
		return rec, err
	}
	return rec, w.hit("wal:" + string(rec.Step))
}

func (w *WAL) appendLocked(rec Record) (Record, error) {
	rec.V = WALVersion
	if isPreviewStep(rec.Step) {
		rec.V = PreviewWALVersion
	}
	w.seq++
	rec.Seq = w.seq
	rec.TS = w.now().UTC()
	line, err := encodeRecord(rec)
	if err != nil {
		w.seq--
		return Record{}, err
	}
	if _, err := w.f.Write(line); err != nil {
		w.seq--
		return Record{}, err
	}
	w.note(rec)
	if stepNeedsFsync(rec.Step) {
		if err := w.f.Sync(); err != nil {
			return rec, err
		}
	}
	return rec, nil
}

func (w *WAL) hit(point string) error {
	if w.crash == nil {
		return nil
	}
	return w.crash(point)
}

func stepNeedsFsync(s Step) bool {
	return s != StepCopied && s != StepVerified
}

func encodeRecord(rec Record) ([]byte, error) {
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(rec); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

func formatTxID(runID string, n int64) string {
	return fmt.Sprintf("%s-%06d", runID, n)
}

// Close fsyncs and closes the WAL file.
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.f == nil {
		return nil
	}
	err := w.f.Sync()
	if cerr := w.f.Close(); err == nil {
		err = cerr
	}
	w.f = nil
	return err
}

func (w *WAL) note(rec Record) {
	switch rec.Step {
	case StepPreviewBegin, StepPreviewDelete:
		w.previews[rec.TxID] = rec
	case StepPreviewDone, StepPreviewFailed, StepPreviewDeleted:
		delete(w.previews, rec.TxID)
	case StepBegin:
		w.begin[rec.TxID] = rec
		w.last[rec.TxID] = rec
	case StepCommit:
		w.last[rec.TxID] = rec
		w.done[rec.TxID] = rec.Step
		if b, ok := w.begin[rec.TxID]; ok {
			w.commits.Add(b.RelPath)
		}
	case StepAborted:
		w.last[rec.TxID] = rec
		w.done[rec.TxID] = rec.Step
	default:
		w.last[rec.TxID] = rec
	}
}
