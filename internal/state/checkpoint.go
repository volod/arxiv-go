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
}

// Sub returns c - o, the counters accumulated since o.
func (c Counters) Sub(o Counters) Counters {
	return Counters{
		Files: c.Files - o.Files, Bytes: c.Bytes - o.Bytes,
		VideosDone: c.VideosDone - o.VideosDone, VideosSkipped: c.VideosSkipped - o.VideosSkipped,
		VideosFailed: c.VideosFailed - o.VideosFailed, VideoBytes: c.VideoBytes - o.VideoBytes,
		ArchiveWritten: c.ArchiveWritten - o.ArchiveWritten, ArchiveFreed: c.ArchiveFreed - o.ArchiveFreed,
		VideoWritten: c.VideoWritten - o.VideoWritten, VideoFreed: c.VideoFreed - o.VideoFreed,
	}
}

// Checkpoint is the content of checkpoint.json. It speeds up resume and restores progress
// counters; the WAL stays the authority for file placement.
type Checkpoint struct {
	V              int       `json:"v"`
	RunID          string    `json:"run_id"`
	Op             string    `json:"op"`
	Phase          string    `json:"phase"`
	ScanCursor     []string  `json:"scan_cursor,omitempty"`
	RegistryOffset int64     `json:"registry_offset"`
	CandidateIndex int64     `json:"candidate_index"`
	WALOffset      int64     `json:"wal_offset"`
	Counters       Counters  `json:"counters"`
	ElapsedS       float64   `json:"elapsed_s"`
	WrittenAt      time.Time `json:"written_at"`
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
