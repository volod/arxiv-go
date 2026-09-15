package archive

import (
	"context"
	"os"
	"runtime"
	"strings"
	"unicode/utf8"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// SplitConfig contains the split flags needed by the archive executor.
type SplitConfig struct {
	Scan         ScanConfig
	Transfer     string
	Verify       fsops.VerifyMode
	Descriptions SplitDescriptionWriter // nil uses MarkdownDescription; recovery must receive the same writer
	BaseURL      string
	Preview      media.PreviewOptions
	Tools        media.Toolset
	// StageCopy is a test seam for source mutation during a copy. Nil uses fsops.StageCopy.
	StageCopy func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error)
}

// SplitBody scans, preflights, and executes the payload's candidate list in walk order. The caller must
// install NewSplitResolver in Config.Recoverer before Start so recovery precedes the scan.
func SplitBody(c SplitConfig) func(context.Context, *Session) error {
	return func(ctx context.Context, s *Session) error { return Split(ctx, s, c) }
}

func Split(ctx context.Context, s *Session, c SplitConfig) error {
	hooks := s.payload.newSplit(s)
	if err := hooks.prepare(ctx, &c); err != nil {
		return err
	}
	if c.Scan.Candidate == nil {
		candidate := s.payload.candidate
		c.Scan.Candidate = func(_ string, ft scanner.FileType) bool { return candidate(ft) }
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
	if err := hooks.plan(ctx, &c, &remaining); err != nil {
		return err
	}
	if _, err := s.Preflight(ctx, remaining); err != nil {
		return err
	}
	if s.cfg.DryRun {
		s.Log.Info("dry run: split plan complete", "payload", s.payload.kind, "candidates", remaining.Count, "bytes", remaining.Bytes)
		return nil
	}
	c.Descriptions = splitDescriptions(s, c)
	if err := s.Phase("execute", Totals{Items: remaining.Count, Bytes: remaining.Bytes}); err != nil {
		return err
	}
	post, err := hooks.start(ctx, w)
	if err != nil {
		return err
	}
	resolver := NewSplitResolver(s.cfg.FS, c.Verify, s.cfg.Crash)
	resolver.Descriptions = c.Descriptions
	var index int64
	folded := caseFoldGuard{}
	err = ReadCandidates(list, func(v Candidate) error {
		index++
		if first, clash := folded.clash(v.RelPath); clash {
			skipCandidate(s, v, "destination case-folds to "+first)
			return nil
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
		if w.Committed().Has(v.RelPath) {
			if err := post.committed(v.RelPath); err != nil {
				return err
			}
		}
		s.Update(func(cp *state.Checkpoint) { cp.CandidateIndex = index })
		return s.Advance(1)
	})
	if err := post.stop(err); err != nil {
		return err
	}
	return hooks.writeRegistry(c)
}

// syncCommittedCounter makes the WAL authoritative after a crash: a committed transaction may
// precede the last checkpoint and its in-memory counter update.
func syncCommittedCounter(s *Session, w *state.WAL) {
	counter := s.payload.counters(s.Stats).done
	if done := int64(w.Committed().Len()); done > counter.Load() {
		counter.Store(done)
	}
}

func countRemaining(list string, w *state.WAL) (Candidates, error) {
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

// splitDescriptions returns the configured description writer with the session's crash hook and clock, or the
// Markdown writer when none is configured.
func splitDescriptions(s *Session, c SplitConfig) SplitDescriptionWriter {
	if c.Descriptions == nil {
		return NewMarkdownDescription(DescriptionConfig{
			Archive: s.cfg.Archive, Mirror: s.cfg.Payload.Root, BaseURL: c.BaseURL,
			Registry: c.Scan.Registry, Version: s.cfg.Version, Verify: c.Verify,
			FS: s.cfg.FS, Crash: s.cfg.Crash, Now: s.cfg.Now,
		})
	}
	if m, ok := c.Descriptions.(*MarkdownDescription); ok {
		if s.cfg.Crash != nil {
			m.cfg.Crash = s.cfg.Crash
		}
		if m.cfg.Now == nil {
			m.cfg.Now = s.cfg.Now
		}
	}
	return c.Descriptions
}

// caseFoldGuard skips a destination that differs from an earlier one only by case on Windows,
// where both would name the same file.
type caseFoldGuard map[string]string

func (g caseFoldGuard) clash(rel string) (string, bool) {
	if runtime.GOOS != "windows" {
		return "", false
	}
	key := strings.ToLower(rel)
	if first, ok := g[key]; ok && first != rel {
		return first, true
	}
	g[key] = rel
	return "", false
}

// missingSourceReason explains a candidate whose source is gone. Candidate and WAL records are
// JSON, which replaces bytes of a file name that is not valid UTF-8, so such a file is never found.
func missingSourceReason(rel string) string {
	if strings.ContainsRune(rel, utf8.RuneError) {
		return "source missing or no longer a regular file (file names that are not valid UTF-8 are not supported)"
	}
	return "source missing or no longer a regular file"
}

// skipCandidate reports a candidate that the run skips and counts it for the payload.
func skipCandidate(s *Session, v Candidate, reason string) {
	s.Issue(state.IssueSkipped, v.RelPath, reason)
	counters := s.payload.counters(s.Stats)
	counters.skipped.Add(1)
	counters.bytes.Add(v.Size)
}

// removeStalePart deletes dst.arxgo-part left by an aborted transfer. Recovery has already closed
// every transaction of the run, so no open transaction owns it.
func removeStalePart(dst string) error {
	if err := os.Remove(fsops.PartPath(dst)); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}
