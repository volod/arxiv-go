package archive

import (
	"context"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// RestoreConfig contains the restore flags needed by the archive executor.
type RestoreConfig struct {
	Scan           ScanConfig
	Transfer       string
	Verify         fsops.VerifyMode
	CreateDirs     bool
	Overwrite      bool
	RegistryUpdate bool
	KeepStubs      bool // --stubs keep
	KeepSource     bool // --transfer copy
	DeletePreviews bool // --previews delete
	// StageCopy is a test seam. Nil uses fsops.StageCopy.
	StageCopy func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error)
}

// RestoreBody scans the video archive, preflights, and restores candidates in walk order.
// The caller must install NewRestoreResolver in Config.Recoverer before Start.
func RestoreBody(c RestoreConfig) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error { return Restore(ctx, s, c) }
}

func Restore(ctx context.Context, s *Session, c RestoreConfig) error {
	history, err := readHistory(s.cfg.Archive, s.cfg.VideoArchive)
	if err != nil {
		return err
	}
	idx, err := newPreviewIndex(s.cfg.Archive, history)
	if err != nil {
		return err
	}
	c.Scan.Root = s.cfg.VideoArchive
	c.Scan.Preflight = false
	if c.Scan.Registry == "" {
		c.Scan.Registry = s.Run.File(state.ScanRegistryFile)
	}
	if c.Scan.LargeThreshold <= 0 {
		c.Scan.LargeThreshold = 1 << 30
	}
	videos, err := loadVideoRegistry(s.cfg.Archive, s.cfg.VideoArchive)
	if err != nil {
		return err
	}
	byVideo := videoRowsByVideoPath(s, videos)
	// A video that split moved stays a candidate even when detection alone would not call it a
	// video, for example one selected by --video-extensions, which restore does not take.
	c.Scan.Include = func(rel string) bool {
		row, ok := byVideo[rel]
		return ok && (row.Status == report.StatusMoved || row.Status == report.StatusRestored)
	}
	if _, err := Scan(ctx, s, c.Scan); err != nil {
		return err
	}
	w, err := s.OpenWAL()
	if err != nil {
		return err
	}
	syncCommittedCounter(s, w)
	list := s.Run.File(state.CandidatesFile)
	remaining, err := countRemaining(list, w)
	if err != nil {
		return err
	}
	if _, err := s.Preflight(ctx, remaining); err != nil {
		return err
	}
	if s.cfg.DryRun {
		s.Log.Info("dry run: restore plan complete", "videos", remaining.Count, "bytes", remaining.Bytes)
		return nil
	}
	if err := restorePreviewsBeforeExecute(s, w, idx, c.DeletePreviews); err != nil {
		return err
	}
	r := restoreResolverOf(s, c)
	if err := s.Phase("execute", Totals{Items: remaining.Count, Bytes: remaining.Bytes}); err != nil {
		return err
	}
	var index int64
	folded := caseFoldGuard{}
	err = ReadCandidates(list, func(v Candidate) error {
		index++
		destRel := restoreDestRel(v, byVideo)
		if first, clash := folded.clash(destRel); clash {
			skipVideo(s, v, "destination case-folds to "+first)
			return nil
		}
		if w.Committed().Has(destRel) || w.Committed().Has(v.RelPath) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := restoreCandidate(ctx, s, w, r, v, destRel, byVideo, &c, list); err != nil {
			return err
		}
		if c.DeletePreviews && w.Committed().Has(destRel) {
			if err := deletePreviews(s, w, idx, destRel); err != nil {
				return err
			}
		}
		s.Update(func(cp *state.Checkpoint) { cp.CandidateIndex = index })
		return s.Advance(1)
	})
	if err != nil {
		return err
	}
	if !c.KeepSource {
		pruneRestoredDirs(s)
	}
	if c.RegistryUpdate {
		return writeRestoreOutputs(s, c, videos)
	}
	return nil
}

// restorePreviewsBeforeExecute removes part files of unfinished preview generation, finishes
// interrupted deletions and, with --previews delete, deletes the previews of videos an earlier
// process of this run already restored.
func restorePreviewsBeforeExecute(s *Session, w *state.WAL, idx *previewIndex, deleteRestored bool) error {
	if err := idx.removeUnfinishedParts(); err != nil {
		return err
	}
	if err := resumePreviewDeletes(s, w, idx); err != nil {
		return err
	}
	if !deleteRestored {
		return nil
	}
	for _, video := range w.Committed().Paths() {
		if err := deletePreviews(s, w, idx, video); err != nil {
			return err
		}
	}
	return nil
}

// restoreResolverOf returns the resolver installed for recovery, so execution and recovery share
// its stub hints, with the run's policies. Without one it builds a new resolver.
func restoreResolverOf(s *Session, c RestoreConfig) *RestoreResolver {
	r, ok := s.cfg.Recoverer.(RestoreResolver)
	if !ok {
		r = NewRestoreResolver(RestoreResolver{Verify: c.Verify})
	}
	r.KeepStubs, r.KeepSource, r.Archive = c.KeepStubs, c.KeepSource, s.cfg.Archive
	if s.cfg.Crash != nil {
		r.Crash = s.cfg.Crash
	}
	if r.FS == nil {
		r.FS = s.cfg.FS
	}
	return &r
}
