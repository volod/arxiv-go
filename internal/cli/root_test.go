package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"
)

func testEnv(stdout, stderr *bytes.Buffer, lookup func(string) (string, bool)) env {
	return env{stdout: stdout, stderr: stderr, lookupEnv: lookup, fs: osRootFS(), handlers: defaultHandlers, finder: noTools}
}

func TestRunExitCodes(t *testing.T) {
	archive, video := fixture(t)
	roots := []string{"--archive", archive, "--video-archive", video}
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"version", []string{"version"}, ExitOK, "arxgo dev\n", ""},
		{"version extra argument", []string{"version", "x"}, ExitUsage, "", "version takes no arguments"},
		{"help", []string{"help"}, ExitOK, "Usage:", ""},
		{"help scan", []string{"help", "scan"}, ExitOK, "--large-threshold SIZE", ""},
		{"help split lists preview settings", []string{"help", "split"}, ExitOK, "--sample MODE", ""},
		{"help split lists catia-text", []string{"help", "split"}, ExitOK, "--catia-text", ""},
		{"help restore", []string{"help", "restore"}, ExitOK, "(default true; env ARXGO_REGISTRY_UPDATE)", ""},
		{"help unknown", []string{"help", "shuffle"}, ExitUsage, "", `unknown operation "shuffle"`},
		{"help publish", []string{"help", "publish"}, ExitUsage, "", "not available in this build"},
		{"help too many", []string{"help", "scan", "split"}, ExitUsage, "", "at most one operation"},
		{"operation --help", []string{"split", "--help"}, ExitOK, "Usage: arxgo split", ""},
		{"default operation -h", []string{"-h"}, ExitOK, "Usage: arxgo [scan]", ""},
		{"no arguments", nil, ExitUsage, "", "--archive is required"},
		{"unknown operation", []string{"shuffle"}, ExitUsage, "", `unknown operation "shuffle"`},
		{"reserved publish operation", []string{"publish"}, ExitUsage, "", `operation "publish": option not available in this build`},
		{"invalid flag names the operation help", []string{"restore", "--descriptions", "x"}, ExitUsage, "", "Run 'arxgo help restore' for usage."},
		{"preview mode requires tools", append([]string{"split", "--image", "start"}, roots...), ExitMissingTool, "", "required tool not found"},
		{"validation lists every error", []string{"split", "--checkpoint-every", "0"}, ExitUsage, "", "arxgo: --video-archive is required"},
		{"default operation is scan", []string{"--archive", archive}, ExitOK, "", "op=scan"},
		{"scan", []string{"scan", "--archive", archive}, ExitOK, "", "scan summary"},
		{"scan catia video extension", []string{"scan", "--archive", archive, "--video-extensions", "CATPart"}, ExitUsage, "", "CATIA extension"},
		{"split", append([]string{"split"}, roots...), ExitOK, "", "op=split"},
		{"restore", append([]string{"restore"}, roots...), ExitOK, "", "scan summary"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := run(context.Background(), tc.args, testEnv(&out, &errOut, noProcessEnv))
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, tc.wantCode, errOut.String())
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("stdout %q does not contain %q", out.String(), tc.wantOut)
			}
			if !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr %q does not contain %q", errOut.String(), tc.wantErr)
			}
			if tc.wantCode == ExitUsage && out.Len() != 0 {
				t.Errorf("usage error wrote to stdout: %q", out.String())
			}
		})
	}
}

func TestRunPassesOptionsAndLogger(t *testing.T) {
	archive, video := fixture(t)
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, func(k string) (string, bool) {
		v, ok := map[string]string{"ARXGO_LOG_FORMAT": "json", "ARXGO_LOG_LEVEL": "debug"}[k]
		return v, ok
	})
	var got SplitOptions
	e.handlers.Split = func(_ context.Context, o SplitOptions, log *slog.Logger) int {
		got = o
		log.Info("handler ran")
		return ExitPartial
	}
	code := run(context.Background(), []string{"split", "--archive", archive, "--video-archive", video, "--verify", "hash"}, e)
	if code != ExitPartial {
		t.Fatalf("exit code = %d, want handler code %d (stderr: %s)", code, ExitPartial, errOut.String())
	}
	if got.Archive != archive || got.VideoArchive != video || got.Verify != VerifyHash {
		t.Errorf("handler options = %+v", got)
	}
	lines := strings.Split(strings.TrimSpace(errOut.String()), "\n")
	if len(lines) != 2 {
		t.Fatalf("want debug options line and handler line, got %q", errOut.String())
	}
	for _, line := range lines {
		var rec map[string]any
		if err := json.Unmarshal([]byte(line), &rec); err != nil {
			t.Fatalf("console log is not JSON: %q", line)
		}
	}
	if !strings.Contains(lines[1], `"msg":"handler ran"`) {
		t.Errorf("handler line = %q", lines[1])
	}
}

func TestRunLogLevelFiltersConsole(t *testing.T) {
	archive, _ := fixture(t)
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	e.handlers.Scan = func(_ context.Context, _ ScanOptions, log *slog.Logger) int {
		log.Info("hidden")
		log.Warn("shown")
		return ExitOK
	}
	if code := run(context.Background(), []string{"--archive", archive, "--log-level", "warn"}, e); code != ExitOK {
		t.Fatalf("exit code = %d", code)
	}
	if s := errOut.String(); strings.Contains(s, "hidden") || !strings.Contains(s, "level=WARN msg=shown") {
		t.Errorf("console = %q", s)
	}
}

func TestRunCanceledContextExitsInterrupted(t *testing.T) {
	archive, _ := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	e.handlers.Scan = func(ctx context.Context, _ ScanOptions, _ *slog.Logger) int {
		cancel()
		<-ctx.Done()
		return ExitFailure
	}
	if code := run(ctx, []string{"--archive", archive}, e); code != ExitInterrupted {
		t.Fatalf("exit code = %d, want %d", code, ExitInterrupted)
	}

	e.handlers.Scan = func(context.Context, ScanOptions, *slog.Logger) int { return ExitOK }
	done, cancelDone := context.WithCancel(context.Background())
	cancelDone()
	if code := run(done, []string{"--archive", archive}, e); code != ExitOK {
		t.Fatalf("completed run after cancel: exit code = %d, want %d", code, ExitOK)
	}
}

// TestRunSignalCancelsContext sends SIGINT to the test process while Run is waiting in a handler.
func TestRunSignalCancelsContext(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("sending os.Interrupt to a process is not supported on Windows")
	}
	archive, _ := fixture(t)
	saved := defaultHandlers
	t.Cleanup(func() { defaultHandlers = saved })
	started := make(chan struct{})
	defaultHandlers.Scan = func(ctx context.Context, _ ScanOptions, _ *slog.Logger) int {
		close(started)
		select {
		case <-ctx.Done():
			return ExitFailure
		case <-time.After(10 * time.Second):
			return ExitOK
		}
	}
	result := make(chan int, 1)
	var out, errOut bytes.Buffer
	go func() { result <- Run([]string{"--archive", archive}, &out, &errOut) }()
	<-started
	proc, err := os.FindProcess(os.Getpid())
	if err != nil {
		t.Fatal(err)
	}
	if err := proc.Signal(os.Interrupt); err != nil {
		t.Fatal(err)
	}
	if code := <-result; code != ExitInterrupted {
		t.Fatalf("exit code = %d, want %d", code, ExitInterrupted)
	}
}
