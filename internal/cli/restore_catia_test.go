package cli

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/internal/state"
)

func runCLI(t *testing.T, want int, args ...string) string {
	t.Helper()
	var out, errOut bytes.Buffer
	if code := run(context.Background(), args, testEnv(&out, &errOut, noProcessEnv)); code != want {
		t.Fatalf("%v: exit %d, want %d: %s", args, code, want, errOut.String())
	}
	return errOut.String()
}

func TestRestoreCatiaCommandRoundTrip(t *testing.T) {
	arc, video, cat, part := catiaCLIFixture(t)
	withLockIdentity(t, 500)
	src := filepath.Join(arc, "cad", "fixture-part.CATPart")
	before, err := os.Stat(src)
	if err != nil {
		t.Fatal(err)
	}
	runCLI(t, ExitOK, "split", "--catia", "--catia-text", "--archive", arc, "--catia-archive", cat, "--min-free", "0")
	for _, p := range []string{src + ".md", src + ".text.md"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("split output %s: %v", p, err)
		}
	}
	runCLI(t, ExitOK, "restore", "--catia", "--archive", arc, "--catia-archive", cat, "--min-free", "0", "--verify", "hash")
	got, err := os.Stat(src)
	if err != nil || got.Size() != before.Size() || !got.ModTime().Equal(before.ModTime()) {
		t.Fatalf("restored file %+v %v", got, err)
	}
	if data, _ := os.ReadFile(src); !bytes.Equal(data, part) {
		t.Fatal("restored bytes differ")
	}
	for _, p := range []string{src + ".md", src + ".text.md", filepath.Join(arc, "arxgo-catia.csv"), filepath.Join(cat, "cad")} {
		if _, err := os.Stat(p); !os.IsNotExist(err) {
			t.Fatalf("%s left after restore: %v", p, err)
		}
	}
	matches, _ := filepath.Glob(filepath.Join(arc, "arxgo-catia.restored-*.csv"))
	if len(matches) != 1 {
		t.Fatalf("retired registry: %v", matches)
	}
	if entries, _ := os.ReadDir(video); len(entries) != 0 {
		t.Fatalf("video archive touched: %v", entries)
	}
	id, err := state.ReadCurrent(arc)
	if err != nil {
		t.Fatal(err)
	}
	var o state.RunOptions
	if err := state.ReadJSON(filepath.Join(state.StateDir(arc), "runs", id, state.OptionsFile), &o); err != nil {
		t.Fatal(err)
	}
	if o.Op != OpRestore || o.Payload != PayloadCatia || o.CatiaArchive != cat || o.VideoArchive != "" {
		t.Fatalf("restore options.json: %+v", o)
	}
	runCLI(t, ExitOK, "restore", "--catia", "--archive", arc, "--catia-archive", cat, "--min-free", "0")
}

func TestRestoreCatiaKeepDescriptionsCommand(t *testing.T) {
	arc, _, cat, _ := catiaCLIFixture(t)
	withLockIdentity(t, 500)
	src := filepath.Join(arc, "cad", "fixture-part.CATPart")
	runCLI(t, ExitOK, "split", "--catia", "--catia-text", "--archive", arc, "--catia-archive", cat, "--min-free", "0")
	runCLI(t, ExitOK, "restore", "--catia", "--descriptions", "keep", "--archive", arc, "--catia-archive", cat, "--min-free", "0")
	for _, p := range []string{src, src + ".md", src + ".text.md"} {
		if _, err := os.Stat(p); err != nil {
			t.Fatalf("%s: %v", p, err)
		}
	}
	csv, err := os.ReadFile(filepath.Join(arc, "arxgo-catia.csv"))
	if err != nil || !bytes.Contains(csv, []byte(",restored,")) {
		t.Fatalf("registry after keep: %s %v", csv, err)
	}
}

// A CATIA restore interrupted after placed is recovered by a video restore command; the next CATIA
// restore deletes the text sidecar the interrupted run, recorded with sidecar_cleanup: true, left.
func TestVideoRestoreCommandRecoversInterruptedCatiaRestore(t *testing.T) {
	arc, video, cat, part := catiaCLIFixture(t)
	withLockIdentity(t, 500)
	src := filepath.Join(arc, "cad", "fixture-part.CATPart")
	runCLI(t, ExitOK, "split", "--catia", "--catia-text", "--archive", arc, "--catia-archive", cat, "--min-free", "0")

	identity := sessionHooks
	sessionHooks = func(cfg *archive.Config) {
		identity(cfg)
		cfg.Crash = func(point string) error {
			if point == "wal:placed" {
				return errors.New("injected crash")
			}
			return nil
		}
	}
	runCLI(t, ExitFailure, "restore", "--catia", "--archive", arc, "--catia-archive", cat, "--min-free", "0")
	sessionHooks = identity
	if data, err := os.ReadFile(src); err != nil || !bytes.Equal(data, part) {
		t.Fatalf("file not placed before the crash: %v", err)
	}

	logs := runCLI(t, ExitOK, "restore", "--archive", arc, "--video-archive", video, "--min-free", "0")
	if !strings.Contains(logs, "recovering the interrupted run") {
		t.Fatalf("video restore did not recover the CATIA run:\n%s", logs)
	}
	if _, err := os.Stat(src + ".md"); !os.IsNotExist(err) {
		t.Fatalf("owned CATIA description after recovery: %v", err)
	}
	if _, err := os.Stat(src + ".text.md"); err != nil {
		t.Fatalf("video restore touched the CATIA text sidecar: %v", err)
	}
	if _, err := os.Stat(state.LockPath(cat)); !os.IsNotExist(err) {
		t.Fatalf("CATIA archive lock left: %v", err)
	}

	runCLI(t, ExitOK, "restore", "--catia", "--archive", arc, "--catia-archive", cat, "--min-free", "0")
	if _, err := os.Stat(src + ".text.md"); !os.IsNotExist(err) {
		t.Fatalf("text sidecar of the recovered restore left: %v", err)
	}
}

// Restore records its sidecar cleanup intent in options.json from the payload's policy flag.
func TestRestoreRecordsSidecarCleanupPerPayload(t *testing.T) {
	arc, video, cat, _ := catiaCLIFixture(t)
	if err := os.Mkdir(cat, 0o755); err != nil {
		t.Fatal(err)
	}
	withLockIdentity(t, 500)
	cases := []struct {
		args []string
		want bool
	}{
		{[]string{"--video-archive", video}, false},
		{[]string{"--video-archive", video, "--previews", "delete"}, true},
		{[]string{"--video-archive", video, "--previews", "delete", "--descriptions", "keep"}, true},
		{[]string{"--catia", "--catia-archive", cat}, true},
		{[]string{"--catia", "--catia-archive", cat, "--descriptions", "keep"}, false},
	}
	for _, tc := range cases {
		runCLI(t, ExitOK, append([]string{"restore", "--archive", arc, "--min-free", "0"}, tc.args...)...)
		id, err := state.ReadCurrent(arc)
		if err != nil {
			t.Fatal(err)
		}
		var o state.RunOptions
		if err := state.ReadJSON(filepath.Join(state.StateDir(arc), "runs", id, state.OptionsFile), &o); err != nil {
			t.Fatal(err)
		}
		if o.SidecarCleanup == nil || *o.SidecarCleanup != tc.want {
			t.Errorf("%v: sidecar_cleanup = %v, want %v", tc.args, o.SidecarCleanup, tc.want)
		}
	}
}
