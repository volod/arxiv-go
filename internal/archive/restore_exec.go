package archive

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"

	"github.com/volod/arxiv-go/internal/fsops"
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

func restoreCandidate(ctx context.Context, s *Session, w *state.WAL, r *RestoreResolver, v Candidate, destRel string, byVideo map[string]report.VideoRow, c *RestoreConfig, list string) error {
	src := filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(v.RelPath))
	dst := filepath.Join(s.cfg.Archive, filepath.FromSlash(destRel))
	if row, ok := byVideo[v.RelPath]; !ok {
		s.Log.Info("unregistered video; restoring to the same relative path", "rel_path", v.RelPath)
	} else if row.Status == report.StatusRestored {
		s.Log.Info("registry row already restored; filesystem is the source of truth", "rel_path", destRel)
	}
	mode := state.TransferCopy
	if c.Transfer != transferCopy {
		same, err := s.cfg.FS.SameDevice(s.cfg.Archive, s.cfg.VideoArchive)
		if err != nil {
			return err
		}
		if same {
			mode = state.TransferRename
		}
	}
	for attempt := 0; attempt < 2; attempt++ {
		fi, err := os.Lstat(src)
		if err != nil || !fi.Mode().IsRegular() {
			skipSplit(s, v, "source missing or no longer a regular file")
			return nil
		}
		missing, err := ensureRestoreDirs(s, dst, c.CreateDirs)
		if err != nil {
			return err
		}
		if missing {
			skipSplit(s, v, "missing-directory")
			return nil
		}
		adopted, conflict, err := destinationStatus(src, dst, fi.Size(), c.Verify)
		if err != nil {
			return err
		}
		overwrite := false
		if conflict {
			if !c.Overwrite {
				skipSplit(s, v, "destination exists with different content")
				return nil
			}
			overwrite = true
			mode = state.TransferCopy
		}
		if adopted && !c.KeepSource {
			mode = state.TransferCopy
		}
		begin := state.Begin{Op: opRestore, RelPath: destRel, Src: src, Dst: dst,
			Size: fi.Size(), Mtime: fi.ModTime(), Transfer: mode}
		rec, err := w.Begin(begin)
		if err != nil {
			return err
		}
		s.Log.Debug("restore begin", "rel_path", destRel, "transfer", mode, "size", fi.Size())
		tx := state.Tx{Begin: rec, Last: rec}
		if err := ctx.Err(); err != nil {
			return err
		}
		if !adopted {
			_, err = placeVideo(ctx, s, w, rec, mode, c.Verify, overwrite, c.StageCopy)
			retry, done, e := restorePlaceError(ctx, s, w, rec, v, destRel, list, c, &mode, attempt, err)
			if e != nil {
				return e
			}
			if done {
				return nil
			}
			if retry {
				continue
			}
		} else {
			s.Log.Info("adopted existing destination", "rel_path", destRel)
		}
		if _, err := w.Append(rec.TxID, state.StepPlaced, state.Record{}); err != nil {
			return err
		}
		if err := r.WriteStub(tx); err != nil {
			return err
		}
		if _, err := w.Append(rec.TxID, state.StepStubRemoved, state.Record{Stub: r.StubPath(tx)}); err != nil {
			return err
		}
		if mode == state.TransferCopy && !c.KeepSource {
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
		s.Stats.VideoBytes.Add(fi.Size())
		s.Stats.ArchiveWritten.Add(fi.Size())
		if !c.KeepSource {
			s.Stats.VideoArchiveFreed.Add(fi.Size())
		}
		if fi.Size() >= c.Scan.LargeThreshold {
			s.Log.Info("video restored", "rel_path", destRel, "bytes", fi.Size(), "transfer", mode)
		} else {
			s.Log.Debug("video restored", "rel_path", destRel, "bytes", fi.Size(), "transfer", mode)
		}
		return nil
	}
	return nil
}

func restorePlaceError(ctx context.Context, s *Session, w *state.WAL, rec state.Record, v Candidate, destRel, list string, c *RestoreConfig, mode *string, attempt int, err error) (retry, done bool, out error) {
	if err == nil {
		return false, false, nil
	}
	if errors.Is(err, fs.ErrExist) {
		if e := os.Remove(fsops.PartPath(rec.Dst)); e != nil && !errors.Is(e, fs.ErrNotExist) {
			return false, false, e
		}
		if _, e := w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "destination conflict"}); e != nil {
			return false, false, e
		}
		skipSplit(s, v, "destination appeared during transfer")
		return false, true, nil
	}
	if fsops.IsCrossDevice(err) && *mode == state.TransferRename {
		if _, e := w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "cross-device rename"}); e != nil {
			return false, false, e
		}
		s.Log.Warn("rename crossed devices; falling back to copy", "rel_path", destRel)
		*mode = state.TransferCopy
		c.Transfer = transferCopy
		left, e := countSplitCandidates(list, w)
		if e != nil {
			return false, false, e
		}
		if e := preflightSplitFallback(ctx, s, left); e != nil {
			return false, false, e
		}
		return true, false, nil
	}
	if errors.Is(err, fsops.ErrSourceChanged) {
		if e := os.Remove(fsops.PartPath(rec.Dst)); e != nil && !errors.Is(e, fs.ErrNotExist) {
			return false, false, e
		}
		if _, e := w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "source changed"}); e != nil {
			return false, false, e
		}
		if attempt == 0 {
			s.Log.Warn("source changed during transfer; retrying", "rel_path", destRel)
			return true, false, nil
		}
		skipSplit(s, v, "source changed twice during transfer")
		return false, true, nil
	}
	return false, false, err
}

func videoRowsByVideoPath(rows []report.VideoRow) map[string]report.VideoRow {
	out := make(map[string]report.VideoRow, len(rows))
	for _, r := range rows {
		key := r.VideoRelPath
		if key == "" {
			key = r.RelPath
		}
		out[key] = r
	}
	return out
}

func loadVideoRegistry(archive, video string) ([]report.VideoRow, error) {
	rows, err := report.LoadVideoFile(filepath.Join(archive, scanner.VideoRegistryName))
	if err != nil {
		return nil, err
	}
	if rows == nil && video != "" {
		return report.LoadVideoFile(filepath.Join(video, scanner.VideoRegistryName))
	}
	return rows, nil
}
