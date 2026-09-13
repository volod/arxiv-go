package archive

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/volod/arxiv-go/internal/state"
)

// Status is the outcome of a run; the cli package maps it to an exit code.
type Status int

// Run outcomes.
const (
	StatusCompleted      Status = iota // everything done, nothing skipped
	StatusPartial                      // done with skipped or failed items
	StatusNotImplemented               // the operation body is not in this build
	StatusInterrupted                  // context canceled; checkpoint written
	StatusFailed                       // unexpected error; rerun resumes
	StatusNeedsOperator                // lock lost or corrupt state; lock kept when still ours
	StatusLocked                       // another run holds the lock, or it is stale or remote
)

// StartFailed logs why Start failed, naming the lock owner when the lock was not acquired, and
// classifies the failure.
func StartFailed(ctx context.Context, log *slog.Logger, err error) Status {
	var locked *state.LockedError
	switch {
	case errors.As(err, &locked):
		attrs := []any{"lock", locked.Path, "state", string(locked.State)}
		if locked.State != state.LockUnreadable {
			o := locked.Owner
			attrs = append(attrs, "owner_run", o.RunID, "owner_op", o.Op, "owner_pid", o.PID,
				"owner_host", o.Host, "owner_started", o.StartedAt)
		}
		log.Error(err.Error(), attrs...)
		return StatusLocked
	case errors.Is(err, state.ErrStateCorrupt), errors.Is(err, state.ErrLockLost):
		log.Error("run state needs operator action", "error", err)
		return StatusNeedsOperator
	case ctx.Err() != nil:
		log.Warn("interrupted before the run started")
		return StatusInterrupted
	default:
		log.Error("cannot start run", "error", err)
		return StatusFailed
	}
}

// Result is what Finish reports.
type Result struct {
	Status Status
	RunID  string
	Err    error
}

// Phase closes the current phase, starts the named one with its known totals, writes a boundary
// checkpoint and a progress line.
func (s *Session) Phase(name string, totals Totals) error {
	s.mu.Lock()
	s.closePhaseLocked()
	s.phases = append(s.phases, state.PhaseStats{Name: name, StartedAt: s.cfg.Now().UTC()})
	s.phaseOpen, s.phaseBase = true, s.Stats.Snapshot()
	s.cp.Phase = name
	s.mu.Unlock()
	s.Progress.StartPhase(name, totals)
	if err := s.Checkpoint(); err != nil {
		return err
	}
	// The run log is diagnostic: flush it at phase boundaries (and on close), not at every
	// checkpoint, where the extra fsync costs about a quarter of the checkpoint time.
	_ = s.runLog.Sync()
	return nil
}

func (s *Session) closePhaseLocked() {
	if s.phaseOpen {
		s.phaseOpen = false
		p := &s.phases[len(s.phases)-1]
		p.WallS = s.cfg.Now().Sub(p.StartedAt).Seconds()
		p.Counters = s.Stats.Snapshot().Sub(s.phaseBase)
	}
}

// Update changes the operation-specific checkpoint fields (cursor, offsets, candidate index) that
// the next checkpoint will store. Callers update them together with the work they describe.
func (s *Session) Update(fn func(cp *state.Checkpoint)) {
	s.mu.Lock()
	defer s.mu.Unlock()
	fn(&s.cp)
}

// Advance records n processed files and writes a checkpoint when --checkpoint-every files or
// --checkpoint-interval have passed since the last one.
func (s *Session) Advance(n int64) error {
	s.mu.Lock()
	due := s.throttle.Add(n)
	s.mu.Unlock()
	if !due {
		return nil
	}
	return s.Checkpoint()
}

// Checkpoint verifies that the locks are still ours and atomically writes checkpoint.json with the
// current counters.
func (s *Session) Checkpoint() error {
	for _, l := range s.locks {
		if err := l.Verify(); err != nil {
			return err
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	cp := s.cp
	cp.RunID, cp.Op = s.Run.ID, s.cfg.Op
	cp.Counters = s.Stats.Snapshot()
	cp.ElapsedS = s.elapsedBase + now.Sub(s.started).Seconds()
	cp.WrittenAt = now.UTC()
	if len(cp.ScanCursor) > 0 {
		cp.ScanCursor = append([]string(nil), cp.ScanCursor...)
	}
	if s.wal != nil {
		cp.WALOffset = s.wal.Offset()
	}
	if err := state.WriteCheckpoint(s.Run.File(state.CheckpointFile), cp); err != nil {
		return fmt.Errorf("write checkpoint: %w", err)
	}
	s.throttle.Reset()
	s.Log.Debug("checkpoint", "phase", cp.Phase, "files", cp.Counters.Files, "elapsed_s", cp.ElapsedS)
	return nil
}

// Issue records a skipped or failed item for the report and logs it.
func (s *Session) Issue(kind, relPath, reason string) {
	s.Log.Warn("item "+kind, "rel_path", relPath, "reason", reason)
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.issues) >= maxIssues {
		s.issuesOmitted++
		return
	}
	s.issues = append(s.issues, state.Issue{Kind: kind, RelPath: relPath, Reason: reason})
}

// Finish ends the run with the error returned by the operation body. It writes a final
// checkpoint unless the lock was lost, writes report.json when the run reached a final state
// (and for every dry run), releases the locks unless operator action is needed, logs the totals
// and closes the run log.
func (s *Session) Finish(ctx context.Context, runErr error) Result {
	s.stopProgress()
	lockLost := errors.Is(runErr, state.ErrLockLost)
	if lockLost {
		// Another process owns the run state now; write nothing more into the run directory.
		s.useConsoleOnly()
	}
	s.Progress.Flush()
	res := Result{Status: s.classify(ctx, runErr), RunID: s.Run.ID, Err: runErr}

	s.mu.Lock()
	s.closePhaseLocked()
	s.mu.Unlock()
	if lockLost {
		s.Log.Error("run lock lost; not writing checkpoint or report", "error", runErr)
	} else if err := s.Checkpoint(); err != nil {
		s.Log.Error("final checkpoint failed", "error", err)
		res = s.escalate(res, err)
	}
	if !lockLost && (reportable(res.Status) || s.cfg.DryRun) {
		if err := state.WriteReport(s.Run.File(state.ReportFile), s.report(res)); err != nil {
			s.Log.Error("write report failed", "error", err)
			res = s.escalate(res, err)
		}
	}
	if res.Status != StatusNeedsOperator {
		for _, err := range s.releaseLocks() {
			s.Log.Error("release run lock", "error", err)
			res = s.escalate(res, err)
		}
	}

	c := s.Stats.Snapshot()
	attrs := []any{"op", s.cfg.Op, "run_id", s.Run.ID, "status", res.Status.String(),
		"wall", FormatDuration(s.cfg.Now().Sub(s.started)), "files", c.Files, "bytes", c.Bytes,
		"videos_done", c.VideosDone, "videos_skipped", c.VideosSkipped, "videos_failed", c.VideosFailed}
	switch res.Status {
	case StatusInterrupted:
		s.Log.Warn("run interrupted; rerun the same command to resume", attrs...)
	case StatusFailed, StatusNeedsOperator:
		s.Log.Error("run stopped", append(attrs, "error", res.Err)...)
	default:
		s.Log.Info("run finished", attrs...)
	}
	if err := s.runLog.Close(); err != nil {
		s.Log.Error("close run log", "error", err)
	}
	if s.wal != nil {
		if err := s.wal.Close(); err != nil {
			s.Log.Error("close wal", "error", err)
		}
		s.wal = nil
	}
	s.Log = slog.New(s.cfg.Console)
	return res
}

func (s *Session) classify(ctx context.Context, err error) Status {
	switch {
	case err == nil && ctx.Err() == nil:
		c := s.Stats.Snapshot()
		s.mu.Lock()
		issues := len(s.issues) > 0 || s.issuesOmitted > 0
		s.mu.Unlock()
		if issues || c.VideosSkipped > 0 || c.VideosFailed > 0 {
			return StatusPartial
		}
		return StatusCompleted
	case errors.Is(err, ErrNotImplemented):
		return StatusNotImplemented
	case errors.Is(err, state.ErrLockLost), errors.Is(err, state.ErrStateCorrupt):
		return StatusNeedsOperator
	case ctx.Err() != nil || errors.Is(err, context.Canceled):
		return StatusInterrupted
	default:
		return StatusFailed
	}
}

// escalate turns a finishing error into a failed (or needs-operator) result.
func (s *Session) escalate(res Result, err error) Result {
	st := StatusFailed
	if errors.Is(err, state.ErrLockLost) || res.Status == StatusNeedsOperator {
		st = StatusNeedsOperator
	}
	if res.Status == StatusInterrupted && st == StatusFailed {
		return Result{Status: res.Status, RunID: res.RunID, Err: errors.Join(res.Err, err)}
	}
	return Result{Status: st, RunID: res.RunID, Err: errors.Join(res.Err, err)}
}

func reportable(st Status) bool {
	return st == StatusCompleted || st == StatusPartial || st == StatusNotImplemented
}

// useConsoleOnly drops the run-log handler so a lost lock writes nothing more
// into the run directory. Progress lines and finish logs stay on the console.
func (s *Session) useConsoleOnly() {
	s.Log = slog.New(s.cfg.Console)
	if s.Progress != nil {
		s.Progress.log = s.Log
	}
}

func (s *Session) report(res Result) state.Report {
	s.mu.Lock()
	defer s.mu.Unlock()
	now := s.cfg.Now()
	c := s.Stats.Snapshot()
	r := state.Report{
		RunID: s.Run.ID, Op: s.cfg.Op, Version: s.cfg.Version, Status: res.Status.String(),
		DryRun: s.cfg.DryRun, Resumed: s.Resumed,
		StartedAt: s.started.UTC(), FinishedAt: now.UTC(), WallS: now.Sub(s.started).Seconds(),
		Counters: c, Phases: append([]state.PhaseStats{}, s.phases...),
		Roots:  []state.RootStats{{Root: s.cfg.Archive, BytesWritten: c.ArchiveWritten, BytesFreed: c.ArchiveFreed}},
		Issues: append([]state.Issue(nil), s.issues...), IssuesOmitted: s.issuesOmitted,
	}
	if s.cfg.VideoArchive != "" {
		r.Roots = append(r.Roots, state.RootStats{Root: s.cfg.VideoArchive, BytesWritten: c.VideoWritten, BytesFreed: c.VideoFreed})
	}
	return r
}

func (st Status) String() string {
	switch st {
	case StatusCompleted:
		return "completed"
	case StatusPartial:
		return "partial"
	case StatusNotImplemented:
		return "not_implemented"
	case StatusInterrupted:
		return "interrupted"
	case StatusFailed:
		return "failed"
	case StatusNeedsOperator:
		return "needs_operator"
	case StatusLocked:
		return "locked"
	}
	return fmt.Sprintf("status(%d)", int(st))
}

// Op returns the operation name.
func (s *Session) Op() string { return s.cfg.Op }
