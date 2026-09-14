package archive

import (
	"context"
	"fmt"
	"os"
	"path/filepath"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

func restoreDestRel(v Candidate, byVideo map[string]report.VideoRow) string {
	if row, ok := byVideo[v.RelPath]; ok && row.RelPath != "" {
		return row.RelPath
	}
	return v.RelPath
}

// restoreCandidate moves one video back in a transaction: begin, place, placed, remove or keep the
// stub, stub_removed, remove the source after a copy unless it is kept, commit.
func restoreCandidate(ctx context.Context, s *Session, w *state.WAL, r *RestoreResolver, v Candidate, destRel string, byVideo map[string]report.VideoRow, c *RestoreConfig, list string) error {
	src := filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(v.RelPath))
	dst := filepath.Join(s.cfg.Archive, filepath.FromSlash(destRel))
	if row, ok := byVideo[v.RelPath]; !ok {
		s.Log.Info("unregistered video; restoring to the same relative path", "rel_path", v.RelPath)
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
			skipVideo(s, v, missingSourceReason(v.RelPath))
			return nil
		}
		missing, err := ensureRestoreDirs(s, dst, c.CreateDirs)
		if err != nil {
			return err
		}
		if missing {
			skipVideo(s, v, "missing-directory")
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
			skipVideo(s, v, "destination exists with different content")
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
		} else if _, err := placeVideo(ctx, s, w, rec, t.mode, c.Verify, conflict, c.StageCopy); err != nil {
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

// finishRestore logs placed, removes or keeps the stub, removes the source of a copy unless it is
// kept and commits.
func finishRestore(s *Session, w *state.WAL, r *RestoreResolver, rec state.Record, c *RestoreConfig) error {
	tx := state.Tx{Begin: rec, Last: rec}
	if _, err := w.Append(rec.TxID, state.StepPlaced, state.Record{}); err != nil {
		return err
	}
	stub := r.StubPath(tx)
	if err := r.WriteStub(tx); err != nil {
		return err
	}
	if _, err := w.Append(rec.TxID, state.StepStubRemoved, state.Record{Stub: stub}); err != nil {
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
	s.Stats.VideosDone.Add(1)
	s.Stats.VideoBytes.Add(rec.Size)
	s.Stats.ArchiveWritten.Add(rec.Size)
	if !c.KeepSource {
		s.Stats.VideoArchiveFreed.Add(rec.Size)
	}
	logMoved(s, "video restored", rec, c.Scan.LargeThreshold)
	return nil
}

// videoRowsByVideoPath indexes registry rows by video_rel_path. The registry lives in a root that
// may be shared, so a row whose paths would leave a root or name a reserved path is ignored and
// its video, if any, is restored as unregistered to its own relative path.
func videoRowsByVideoPath(s *Session, rows []report.VideoRow) map[string]report.VideoRow {
	out := make(map[string]report.VideoRow, len(rows))
	for _, r := range rows {
		key := r.VideoRelPath
		if key == "" {
			key = r.RelPath
		}
		if !scanner.LocalRelPath(key) || !scanner.LocalRelPath(r.RelPath) ||
			(r.StubRelPath != "" && !scanner.LocalRelPath(r.StubRelPath)) {
			s.Log.Warn("video registry row ignored: a path is not a local path below its root",
				"rel_path", r.RelPath, "video_rel_path", r.VideoRelPath, "stub_rel_path", r.StubRelPath)
			continue
		}
		out[key] = r
	}
	return out
}

func loadVideoRegistry(archive, video string) ([]report.VideoRow, error) {
	rows, err := report.LoadVideoFile(filepath.Join(archive, scanner.VideoRegistryName))
	if err != nil {
		return nil, fmt.Errorf("%w: cannot parse %s: %v; restore a valid registry backup or repair the CSV, then rerun", state.ErrStateCorrupt, filepath.Join(archive, scanner.VideoRegistryName), err)
	}
	if rows == nil && video != "" {
		rows, err = report.LoadVideoFile(filepath.Join(video, scanner.VideoRegistryName))
		if err != nil {
			return nil, fmt.Errorf("%w: cannot parse %s: %v; restore a valid registry backup or repair the CSV, then rerun", state.ErrStateCorrupt, filepath.Join(video, scanner.VideoRegistryName), err)
		}
	}
	return rows, nil
}
