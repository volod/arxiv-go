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
		Archive: c.Archive, Payload: payloadOf(c),
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

// payloadOf is the payload kind and mirror root of split and restore options; zero for scan.
func payloadOf(c Common) archive.Payload {
	if c.Payload == "" {
		return archive.Payload{}
	}
	root := c.VideoArchive
	if c.Payload == PayloadCatia {
		root = c.CatiaArchive
	}
	return archive.Payload{Kind: archive.PayloadKind(c.Payload), Root: root}
}

func verifyMode(v string) fsops.VerifyMode {
	if v == VerifyHash {
		return fsops.VerifyHash
	}
	return fsops.VerifySize
}

// splitResolver is the recovery resolver of a split run; execute uses its description writer too.
func splitResolver(o SplitOptions) archive.SplitResolver {
	verify := verifyMode(o.Verify)
	r := archive.NewSplitResolver(nil, verify, nil)
	r.Descriptions = archive.NewMarkdownDescription(archive.DescriptionConfig{
		Archive: o.Archive, Mirror: payloadOf(o.Common).Root, BaseURL: o.BaseURL,
		Registry: o.Registry, Version: version, Payload: archive.PayloadKind(o.Payload), Verify: verify,
	})
	return r
}

// restoreResolver is the recovery resolver of a restore run.
func restoreResolver(o RestoreOptions) archive.RestoreResolver {
	return archive.NewRestoreResolver(archive.RestoreResolver{
		Verify: verifyMode(o.Verify), KeepDescriptions: o.Descriptions == PolicyKeep,
		KeepSource: o.Transfer == TransferCopy, Archive: o.Archive, Payload: archive.PayloadKind(o.Payload),
	})
}

// recovererFor rebuilds the resolver of an earlier incomplete run from the payload and options.json
// it recorded, so a run that a new run replaces finishes its transactions with the payload and
// options that started them. The payload is required and must match the stored options.
func recovererFor(op string, payload archive.PayloadKind, raw json.RawMessage) (archive.Resolver, error) {
	var common struct{ Payload string }
	if err := json.Unmarshal(raw, &common); err != nil {
		return nil, err
	}
	switch {
	case payload == "":
		return nil, fmt.Errorf("%s run has no payload", op)
	case payload != archive.PayloadVideo && (payload != archive.PayloadCatia || op != OpSplit):
		return nil, fmt.Errorf("%s run of payload %q: not available in this build", op, payload)
	case common.Payload != string(payload):
		return nil, fmt.Errorf("%s run options name payload %q, run payload %q", op, common.Payload, payload)
	}
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
	return Common{Archive: c.Archive, Payload: c.Payload, VideoArchive: c.VideoArchive, CatiaArchive: c.CatiaArchive}
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
