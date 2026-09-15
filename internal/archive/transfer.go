package archive

import (
	"context"
	"errors"
	"io/fs"
	"os"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// stageCopyFunc is the copy seam shared by split and restore; nil uses fsops.StageCopy.
type stageCopyFunc func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error)

// placePayload moves rec.Src to rec.Dst: a verified rename on the same device, otherwise a staged
// copy logged as copied and verified, then renamed (or, with overwrite, replacing dst). It returns
// the SHA-256 of a hash-verified copy.
func placePayload(ctx context.Context, s *Session, w *state.WAL, rec state.Record, mode string, verify fsops.VerifyMode, overwrite bool, stage stageCopyFunc) (string, error) {
	if mode == state.TransferRename && !overwrite {
		fi, err := os.Lstat(rec.Src)
		if err != nil || !fi.Mode().IsRegular() || fi.Size() != rec.Size || !fi.ModTime().Equal(rec.Mtime) {
			return "", fsops.ErrSourceChanged
		}
		if err := placeFile(s, s.cfg.FS.Rename, rec.Src, rec.Dst, rec.Size); err != nil {
			return "", err
		}
		return "", hitCrash(s.cfg.Crash, "fs:place")
	}
	if stage == nil {
		stage = fsops.StageCopy
	}
	res, err := stage(ctx, rec.Src, rec.Dst, fsops.CopyOptions{
		Verify: verify, Overwrite: overwrite, ExpectSize: rec.Size, ExpectModTime: rec.Mtime,
	})
	if err != nil {
		return "", err
	}
	if err := hitCrash(s.cfg.Crash, "fs:copy"); err != nil {
		return "", err
	}
	if _, err := w.Append(rec.TxID, state.StepCopied, state.Record{}); err != nil {
		return "", err
	}
	if _, err := w.Append(rec.TxID, state.StepVerified, state.Record{SHA256: res.SHA256}); err != nil {
		return "", err
	}
	move := s.cfg.FS.Rename
	if overwrite {
		move = s.cfg.FS.Replace
	}
	if err := placeFile(s, move, fsops.PartPath(rec.Dst), rec.Dst, rec.Size); err != nil {
		return "", err
	}
	return res.SHA256, hitCrash(s.cfg.Crash, "fs:place")
}

// placeFile moves oldpath to dst. A directory-flush failure after the move is already placed:
// recovery would roll forward, so the run continues from placed instead of aborting.
func placeFile(s *Session, move func(string, string) error, oldpath, dst string, size int64) error {
	err := move(oldpath, dst)
	if err == nil {
		return nil
	}
	var le *os.LinkError
	if errors.As(err, &le) {
		return err
	}
	fi, st := os.Lstat(dst)
	if st != nil || !fi.Mode().IsRegular() || fi.Size() != size {
		return err
	}
	s.Log.Warn("destination placed but directory flush failed; continuing", "dst", dst, "error", err)
	return nil
}

// placeOutcome tells a transfer loop how to continue after placePayload failed.
type placeOutcome int

const (
	placeFatal placeOutcome = iota // return the error
	placeRetry                     // the transaction was aborted; begin a new one
	placeSkip                      // the transaction was aborted and the file skipped
)

// transferAttempt is the state of one payload file's transfer loop.
type transferAttempt struct {
	s        *Session
	w        *state.WAL
	list     string  // candidate list, for a new preflight when falling back to copies
	transfer *string // the operation's --transfer setting, switched to copy after EXDEV
	mode     string  // transfer mode of the next transaction
	changes  int     // source changes seen so far
}

// placeFailed aborts the transaction of a failed placement. A destination that appeared is a
// conflict, a rename that crossed devices retries as a copy after a new preflight, and a source
// that changed is retried once.
func (t *transferAttempt) placeFailed(ctx context.Context, rec state.Record, v Candidate, err error) (placeOutcome, error) {
	switch {
	case errors.Is(err, fs.ErrExist):
		if err := abortConflict(t.s, t.w, rec); err != nil {
			return placeFatal, err
		}
		skipCandidate(t.s, v, "destination appeared during transfer")
		return placeSkip, nil
	case fsops.IsCrossDevice(err) && t.mode == state.TransferRename:
		if _, err := t.w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "cross-device rename"}); err != nil {
			return placeFatal, err
		}
		t.s.Log.Warn("rename crossed devices; falling back to copy", "rel_path", rec.RelPath)
		t.mode, *t.transfer = state.TransferCopy, transferCopy
		left, err := countRemaining(t.list, t.w)
		if err != nil {
			return placeFatal, err
		}
		return placeRetry, preflightCopyFallback(ctx, t.s, left)
	case errors.Is(err, fsops.ErrSourceChanged):
		if err := removeStalePart(rec.Dst); err != nil {
			return placeFatal, err
		}
		if _, err := t.w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "source changed"}); err != nil {
			return placeFatal, err
		}
		if t.changes++; t.changes < 2 {
			t.s.Log.Warn("source changed during transfer; retrying", "rel_path", rec.RelPath)
			return placeRetry, nil
		}
		skipCandidate(t.s, v, "source changed twice during transfer")
		return placeSkip, nil
	}
	return placeFatal, err
}

// preflightCopyFallback sizes all remaining transfers as copies: a rename reporting EXDEV
// overrides a stale device classification.
func preflightCopyFallback(ctx context.Context, s *Session, left Candidates) error {
	oldFS, oldTransfer := s.cfg.FS, s.cfg.Preflight.Transfer
	s.cfg.FS = forcedOtherDevice{oldFS}
	s.cfg.Preflight.Transfer = transferCopy
	defer func() { s.cfg.FS, s.cfg.Preflight.Transfer = oldFS, oldTransfer }()
	_, err := s.Preflight(ctx, left)
	return err
}

type forcedOtherDevice struct{ fsops.Ops }

func (forcedOtherDevice) SameDevice(string, string) (bool, error) { return false, nil }

// abortConflict ends a transaction whose destination appeared before placement. The abort is
// durable before the part file is removed: a copied or verified transaction without its part
// and with a same-size destination would otherwise be rolled forward, removing the source.
func abortConflict(s *Session, w *state.WAL, rec state.Record) error {
	if _, err := w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "destination conflict"}); err != nil {
		return err
	}
	if err := removeStalePart(rec.Dst); err != nil {
		return err
	}
	return hitCrash(s.cfg.Crash, "fs:delete_part")
}

// transferMode is rename when the roots share a device and the operation allows it. Classifying
// from the roots makes a same-device layout always rename; a later EXDEV still falls back to copy.
func transferMode(s *Session, transfer string) (string, error) {
	if transfer == transferCopy {
		return state.TransferCopy, nil
	}
	same, err := s.cfg.FS.SameDevice(s.cfg.Archive, s.cfg.Payload.Root)
	if err != nil || !same {
		return state.TransferCopy, err
	}
	return state.TransferRename, nil
}

func hitCrash(h state.CrashHook, point string) error {
	if h != nil {
		return h(point)
	}
	return nil
}
