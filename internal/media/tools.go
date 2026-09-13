package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// Tool names an external program that arxgo runs through this package.
type Tool string

// Tools known to discovery.
const (
	FFprobe Tool = "ffprobe"
	FFmpeg  Tool = "ffmpeg"
)

// DefaultVersionTimeout bounds the "-version" validation of one candidate.
const DefaultVersionTimeout = 10 * time.Second

// versionOutputLimit bounds how much "-version" output is read; only the first line is kept.
const versionOutputLimit = 64 << 10

// ExecutableName returns the file name of tool on goos: "ffprobe.exe" on Windows, "ffprobe" elsewhere.
func ExecutableName(tool Tool, goos string) string {
	if goos == "windows" {
		return string(tool) + ".exe"
	}
	return string(tool)
}

// Needs is the part of the validated options that decides which tools a run requires.
type Needs struct {
	// MetadataMedia is true for --metadata media.
	MetadataMedia bool
	// Sample and Image are the stage-2 preview modes; "" and "none" need no tool. The CLI leaves
	// them empty until the stage-2 flags are available.
	Sample, Image string
}

// Requirement is one required tool and the options, as the operator wrote them, that need it.
type Requirement struct {
	Tool    Tool
	Options []string
}

// Requirements computes the tools a run needs, ffprobe first, each with the options that need it.
func Requirements(n Needs) []Requirement {
	var probe, mpeg []string
	if n.MetadataMedia {
		probe = append(probe, "--metadata media")
	}
	for _, p := range []struct{ flag, mode string }{{"--sample", n.Sample}, {"--image", n.Image}} {
		if p.mode != "" && p.mode != "none" {
			opt := p.flag + " " + p.mode
			probe = append(probe, opt)
			mpeg = append(mpeg, opt)
		}
	}
	var reqs []Requirement
	if len(probe) > 0 {
		reqs = append(reqs, Requirement{Tool: FFprobe, Options: probe})
	}
	if len(mpeg) > 0 {
		reqs = append(reqs, Requirement{Tool: FFmpeg, Options: mpeg})
	}
	return reqs
}

// Found is a validated tool.
type Found struct {
	Tool    Tool
	Path    string // absolute path of the executable
	Version string // first line of "-version" output
}

// Toolset holds the validated tool paths of a run, keyed by tool.
type Toolset map[Tool]Found

// Path returns the executable path of tool, or "" when it was not required or not found.
func (s Toolset) Path(tool Tool) string { return s[tool].Path }

// ErrToolNotFound reports that no candidate of a tool passed validation.
var ErrToolNotFound = errors.New("tool not found")

// Finder locates tools next to the running executable, then on PATH. The zero value uses the
// process executable, the PATH environment variable, runtime.GOOS and DefaultVersionTimeout;
// tests replace the fields.
type Finder struct {
	// Executable returns the running executable path; nil means os.Executable. Symlinks in the
	// result are resolved.
	Executable func() (string, error)
	// SearchPath is a path list in the platform format; nil means the PATH environment variable.
	SearchPath *string
	// GOOS selects the executable name; "" means runtime.GOOS.
	GOOS string
	// Timeout bounds each "-version" run; 0 means DefaultVersionTimeout.
	Timeout time.Duration
	// Log receives the accepted version line and rejected candidates; nil discards.
	Log *slog.Logger
	// checkCandidate and probeVersion are test seams. Nil preserves the real filesystem and
	// process checks; keeping them private prevents callers from bypassing validation.
	checkCandidate func(string) (string, error)
	probeVersion   func(context.Context, string) (string, error)
}

// Candidates returns the paths tried for tool, in order and without duplicates: the executable's
// directory, then each absolute PATH entry. Relative PATH entries are ignored so that the current
// directory never supplies a tool.
func (f Finder) Candidates(tool Tool) []string {
	name := ExecutableName(tool, f.goos())
	var dirs []string
	exe := f.Executable
	if exe == nil {
		exe = os.Executable
	}
	if p, err := exe(); err == nil {
		if resolved, err := filepath.EvalSymlinks(p); err == nil {
			p = resolved
		}
		dirs = append(dirs, filepath.Dir(p))
	}
	pathList := os.Getenv("PATH")
	if f.SearchPath != nil {
		pathList = *f.SearchPath
	}
	dirs = append(dirs, filepath.SplitList(pathList)...)

	var out []string
	seen := map[string]bool{}
	for _, dir := range dirs {
		if dir == "" || !filepath.IsAbs(dir) {
			continue
		}
		p := filepath.Join(filepath.Clean(dir), name)
		if !seen[p] {
			seen[p] = true
			out = append(out, p)
		}
	}
	return out
}

// Find returns the first candidate of tool that is an executable file and whose "-version" exits
// 0 within the timeout. It returns ErrToolNotFound when none does, or the context error when ctx
// ends first.
func (f Finder) Find(ctx context.Context, tool Tool) (Found, error) {
	log := f.logger()
	check := f.checkCandidate
	if check == nil {
		check = exec.LookPath
	}
	probe := f.probeVersion
	if probe == nil {
		probe = f.version
	}
	for _, candidate := range f.Candidates(tool) {
		path, err := check(candidate)
		if err != nil {
			continue // absent or not executable
		}
		version, err := probe(ctx, path)
		if ctx.Err() != nil {
			return Found{}, ctx.Err()
		}
		if err != nil {
			log.Warn("tool candidate rejected", "tool", string(tool), "path", path, "err", err)
			continue
		}
		log.Info("tool found", "tool", string(tool), "path", path, "version", version)
		return Found{Tool: tool, Path: path, Version: version}, nil
	}
	return Found{}, fmt.Errorf("%s: %w", tool, ErrToolNotFound)
}

// Discover finds every required tool. It returns the tools found and the requirements whose tool
// is missing, in requirement order; a non-nil error is the context error.
func (f Finder) Discover(ctx context.Context, reqs []Requirement) (Toolset, []Requirement, error) {
	found := Toolset{}
	var missing []Requirement
	for _, r := range reqs {
		t, err := f.Find(ctx, r.Tool)
		switch {
		case err == nil:
			found[r.Tool] = t
		case errors.Is(err, ErrToolNotFound):
			missing = append(missing, r)
		default:
			return found, missing, err
		}
	}
	return found, missing, nil
}

// version runs "<path> -version" and returns the first output line.
func (f Finder) version(ctx context.Context, path string) (string, error) {
	timeout := f.Timeout
	if timeout <= 0 {
		timeout = DefaultVersionTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, path, "-version")
	out := &limitedBuffer{limit: versionOutputLimit}
	cmd.Stdout = out
	// A child that inherits stdout and outlives the killed tool must not stall discovery: Wait
	// closes the pipe after this delay.
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	first := firstLine(out.String())
	switch {
	case ctx.Err() == context.DeadlineExceeded:
		return "", fmt.Errorf("-version did not finish within %s", timeout)
	case err != nil:
		return "", fmt.Errorf("-version failed: %w", err)
	case first == "":
		return "", errors.New("-version printed nothing")
	}
	return first, nil
}

func firstLine(s string) string {
	line, _, _ := strings.Cut(s, "\n")
	return strings.TrimSpace(line)
}

// limitedBuffer keeps the first limit bytes written and discards the rest without failing the
// writer, so a verbose tool is not killed by a broken pipe.
type limitedBuffer struct {
	buf   []byte
	limit int
}

func (b *limitedBuffer) Write(p []byte) (int, error) {
	if room := b.limit - len(b.buf); room > 0 {
		b.buf = append(b.buf, p[:min(room, len(p))]...)
	}
	return len(p), nil
}

func (b *limitedBuffer) String() string { return string(b.buf) }

func (f Finder) goos() string {
	if f.GOOS != "" {
		return f.GOOS
	}
	return runtime.GOOS
}

func (f Finder) logger() *slog.Logger {
	if f.Log != nil {
		return f.Log
	}
	return slog.New(slog.DiscardHandler)
}
