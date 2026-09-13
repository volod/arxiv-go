package cli

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/state"
)

// noTools is a finder with an unknown executable location and an empty search path.
var noTools = media.Finder{
	Executable: func() (string, error) { return "", errors.New("no executable in tests") },
	SearchPath: new(string),
	GOOS:       "linux",
}

// fakeTools returns a finder whose search path holds passing fake scripts for tools.
func fakeTools(t *testing.T, tools ...media.Tool) media.Finder {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake tools are shell scripts; Windows discovery is checked in scenario step W6")
	}
	dir := t.TempDir()
	for _, tool := range tools {
		script := "#!/bin/sh\necho \"" + string(tool) + " version 9.9-fake\"\n"
		if err := os.WriteFile(filepath.Join(dir, string(tool)), []byte(script), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f := noTools
	f.SearchPath = &dir
	f.GOOS = runtime.GOOS
	return f
}

func TestMissingToolExitsThreeBeforeLockOrWrite(t *testing.T) {
	cases := []struct {
		goos, goarch string
		links        []string
		name         string
	}{
		{"linux", "amd64", []string{"https://johnvansickle.com/ffmpeg/", "https://ffmpeg.org/download.html#build-linux"}, "ffprobe"},
		{"windows", "amd64", []string{"https://www.gyan.dev/ffmpeg/builds/", "https://github.com/BtbN/FFmpeg-Builds/releases"}, "ffprobe.exe"},
	}
	for _, tc := range cases {
		for _, op := range []string{OpScan, OpSplit} {
			t.Run(tc.goos+"/"+op, func(t *testing.T) {
				arc, video := fixture(t)
				args := []string{op, "--archive", arc, "--metadata", "media"}
				if op == OpSplit {
					args = append(args, "--video-archive", video)
				}
				var out, errOut bytes.Buffer
				e := testEnv(&out, &errOut, noProcessEnv)
				e.finder.GOOS, e.goarch = tc.goos, tc.goarch
				e.handlers.Scan = func(context.Context, ScanOptions, *slog.Logger) int { t.Error("scan handler ran"); return ExitOK }
				e.handlers.Split = func(context.Context, SplitOptions, *slog.Logger) int { t.Error("split handler ran"); return ExitOK }
				if code := run(context.Background(), args, e); code != ExitMissingTool {
					t.Fatalf("exit code = %d, want %d (stderr %s)", code, ExitMissingTool, errOut.String())
				}
				stderr := errOut.String()
				wants := append([]string{
					`level=ERROR msg="required tool not found" tool=` + tc.name + ` unavailable="--metadata media"`,
					"place " + tc.name + " next to arxgo",
				}, tc.links...)
				for _, w := range wants {
					if !strings.Contains(stderr, w) {
						t.Errorf("stderr does not contain %q:\n%s", w, stderr)
					}
				}
				if out.Len() != 0 {
					t.Errorf("stdout = %q", out.String())
				}
				for _, root := range []string{arc, video} {
					if _, err := os.Stat(state.StateDir(root)); !os.IsNotExist(err) {
						t.Errorf("%s written before tool discovery failed: %v", state.StateDir(root), err)
					}
					if entries, _ := os.ReadDir(root); len(entries) != 0 {
						t.Errorf("root %s not empty: %d entries", root, len(entries))
					}
				}
			})
		}
	}
}

func TestFileMetadataNeedsNoTool(t *testing.T) {
	arc, video := fixture(t)
	for _, args := range [][]string{
		{"scan", "--archive", arc},
		{"split", "--archive", arc, "--video-archive", video, "--metadata", "file"},
		{"restore", "--archive", arc, "--video-archive", video},
	} {
		var out, errOut bytes.Buffer
		e := testEnv(&out, &errOut, noProcessEnv)
		e.finder.Executable = func() (string, error) {
			t.Error("discovery ran without a tool requirement")
			return "", errors.New("x")
		}
		e.handlers = Handlers{
			Scan:    func(context.Context, ScanOptions, *slog.Logger) int { return ExitOK },
			Split:   func(context.Context, SplitOptions, *slog.Logger) int { return ExitOK },
			Restore: func(context.Context, RestoreOptions, *slog.Logger) int { return ExitOK },
		}
		if code := run(context.Background(), args, e); code != ExitOK {
			t.Errorf("%v: exit code = %d (stderr %s)", args, code, errOut.String())
		}
	}
}

func TestFoundToolsReachTheHandler(t *testing.T) {
	arc, _ := fixture(t)
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	e.finder = fakeTools(t, media.FFprobe)
	var got media.Toolset
	e.handlers.Scan = func(_ context.Context, o ScanOptions, _ *slog.Logger) int { got = o.Tools; return ExitOK }
	if code := run(context.Background(), []string{"scan", "--archive", arc, "--metadata", "media"}, e); code != ExitOK {
		t.Fatalf("exit code = %d (stderr %s)", code, errOut.String())
	}
	want := filepath.Join(*e.finder.SearchPath, "ffprobe")
	if got.Path(media.FFprobe) != want || got[media.FFprobe].Version != "ffprobe version 9.9-fake" {
		t.Fatalf("tools = %+v, want %s", got, want)
	}
	if !strings.Contains(errOut.String(), `msg="tool found" tool=ffprobe path=`+want) {
		t.Errorf("tool version not logged: %s", errOut.String())
	}
}

func TestToolDiscoveryInterrupted(t *testing.T) {
	arc, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	e.finder = fakeTools(t, media.FFprobe)
	if code := run(ctx, []string{"scan", "--archive", arc, "--metadata", "media"}, e); code != ExitInterrupted {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, ExitInterrupted, errOut.String())
	}
}
