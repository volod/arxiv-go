package cli

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/state"
)

// withIndexLockIdentity makes catia-index judge locks as process pid on host cli-test, with the
// listed pids alive.
func withIndexLockIdentity(t *testing.T, pid int, alive ...int) {
	t.Helper()
	saved := lockHooks
	t.Cleanup(func() { lockHooks = saved })
	lockHooks = func(o *state.LockOptions) {
		*o = state.LockOptions{Host: "cli-test", PID: pid, Alive: func(p int) bool {
			for _, a := range alive {
				if a == p {
					return true
				}
			}
			return false
		}}
	}
}

func TestCatiaIndexCommandAfterTextSplit(t *testing.T) {
	arc, _, cat, _ := catiaCLIFixture(t)
	withLockIdentity(t, 500)
	withIndexLockIdentity(t, 501)
	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	if code := run(context.Background(), []string{"split", "--catia", "--catia-text", "--archive", arc,
		"--catia-archive", cat, "--min-free", "0"}, e); code != ExitOK {
		t.Fatalf("split exit %d: %s", code, errOut.String())
	}
	if code := run(context.Background(), []string{"catia-index", "--archive", arc}, e); code != ExitOK {
		t.Fatalf("catia-index exit %d: %s", code, errOut.String())
	}
	doc, err := os.ReadFile(filepath.Join(arc, "arxgo-catia-text.md"))
	if err != nil || !bytes.Contains(doc, []byte("\nfiles: 1\n")) || !bytes.Contains(doc, []byte("\n## cad/fixture-part.CATPart\n\n")) ||
		!bytes.Contains(doc, []byte("\ntext: cad/fixture-part.CATPart.text.md\n")) {
		t.Fatalf("index: %s %v", doc, err)
	}
	custom := filepath.Join(t.TempDir(), "with-strings.md")
	e = testEnv(&out, &errOut, mapLookup(map[string]string{"ARXGO_OUT": custom, "ARXGO_STRINGS": "true"}))
	if code := run(context.Background(), []string{"catia-index", "--archive", arc}, e); code != ExitOK {
		t.Fatalf("catia-index from environment exit %d: %s", code, errOut.String())
	}
	withStrings, err := os.ReadFile(custom)
	if err != nil || !bytes.Contains(withStrings, []byte("\nstrings:\n- V5_CFV2\n- invented part body\n")) {
		t.Fatalf("index with strings: %s %v", withStrings, err)
	}
	if !strings.Contains(errOut.String(), "wrote CATIA text index") {
		t.Fatalf("log: %s", errOut.String())
	}

	lock, err := state.AcquireLock(arc, state.LockInfo{RunID: "20260916T100000Z-00000001", Op: "split", Role: state.RoleArchive},
		state.LockOptions{Host: "cli-test", PID: 700})
	if err != nil {
		t.Fatal(err)
	}
	defer lock.Release()
	withIndexLockIdentity(t, 501, 700)
	errOut.Reset()
	if code := run(context.Background(), []string{"catia-index", "--archive", arc, "--out", custom}, testEnv(&out, &errOut, noProcessEnv)); code != ExitLocked {
		t.Fatalf("catia-index under a live lock exit %d: %s", code, errOut.String())
	}
}

func TestCatiaIndexUsageErrors(t *testing.T) {
	arc, video := fixture(t)
	outside := t.TempDir()
	description := filepath.Join(arc, "part.CATPart.md")
	foreign := filepath.Join(outside, "notes.md")
	for p, body := range map[string]string{description: "arxgo: part.CATPart\n", foreign: "operator notes\n"} {
		if err := os.WriteFile(p, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		name string
		args []string
		want string
	}{
		{"missing archive", []string{"catia-index"}, "--archive is required"},
		{"archive not a directory", []string{"catia-index", "--archive", description}, "is not a directory"},
		{"out directory", []string{"catia-index", "--archive", arc, "--out", outside}, "is a directory"},
		{"out parent missing", []string{"catia-index", "--archive", arc, "--out", filepath.Join(outside, "no", "x.md")}, "parent directory does not exist"},
		{"out registry", []string{"catia-index", "--archive", arc, "--out", filepath.Join(arc, "arxgo-catia.csv")}, "reserved arxgo path"},
		{"out state", []string{"catia-index", "--archive", arc, "--out", filepath.Join(arc, ".arxgo", "x.md")}, "reserved arxgo path"},
		{"out part file", []string{"catia-index", "--archive", arc, "--out", filepath.Join(outside, "x.md.arxgo-part")}, "reserved arxgo path"},
		{"out description", []string{"catia-index", "--archive", arc, "--out", description}, "arxgo description or text sidecar"},
		{"run flag", []string{"catia-index", "--archive", arc, "--dry-run"}, "flag provided but not defined: --dry-run"},
		{"mirror flag", []string{"catia-index", "--archive", arc, "--catia-archive", video}, "flag provided but not defined: --catia-archive"},
		{"positional", []string{"catia-index", "--archive", arc, "extra"}, `unexpected argument "extra"`},
		{"index flag on split", []string{"split", "--archive", arc, "--video-archive", video, "--strings"}, "flag provided but not defined: --strings"},
	}
	for _, tc := range cases {
		var out, errOut bytes.Buffer
		code := run(context.Background(), tc.args, testEnv(&out, &errOut, noProcessEnv))
		if code != ExitUsage || !strings.Contains(errOut.String(), tc.want) {
			t.Errorf("%s: exit %d, stderr %q, want %q", tc.name, code, errOut.String(), tc.want)
		}
	}
	if _, err := os.Stat(state.StateDir(arc)); !os.IsNotExist(err) {
		t.Fatalf("usage errors created state: %v", err)
	}
	var out, errOut bytes.Buffer
	withIndexLockIdentity(t, 501)
	if code := run(context.Background(), []string{"catia-index", "--archive", arc, "--out", foreign}, testEnv(&out, &errOut, noProcessEnv)); code != ExitOK {
		t.Fatalf("explicit --out over an operator file: exit %d %s", code, errOut.String())
	}
}

func TestCatiaIndexHelp(t *testing.T) {
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"help", "catia-index"}, testEnv(&out, &errOut, noProcessEnv)); code != ExitOK {
		t.Fatalf("exit %d: %s", code, errOut.String())
	}
	help := out.String()
	for _, want := range []string{"Usage: arxgo catia-index --archive PATH", "--out PATH", "env ARXGO_OUT", "--strings", "--log-level"} {
		if !strings.Contains(help, want) {
			t.Errorf("help lacks %q:\n%s", want, help)
		}
	}
	for _, unwanted := range []string{"--dry-run", "--min-free", "--catia-archive", "--checkpoint-every"} {
		if strings.Contains(help, unwanted) {
			t.Errorf("help lists %s:\n%s", unwanted, help)
		}
	}
	out.Reset()
	run(context.Background(), []string{"help"}, testEnv(&out, &errOut, noProcessEnv))
	if !strings.Contains(out.String(), "arxgo catia-index --archive PATH") {
		t.Fatalf("general help:\n%s", out.String())
	}
}

func TestCatiaIndexOutReservedCaseFolded(t *testing.T) {
	arc, _ := fixture(t)
	fsys := osRootFS()
	fsys.foldCase = true
	for _, out := range []string{filepath.Join(arc, "ARXGO-CATIA.CSV"), filepath.Join(arc, ".ArxGo", "x.md")} {
		v := &validator{}
		checkIndexOut(fsys, arc, out, v)
		if err := v.err(); err == nil || !strings.Contains(err.Error(), "reserved arxgo path") {
			t.Errorf("%s: %v", out, err)
		}
	}
	v := &validator{}
	checkIndexOut(fsys, arc, filepath.Join(arc, "ARXGO-CATIA-TEXT.MD"), v)
	if err := v.err(); err != nil {
		t.Errorf("case variant of the default output: %v", err)
	}
}
