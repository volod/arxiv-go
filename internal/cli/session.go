package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/internal/fsops"
)

// sessionHooks lets tests replace the process identity used by the run lock and the clock. It sees
// the complete configuration just before the session starts.
var sessionHooks = func(cfg *archive.Config) {}

// body is an operation's work inside a started session.
type body func(ctx context.Context, s *archive.Session) error

// sessionConfig maps the common options onto an archive run configuration. opts is stored in
// options.json; defining is the subset that must match to resume an incomplete run.
func sessionConfig(op string, c Common, opts, defining any) archive.Config {
	cfg := archive.Config{
		Op: op, Version: version,
		Archive: c.Archive, VideoArchive: c.VideoArchive,
		DryRun: c.DryRun, NewRun: c.NewRun, ForceUnlock: c.ForceUnlock,
		Options: opts, Defining: defining,
		LogLevel:           c.LogLevel,
		ProgressInterval:   c.ProgressInterval,
		CheckpointEvery:    c.CheckpointEvery,
		CheckpointInterval: c.CheckpointInterval,
		Preflight:          archive.PreflightOptions{MinFree: int64(c.MinFree)},
		RecovererFor:       recovererFor,
	}
	return cfg
}

func verifyMode(v string) fsops.VerifyMode {
	if v == VerifyHash {
		return fsops.VerifyHash
	}
	return fsops.VerifySize
}

// splitResolver is the recovery resolver of a split run; execute uses its stub writer too.
func splitResolver(o SplitOptions) archive.SplitResolver {
	verify := verifyMode(o.Verify)
	r := archive.NewSplitResolver(nil, verify, nil)
	r.Stubs = archive.NewMarkdownStub(archive.StubConfig{
		Archive: o.Archive, VideoArchive: o.VideoArchive, BaseURL: o.BaseURL,
		Registry: o.Registry, Version: version, Verify: verify,
	})
	return r
}

// restoreResolver is the recovery resolver of a restore run.
func restoreResolver(o RestoreOptions) archive.RestoreResolver {
	return archive.NewRestoreResolver(archive.RestoreResolver{
		Verify: verifyMode(o.Verify), KeepStubs: o.Stubs == StubsKeep,
		KeepSource: o.Transfer == TransferCopy, Archive: o.Archive,
	})
}

// recovererFor rebuilds the resolver of an earlier incomplete run from its options.json, so a run
// that a new run replaces finishes its transactions with the options that started them.
func recovererFor(op string, raw json.RawMessage) (archive.Resolver, error) {
	switch op {
	case OpSplit:
		var o SplitOptions
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, err
		}
		return splitResolver(o), nil
	case OpRestore:
		var o RestoreOptions
		if err := json.Unmarshal(raw, &o); err != nil {
			return nil, err
		}
		return restoreResolver(o), nil
	}
	return nil, fmt.Errorf("operation %q writes no transactions", op)
}

// scanConfig maps the scan settings onto the registry scan of root.
func scanConfig(root string, sc ScanSettings, probePath string, preflight bool) archive.ScanConfig {
	return archive.ScanConfig{
		Root: root, Registry: sc.Registry, Metadata: sc.Metadata, FFprobePath: probePath, LargeThreshold: int64(sc.LargeThreshold),
		VideoExtensions: sc.VideoExtensions, Exclude: sc.Exclude, Preflight: preflight,
	}
}

// definingCommon keeps only the common options that define a run's output: the roots. Logging,
// progress, checkpoint cadence, --min-free, --dry-run, --new-run and --force-unlock may change
// between the interrupted process and the one resuming it.
func definingCommon(c Common) Common {
	return Common{Archive: c.Archive, VideoArchive: c.VideoArchive}
}

// runSession starts the run, executes fn and maps the outcome to an exit code.
func runSession(ctx context.Context, cfg archive.Config, log *slog.Logger, fn body) int {
	cfg.Console = log.Handler()
	sessionHooks(&cfg)
	s, err := archive.Start(ctx, cfg)
	if err != nil {
		return exitCode(archive.StartFailed(ctx, log, err))
	}
	res := s.Finish(ctx, fn(ctx, s))
	return exitCode(res.Status)
}

func exitCode(st archive.Status) int {
	switch st {
	case archive.StatusCompleted:
		return ExitOK
	case archive.StatusPartial:
		return ExitPartial
	case archive.StatusNotImplemented:
		return ExitNotImplemented
	case archive.StatusInterrupted:
		return ExitInterrupted
	case archive.StatusLocked, archive.StatusNeedsOperator:
		return ExitLocked
	case archive.StatusInsufficientSpace:
		return ExitInsufficientDisk
	default:
		return ExitFailure
	}
}

// notImplemented is the body of operations whose work is not in this build: the run lifecycle
// (lock, run directory, log, checkpoint, report) runs, then the operation exits 70.
func notImplemented(ctx context.Context, s *archive.Session) error {
	if err := s.Phase("validate", archive.Totals{}); err != nil {
		return err
	}
	s.Log.Error("operation not implemented in this build", "op", s.Op())
	return archive.ErrNotImplemented
}
