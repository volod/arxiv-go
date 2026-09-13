package archive

import (
	"context"
	"runtime"
	"strings"

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
	// StageCopy is a test seam. Nil uses fsops.StageCopy.
	StageCopy func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error)
	Resolver  *RestoreResolver
}

// RestoreBody scans the video archive, preflights, and restores candidates in walk order.
// The caller must install NewRestoreResolver in Config.Recoverer before Start.
func RestoreBody(c RestoreConfig) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error { return Restore(ctx, s, c) }
}

func Restore(ctx context.Context, s *Session, c RestoreConfig) error {
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
	if done := int64(w.Committed().Len()); done > s.Stats.VideosDone.Load() {
		s.Stats.VideosDone.Store(done)
	}
	list := s.Run.File(state.CandidatesFile)
	remaining, err := countSplitCandidates(list, w)
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
	r := restoreResolverOf(s, c)
	if err := s.Phase("execute", Totals{Items: remaining.Count, Bytes: remaining.Bytes}); err != nil {
		return err
	}
	var index int64
	seen := make(map[string]string)
	err = ReadCandidates(list, func(v Candidate) error {
		index++
		destRel := restoreDestRel(v, byVideo)
		if runtime.GOOS == "windows" {
			key := strings.ToLower(destRel)
			if first, ok := seen[key]; ok && first != destRel {
				skipSplit(s, v, "destination case-folds to "+first)
				return nil
			}
			seen[key] = destRel
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
		s.Update(func(cp *state.Checkpoint) { cp.CandidateIndex = index })
		return s.Advance(1)
	})
	if err != nil {
		return err
	}
	if !c.KeepSource {
		if err := pruneRestoredDirs(s); err != nil {
			return err
		}
	}
	if c.RegistryUpdate {
		return writeRestoreOutputs(s, c, videos)
	}
	return nil
}

func restoreResolverOf(s *Session, c RestoreConfig) *RestoreResolver {
	r := c.Resolver
	if r == nil {
		switch rr := s.cfg.Recoverer.(type) {
		case *RestoreResolver:
			r = rr
		case RestoreResolver:
			cp := rr
			r = &cp
		default:
			tmp := NewRestoreResolver(RestoreResolver{
				FS: s.cfg.FS, Verify: c.Verify, Crash: s.cfg.Crash,
				KeepStubs: c.KeepStubs, KeepSource: c.KeepSource, Archive: s.cfg.Archive,
			})
			r = &tmp
		}
	}
	r.KeepStubs, r.KeepSource, r.Archive = c.KeepStubs, c.KeepSource, s.cfg.Archive
	if s.cfg.Crash != nil {
		r.Crash = s.cfg.Crash
	}
	if r.FS == nil {
		r.FS = s.cfg.FS
	}
	return r
}
