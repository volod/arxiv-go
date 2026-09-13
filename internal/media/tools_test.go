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

const okVersion = "ffprobe version 9.9-fake Copyright (c) the testers"

type fakeToolResult struct {
	version string
	err     error
	probe   func(context.Context) (string, error)
}

// fakeFinder searches candidate paths but validates only entries in the in-memory map.
func fakeFinder(exeDir string, entries map[string]fakeToolResult, path ...string) Finder {
	list := strings.Join(path, string(os.PathListSeparator))
	return Finder{
		Executable: func() (string, error) { return filepath.Join(exeDir, "arxgo"), nil },
		SearchPath: &list,
		checkCandidate: func(path string) (string, error) {
			if _, ok := entries[path]; !ok {
				return "", exec.ErrNotFound
			}
			return path, nil
		},
		probeVersion: func(ctx context.Context, path string) (string, error) {
			entry := entries[path]
			if entry.probe != nil {
				return entry.probe(ctx)
			}
			return entry.version, entry.err
		},
	}
}

func toolPath(dir string, tool Tool) string {
	return filepath.Join(dir, ExecutableName(tool, runtime.GOOS))
}

func TestFindPrefersExecutableDirectoryOverPath(t *testing.T) {
	root := t.TempDir()
	exeDir, pathDir := filepath.Join(root, "bin"), filepath.Join(root, "path")
	want, other := toolPath(exeDir, FFprobe), toolPath(pathDir, FFprobe)
	f := fakeFinder(exeDir, map[string]fakeToolResult{want: {version: okVersion}, other: {version: "ffprobe version from PATH"}}, pathDir)
	got, err := f.Find(context.Background(), FFprobe)
	if err != nil || got.Path != want || got.Version != okVersion {
		t.Fatalf("Find = %+v, %v; want %s, %s", got, err, want, okVersion)
	}
}

func TestFindOnPathOnly(t *testing.T) {
	root := t.TempDir()
	first, second, third := filepath.Join(root, "p1"), filepath.Join(root, "p2"), filepath.Join(root, "p3")
	want := toolPath(second, FFprobe)
	f := fakeFinder(filepath.Join(root, "bin"), map[string]fakeToolResult{want: {version: okVersion}, toolPath(third, FFprobe): {version: okVersion}}, first, second, third)
	got, err := f.Find(context.Background(), FFprobe)
	if err != nil || got.Path != want {
		t.Fatalf("Find = %+v, %v; want %s", got, err, want)
	}
}

func TestFindResolvesSymlinkedExecutable(t *testing.T) {
	root := t.TempDir()
	realDir, linkDir := filepath.Join(root, "real"), filepath.Join(root, "link")
	for _, dir := range []string{realDir, linkDir} {
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	exe := filepath.Join(realDir, "arxgo")
	if err := os.WriteFile(exe, nil, 0o644); err != nil {
		t.Fatal(err)
	}
	link := filepath.Join(linkDir, "arxgo")
	if err := os.Symlink(exe, link); err != nil {
		// Some Windows hosts disallow symlink creation. Keep the candidate-order check
		// active there; Linux verifies resolution through the real link.
		t.Logf("symlink unavailable, checking executable directory directly: %v", err)
		link = exe
	}
	want := toolPath(realDir, FFprobe)
	f := fakeFinder(realDir, map[string]fakeToolResult{want: {version: okVersion}})
	f.Executable = func() (string, error) { return link, nil }
	got, err := f.Find(context.Background(), FFprobe)
	if err != nil || got.Path != want {
		t.Fatalf("Find = %+v, %v; want %s", got, err, want)
	}
}

func TestFindMissing(t *testing.T) {
	root := t.TempDir()
	_, err := fakeFinder(root, nil, root).Find(context.Background(), FFmpeg)
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
}

func TestFindTreatsFailedVersionAsMissing(t *testing.T) {
	cases := map[string]fakeToolResult{
		"non-zero exit": {version: okVersion, err: errors.New("exit status 1")},
		"no output":     {err: errors.New("-version printed nothing")},
		"blank output":  {err: errors.New("-version printed nothing")},
		"probe failure": {err: errors.New("could not start")},
		"timeout":       {err: context.DeadlineExceeded},
	}
	for name, result := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			var logs bytes.Buffer
			f := fakeFinder(dir, map[string]fakeToolResult{toolPath(dir, FFprobe): result})
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
	for name, failure := range map[string]error{"nonzero": errors.New("exit status 3"), "timeout": context.DeadlineExceeded} {
		t.Run(name, func(t *testing.T) {
			root := t.TempDir()
			exeDir, pathDir := filepath.Join(root, "bin"), filepath.Join(root, "path")
			want := toolPath(pathDir, FFprobe)
			f := fakeFinder(exeDir, map[string]fakeToolResult{toolPath(exeDir, FFprobe): {err: failure}, want: {version: okVersion}}, pathDir)
			got, err := f.Find(context.Background(), FFprobe)
			if err != nil || got.Path != want {
				t.Fatalf("Find = %+v, %v; want %s", got, err, want)
			}
		})
	}
}

func TestFindRejectsTimedOutProbe(t *testing.T) {
	dir := t.TempDir()
	f := fakeFinder(dir, map[string]fakeToolResult{toolPath(dir, FFprobe): {probe: func(ctx context.Context) (string, error) {
		select {
		case <-ctx.Done():
			return "", ctx.Err()
		case <-time.After(100 * time.Millisecond):
			return "", context.DeadlineExceeded
		}
	}}})
	start := time.Now()
	_, err := f.Find(context.Background(), FFprobe)
	if !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v, want ErrToolNotFound", err)
	}
	if elapsed := time.Since(start); elapsed > time.Second {
		t.Fatalf("timeout took %s", elapsed)
	}
}

func TestFindIgnoresRelativeAndDirectoryCandidates(t *testing.T) {
	root := t.TempDir()
	dirCandidate := filepath.Join(root, "dir")
	if err := os.MkdirAll(toolPath(dirCandidate, FFprobe), 0o755); err != nil {
		t.Fatal(err)
	}
	f := fakeFinder(filepath.Join(root, "none"), nil, dirCandidate, "rel", "", ".")
	// This test uses the real candidate check, while the probe must never run.
	f.checkCandidate = nil
	f.probeVersion = func(context.Context, string) (string, error) {
		t.Fatal("directory candidate was probed")
		return "", nil
	}
	if _, err := f.Find(context.Background(), FFprobe); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v", err)
	}
	want := []string{toolPath(filepath.Join(root, "none"), FFprobe), toolPath(dirCandidate, FFprobe)}
	if got := f.Candidates(FFprobe); !reflect.DeepEqual(got, want) {
		t.Errorf("candidates = %q, want %q", got, want)
	}
}

func TestFindRejectsNonExecutableFile(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("Windows does not use Unix executable bits")
	}
	dir := t.TempDir()
	if err := os.WriteFile(toolPath(dir, FFprobe), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	f := fakeFinder(dir, nil)
	f.checkCandidate = nil
	f.probeVersion = func(context.Context, string) (string, error) {
		t.Fatal("non-executable file was probed")
		return "", nil
	}
	if _, err := f.Find(context.Background(), FFprobe); !errors.Is(err, ErrToolNotFound) {
		t.Fatalf("err = %v", err)
	}
}

func TestCandidatesDeduplicateAndUseWindowsNames(t *testing.T) {
	root := t.TempDir()
	f := fakeFinder(root, nil, root+string(filepath.Separator), filepath.Join(root, "x"))
	f.GOOS = "windows"
	want := []string{filepath.Join(root, "ffmpeg.exe"), filepath.Join(root, "x", "ffmpeg.exe")}
	if got := f.Candidates(FFmpeg); !reflect.DeepEqual(got, want) {
		t.Fatalf("candidates = %q, want %q", got, want)
	}
	f.Executable = func() (string, error) { return "", errors.New("unknown") }
	if got := f.Candidates(FFmpeg); !reflect.DeepEqual(got, want) {
		t.Fatalf("without executable: candidates = %q, want %q", got, want)
	}
}

func TestFindStopsWhenContextEnds(t *testing.T) {
	dir := t.TempDir()
	f := fakeFinder(dir, map[string]fakeToolResult{toolPath(dir, FFprobe): {probe: func(ctx context.Context) (string, error) {
		<-ctx.Done()
		return "", ctx.Err()
	}}})
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	if _, err := f.Find(ctx, FFprobe); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
}

func TestDiscoverReportsMissingRequirementsInOrder(t *testing.T) {
	dir := t.TempDir()
	want := toolPath(dir, FFmpeg)
	reqs := Requirements(Needs{MetadataMedia: true, Image: "start"})
	found, missing, err := fakeFinder(dir, map[string]fakeToolResult{want: {version: "ffmpeg version 9.9-fake"}}).Discover(context.Background(), reqs)
	if err != nil {
		t.Fatal(err)
	}
	if found.Path(FFmpeg) != want || found.Path(FFprobe) != "" {
		t.Errorf("found = %+v", found)
	}
	if len(missing) != 1 || missing[0].Tool != FFprobe {
		t.Errorf("missing = %+v", missing)
	}
}

func TestFindRealFFprobe(t *testing.T) {
	if _, err := exec.LookPath("ffprobe"); err != nil {
		t.Skip("ffprobe not on PATH; live discovery runs only where it is installed")
	}
	got, err := (Finder{}).Find(context.Background(), FFprobe)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.HasPrefix(got.Version, "ffprobe version ") || !filepath.IsAbs(got.Path) {
		t.Fatalf("found %+v", got)
	}
	t.Logf("found %s: %s", got.Path, got.Version)
}
