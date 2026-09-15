package archive

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"

	"github.com/volod/arxiv-go/internal/state"
)

// openRun resumes the current incomplete run or creates a new run directory.
func (s *Session) openRun(ctx context.Context) error {
	cfg := s.cfg
	options, err := json.Marshal(cfg.Options)
	if err != nil {
		return err
	}
	defining, err := json.Marshal(cfg.Defining)
	if err != nil {
		return err
	}
	if !cfg.DryRun {
		if resumed, err := s.tryResume(ctx, defining); err != nil || resumed {
			return err
		}
	}
	if s.Run, err = state.CreateRunDir(cfg.Archive, s.started, cfg.Rand); err != nil {
		return err
	}
	ro := state.RunOptions{
		V: 1, RunID: s.Run.ID, Op: cfg.Op, Version: cfg.Version, CreatedAt: s.started.UTC(),
		Archive: cfg.Archive, DryRun: cfg.DryRun, Defining: defining, Options: options,
	}
	setRunPayload(&ro, cfg.Payload)
	if err := state.WriteJSON(s.Run.File(state.OptionsFile), ro); err != nil {
		return err
	}
	s.cp = state.Checkpoint{RunID: s.Run.ID, Op: cfg.Op, Phase: "start"}
	if cfg.DryRun {
		return nil // a dry run never becomes the run that recovery and resume look at
	}
	return state.WriteCurrent(cfg.Archive, s.Run.ID)
}

// tryResume continues the run named by current when it has no report and was started with the
// same operation and defining options. An incomplete run that is not resumed is recovered first,
// so a new run never replaces unfinished transactions. Corrupt state stops the run for operator
// action.
func (s *Session) tryResume(ctx context.Context, defining json.RawMessage) (bool, error) {
	id, err := state.ReadCurrent(s.cfg.Archive)
	if err != nil || id == "" {
		return false, err
	}
	rd, err := state.OpenRunDir(s.cfg.Archive, id)
	if errors.Is(err, fs.ErrNotExist) {
		s.Log.Warn("current run directory is missing; starting a new run", "run_id", id)
		return false, nil
	}
	if err != nil {
		return false, err
	}
	if done, err := rd.Complete(); err != nil || done {
		return false, err
	}
	var prev state.RunOptions
	if err := state.ReadJSON(rd.File(state.OptionsFile), &prev); err != nil {
		return false, err
	}
	if s.cfg.NewRun || !prev.SameDefinition(s.cfg.Op, defining) || runPayload(prev) != s.cfg.Payload {
		if err := s.recoverReplaced(ctx, rd, prev); err != nil {
			return false, err
		}
		if s.cfg.NewRun {
			s.Log.Info("--new-run: leaving the incomplete run and starting a new one", "previous_run", id)
		} else {
			s.Log.Warn("incomplete run has a different operation or options; leaving it and starting a new run",
				"previous_run", id, "previous_op", prev.Op)
		}
		return false, nil
	}
	cp, err := state.ReadCheckpoint(rd.File(state.CheckpointFile))
	switch {
	case errors.Is(err, state.ErrNoCheckpoint):
		cp = state.Checkpoint{RunID: id, Op: s.cfg.Op, Phase: "start"}
	case err != nil:
		return false, err
	case cp.RunID != id || cp.Op != s.cfg.Op:
		return false, fmt.Errorf("%w: checkpoint of run %s names run %q op %q", state.ErrStateCorrupt, id, cp.RunID, cp.Op)
	}
	s.Run, s.Resumed, s.cp, s.elapsedBase = rd, true, cp, cp.ElapsedS
	s.Stats.Restore(cp.Counters)
	return true, nil
}

func (s *Session) recoverWAL(ctx context.Context) error {
	path := s.Run.File(state.WALFile)
	if _, err := os.Stat(path); errors.Is(err, fs.ErrNotExist) {
		return nil
	} else if err != nil {
		return err
	}
	w, err := s.openWALFile()
	if err != nil {
		return err
	}
	if s.cfg.Recoverer == nil {
		return nil
	}
	resolverLog(s.cfg.Recoverer, s.Log)
	_, err = state.Recover(ctx, w, s.cfg.Recoverer, s.Log)
	return err
}

// OpenWAL opens (or creates) wal.jsonl for this run. Split and restore write transactions here.
func (s *Session) OpenWAL() (*state.WAL, error) {
	if s.wal != nil {
		return s.wal, nil
	}
	return s.openWALFile()
}

func (s *Session) openWALFile() (*state.WAL, error) {
	w, err := state.OpenWAL(s.Run.File(state.WALFile), s.Run.ID, state.WALOptions{
		Now: s.cfg.Now, Crash: s.cfg.Crash,
	})
	if err != nil {
		return nil, err
	}
	s.wal = w
	return w, nil
}
