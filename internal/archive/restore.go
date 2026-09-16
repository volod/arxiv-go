package archive

import (
	"context"
	"fmt"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// RestoreConfig contains the restore flags needed by the archive executor.
type RestoreConfig struct {
	Scan             ScanConfig
	Transfer         string
	Verify           fsops.VerifyMode
	CreateDirs       bool
	Overwrite        bool
	RegistryUpdate   bool
	KeepDescriptions bool // --descriptions keep
	KeepSource       bool // --transfer copy
	DeletePreviews   bool // --previews delete
	// StageCopy is a test seam. Nil uses fsops.StageCopy.
	StageCopy func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error)
}

// RestoreBody scans the payload's mirror root, preflights, and restores candidates in walk order.
// The caller must install NewRestoreResolver in Config.Recoverer before Start.
func RestoreBody(c RestoreConfig) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error { return Restore(ctx, s, c) }
}

// RestoreSidecarCleanup reports whether a restore of kind deletes the owned post-commit sidecars of
// the files it restores: video previews with --previews delete, CATIA text sidecars with
// --descriptions delete. Callers store it in Config.SidecarCleanup.
func RestoreSidecarCleanup(kind PayloadKind, c RestoreConfig) bool {
	if kind == PayloadCatia {
		return !c.KeepDescriptions
	}
	return c.DeletePreviews
}

func Restore(ctx context.Context, s *Session, c RestoreConfig) error {
	if RestoreSidecarCleanup(s.payload.kind, c) != s.cfg.SidecarCleanup {
		return fmt.Errorf("restore: sidecar cleanup %v does not match the run's recorded sidecar_cleanup %v",
			RestoreSidecarCleanup(s.payload.kind, c), s.cfg.SidecarCleanup)
	}
	hooks, err := s.payload.newRestore(ctx, s, &c)
	if err != nil {
		return err
	}
	c.Scan.Root = s.cfg.Payload.Root
	c.Scan.Preflight, c.Scan.mirror = false, true
	if c.Scan.Registry == "" {
		c.Scan.Registry = s.Run.File(state.ScanRegistryFile)
	}
	if c.Scan.LargeThreshold <= 0 {
		c.Scan.LargeThreshold = 1 << 30
	}
	byRel := hooks.rows()
	candidate := s.payload.candidate
	c.Scan.Candidate = func(rel string, ft scanner.FileType) bool { return candidate(ft) || hooks.include(rel) }
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
		s.Log.Info("dry run: restore plan complete", "payload", s.payload.kind, "candidates", remaining.Count, "bytes", remaining.Bytes)
		return nil
	}
	if err := hooks.beforeExecute(w); err != nil {
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
		destRel := restoreDestRel(v, byRel)
		if first, clash := folded.clash(destRel); clash {
			skipCandidate(s, v, "destination case-folds to "+first)
			return nil
		}
		if w.Committed().Has(destRel) || w.Committed().Has(v.RelPath) {
			return nil
		}
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := restoreCandidate(ctx, s, w, r, v, destRel, byRel, &c, list); err != nil {
			return err
		}
		if w.Committed().Has(destRel) {
			if err := hooks.committed(w, destRel); err != nil {
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
	if !c.RegistryUpdate {
		return nil
	}
	if err := hooks.updateRegistry(c); err != nil {
		return err
	}
	return updateRestoredLocations(s)
}

// restoreResolverOf returns the resolver installed for recovery, so execution and recovery share
// its description hints, with the run's policies. Without one it builds a new resolver.
func restoreResolverOf(s *Session, c RestoreConfig) *RestoreResolver {
	r, ok := s.cfg.Recoverer.(RestoreResolver)
	if !ok {
		r = NewRestoreResolver(RestoreResolver{Verify: c.Verify})
	}
	r.KeepDescriptions, r.KeepSource, r.Archive, r.Payload = c.KeepDescriptions, c.KeepSource, s.cfg.Archive, s.payload.kind
	if s.cfg.Crash != nil {
		r.Crash = s.cfg.Crash
	}
	if r.FS == nil {
		r.FS = s.cfg.FS
	}
	return &r
}
