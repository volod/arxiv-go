package archive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/state"
)

// SplitConfig contains the split flags needed by the archive executor.
type SplitConfig struct {
	Scan     ScanConfig
	Transfer string
	Verify   fsops.VerifyMode
	Stubs    SplitStubWriter // nil uses MarkdownStub; recovery must receive the same writer
	BaseURL  string
	Preview  media.PreviewOptions
	Tools    media.Toolset
	// StageCopy is a test seam for source mutation during a copy. Nil uses fsops.StageCopy.
	StageCopy func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error)
}

// SplitBody scans, preflights, and executes the candidate list in walk order. The caller must
// install NewSplitResolver in Config.Recoverer before Start so recovery precedes the scan.
func SplitBody(c SplitConfig) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error { return Split(ctx, s, c) }
}

func Split(ctx context.Context, s *Session, c SplitConfig) error {
	// A corrupt operator registry must stop the run before the first archive move.
	if _, err := loadVideoRegistry(s.cfg.Archive, s.cfg.VideoArchive); err != nil {
		return err
	}
	idx, err := readPreviewIndex(s.cfg.Archive)
	if err != nil {
		return err
	}
	if !s.cfg.DryRun {
		if err := cleanupPreviewParts(s.cfg.Archive, idx); err != nil {
			return err
		}
	}
	c.Scan.SkipPaths = append(c.Scan.SkipPaths, idx.skipPaths(s.cfg.Archive)...)
	if _, err := Scan(ctx, s, c.Scan); err != nil {
		return err
	}
	w, err := s.OpenWAL()
	if err != nil {
		return err
	}
	// The WAL is authoritative after a crash; a committed transaction may precede the last
	// checkpoint and its in-memory counter update.
	if done := int64(w.Committed().Len()); done > s.Stats.VideosDone.Load() {
		s.Stats.VideosDone.Store(done)
	}
	list := s.Run.File(state.CandidatesFile)
	remaining, err := countSplitCandidates(list, w)
	if err != nil {
		return err
	}
	if c.Preview.SampleMode == "" && c.Preview.ImageMode == "" {
		c.Preview = media.DefaultPreviewOptions()
	}
	runner := &media.Runner{Tools: c.Tools, Log: s.Log}
	plans, estimate, err := planSplitPreviews(ctx, s, c, idx, list, runner)
	if err != nil {
		return err
	}
	remaining.PreviewBytes = estimate
	if _, err := s.Preflight(ctx, remaining); err != nil {
		return err
	}
	if s.cfg.DryRun {
		s.Log.Info("dry run: split plan complete", "videos", remaining.Count, "bytes", remaining.Bytes)
		return nil
	}
	if c.Stubs == nil {
		c.Stubs = NewMarkdownStub(StubConfig{
			Archive: s.cfg.Archive, VideoArchive: s.cfg.VideoArchive, BaseURL: c.BaseURL,
			Registry: c.Scan.Registry, Version: s.cfg.Version, Verify: c.Verify,
			FS: s.cfg.FS, Crash: s.cfg.Crash, Now: s.cfg.Now,
		})
	} else if m, ok := c.Stubs.(*MarkdownStub); ok {
		if s.cfg.Crash != nil {
			m.cfg.Crash = s.cfg.Crash
		}
		if m.cfg.Now == nil {
			m.cfg.Now = s.cfg.Now
		}
	}
	if err := s.Phase("execute", Totals{Items: remaining.Count, Bytes: remaining.Bytes}); err != nil {
		return err
	}
	var worker *previewWorker
	if previewsEnabled(c.Preview) {
		worker = startPreviewWorker(ctx, s, w, idx, runner)
		ordered := make([]string, 0, len(plans))
		for rel := range plans {
			ordered = append(ordered, rel)
		}
		sort.Strings(ordered)
		for _, rel := range ordered {
			item := plans[rel]
			if item.moved || w.Committed().Has(rel) {
				worker.jobs <- item
			}
		}
	}
	resolver := NewSplitResolver(s.cfg.FS, c.Verify, s.cfg.Crash)
	resolver.Stubs = c.Stubs
	var index int64
	seen := make(map[string]string)
	err = ReadCandidates(list, func(v Candidate) error {
		index++
		if runtime.GOOS == "windows" {
			key := strings.ToLower(v.RelPath)
			if first, ok := seen[key]; ok && first != v.RelPath {
				skipSplit(s, v, "destination case-folds to "+first)
				return nil
			}
			seen[key] = v.RelPath
		}
		if w.Committed().Has(v.RelPath) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := splitCandidate(ctx, s, w, resolver, v, &c, list); err != nil {
			return err
		}
		if worker != nil && w.Committed().Has(v.RelPath) {
			if item, ok := plans[v.RelPath]; ok {
				worker.jobs <- item
			}
		}
		s.Update(func(cp *state.Checkpoint) { cp.CandidateIndex = index })
		return s.Advance(1)
	})
	if err != nil {
		if worker != nil {
			err = errors.Join(err, worker.close())
		}
		return err
	}
	if worker != nil {
		if err := worker.close(); err != nil {
			return err
		}
	}
	return writeVideoOutputs(s, c)
}

func countSplitCandidates(list string, w *state.WAL) (Candidates, error) {
	var remaining Candidates
	err := ReadCandidates(list, func(v Candidate) error {
		if !w.Committed().Has(v.RelPath) {
			remaining.Count++
			remaining.Bytes = addSat(remaining.Bytes, v.Size)
			remaining.Largest = max(remaining.Largest, v.Size)
		}
		return nil
	})
	return remaining, err
}

func splitCandidate(ctx context.Context, s *Session, w *state.WAL, r SplitResolver, v Candidate, c *SplitConfig, list string) error {
	src := filepath.Join(s.cfg.Archive, filepath.FromSlash(v.RelPath))
	dst := filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(v.RelPath))
	mode := state.TransferCopy
	if c.Transfer != transferCopy {
		// Classify from the archive roots so a same-device layout always renames
		// rather than copying every video. A later EXDEV still falls back to copy.
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
			skipSplit(s, v, missingSourceReason(v.RelPath))
			return nil
		}
		adopted, conflict, err := destinationStatus(src, dst, fi.Size(), c.Verify)
		if err != nil {
			return err
		}
		if adopted {
			// An adopted destination leaves a source to remove even when the roots share a device.
			mode = state.TransferCopy
		}
		if conflict {
			if err := removeStalePart(dst); err != nil {
				return err
			}
			skipSplit(s, v, "destination exists with different content")
			return nil
		}
		begin := state.Begin{Op: opSplit, RelPath: v.RelPath, Src: src, Dst: dst,
			Size: fi.Size(), Mtime: fi.ModTime(), Transfer: mode}
		rec, err := w.Begin(begin)
		if err != nil {
			return err
		}
		s.Log.Debug("split begin", "rel_path", v.RelPath, "transfer", mode, "size", fi.Size())
		tx := state.Tx{Begin: rec, Last: rec}
		if err := ensureMirrorDirs(s, dst); err != nil {
			return err
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		var sum string
		if !adopted {
			var err error
			sum, err = placeSplit(ctx, s, w, rec, mode, *c)
			if errors.Is(err, fs.ErrExist) {
				if e := abortConflict(s, w, rec); e != nil {
					return e
				}
				skipSplit(s, v, "destination appeared during transfer")
				return nil
			}
			if fsops.IsCrossDevice(err) && mode == state.TransferRename {
				if _, e := w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "cross-device rename"}); e != nil {
					return e
				}
				s.Log.Warn("rename crossed devices; falling back to copy", "rel_path", v.RelPath)
				mode = state.TransferCopy
				c.Transfer = transferCopy
				left, e := countSplitCandidates(list, w)
				if e != nil {
					return e
				}
				if e := preflightSplitFallback(ctx, s, left); e != nil {
					return e
				}
				attempt--
				continue
			}
			if errors.Is(err, fsops.ErrSourceChanged) {
				if e := os.Remove(fsops.PartPath(dst)); e != nil && !errors.Is(e, fs.ErrNotExist) {
					return e
				}
				if _, e := w.Append(rec.TxID, state.StepAborted, state.Record{Reason: "source changed"}); e != nil {
					return e
				}
				if attempt == 0 {
					s.Log.Warn("source changed during transfer; retrying", "rel_path", v.RelPath)
					continue
				}
				skipSplit(s, v, "source changed twice during transfer")
				return nil
			}
			if err != nil {
				return err
			}
		} else {
			s.Log.Info("adopted existing destination", "rel_path", v.RelPath)
		}
		if _, err := w.Append(rec.TxID, state.StepPlaced, state.Record{}); err != nil {
			return err
		}
		r.Stubs.RememberSHA256(v.RelPath, sum)
		if err := r.WriteStub(tx); err != nil {
			return err
		}
		stub := r.StubPath(tx)
		if stub == "" {
			return fmt.Errorf("stub path for %s", v.RelPath)
		}
		if stub != src+".md" {
			s.Log.Warn("stub collision; wrote fallback", "rel_path", v.RelPath, "stub", stub)
		}
		if _, err := w.Append(rec.TxID, state.StepStubbed, state.Record{Stub: stub}); err != nil {
			return err
		}
		if mode == state.TransferCopy {
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
		s.Stats.VideoArchiveWritten.Add(fi.Size())
		s.Stats.ArchiveFreed.Add(fi.Size())
		if info, err := os.Stat(stub); err == nil {
			s.Stats.ArchiveWritten.Add(info.Size())
		}
		if fi.Size() >= c.Scan.LargeThreshold {
			s.Log.Info("video moved", "rel_path", v.RelPath, "bytes", fi.Size(), "transfer", mode)
		} else {
			s.Log.Debug("video moved", "rel_path", v.RelPath, "bytes", fi.Size(), "transfer", mode)
		}
		return nil
	}
	return nil
}

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
	return hitSplit(s.cfg.Crash, "fs:delete_part")
}

// removeStalePart deletes dst.arxgo-part left by an aborted transfer. Recovery has already closed
// every transaction of the run, so no open transaction owns it.
func removeStalePart(dst string) error {
	if err := os.Remove(fsops.PartPath(dst)); err != nil && !errors.Is(err, fs.ErrNotExist) {
		return err
	}
	return nil
}

// missingSourceReason explains a candidate whose source is gone. Candidate and WAL records are
// JSON, which replaces bytes of a file name that is not valid UTF-8, so such a video is never found.
func missingSourceReason(rel string) string {
	if strings.ContainsRune(rel, utf8.RuneError) {
		return "source missing or no longer a regular file (file names that are not valid UTF-8 are not supported)"
	}
	return "source missing or no longer a regular file"
}

func skipSplit(s *Session, v Candidate, reason string) {
	s.Issue(state.IssueSkipped, v.RelPath, reason)
	s.Stats.VideosSkipped.Add(1)
	s.Stats.VideoBytes.Add(v.Size)
}
