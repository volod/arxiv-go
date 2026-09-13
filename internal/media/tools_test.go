package media

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"
)

// fakeTool writes an executable shell script named tool into dir that runs body. Fake tools are
// POSIX scripts; the Windows .bat variant is step W6 of the Windows verification scenario.
func fakeTool(t *testing.T, dir string, tool Tool, body string) string {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("fake tools are shell scripts; Windows discovery is checked in scenario step W6")
	}
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, ExecutableName(tool, runtime.GOOS))
	if err := os.WriteFile(p, []byte("#!/bin/sh\n"+body+"\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

const okVersion = `echo "ffprobe version 9.9-fake Copyright (c) the testers"; echo "built with fake"`

// testFinder searches the executable directory exeDir, then the directories in path.
func testFinder(exeDir string, path ...string) Finder {
	list := strings.Join(path, string(os.PathListSeparator))
	return Finder{
		Executable: func() (string, error) { return filepath.Join(exeDir, "arxgo"), nil },
		SearchPath: &list,
		Timeout:    5 * time.Second,
	}
}

func TestFindPrefersExecutableDirectoryOverPath(t *testing.T) {
	root := t.TempDir()
	exeDir, pathDir := filepath.Join(root, "bin"), filepath.Join(root, "path")
	want := fakeTool(t, exeDir, FFprobe, okVersion)
	fakeTool(t, pathDir, FFprobe, `echo "ffprobe version from PATH"`)

	got, err := testFinder(exeDir, pathDir).Find(context.Background(), FFprobe)
	if err != nil {
		t.Fatal(err)
	}
	if got.Path != want || got.Version != "ffprobe version 9.9-fake Copyright (c) the testers" {
		t.Fatalf("found %+v, want %s with the first version line", got, want)
	}
}

func TestFindOnPathOnly(t *testing.T) {
	root := t.TempDir()
	exeDir := filepath.Join(root, "bin")
	if err := os.Mkdir(exeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	first, second := filepath.Join(root, "p1"), filepath.Join(root, "p2")
	if err := os.Mkdir(first, 0o755); err != nil {
		t.Fatal(err)
	}
	want := fakeTool(t, second, FFprobe, okVersion)
	fakeTool(t, filepath.Join(root, "p3"), FFprobe, okVersion)

	got, err := testFinder(exeDir, first, second, filepath.Join(root, "p3")).Find(context.Background(), FFprobe)
	if err != nil || got.Path != want {
		t.Fatalf("Find = %+v, %v; want %s", got, err, want)
	}
}

func TestFindResolvesSymlinkedExecutable(t *testing.T) {
	root := t.TempDir()
	realDir, linkDir := filepath.Join(root, "real"), filepath.Join(root, "link")
	want := fakeTool(t, realDir, FFprobe, okVersion)
	if err := os.Mkdir(linkDir, 0o755); err != nil {
		t.Fatal(err)
	}
	exe := filepath.Join(realDir, "arxgo")
	if err := os.WriteFile(exe, nil, 0o755); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "arxgo")
	if err := os.Symlink(exe, link); err != nil {
		t.Skipf("symlink: %v", err)
	}
	empty := ""
	f := Finder{Executable: func() (string, error) { return link, nil }, SearchPath: &empty}
	got, err := f.Find(context.Background(), FFprobe)
	if err != nil || got.Path != want {
		t.Fatalf("Find = %+v, %v; want the tool next to the symlink target %s", got, err, want)
	}
}

func TestFindMissing(t *testing.T) {
	root := t.TempDir()
	_, err := testFinder(root, root).Find(context.Background(), FFmpeg)
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}

func TestFindTreatsFailedVersionAsMissing(t *testing.T) {
	cases := map[string]string{
		"non-zero exit":  `echo "ffprobe version 9.9"; exit 1`,
		"killed":         `kill -9 $$`,
		"no output":      `exit 0`,
		"blank output":   `echo; echo "second line"`,
		"not a program":  `exec /nonexistent/arxgo-test-binary`,
		"args are wrong": `[ "$1" = "-version" ] || { echo ok; exit 0; }; exit 2`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			fakeTool(t, dir, FFprobe, body)
			var logs bytes.Buffer
			f := testFinder(dir)
			f.Log = slog.New(slog.NewTextHandler(&logs, nil))
			_, err := f.Find(context.Background(), FFprobe)
			if !errors.Is(err, ErrToolNotFound) {
				t.Fatalf("err = %v, want ErrToolNotFound", err)
			}
			if !strings.Contains(logs.String(), "tool candidate rejected") {
				t.Errorf("rejection not logged: %s", logs.String())
			}
		})
	}
}

func TestFindFallsBackToPathWhenExecutableDirectoryToolFails(t *testing.T) {
	root := t.TempDir()
	exeDir, pathDir := filepath.Join(root, "bin"), filepath.Join(root, "path")
	fakeTool(t, exeDir, FFprobe, `exit 3`)
	want := fakeTool(t, pathDir, FFprobe, okVersion)
	got, err := testFinder(exeDir, pathDir).Find(context.Background(), FFprobe)
	if err != nil || got.Path != want {
		t.Fatalf("Find = %+v, %v; want %s", got, err, want)
	}
}

func TestFindVersionTimeoutKillsTool(t *testing.T) {
	dir := t.TempDir()
	// The background sleep keeps stdout open after the shell is killed; WaitDelay must still end it.
	fakeTool(t, dir, FFprobe, `sleep 5 & sleep 5`)
	f := testFinder(dir)
	f.Timeout = 200 * time.Millisecond
	start := time.Now()
	_, err := f.Find(context.Background(), FFprobe)
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestFindIgnoresNonExecutableRelativeAndDirectoryCandidates(t *testing.T) {
	root := t.TempDir()
	plain := filepath.Join(root, "plain")
	if err := os.Mkdir(plain, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(plain, "ffprobe"), []byte("#!/bin/sh\necho ffprobe version\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	dirCandidate := filepath.Join(root, "dir")
	if err := os.MkdirAll(filepath.Join(dirCandidate, "ffprobe"), 0o755); err != nil {
		t.Fatal(err)
	}
	rel := filepath.Join(root, "rel")
	fakeTool(t, rel, FFprobe, okVersion)
	t.Chdir(root)

	f := testFinder(filepath.Join(root, "none"), plain, dirCandidate, "rel", "", ".")
	if _, err := f.Find(context.Background(), FFprobe); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	cands := f.Candidates(FFprobe)
	want := []string{filepath.Join(root, "none", "ffprobe"), filepath.Join(plain, "ffprobe"), filepath.Join(dirCandidate, "ffprobe")}
	if !reflect.DeepEqual(cands, want) {
		t.Errorf("candidates = %q, want %q", cands, want)
	}
}

func TestCandidatesDeduplicateAndUseWindowsNames(t *testing.T) {
	root := t.TempDir()
	f := testFinder(root, root+string(filepath.Separator), filepath.Join(root, "x"))
	f.GOOS = "windows"
	want := []string{filepath.Join(root, "ffmpeg.exe"), filepath.Join(root, "x", "ffmpeg.exe")}
	if got := f.Candidates(FFmpeg); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
	f.Executable = func() (string, error) { return "", errors.New("unknown") }
	if got := f.Candidates(FFmpeg); !reflect.DeepEqual(got, want) {
		t.Fatalf("without an executable path: candidates = %q, want %q", got, want)
	}
}

func TestFindStopsWhenContextEnds(t *testing.T) {
	dir := t.TempDir()
	fakeTool(t, dir, FFprobe, `exec sleep 5`)
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := testFinder(dir).Find(ctx, FFprobe); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v, want the context error", err)
	}
}

func TestDiscoverReportsMissingRequirementsInOrder(t *testing.T) {
	dir := t.TempDir()
	want := fakeTool(t, dir, FFmpeg, `echo "ffmpeg version 9.9-fake"`)
	reqs := Requirements(Needs{MetadataMedia: true, Image: "start"})
	found, missing, err := testFinder(dir).Discover(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if found.Path(FFmpeg) != want || found.Path(FFprobe) != "" {
		t.Errorf("found = %+v", found)
	}
	if len(missing) != 1 || missing[0].Tool != FFprobe {
		t.Errorf("missing = %+v, want ffprobe only", missing)
	}
}

func TestFindRealFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH; live discovery runs only where it is installed")
	}
	got, err := Finder{}.Find(context.Background(), FFprobe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Version, "ffprobe version ") || !filepath.IsAbs(got.Path) {
		t.Fatalf("found %+v", got)
	}
	t.Logf("found %s: %s", got.Path, got.Version)
}
