package archive

import (
	"context"
	"os"
	"path/filepath"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

func restoreDestRel(v Candidate, byRel map[string]report.PayloadRow) string {
	if row, ok := byRel[v.RelPath]; ok && row.RelPath != "" {
		return row.RelPath
	}
	return v.RelPath
}

// restoreCandidate moves one payload file back in a transaction: begin, place, placed, remove or keep the
// description, description_removed, remove the source after a copy unless it is kept, commit.
func restoreCandidate(ctx context.Context, s *Session, w *state.WAL, r *RestoreResolver, v Candidate, destRel string, byRel map[string]report.PayloadRow, c *RestoreConfig, list string) error {
	src := filepath.Join(s.cfg.Payload.Root, filepath.FromSlash(v.RelPath))
	dst := filepath.Join(s.cfg.Archive, filepath.FromSlash(destRel))
	if row, ok := byRel[v.RelPath]; !ok {
		s.Log.Info("unregistered "+s.payload.noun+"; restoring to the same relative path", "rel_path", v.RelPath)
	} else if row.Status == report.StatusRestored {
		s.Log.Info("registry row already restored; filesystem is the source of truth", "rel_path", destRel)
	}
	mode, err := transferMode(s, c.Transfer)
	if err != nil {
		return err
	}
	t := &transferAttempt{s: s, w: w, list: list, transfer: &c.Transfer, mode: mode}
	for {
		fi, err := os.Lstat(src)
		if err != nil || !fi.Mode().IsRegular() {
			skipCandidate(s, v, missingSourceReason(v.RelPath))
			return nil
		}
		missing, err := ensureRestoreDirs(s, dst, c.CreateDirs)
		if err != nil {
			return err
		}
		if missing {
			skipCandidate(s, v, "missing-directory")
			return nil
		}
		adopted, conflict, err := destinationStatus(src, dst, fi.Size(), c.Verify)
		if err != nil {
			return err
		}
		if conflict && !c.Overwrite {
			if err := removeStalePart(dst); err != nil {
				return err
			}
			skipCandidate(s, v, "destination exists with different content")
			return nil
		}
		if conflict || adopted && !c.KeepSource {
			t.mode = state.TransferCopy
		}
		rec, err := w.Begin(state.Begin{Op: opRestore, RelPath: destRel, Src: src, Dst: dst,
			Size: fi.Size(), Mtime: fi.ModTime(), Transfer: t.mode})
		if err != nil {
			return err
		}
		s.Log.Debug("restore begin", "rel_path", destRel, "transfer", t.mode, "size", fi.Size())
		if err := ctx.Err(); err != nil {
			return err
		}
		if adopted {
			s.Log.Info("adopted existing destination", "rel_path", destRel)
		} else if _, err := placePayload(ctx, s, w, rec, t.mode, c.Verify, conflict, c.StageCopy); err != nil {
			switch outcome, err := t.placeFailed(ctx, rec, v, err); outcome {
			case placeRetry:
				if err != nil {
					return err
				}
				continue
			case placeSkip:
				return nil
			default:
				return err
			}
		}
		return finishRestore(s, w, r, rec, c)
	}
}

// finishRestore logs placed, removes or keeps the description, removes the source of a copy unless it is
// kept and commits.
func finishRestore(s *Session, w *state.WAL, r *RestoreResolver, rec state.Record, c *RestoreConfig) error {
	tx := state.Tx{Begin: rec, Last: rec}
	if _, err := w.Append(rec.TxID, state.StepPlaced, state.Record{}); err != nil {
		return err
	}
	description := r.DescriptionPath(tx)
	if err := r.WriteDescription(tx); err != nil {
		return err
	}
	if _, err := w.Append(rec.TxID, state.StepDescriptionRemoved, state.Record{Description: description}); err != nil {
		return err
	}
	if rec.Transfer == state.TransferCopy && !c.KeepSource {
		if err := r.RemoveSource(tx); err != nil {
			return err
		}
		if _, err := w.Append(rec.TxID, state.StepSourceRemoved, state.Record{}); err != nil {
			return err
		}
	}
	if _, err := w.Append(rec.TxID, state.StepCommit, state.Record{}); err != nil {
		return err
	}
	counters := s.payload.counters(s.Stats)
	counters.done.Add(1)
	counters.bytes.Add(rec.Size)
	s.Stats.ArchiveWritten.Add(rec.Size)
	if !c.KeepSource {
		counters.mirrorFreed.Add(rec.Size)
	}
	logMoved(s, s.payload.noun+" restored", rec, c.Scan.LargeThreshold)
	return nil
}
