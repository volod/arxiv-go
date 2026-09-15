package archive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strings"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// Scan phases.
const (
	PhaseScan    = "scan"
	PhaseSummary = "summary"
)

// ScanConfig configures the registry scan of one root.
type ScanConfig struct {
	Root            string   // tree to scan
	Registry        string   // final registry path; the part file is Registry + ".arxgo-part"
	Metadata        string   // file (ISO BMFF) or media (ISO BMFF plus ffprobe)
	FFprobePath     string   // validated ffprobe path for media mode; not persisted in options
	LargeThreshold  int64    // --large-threshold
	VideoExtensions []string // --video-extensions
	Exclude         []string // --exclude globs
	SkipPaths       []string // OS paths excluded besides the registry (a nested mirror root)
	// Candidate selects the regular files written to candidates.jsonl: the payload's files for
	// split, plus paths its registry lists as moved for restore. Nil writes no candidates (scan).
	Candidate func(rel string, ft scanner.FileType) bool
	// Preflight runs the scan free-space preflight before traversal (the scan operation). Split
	// and restore run their own preflight after the scan.
	Preflight bool
	// Workers is the number of concurrent detections; zero picks a default. Window bounds the
	// entries between the walker and the writer; zero picks a default.
	Workers int
	Window  int

	// afterEntry is a test hook called after each written entry with the count written by this
	// process. An error simulates a crash: the scan stops without making buffered output durable.
	afterEntry func(n int64) error
}

// defaultScanWorkers: on a local NVMe tree throughput plateaus at 8 workers, where the single
// walker becomes the limit; latency-bound network shares gain from more concurrent opens.
const defaultScanWorkers = 16

func (c *ScanConfig) defaults() {
	if c.Workers <= 0 {
		c.Workers = defaultScanWorkers
	}
	if c.Window <= 0 {
		c.Window = max(256, 8*c.Workers)
	}
}

// ScanBody returns the body of the scan operation.
func ScanBody(c ScanConfig) func(ctx context.Context, s *Session) error {
	return func(ctx context.Context, s *Session) error {
		_, err := Scan(ctx, s, c)
		return err
	}
}

// Scan writes the file registry of c.Root through its part file and candidates.jsonl in the run
// directory, resuming from the session's checkpoint, then renames the registry into place (not in
// a dry run) and records the statistics. Skipped entries make the run partial.
func Scan(ctx context.Context, s *Session, c ScanConfig) (*state.ScanStats, error) {
	c.defaults()
	cp := s.Checkpointed()
	if cp.Scan != nil && cp.Scan.Complete {
		s.Log.Info("scan already completed in this run", "registry", c.Registry, "rows", cp.Scan.Rows())
		s.summarizeScan(cp.Scan)
		return cp.Scan, nil
	}
	if c.Preflight {
		if _, err := s.Preflight(ctx, scanEstimate(c, cp)); err != nil {
			return nil, err
		}
	}
	run, err := openScan(s, c, cp)
	if err != nil {
		return nil, err
	}
	if err := s.Phase(PhaseScan, Totals{}); err != nil {
		_ = run.close()
		return nil, err
	}
	if err := run.pipeline(ctx); err != nil {
		if run.healthy {
			err = errors.Join(err, run.sync())
		}
		return nil, errors.Join(err, run.close())
	}
	if err := errors.Join(run.sync(), run.close()); err != nil {
		return nil, err
	}
	if !s.cfg.DryRun {
		if err := report.DropEmptyCSVColumns(run.part, report.FileRegistryKeep); err != nil {
			return nil, fmt.Errorf("compact registry: %w", err)
		}
		if err := fsops.Replace(run.part, c.Registry); err != nil {
			return nil, fmt.Errorf("place registry: %w", err)
		}
	}
	run.stats.Complete = true
	final := run.stats.Clone()
	s.Update(func(cp *state.Checkpoint) { cp.Scan = final })
	if err := s.Phase(PhaseSummary, Totals{}); err != nil {
		return nil, err
	}
	s.summarizeScan(final)
	return final, nil
}

// scanEstimate sizes the registry from the one it replaces, less what the part file already holds.
func scanEstimate(c ScanConfig, cp state.Checkpoint) Candidates {
	var prev int64
	if info, err := os.Stat(c.Registry); err == nil && info.Mode().IsRegular() {
		prev = info.Size()
	}
	remaining := max(prev-cp.RegistryOffset, 0)
	return Candidates{Count: (remaining + registryRowBytes - 1) / registryRowBytes}
}

// scanRun is the state of one process's scan: outputs, statistics and the resume cursor.
type scanRun struct {
	s       *Session
	cfg     ScanConfig
	part    string
	detect  scanner.DetectOptions
	reg     *report.RegistryWriter
	cand    *candidateWriter
	stats   *state.ScanStats
	start   scanner.Key // resume cursor from the checkpoint
	cursor  scanner.Key // key of the last written entry
	healthy bool        // outputs accepted every write so far
}

// openScan opens the outputs: after the checkpointed offsets when the run resumes with intact part
// files, otherwise from scratch with the header.
func openScan(s *Session, c ScanConfig, cp state.Checkpoint) (*scanRun, error) {
	r := &scanRun{
		s: s, cfg: c, part: fsops.PartPath(c.Registry), healthy: true,
		detect: scanner.NewDetectOptions(c.LargeThreshold, c.VideoExtensions),
	}
	candPath := s.Run.File(state.CandidatesFile)
	if cp.Scan != nil && cp.RegistryOffset > 0 && !s.cfg.DryRun {
		err := r.resume(candPath, cp)
		if err == nil {
			return r, nil
		}
		if !errors.Is(err, fs.ErrNotExist) && !errors.Is(err, report.ErrPartTooShort) {
			return nil, err
		}
		s.Log.Warn("scan output does not match the checkpoint; scanning again from the start", "error", err)
	}
	var err error
	if s.cfg.DryRun {
		r.reg = report.DiscardRegistry()
	} else if r.reg, err = report.CreateRegistry(r.part); err != nil {
		return nil, fmt.Errorf("create registry: %w", err)
	}
	if r.cand, err = createCandidates(candPath); err != nil {
		_ = r.reg.Close()
		return nil, err
	}
	r.stats = &state.ScanStats{}
	s.Stats.Files.Store(0)
	s.Stats.Bytes.Store(0)
	return r, nil
}

func (r *scanRun) resume(candPath string, cp state.Checkpoint) error {
	reg, err := report.ResumeRegistry(r.part, cp.RegistryOffset)
	if err != nil {
		return err
	}
	cand, err := resumeCandidates(candPath, cp.CandidatesOffset)
	if err != nil {
		_ = reg.Close()
		return err
	}
	r.reg, r.cand, r.stats = reg, cand, cp.Scan.Clone()
	r.start = scanner.Key(cp.ScanCursor)
	r.cursor = r.start
	r.s.Stats.Files.Store(r.stats.Rows())
	r.s.Stats.Bytes.Store(r.stats.Bytes)
	r.s.Log.Info("resuming scan", "cursor", strings.Join(cp.ScanCursor, "/"), "rows", r.stats.Rows(),
		"registry_offset", cp.RegistryOffset)
	return nil
}

// sync makes the outputs durable and stores the matching cursor, offsets and statistics for the
// next checkpoint.
func (r *scanRun) sync() error {
	roff, err := r.reg.Sync()
	if err == nil {
		var coff int64
		if coff, err = r.cand.sync(); err == nil {
			cursor, stats := append([]string(nil), r.cursor...), r.stats.Clone()
			r.s.Update(func(cp *state.Checkpoint) {
				cp.ScanCursor, cp.RegistryOffset, cp.CandidatesOffset, cp.Scan = cursor, roff, coff, stats
			})
			return nil
		}
	}
	r.healthy = false
	return fmt.Errorf("write scan output: %w", err)
}

// periodicSync runs before each periodic checkpoint: sync, then the free-space check.
func (r *scanRun) periodicSync() error {
	if err := r.sync(); err != nil {
		return err
	}
	return r.checkSpace()
}

// checkSpace stops the scan with exit 4 when writing has brought the registry device below
// --min-free; the part file and checkpoint stay, so the same command resumes after freeing space.
func (r *scanRun) checkSpace() error {
	minFree := r.s.cfg.Preflight.MinFree
	if r.s.cfg.DryRun || minFree <= 0 {
		return nil
	}
	space, err := r.s.cfg.FS.FreeSpace(r.part)
	if err != nil || space.Total == 0 || capInt64(space.Available) >= minFree {
		return nil // unknown free space is not a reason to stop a scan
	}
	d := DeviceRequirement{Roles: []Role{RoleRegistry}, Path: r.part, MinFree: minFree,
		Available: capInt64(space.Available), Known: true, Shortfall: minFree - capInt64(space.Available)}
	r.s.Log.Error("free space fell below --min-free during scan; free space and rerun to resume", d.Attrs()...)
	return &InsufficientSpaceError{Requirement: Requirement{Op: opScan, Devices: []DeviceRequirement{d}}}
}

func (r *scanRun) close() error {
	return errors.Join(r.reg.Close(), r.cand.close())
}

// summarizeScan logs the statistics, stores them for the report and marks skipped entries.
func (s *Session) summarizeScan(st *state.ScanStats) {
	sum := st.Summary(s.ElapsedS())
	top := make([]string, len(sum.TopMIME))
	for i, m := range sum.TopMIME {
		top[i] = fmt.Sprintf("%s=%d/%s", m.MIME, m.Count, FormatBytes(m.Bytes))
	}
	flag := func(c state.CountBytes) string { return fmt.Sprintf("%d/%s", c.Count, FormatBytes(c.Bytes)) }
	s.Log.Info("scan summary", "files", sum.Files, "dirs", sum.Dirs, "symlinks", sum.Symlinks,
		"bytes", FormatBytes(sum.Bytes), "binary", flag(sum.Binary), "media", flag(sum.Media),
		"picture", flag(sum.Picture), "video", flag(sum.Video), "catia", flag(sum.Catia), "large", flag(sum.Large),
		"skipped", st.SkippedTotal(), "skipped_unreadable", st.Skipped[scanner.ReasonUnreadable],
		"skipped_special", st.Skipped[scanner.ReasonSpecial], "top_mime", strings.Join(top, " "),
		"elapsed", FormatDuration(secondsDuration(sum.ElapsedS)))
	s.SetScanSummary(sum)
	if st.SkippedTotal() > 0 {
		s.MarkPartial()
	}
}
