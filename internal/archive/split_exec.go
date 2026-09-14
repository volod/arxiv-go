package archive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// splitCandidate moves one video in a transaction: begin, place (rename, or copy and verify),
// placed, description, described, remove the source after a copy, commit. A destination with the same
// content is adopted; a different one is a conflict and the video is skipped.
func splitCandidate(ctx context.Context, s *Session, w *state.WAL, r SplitResolver, v Candidate, c *SplitConfig, list string) error {
	src := filepath.Join(s.cfg.Archive, filepath.FromSlash(v.RelPath))
	dst := filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(v.RelPath))
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
		adopted, conflict, err := destinationStatus(src, dst, fi.Size(), c.Verify)
		if err != nil {
			return err
		}
		if conflict {
			if err := removeStalePart(dst); err != nil {
				return err
			}
			skipVideo(s, v, "destination exists with different content")
			return nil
		}
		if adopted {
			// An adopted destination leaves a source to remove even when the roots share a device.
			t.mode = state.TransferCopy
		}
		rec, err := w.Begin(state.Begin{Op: opSplit, RelPath: v.RelPath, Src: src, Dst: dst,
			Size: fi.Size(), Mtime: fi.ModTime(), Transfer: t.mode})
		if err != nil {
			return err
		}
		s.Log.Debug("split begin", "rel_path", v.RelPath, "transfer", t.mode, "size", fi.Size())
		if err := ensureMirrorDirs(s, dst); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var sum string
		if adopted {
			s.Log.Info("adopted existing destination", "rel_path", v.RelPath)
		} else if sum, err = placeVideo(ctx, s, w, rec, t.mode, c.Verify, false, c.StageCopy); err != nil {
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
		return finishSplit(s, w, r, rec, sum, c.Scan.LargeThreshold)
	}
}

// finishSplit logs placed, writes the description, removes the source of a copy and commits.
func finishSplit(s *Session, w *state.WAL, r SplitResolver, rec state.Record, sum string, largeThreshold int64) error {
	tx := state.Tx{Begin: rec, Last: rec}
	if _, err := w.Append(rec.TxID, state.StepPlaced, state.Record{}); err != nil {
		return err
	}
	r.Descriptions.RememberSHA256(rec.RelPath, sum)
	if err := r.WriteDescription(tx); err != nil {
		return err
	}
	description := r.DescriptionPath(tx)
	if description == "" {
		return fmt.Errorf("description path for %s", rec.RelPath)
	}
	if description != rec.Src+".md" {
		s.Log.Warn("description collision; wrote fallback", "rel_path", rec.RelPath, "description", description)
	}
	if _, err := w.Append(rec.TxID, state.StepDescribed, state.Record{Description: description}); err != nil {
		return err
	}
	if rec.Transfer == state.TransferCopy {
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
	s.Stats.VideoArchiveWritten.Add(rec.Size)
	s.Stats.ArchiveFreed.Add(rec.Size)
	if info, err := os.Stat(description); err == nil {
		s.Stats.ArchiveWritten.Add(info.Size())
	}
	logMoved(s, "video moved", rec, largeThreshold)
	return nil
}

func logMoved(s *Session, msg string, rec state.Record, largeThreshold int64) {
	log := s.Log.Debug
	if rec.Size >= largeThreshold {
		log = s.Log.Info
	}
	log(msg, "rel_path", rec.RelPath, "bytes", rec.Size, "transfer", rec.Transfer)
}

func ensureMirrorDirs(s *Session, dst string) error {
	rel, err := filepath.Rel(s.cfg.VideoArchive, filepath.Dir(dst))
	if err != nil {
		return err
	}
	if rel == "." {
		return nil
	}
	var chain []string
	for p := rel; p != "."; p = filepath.Dir(p) {
		chain = append(chain, p)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		path := filepath.Join(s.cfg.VideoArchive, chain[i])
		sourceDir := filepath.Join(s.cfg.Archive, chain[i])
		fi, err := os.Stat(sourceDir)
		if err != nil || !fi.IsDir() {
			return fmt.Errorf("source parent is not a directory: %s", sourceDir)
		}
		err = os.Mkdir(path, fi.Mode().Perm())
		created := err == nil
		if errors.Is(err, fs.ErrExist) {
			var got fs.FileInfo
			got, err = os.Lstat(path)
			if err == nil && !got.IsDir() {
				err = fmt.Errorf("destination parent is not a directory: %s", path)
			}
		}
		if err != nil {
			return err
		}
		if !created {
			continue
		}
		if runtime.GOOS != "windows" {
			if err := os.Chmod(path, fi.Mode().Perm()); err != nil && !errors.Is(err, fs.ErrPermission) && !errors.Is(err, errors.ErrUnsupported) {
				return err
			}
		}
		if err := fsops.SyncDir(filepath.Dir(path)); err != nil {
			return err
		}
		if err := hitCrash(s.cfg.Crash, "fs:mkdir"); err != nil {
			return err
		}
	}
	return nil
}
