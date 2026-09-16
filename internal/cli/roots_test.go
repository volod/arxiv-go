package cli

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func validateRoots(t *testing.T, op, archive, video string, fsys rootFS) (Common, bool, error) {
	t.Helper()
	s := &settings{archive: archive, videoArchive: video, checkpointEvery: 1, progressInterval: 1, checkpointInterval: 1}
	v := &validator{}
	c, missing := buildCommon(op, s, fsys, v)
	return c, missing, v.err()
}

func TestRootValidation(t *testing.T) {
	dir := t.TempDir()
	mk := func(parts ...string) string {
		p := filepath.Join(append([]string{dir}, parts...)...)
		if err := os.MkdirAll(p, 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	archive := mk("archive")
	video := mk("video")
	nested := mk("archive", "sub", "video")
	prefixSibling := mk("archive2")
	file := filepath.Join(dir, "file.txt")
	if err := os.WriteFile(file, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	missing := filepath.Join(dir, "missing")

	cases := []struct {
		name, op, archive, video string
		want                     string // empty means valid
		wantMissing              bool
	}{
		{"scan needs archive", OpScan, "", "", "--archive is required", false},
		{"scan ignores video archive", OpScan, archive, missing, "", false},
		{"split needs video archive", OpSplit, archive, "", "--video-archive is required", false},
		{"restore needs both", OpRestore, "", "", "--archive is required", false},
		{"archive missing", OpScan, missing, "", "does not exist", false},
		{"archive is a file", OpScan, file, "", "is not a directory", false},
		{"restore video archive missing", OpRestore, archive, missing, "does not exist", false},
		{"split video archive created later", OpSplit, archive, missing, "", true},
		{"split video archive parent missing", OpSplit, archive, filepath.Join(missing, "v"), "neither does its parent", false},
		{"video archive is a file", OpSplit, archive, file, "is not a directory", false},
		{"siblings", OpSplit, archive, video, "", false},
		{"name prefix is not nesting", OpRestore, archive, prefixSibling, "", false},
		{"equal roots", OpSplit, archive, archive, "resolve to the same directory", false},
		{"equal after cleaning", OpRestore, archive, archive + string(filepath.Separator) + "." + string(filepath.Separator), "same directory", false},
		{"video inside archive", OpSplit, archive, nested, "is inside --archive", false},
		{"missing video root inside archive", OpSplit, archive, filepath.Join(archive, "new"), "is inside --archive", true},
		{"archive inside video", OpRestore, nested, archive, "is inside --video-archive", false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, gotMissing, err := validateRoots(t, tc.op, tc.archive, tc.video, osRootFS())
			if tc.want == "" {
				if err != nil {
					t.Fatalf("unexpected error: %v", err)
				}
			} else if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
			if gotMissing != tc.wantMissing {
				t.Errorf("video archive missing = %v, want %v", gotMissing, tc.wantMissing)
			}
			if tc.want == "" && (!filepath.IsAbs(c.Archive) || (tc.op != OpScan && !filepath.IsAbs(c.VideoArchive))) {
				t.Errorf("roots not absolute: %+v", c)
			}
			if tc.op == OpScan && c.VideoArchive != "" {
				t.Errorf("scan kept video archive %q", c.VideoArchive)
			}
		})
	}
	if _, err := os.Stat(missing); !os.IsNotExist(err) {
		t.Errorf("validation created %s", missing)
	}
}

func TestRootNestingThroughSymlinks(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "archive")
	if err := os.MkdirAll(filepath.Join(archive, "inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	linkToInner := filepath.Join(dir, "link-inner")
	linkToArchive := filepath.Join(dir, "link-archive")
	if err := os.Symlink(filepath.Join(archive, "inner"), linkToInner); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.Symlink(archive, linkToArchive); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	cases := []struct{ name, archive, video, want string }{
		{"video link points inside archive", archive, linkToInner, "is inside --archive"},
		{"archive link equals video", linkToArchive, archive, "same directory"},
		{"missing root below a linked parent", archive, filepath.Join(linkToInner, "new"), "is inside --archive"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			_, _, err := validateRoots(t, OpSplit, tc.archive, tc.video, osRootFS())
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestRootCaseFolding(t *testing.T) {
	dir := t.TempDir()
	archive := filepath.Join(dir, "Archive")
	if err := os.MkdirAll(filepath.Join(archive, "Video"), 0o755); err != nil {
		t.Fatal(err)
	}
	// Simulate a case-insensitive filesystem: every spelling resolves to itself and exists.
	folding := osRootFS()
	folding.foldCase = true
	folding.stat = func(p string) (os.FileInfo, error) { return os.Stat(dir) }
	folding.evalSymlinks = func(p string) (string, error) { return p, nil }

	upper := strings.ToUpper(archive)
	if _, _, err := validateRoots(t, OpRestore, archive, upper, folding); err == nil ||
		!strings.Contains(err.Error(), "same directory") {
		t.Errorf("case-variant equal roots: error = %v", err)
	}
	if _, _, err := validateRoots(t, OpRestore, archive, filepath.Join(upper, "VIDEO"), folding); err == nil ||
		!strings.Contains(err.Error(), "is inside --archive") {
		t.Errorf("case-variant nested roots: error = %v", err)
	}
	folding.foldCase = false
	if _, _, err := validateRoots(t, OpRestore, archive, upper, folding); err != nil {
		t.Errorf("case-sensitive comparison rejected distinct roots: %v", err)
	}
}

// TestRootCaseVariantOnWindows uses the real filesystem; it runs in the Windows CI job.
func TestRootCaseVariantOnWindows(t *testing.T) {
	if runtime.GOOS != "windows" {
		t.Skip("case-insensitive path comparison applies on Windows only")
	}
	dir := t.TempDir()
	archive := filepath.Join(dir, "Archive")
	if err := os.MkdirAll(filepath.Join(archive, "Inner"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, _, err := validateRoots(t, OpSplit, archive, strings.ToUpper(archive), osRootFS()); err == nil ||
		!strings.Contains(err.Error(), "same directory") {
		t.Errorf("equal roots: error = %v", err)
	}
	if _, _, err := validateRoots(t, OpSplit, strings.ToLower(archive), filepath.Join(archive, "INNER"), osRootFS()); err == nil ||
		!strings.Contains(err.Error(), "is inside --archive") {
		t.Errorf("nested roots: error = %v", err)
	}
}

func TestWithin(t *testing.T) {
	sep := string(filepath.Separator)
	root := sep
	if runtime.GOOS == "windows" {
		root = `C:\`
	}
	a := filepath.Join(root, "a")
	cases := []struct {
		parent, child string
		want          bool
	}{
		{a, filepath.Join(a, "b"), true},
		{a, filepath.Join(a, "b", "c"), true},
		{a, a, false},
		{a, a + "b", false},
		{filepath.Join(a, "b"), a, false},
		{root, a, true},
	}
	for _, tc := range cases {
		if got := within(tc.parent, tc.child); got != tc.want {
			t.Errorf("within(%q, %q) = %v, want %v", tc.parent, tc.child, got, tc.want)
		}
	}
}
