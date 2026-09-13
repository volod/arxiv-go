package archive

import (
	"context"
	"crypto/rand"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// ErrNotImplemented is returned by an operation body that this build does not provide yet.
var ErrNotImplemented = errors.New("operation not implemented in this build")

// Config describes one run. The cli package maps validated options onto it.
type Config struct {
	Op                 string
	Version            string
	Archive            string
	VideoArchive       string // empty for scan
	CreateVideoArchive bool   // split: create the video archive root after taking the lock
	DryRun             bool
	NewRun             bool
	ForceUnlock        bool

	// Options is every validated option, stored in options.json. Defining is the subset that must
	// be equal for a later process to resume the run.
	Options  any
	Defining any

	Console            slog.Handler // console handler; the run log is added as a second handler
	LogLevel           slog.Level
	ProgressInterval   time.Duration
	CheckpointEvery    int
	CheckpointInterval time.Duration

	// Test hooks; zero values use the real system.
	Now   func() time.Time
	Rand  io.Reader
	Lock  state.LockOptions // Host, PID and Alive; ForceUnlock comes from the field above
	Ticks func(d time.Duration) (<-chan time.Time, func())

	// Recoverer, when set, is applied to an existing wal.jsonl after the lock is taken.
	Recoverer state.Resolver
	Crash     state.CrashHook
}

func (c *Config) defaults() {
	if c.Now == nil {
		c.Now = time.Now
	}
	if c.Rand == nil {
		c.Rand = rand.Reader
	}
	if c.Console == nil {
		c.Console = slog.DiscardHandler
	}
	if c.Ticks == nil {
		c.Ticks = func(d time.Duration) (<-chan time.Time, func()) {
			t := time.NewTicker(d)
			return t.C, t.Stop
		}
	}
	c.Lock.ForceUnlock = c.ForceUnlock
}

// maxIssues caps the issues kept for report.json; every issue is also logged.
const maxIssues = 10000

// Session is a running operation: it holds the locks, the run directory, the run log, counters,
// checkpoints and the progress reporter. Methods other than Finish are safe for concurrent use.
type Session struct {
	cfg      Config
	Log      *slog.Logger
	Run      state.RunDir
	Resumed  bool
	Stats    *Stats
	Progress *Progress

	locks   []*state.Lock
	runLog  *state.RunLog
	wal     *state.WAL
	started time.Time

	mu            sync.Mutex
	cp            state.Checkpoint
	elapsedBase   float64
	throttle      *state.Throttle
	phases        []state.PhaseStats
	phaseOpen     bool
	phaseBase     state.Counters
	issues        []state.Issue
	issuesOmitted int64

	stopProgress func()
	progressDone chan struct{}
}

// Start takes the run lock in the archive root (and the mirror lock in the video archive root),
// resumes the incomplete current run when its defining options match or creates a new run
// directory, opens the run log and writes the first checkpoint. A lock that cannot be taken
// returns a *state.LockedError before anything else is written.
func Start(ctx context.Context, cfg Config) (*Session, error) {
	cfg.defaults()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	console := slog.New(cfg.Console)
	s := &Session{cfg: cfg, Log: console, Stats: &Stats{}, started: cfg.Now()}
	ok := false
	defer func() {
		if !ok {
			s.releaseLocks()
			if s.wal != nil {
				_ = s.wal.Close()
			}
			if s.runLog != nil {
				s.runLog.Close()
			}
		}
	}()

	provisional, err := state.NewRunID(s.started, cfg.Rand)
	if err != nil {
		return nil, err
	}
	if err := s.lockRoots(provisional); err != nil {
		return nil, err
	}
	if err := s.openRun(); err != nil {
		return nil, err
	}
	for _, l := range s.locks {
		if l.Info().RunID != s.Run.ID {
			if err := l.SetRunID(s.Run.ID); err != nil {
				return nil, err
			}
		}
	}

	fileLevel := slog.LevelInfo
	if cfg.LogLevel < slog.LevelInfo {
		fileLevel = cfg.LogLevel
	}
	if s.runLog, err = state.OpenRunLog(s.Run.File(state.LogFile), fileLevel); err != nil {
		return nil, err
	}
	s.Log = slog.New(state.Fanout(cfg.Console, s.runLog.Handler()))
	s.logStart()
	if err := s.recoverWAL(ctx); err != nil {
		return nil, err
	}

	s.throttle = state.NewThrottle(cfg.CheckpointEvery, cfg.CheckpointInterval, cfg.Now)
	s.phaseBase = s.Stats.Snapshot()
	if err := s.Checkpoint(); err != nil {
		return nil, err
	}
	s.Progress = NewProgress(s.Log, s.Stats, cfg.ProgressInterval, cfg.Now)
	s.startProgress()
	ok = true
	return s, nil
}

func (s *Session) lockRoots(runID string) error {
	cfg := s.cfg
	info := state.LockInfo{RunID: runID, StartedAt: s.started.UTC(), Op: cfg.Op, Role: state.RoleArchive, Peer: cfg.VideoArchive}
	if err := s.lock(cfg.Archive, info); err != nil {
		return err
	}
	if cfg.VideoArchive == "" {
		return nil
	}
	if cfg.CreateVideoArchive {
		if cfg.DryRun {
			s.Log.Info("dry run: video archive root does not exist; not creating it or its lock", "video_archive", cfg.VideoArchive)
			return nil
		}
		if err := os.Mkdir(cfg.VideoArchive, 0o755); err != nil && !errors.Is(err, fs.ErrExist) {
			return fmt.Errorf("create video archive root: %w", err)
		}
		if err := fsops.SyncDir(filepath.Dir(cfg.VideoArchive)); err != nil {
			return err
		}
	}
	info.Role, info.Peer = state.RoleMirror, cfg.Archive
	return s.lock(cfg.VideoArchive, info)
}

func (s *Session) lock(root string, info state.LockInfo) error {
	l, err := state.AcquireLock(root, info, s.cfg.Lock)
	if err != nil {
		return err
	}
	switch prev := l.TookOver; {
	case prev == nil:
	case prev.RunID == "":
		s.Log.Warn("replaced unreadable run lock (--force-unlock)", "lock", l.Path())
	default:
		s.Log.Warn("took over stale run lock (--force-unlock)", "lock", l.Path(),
			"owner_run", prev.RunID, "owner_pid", prev.PID, "owner_host", prev.Host, "owner_op", prev.Op)
	}
	s.locks = append(s.locks, l)
	return nil
}

func (s *Session) logStart() {
	lock := s.locks[0].Info()
	s.Log.Info("run started", "op", s.cfg.Op, "run_id", s.Run.ID, "resumed", s.Resumed,
		"dry_run", s.cfg.DryRun, "phase", s.cp.Phase, "archive", s.cfg.Archive,
		"video_archive", s.cfg.VideoArchive, "pid", lock.PID, "host", lock.Host,
		"version", s.cfg.Version, "run_dir", s.Run.Path)
}

func (s *Session) startProgress() {
	ctx, cancel := context.WithCancel(context.Background())
	ticks, stopTicks := s.cfg.Ticks(s.cfg.ProgressInterval)
	s.progressDone = make(chan struct{})
	s.stopProgress = func() {
		cancel()
		<-s.progressDone
		stopTicks()
	}
	go func() {
		defer close(s.progressDone)
		s.Progress.Run(ctx, ticks)
	}()
}

func (s *Session) releaseLocks() []error {
	var errs []error
	for i := len(s.locks) - 1; i >= 0; i-- {
		if err := s.locks[i].Release(); err != nil {
			errs = append(errs, err)
		}
	}
	s.locks = nil
	return errs
}
