//go:build integration

package integration

import (
	"bytes"
	"context"
	"errors"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

const opTimeout = 5 * time.Minute

// arxgoBin is the arxgo executable built for one test, with the environment it runs in.
type arxgoBin struct {
	path string
	env  []string
	work string // replaced by $WORK in logged arguments
}

// buildArxgo builds cmd/arxgo into a test temporary directory, so no bin/.env of the developer
// is next to it, and scrubs every ARXGO_* variable from the environment the binary gets.
func buildArxgo(t *testing.T) *arxgoBin {
	t.Helper()
	goTool, err := exec.LookPath("go")
	if err != nil {
		goTool = filepath.Join(runtime.GOROOT(), "bin", "go")
	}
	moduleRoot, err := filepath.Abs(filepath.Join("..", ".."))
	if err != nil {
		t.Fatal(err)
	}
	exe := "arxgo"
	if runtime.GOOS == "windows" {
		exe += ".exe"
	}
	out := filepath.Join(t.TempDir(), exe)
	cmd := exec.Command(goTool, "build", "-trimpath", "-buildvcs=false", "-o", out, "./cmd/arxgo")
	cmd.Dir = moduleRoot
	cmd.Env = append(os.Environ(), "CGO_ENABLED=0")
	if msg, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("build arxgo: %v\n%s", err, msg)
	}
	// The binary finds ffmpeg and ffprobe on PATH; put the pinned tools first so it runs the same
	// build as the live tests.
	var env []string
	for _, kv := range os.Environ() {
		switch upper := strings.ToUpper(kv); {
		case strings.HasPrefix(upper, "ARXGO_"):
		case strings.HasPrefix(upper, "PATH="):
			env = append(env, "PATH="+tooltest.PinnedDir()+string(os.PathListSeparator)+kv[len("PATH="):])
		default:
			env = append(env, kv)
		}
	}
	return &arxgoBin{path: out, env: env}
}

func (b *arxgoBin) show(args []string) string {
	s := strings.Join(args, " ")
	if b.work != "" {
		s = strings.ReplaceAll(s, b.work, "$WORK")
	}
	return s
}

// result is one finished arxgo process.
type result struct {
	code   int
	stderr string
}

func (b *arxgoBin) command(ctx context.Context, env []string, args []string) (*exec.Cmd, *bytes.Buffer) {
	cmd := exec.CommandContext(ctx, b.path, args...)
	cmd.Env = env
	var stderr bytes.Buffer
	cmd.Stdout = &stderr
	cmd.Stderr = &stderr
	return cmd, &stderr
}

// run executes arxgo to completion and returns its exit code.
func (b *arxgoBin) run(t *testing.T, args ...string) result {
	t.Helper()
	return b.runEnv(t, b.env, args...)
}

func (b *arxgoBin) runEnv(t *testing.T, env []string, args ...string) result {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	cmd, out := b.command(ctx, env, args)
	err := cmd.Run()
	r := result{code: exitCode(t, err), stderr: out.String()}
	t.Logf("arxgo %s -> exit %d", b.show(args), r.code)
	return r
}

func exitCode(t *testing.T, err error) int {
	t.Helper()
	if err == nil {
		return 0
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return exit.ExitCode()
	}
	t.Fatalf("run arxgo: %v", err)
	return -1
}

// killPoint is where a killed run stops: after this process appended `records` WAL records to
// the run it works on.
type killPoint struct {
	records int
}

// runKilled starts arxgo and kills it (os.Process.Kill: SIGKILL on Unix, TerminateProcess on
// Windows) once the WAL of its run has grown by p.records records. It fails the test when the
// process exits before the kill lands, because then the kill point was never exercised.
func (b *arxgoBin) runKilled(t *testing.T, archive string, p killPoint, args ...string) walState {
	t.Helper()
	before := currentWAL(archive)
	ctx, cancel := context.WithTimeout(context.Background(), opTimeout)
	defer cancel()
	cmd, out := b.command(ctx, b.env, args)
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	for {
		select {
		case err := <-done:
			t.Fatalf("arxgo %s finished (exit %d) before %d WAL records were written; output:\n%s",
				b.show(args), exitCode(t, err), p.records, out.String())
		default:
		}
		now := currentWAL(archive)
		if now.grownSince(before) >= p.records {
			if err := cmd.Process.Kill(); err != nil {
				t.Fatalf("kill: %v", err)
			}
			err := <-done
			var exit *exec.ExitError
			if err == nil || !errors.As(err, &exit) {
				t.Fatalf("arxgo %s was not killed (wait: %v); output:\n%s", b.show(args), err, out.String())
			}
			landed := currentWAL(archive)
			t.Logf("arxgo %s killed after %d new WAL records (target %d), run %s, last step %q",
				b.show(args), landed.grownSince(before), p.records, landed.runID, landed.lastStep)
			return landed
		}
		time.Sleep(200 * time.Microsecond)
	}
}

// walState is the WAL of the run named in <archive>/.arxgo/current.
type walState struct {
	runID    string
	records  int
	lastStep string
}

func (w walState) grownSince(before walState) int {
	if w.runID != before.runID {
		return w.records
	}
	return w.records - before.records
}

func currentWAL(archive string) walState {
	id, err := os.ReadFile(filepath.Join(archive, ".arxgo", "current"))
	if err != nil {
		return walState{}
	}
	s := walState{runID: strings.TrimSpace(string(id))}
	data, err := os.ReadFile(filepath.Join(archive, ".arxgo", "runs", s.runID, "wal.jsonl"))
	if err != nil {
		return s
	}
	// Count complete records only; a torn tail is not yet a record.
	data = data[:bytes.LastIndexByte(data, '\n')+1]
	s.records = bytes.Count(data, []byte("\n"))
	if s.records > 0 {
		lines := bytes.Split(bytes.TrimSuffix(data, []byte("\n")), []byte("\n"))
		last := string(lines[len(lines)-1])
		if i := strings.Index(last, `"step":"`); i >= 0 {
			rest := last[i+len(`"step":"`):]
			s.lastStep = rest[:strings.IndexByte(rest, '"')]
		}
	}
	return s
}
