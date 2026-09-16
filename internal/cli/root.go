// Package cli owns command-line parsing, flag validation, logger construction, signal handling
// and exit codes. It passes validated, typed Options to the domain packages.
//
// The contract is specified in docs/openspec/stage-1-core/cli.md.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/internal/media"
)

// Exit codes are part of the operator contract; see the CLI specification.
const (
	ExitOK               = 0
	ExitFailure          = 1
	ExitUsage            = 2
	ExitMissingTool      = 3
	ExitInsufficientDisk = 4
	ExitLocked           = 5
	ExitPartial          = 6
	ExitInterrupted      = 130
)

// Operation names accepted as the first argument. Scan is the default.
const (
	OpScan    = "scan"
	OpSplit   = "split"
	OpRestore = "restore"
	// OpPublish is reserved for cloud publishing.
	OpPublish = "publish"
)

// version is the VERSION file, stamped by make with -ldflags "-X .../internal/cli.version=...";
// a plain go build reports dev.
var version = "dev"

// Handlers execute validated operations and return an exit code. Handlers must return promptly
// after ctx is canceled; the dispatcher then reports ExitInterrupted.
type Handlers struct {
	Scan    func(ctx context.Context, opts ScanOptions, log *slog.Logger) int
	Split   func(ctx context.Context, opts SplitOptions, log *slog.Logger) int
	Restore func(ctx context.Context, opts RestoreOptions, log *slog.Logger) int
}

// defaultHandlers is the operation table of this build. Each operation runs inside a run session.
var defaultHandlers = Handlers{
	Scan: func(ctx context.Context, o ScanOptions, log *slog.Logger) int {
		d := o
		d.Common = definingCommon(o.Common)
		cfg := sessionConfig(OpScan, o.Common, o, d)
		cfg.Registry, cfg.Preflight.Metadata = o.Registry, o.Metadata
		return runSession(ctx, cfg, log, archive.ScanBody(scanConfig(o.Archive, o.ScanSettings, o.Tools.Path(media.FFprobe), true)))
	},
	Split: func(ctx context.Context, o SplitOptions, log *slog.Logger) int {
		d := o
		d.Common, d.CreateMirror = definingCommon(o.Common), false
		cfg := sessionConfig(OpSplit, o.Common, o, d)
		cfg.CreateMirror = o.CreateMirror
		cfg.Preflight.Transfer = o.Transfer
		scan := scanConfig(o.Archive, o.ScanSettings, o.Tools.Path(media.FFprobe), false)
		scan.SkipPaths = []string{cfg.Payload.Root}
		resolver := splitResolver(o)
		cfg.Recoverer = resolver
		return runSession(ctx, cfg, log, archive.SplitBody(archive.SplitConfig{
			Scan: scan, Transfer: o.Transfer, Verify: verifyMode(o.Verify), BaseURL: o.BaseURL, Descriptions: resolver.Descriptions,
			Preview: o.Preview, Tools: o.Tools, CatiaText: o.CatiaText,
		}))
	},
	Restore: func(ctx context.Context, o RestoreOptions, log *slog.Logger) int {
		d := o
		d.Common = definingCommon(o.Common)
		cfg := sessionConfig(OpRestore, o.Common, o, d)
		cfg.Preflight.Transfer = o.Transfer
		resolver := restoreResolver(o)
		cfg.Recoverer = resolver
		rc := archive.RestoreConfig{
			Scan: archive.ScanConfig{
				Root: cfg.Payload.Root, Metadata: MetadataFile, LargeThreshold: int64(defaultLarge),
				SkipPaths: []string{o.Archive},
			},
			Transfer: o.Transfer, Verify: resolver.Verify,
			CreateDirs: o.CreateDirs, Overwrite: o.Overwrite, RegistryUpdate: o.RegistryUpdate,
			KeepDescriptions: resolver.KeepDescriptions, KeepSource: resolver.KeepSource,
			DeletePreviews: o.Previews == PolicyDelete,
		}
		cfg.SidecarCleanup = archive.RestoreSidecarCleanup(cfg.Payload.Kind, rc)
		return runSession(ctx, cfg, log, archive.RestoreBody(rc))
	},
}

// env is the process environment seen by the dispatcher; tests replace it.
type env struct {
	stdout, stderr io.Writer
	lookupEnv      func(string) (string, bool)
	envFile        string // optional dotenv file; "" disables it
	fs             rootFS
	handlers       Handlers
	finder         media.Finder // tool discovery; its GOOS also selects the download links
	discover       toolDiscover // test seam
	goarch         string       // overrides runtime.GOARCH for the download links; tests only
}

// Run executes one command with the process environment and returns the process exit code.
// SIGINT and SIGTERM cancel the operation context; a second signal terminates immediately.
func Run(args []string, stdout, stderr io.Writer) int {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	go func() {
		<-ctx.Done()
		stop() // restore default handling so a second signal ends the process
	}()
	return run(ctx, args, env{
		stdout:    stdout,
		stderr:    stderr,
		lookupEnv: os.LookupEnv,
		envFile:   defaultEnvFile(),
		fs:        osRootFS(),
		handlers:  defaultHandlers,
	})
}

func isOperation(s string) bool {
	return s == OpScan || s == OpSplit || s == OpRestore
}

func run(ctx context.Context, args []string, e env) int {
	op := OpScan
	if len(args) > 0 && (args[0] == "" || args[0][0] != '-') {
		op, args = args[0], args[1:]
	}
	switch {
	case op == "help":
		return runHelp(args, e)
	case op == "version":
		if len(args) > 0 {
			return usageError(e, "", fmt.Errorf("version takes no arguments, got %q", args[0]))
		}
		fmt.Fprintf(e.stdout, "arxgo %s\n", version)
		return ExitOK
	case op == OpPublish:
		return usageError(e, "", fmt.Errorf("operation %q: option not available in this build", op))
	case !isOperation(op):
		return usageError(e, "", fmt.Errorf("unknown operation %q", op))
	}

	fileValues, err := readEnvFile(e.envFile)
	if err != nil && !wantsHelp(args) {
		return usageError(e, op, err)
	}
	s, err := parseFlags(op, args, layeredLookup(e.lookupEnv, fileValues, e.envFile))
	if errors.Is(err, errHelp) {
		writeOpHelp(e.stdout, op)
		return ExitOK
	}
	if err != nil {
		return usageError(e, op, err)
	}

	var (
		code int
		ok   bool
	)
	switch op {
	case OpScan:
		o, err := buildScanOptions(s, e.fs)
		if err != nil {
			return usageError(e, op, err)
		}
		log := NewLogger(e.stderr, o.LogLevel, o.LogFormat)
		logOptions(log, op, o, e.envFile, fileValues)
		if o.Tools, code, ok = requireTools(ctx, e, log, scanNeeds(o.ScanSettings)); ok {
			code = e.handlers.Scan(ctx, o, log)
		}
	case OpSplit:
		o, err := buildSplitOptions(s, e.fs)
		if err != nil {
			return usageError(e, op, err)
		}
		log := NewLogger(e.stderr, o.LogLevel, o.LogFormat)
		logOptions(log, op, o, e.envFile, fileValues)
		if o.Tools, code, ok = requireTools(ctx, e, log, media.Needs{MetadataMedia: o.Metadata == MetadataMedia,
			Sample: o.Preview.SampleMode, Image: o.Preview.ImageMode}); ok {
			code = e.handlers.Split(ctx, o, log)
		}
	case OpRestore:
		o, err := buildRestoreOptions(s, e.fs)
		if err != nil {
			return usageError(e, op, err)
		}
		log := NewLogger(e.stderr, o.LogLevel, o.LogFormat)
		logOptions(log, op, o, e.envFile, fileValues)
		code = e.handlers.Restore(ctx, o, log)
	}
	if ctx.Err() != nil && code != ExitOK {
		return ExitInterrupted
	}
	return code
}

// logOptions logs the validated options and whether the environment file supplied values. Values
// from the file are never logged because the file may hold credentials; only unknown ARXGO_*
// names are reported.
func logOptions(log *slog.Logger, op string, opts any, envFile string, fileValues map[string]string) {
	if fileValues != nil {
		log.Debug("environment file loaded", "path", envFile, "variables", len(fileValues))
		for _, key := range unknownArxgoKeys(fileValues) {
			log.Warn("unknown variable in environment file", "path", envFile, "variable", key)
		}
	}
	log.Debug("options", "op", op, "options", fmt.Sprintf("%+v", opts))
}

// wantsHelp reports whether args ask for operation help, which must work with a broken .env file.
func wantsHelp(args []string) bool {
	for _, a := range args {
		switch a {
		case "-h", "--h", "-help", "--help":
			return true
		case "--":
			return false
		}
	}
	return false
}

func runHelp(args []string, e env) int {
	switch {
	case len(args) == 0:
		fmt.Fprint(e.stdout, generalUsage)
		return ExitOK
	case len(args) > 1:
		return usageError(e, "", fmt.Errorf("help takes at most one operation, got %d arguments", len(args)))
	case isOperation(args[0]):
		writeOpHelp(e.stdout, args[0])
		return ExitOK
	case args[0] == OpPublish:
		return usageError(e, "", fmt.Errorf("operation %q: option not available in this build", args[0]))
	default:
		return usageError(e, "", fmt.Errorf("unknown operation %q", args[0]))
	}
}

// usageError prints a validation or usage failure and returns ExitUsage. Each joined error is
// printed on its own line.
func usageError(e env, op string, err error) int {
	var joined interface{ Unwrap() []error }
	errs := []error{err}
	if errors.As(err, &joined) {
		errs = joined.Unwrap()
	}
	for _, one := range errs {
		fmt.Fprintf(e.stderr, "arxgo: %v\n", one)
	}
	if op != "" {
		fmt.Fprintf(e.stderr, "Run 'arxgo help %s' for usage.\n", op)
	} else {
		fmt.Fprintf(e.stderr, "Run 'arxgo help' for usage.\n")
	}
	return ExitUsage
}
