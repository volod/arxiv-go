package scanner

import (
	"bytes"
	"log/slog"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
)

func skipUnlessPermissionsEnforced(t *testing.T) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("directory permission bits are enforced by this test only on Linux")
	}
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks")
	}
}

func lockDir(t *testing.T, dir string) {
	t.Helper()
	if err := os.Chmod(dir, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
}

func TestWalkCountsUnreadableDirectoryAndContinues(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	root := t.TempDir()
	makeTree(t, root, "a/1", "b/hidden/x", "b/hidden/y", "c/2")
	lockDir(t, filepath.Join(root, "b", "hidden"))
	var logs bytes.Buffer
	got := walkAll(t, root, Options{Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if want := []string{"a", "a/1", "b", "b/hidden", "c", "c/2"}; !slices.Equal(rels(got), want) {
		t.Fatalf("got %q, want %q", rels(got), want)
	}
	var stats Stats
	for _, e := range got {
		stats.Count(e)
	}
	locked := got[3]
	if locked.Kind != KindDir || locked.Err == nil || locked.SkipReason() != ReasonUnreadable {
		t.Errorf("locked entry = %+v, want unreadable directory", locked)
	}
	if stats.Dirs != 4 || stats.Files != 2 || stats.Skipped[ReasonUnreadable] != 1 || stats.SkippedTotal() != 1 {
		t.Errorf("stats = %+v", stats)
	}
	if out := logs.String(); strings.Count(out, "skipped entry") != 1 || !strings.Contains(out, "path=b/hidden") ||
		!strings.Contains(out, "reason=unreadable") || !strings.Contains(out, "permission denied") {
		t.Errorf("log = %s", out)
	}

	// Resuming with the cursor on the unreadable directory reports nothing twice.
	if got := rels(walkAll(t, root, Options{Cursor: KeyOf("b/hidden")})); !slices.Equal(got, []string{"c", "c/2"}) {
		t.Errorf("resume at unreadable dir: got %q", got)
	}
}

func TestWalkUnreadableDirectoryOnCursorPathIsStillReported(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	root := t.TempDir()
	makeTree(t, root, "b/hidden/x", "b/hidden/y", "c")
	lockDir(t, filepath.Join(root, "b", "hidden"))
	got := walkAll(t, root, Options{Cursor: KeyOf("b/hidden/x")})
	if len(got) != 2 || got[0].Rel != "b/hidden" || got[0].SkipReason() != ReasonUnreadable || got[1].Rel != "c" {
		t.Fatalf("got %+v", got)
	}
}

func TestWalkListableButUnsearchableDirectory(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	root := t.TempDir()
	makeTree(t, root, "d/f1", "d/f2")
	dir := filepath.Join(root, "d")
	if err := os.Chmod(dir, 0o644); err != nil { // read without search: names but no lstat
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o755) })
	got := walkAll(t, root, Options{})
	var stats Stats
	for _, e := range got {
		stats.Count(e)
	}
	if stats.Files != 2 || stats.Skipped[ReasonUnreadable] != 2 {
		t.Fatalf("entries %q, stats %+v; want two unreadable files", rels(got), stats)
	}
}

func TestWalkSkipsSpecialEntries(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("socket fixture used on Linux only")
	}
	root := t.TempDir()
	makeTree(t, root, "a.txt", "z.txt")
	sock := filepath.Join(root, "m.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Skipf("unix sockets unavailable: %v", err)
	}
	defer l.Close()
	var logs bytes.Buffer
	got := walkAll(t, root, Options{Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if want := []string{"a.txt", "m.sock", "z.txt"}; !slices.Equal(rels(got), want) {
		t.Fatalf("got %q, want %q", rels(got), want)
	}
	if got[1].Kind != KindSpecial || got[1].Info != nil || got[1].SkipReason() != ReasonSpecial {
		t.Errorf("socket entry = %+v", got[1])
	}
	if !strings.Contains(logs.String(), "path=m.sock") || !strings.Contains(logs.String(), "reason=special") {
		t.Errorf("log = %s", logs.String())
	}
}

func TestReservedPartSuffixMatchesFsops(t *testing.T) {
	if PartSuffix != fsops.PartSuffix {
		t.Fatalf("PartSuffix %q != fsops.PartSuffix %q", PartSuffix, fsops.PartSuffix)
	}
}
