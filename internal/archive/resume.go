package archive

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"

	"github.com/volod/arxiv-go/internal/state"
)

// openRun resumes the current incomplete run or creates a new run directory.
func (s *Session) openRun() error {
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
		if resumed, err := s.tryResume(defining); err != nil || resumed {
			return err
		}
	}
	if s.Run, err = state.CreateRunDir(cfg.Archive, s.started, cfg.Rand); err != nil {
		return err
	}
	ro := state.RunOptions{
		V: 1, RunID: s.Run.ID, Op: cfg.Op, Version: cfg.Version, CreatedAt: s.started.UTC(),
		Archive: cfg.Archive, VideoArchive: cfg.VideoArchive, DryRun: cfg.DryRun,
		Defining: defining, Options: options,
	}
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
// same operation and defining options. Corrupt state stops the run for operator action.
func (s *Session) tryResume(defining json.RawMessage) (bool, error) {
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
	switch {
	case s.cfg.NewRun:
		s.Log.Info("--new-run: leaving the incomplete run and starting a new one", "previous_run", id)
		return false, nil
	case !prev.SameDefinition(s.cfg.Op, defining):
		s.Log.Warn("incomplete run has a different operation or options; starting a new run "+
			"(rerun with the same options to resume it)", "previous_run", id, "previous_op", prev.Op)
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
