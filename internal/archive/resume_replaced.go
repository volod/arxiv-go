package archive

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"

	"github.com/volod/arxiv-go/internal/state"
)

// ErrUnrecoveredRun reports an interrupted run with unfinished transactions that this process may
// not recover because it does not lock that run's roots. It needs operator action (exit 5).
var ErrUnrecoveredRun = errors.New("an interrupted run has unfinished transactions")

// recoverReplaced finishes the open WAL transactions of the incomplete run rd before a new run
// replaces it as current, so current never moves away from unfinished work. The resolver is
// rebuilt from the payload and options that run recorded. Recovery needs the locks of that run's
// roots: a scan, or a run of another payload or mirror root, stops with ErrUnrecoveredRun and
// changes nothing. A split or restore run without a payload is corrupt.
func (s *Session) recoverReplaced(ctx context.Context, rd state.RunDir, prev state.RunOptions) (err error) {
	path := rd.File(state.WALFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	w, err := state.OpenWAL(path, rd.ID, state.WALOptions{Now: s.cfg.Now, Crash: s.cfg.Crash})
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, w.Close()) }()
	open := len(w.OpenTransactions())
	if open == 0 {
		return nil
	}
	payload := runPayload(prev)
	if payload.Kind == "" || payload.Root == "" {
		return fmt.Errorf("%w: %s run %s has %d unfinished transactions but %s names no payload and mirror root",
			state.ErrStateCorrupt, prev.Op, rd.ID, open, state.OptionsFile)
	}
	if prev.Archive != s.cfg.Archive || payload != s.cfg.Payload || s.cfg.RecovererFor == nil {
		return fmt.Errorf("%w: %s %s run %s has %d unfinished transactions; rerun %s with --archive %q "+
			"%s %q (add --new-run to start over after recovery)",
			ErrUnrecoveredRun, payload.Kind, prev.Op, rd.ID, open, prev.Op, prev.Archive, payload.Kind.MirrorFlag(), payload.Root)
	}
	res, err := s.cfg.RecovererFor(prev.Op, payload.Kind, prev.Options)
	if err != nil {
		return fmt.Errorf("%w: run %s: rebuild recovery from %s: %v", state.ErrStateCorrupt, rd.ID, state.OptionsFile, err)
	}
	runLog, err := state.OpenRunLog(rd.File(state.LogFile), runLogLevel(s.cfg.LogLevel))
	if err != nil {
		return err
	}
	defer func() { err = errors.Join(err, runLog.Close()) }()
	log := slog.New(state.Fanout(s.cfg.Console, runLog.Handler()))
	log.Warn("recovering the interrupted run before a new run replaces it", "previous_run", rd.ID,
		"previous_op", prev.Op, "payload", payload.Kind, "open_transactions", open)
	n, err := state.Recover(ctx, w, res, log)
	if err != nil {
		return err
	}
	log.Info("interrupted run recovered", "previous_run", rd.ID, "aborted", n.Aborted, "committed", n.Committed)
	return nil
}

// runLogLevel is the run log level: info, or debug when the console asks for it.
func runLogLevel(console slog.Level) slog.Level {
	return min(console, slog.LevelInfo)
}
