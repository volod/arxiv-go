package cli

import (
	"errors"
	"flag"
	"fmt"
	"io"
	"strings"
	"time"
)

// settings holds raw flag values before they are validated into per-operation Options.
type settings struct {
	archive, videoArchive                 string
	logLevel, logFormat                   string
	progressInterval, checkpointInterval  time.Duration
	checkpointEvery                       int
	dryRun, newRun, forceUnlock           bool
	minFree, largeThreshold               Size
	registry, metadata                    string
	exclude                               []string
	followSymlinks                        bool
	transfer, verify, stubs               string
	baseURL, videoExtensions              string
	createDirs, overwrite, registryUpdate bool
	sampleMode, imageMode                 string
	sampleResolution, imageResolution     string
	sampleQuality, imageQuality           string
	sampleDuration, sampleEvery           time.Duration
	imageEvery                            time.Duration
	previewMaxItems                       int

	// reserved maps a later-stage flag name to its value, which records whether it was given.
	reserved map[string]*reservedValue
	// explicit maps every flag given on the command line or environment to its source.
	explicit map[string]string
}

type group string

const (
	groupCommon  group = "Common flags"
	groupScan    group = "Scan flags"
	groupSplit   group = "Split flags"
	groupRestore group = "Restore flags"
)

// flagDef is one row of the shared flag table. A name may appear in several rows when its
// meaning differs per operation.
type flagDef struct {
	name  string
	ops   []string
	group group
	stage int // 1 = available; 2 or 3 = reserved for that stage
	arg   string
	usage string
	bind  func(fs *flag.FlagSet, s *settings, name, usage string)
}

var (
	allOps          = []string{OpScan, OpSplit, OpRestore}
	scanSplitOps    = []string{OpScan, OpSplit}
	splitOnly       = []string{OpSplit}
	restoreOnly     = []string{OpRestore}
	logLevels       = []string{"debug", "info", "warn", "error"}
	logFormats      = []string{"text", "json"}
	metadataModes   = []string{MetadataFile, MetadataMedia}
	transferModes   = []string{TransferAuto, TransferCopy}
	verifyModes     = []string{VerifySize, VerifyHash}
	stubPolicies    = []string{StubsDelete, StubsKeep}
	previewModes    = []string{"none", "start", "middle", "end", "series"}
	previewSizes    = []string{"sd", "hd", "4k"}
	previewQuality  = []string{"low", "medium", "high"}
	defaultMinFree  = Size(1 << 30)
	defaultLarge    = Size(1 << 30)
	defaultEvery    = 500
	defaultProgress = 10 * time.Second
	defaultCkptTime = 30 * time.Second
)

func str(def string) func(*flag.FlagSet, *settings, string, string) {
	return func(fs *flag.FlagSet, s *settings, name, usage string) {
		fs.StringVar(stringField(s, name), name, def, usage)
	}
}

func enum(def string, allowed []string) func(*flag.FlagSet, *settings, string, string) {
	return func(fs *flag.FlagSet, s *settings, name, usage string) {
		target := stringField(s, name)
		*target = def
		fs.Var(&enumValue{target: target, allowed: allowed}, name, usage)
	}
}

func boolean(def bool) func(*flag.FlagSet, *settings, string, string) {
	return func(fs *flag.FlagSet, s *settings, name, usage string) {
		fs.BoolVar(boolField(s, name), name, def, usage)
	}
}

func duration(def time.Duration, target func(*settings) *time.Duration) func(*flag.FlagSet, *settings, string, string) {
	return func(fs *flag.FlagSet, s *settings, name, usage string) {
		fs.DurationVar(target(s), name, def, usage)
	}
}

func size(def Size, target func(*settings) *Size) func(*flag.FlagSet, *settings, string, string) {
	return func(fs *flag.FlagSet, s *settings, name, usage string) {
		t := target(s)
		*t = def
		fs.Var(t, name, usage)
	}
}

func reserved(isBool bool) func(*flag.FlagSet, *settings, string, string) {
	return func(fs *flag.FlagSet, s *settings, name, usage string) {
		v := &reservedValue{isBool: isBool}
		s.reserved[name] = v
		fs.Var(v, name, usage)
	}
}

func stringField(s *settings, name string) *string {
	fields := map[string]*string{
		"archive": &s.archive, "video-archive": &s.videoArchive, "log-level": &s.logLevel,
		"log-format": &s.logFormat, "registry": &s.registry, "metadata": &s.metadata,
		"transfer": &s.transfer, "verify": &s.verify, "stubs": &s.stubs,
		"base-url": &s.baseURL, "video-extensions": &s.videoExtensions,
		"sample": &s.sampleMode, "image": &s.imageMode,
		"sample-resolution": &s.sampleResolution, "image-resolution": &s.imageResolution,
		"sample-quality": &s.sampleQuality, "image-quality": &s.imageQuality,
	}
	return fields[name]
}

func boolField(s *settings, name string) *bool {
	fields := map[string]*bool{
		"dry-run": &s.dryRun, "new-run": &s.newRun, "force-unlock": &s.forceUnlock,
		"follow-symlinks": &s.followSymlinks, "create-dirs": &s.createDirs,
		"overwrite": &s.overwrite, "registry-update": &s.registryUpdate,
	}
	return fields[name]
}

// errHelp reports that the operator asked for operation help with -h or --help.
var errHelp = errors.New("help requested")

// newFlagSet builds the flag set for one operation from the shared table.
func newFlagSet(op string, s *settings) *flag.FlagSet {
	fs := flag.NewFlagSet(op, flag.ContinueOnError)
	fs.SetOutput(io.Discard)
	s.reserved = map[string]*reservedValue{}
	for _, d := range flagTable {
		if d.appliesTo(op) {
			d.bind(fs, s, d.name, d.usage)
		}
	}
	return fs
}

func (d flagDef) appliesTo(op string) bool {
	for _, o := range d.ops {
		if o == op {
			return true
		}
	}
	return false
}

// EnvName returns the environment variable that overrides a flag default.
func EnvName(flagName string) string {
	return "ARXGO_" + strings.ToUpper(strings.ReplaceAll(flagName, "-", "_"))
}

// parseFlags parses args for op, then applies environment overrides for flags not given on the
// command line. Command-line values win; lookupEnv decides between the process environment and
// the environment file and ignores empty values.
func parseFlags(op string, args []string, lookupEnv lookupFunc) (*settings, error) {
	s := &settings{explicit: map[string]string{}}
	fs := newFlagSet(op, s)
	if err := fs.Parse(args); err != nil {
		if errors.Is(err, flag.ErrHelp) {
			return nil, errHelp
		}
		return nil, errors.New(dashes(err.Error()))
	}
	if fs.NArg() > 0 {
		extra := fs.Arg(0)
		if isOperation(extra) {
			return nil, fmt.Errorf("unexpected argument %q: the operation must come before flags", extra)
		}
		return nil, fmt.Errorf("unexpected argument %q", extra)
	}
	fs.Visit(func(f *flag.Flag) { s.explicit[f.Name] = "--" + f.Name })
	for _, d := range flagTable {
		if !d.appliesTo(op) || s.explicit[d.name] != "" {
			continue
		}
		v, source, ok := lookupEnv(EnvName(d.name))
		if !ok {
			continue
		}
		if err := setFromEnv(fs, d.name, v); err != nil {
			return nil, fmt.Errorf("invalid value %q for %s: %v", v, source, err)
		}
		s.explicit[d.name] = source
	}
	for _, d := range flagTable {
		if r := s.reserved[d.name]; r != nil && r.set {
			return nil, fmt.Errorf("%s: option not available in this build", s.explicit[d.name])
		}
	}
	return s, nil
}

func setFromEnv(fs *flag.FlagSet, name, value string) error {
	if _, ok := fs.Lookup(name).Value.(*listValue); ok {
		for _, item := range splitList(value) {
			if err := fs.Set(name, item); err != nil {
				return err
			}
		}
		return nil
	}
	return fs.Set(name, value)
}

// dashes rewrites the flag package's single-dash flag names in error text to the documented
// double-dash form.
func dashes(msg string) string {
	msg = strings.Replace(msg, "flag -", "flag --", 1)
	return strings.Replace(msg, "defined: -", "defined: --", 1)
}
