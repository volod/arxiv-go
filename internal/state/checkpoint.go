package state

import (
	"errors"
	"fmt"
	"io/fs"
	"time"
)

// CheckpointVersion is the checkpoint.json format version.
const CheckpointVersion = 1

// Counters are the cumulative run counters stored in checkpoints and reports.
type Counters struct {
	Files         int64 `json:"files"`
	Bytes         int64 `json:"bytes"`
	VideosDone    int64 `json:"videos_done"`
	VideosSkipped int64 `json:"videos_skipped"`
	VideosFailed  int64 `json:"videos_failed"`
	// VideoBytes counts bytes of videos handled (done, skipped or failed) in split or restore.
	VideoBytes int64 `json:"video_bytes,omitempty"`
	// Bytes written to and freed from each root.
	ArchiveWritten int64 `json:"archive_bytes_written,omitempty"`
	ArchiveFreed   int64 `json:"archive_bytes_freed,omitempty"`
	VideoWritten   int64 `json:"video_archive_bytes_written,omitempty"`
	VideoFreed     int64 `json:"video_archive_bytes_freed,omitempty"`
	// Previews generated and failed by split; a failed preview never counts as a failed video.
	PreviewsDone   int64 `json:"previews_done,omitempty"`
	PreviewsFailed int64 `json:"previews_failed,omitempty"`
	// CATIA text sidecars generated and failed by split --catia-text; a failed sidecar never
	// counts as a failed CATIA file.
	TextsDone   int64 `json:"texts_done,omitempty"`
	TextsFailed int64 `json:"texts_failed,omitempty"`
	// CATIA split and restore counters; a CATIA run never updates the video counters.
	CatiaDone    int64 `json:"catia_done,omitempty"`
	CatiaSkipped int64 `json:"catia_skipped,omitempty"`
	CatiaFailed  int64 `json:"catia_failed,omitempty"`
	CatiaBytes   int64 `json:"catia_bytes,omitempty"`
	CatiaWritten int64 `json:"catia_archive_bytes_written,omitempty"`
	CatiaFreed   int64 `json:"catia_archive_bytes_freed,omitempty"`
}

// Sub returns c - o, the counters accumulated since o.
func (c Counters) Sub(o Counters) Counters {
	return Counters{
		Files: c.Files - o.Files, Bytes: c.Bytes - o.Bytes,
		VideosDone: c.VideosDone - o.VideosDone, VideosSkipped: c.VideosSkipped - o.VideosSkipped,
		VideosFailed: c.VideosFailed - o.VideosFailed, VideoBytes: c.VideoBytes - o.VideoBytes,
		ArchiveWritten: c.ArchiveWritten - o.ArchiveWritten, ArchiveFreed: c.ArchiveFreed - o.ArchiveFreed,
		VideoWritten: c.VideoWritten - o.VideoWritten, VideoFreed: c.VideoFreed - o.VideoFreed,
		PreviewsDone: c.PreviewsDone - o.PreviewsDone, PreviewsFailed: c.PreviewsFailed - o.PreviewsFailed,
		TextsDone: c.TextsDone - o.TextsDone, TextsFailed: c.TextsFailed - o.TextsFailed,
		CatiaDone: c.CatiaDone - o.CatiaDone, CatiaSkipped: c.CatiaSkipped - o.CatiaSkipped,
		CatiaFailed: c.CatiaFailed - o.CatiaFailed, CatiaBytes: c.CatiaBytes - o.CatiaBytes,
		CatiaWritten: c.CatiaWritten - o.CatiaWritten, CatiaFreed: c.CatiaFreed - o.CatiaFreed,
	}
}

// Checkpoint is the content of checkpoint.json. It speeds up resume and restores progress
// counters; the WAL stays the authority for file placement.
type Checkpoint struct {
	V              int      `json:"v"`
	RunID          string   `json:"run_id"`
	Op             string   `json:"op"`
	Phase          string   `json:"phase"`
	ScanCursor     []string `json:"scan_cursor,omitempty"`
	RegistryOffset int64    `json:"registry_offset"`
	// CandidatesOffset is the durable length of candidates.jsonl written with RegistryOffset.
	CandidatesOffset int64    `json:"candidates_offset,omitempty"`
	CandidateIndex   int64    `json:"candidate_index"`
	WALOffset        int64    `json:"wal_offset"`
	Counters         Counters `json:"counters"`
	ElapsedS         float64  `json:"elapsed_s"`
	// RegistryBase identifies the base registry the scan reuses rows from; nil without a base. A
	// resumed scan whose base no longer matches starts again from the beginning.
	RegistryBase *FileID `json:"registry_base,omitempty"`
	// Scan holds the scan statistics matching ScanCursor and the offsets; nil before the scan.
	Scan      *ScanStats `json:"scan,omitempty"`
	WrittenAt time.Time  `json:"written_at"`
}

// FileID identifies the content of a file by its size and SHA-256.
type FileID struct {
	Size   int64  `json:"size"`
	SHA256 string `json:"sha256"`
}

// SameFileID reports whether a and b identify the same content; two nil identities are the same.
func SameFileID(a, b *FileID) bool {
	if a == nil || b == nil {
		return a == b
	}
	return *a == *b
}

// ErrNoCheckpoint reports that a run has not written a checkpoint yet.
var ErrNoCheckpoint = errors.New("no checkpoint")

// WriteCheckpoint atomically replaces the checkpoint at path through checkpoint.json.arxgo-part.
func WriteCheckpoint(path string, cp Checkpoint) error {
	cp.V = CheckpointVersion
	return WriteJSON(path, cp)
}

// ReadCheckpoint reads the last durable checkpoint. A leftover part file from an interrupted write
// is ignored. A missing file wraps ErrNoCheckpoint; an undecodable file or unknown version wraps
// ErrStateCorrupt.
func ReadCheckpoint(path string) (Checkpoint, error) {
	var cp Checkpoint
	err := ReadJSON(path, &cp)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return Checkpoint{}, fmt.Errorf("%w: %s", ErrNoCheckpoint, path)
	case err != nil:
		return Checkpoint{}, err
	case cp.V != CheckpointVersion:
		return Checkpoint{}, fmt.Errorf("%w: %s: unsupported checkpoint version %d", ErrStateCorrupt, path, cp.V)
	}
	return cp, nil
}

// Throttle decides when periodic work is due: after every Every counted items or after Interval,
// whichever comes first. Every <= 0 disables the count trigger. It is not safe for concurrent use.
type Throttle struct {
	every    int64
	interval time.Duration
	now      func() time.Time
	last     time.Time
	pending  int64
}

// NewThrottle starts a throttle whose interval is measured from now().
func NewThrottle(every int, interval time.Duration, now func() time.Time) *Throttle {
	return &Throttle{every: int64(every), interval: interval, now: now, last: now()}
}

// Add counts n items and reports whether the work is due.
func (t *Throttle) Add(n int64) bool {
	t.pending += n
	return t.Due()
}

// Due reports whether the count or the interval has been reached since the last Reset.
func (t *Throttle) Due() bool {
	if t.every > 0 && t.pending >= t.every {
		return true
	}
	return t.interval > 0 && t.now().Sub(t.last) >= t.interval
}

// Reset records that the work was just done.
func (t *Throttle) Reset() {
	t.pending = 0
	t.last = t.now()
}
