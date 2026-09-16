// Package tooltest finds the external tools that live media tests run. Production code never
// imports it.
package tooltest

import (
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"testing"
)

// RequireEnv, set to 1, turns a missing tool into a test failure instead of a skip, so a local
// run meant to exercise ffmpeg and ffprobe cannot pass by skipping every live test.
const RequireEnv = "ARXGO_TEST_REQUIRE_TOOLS"

// LookPath returns the absolute path of name, preferring the pinned build that `make ffmpeg`
// writes to the repository's bin directory, as tool discovery prefers the executable directory.
// When the tool is missing the test is skipped with the reason, or fails when RequireEnv is 1.
func LookPath(t testing.TB, name string) string {
	t.Helper()
	p, err := Path(name)
	if err == nil {
		return p
	}
	if os.Getenv(RequireEnv) == "1" {
		t.Fatalf("%s is required by %s=1 but not found: %v", name, RequireEnv, err)
	}
	t.Skipf("%s unavailable, live test skipped (set %s=1 to fail instead): %v", name, RequireEnv, err)
	return ""
}

// Path returns the pinned tool in the repository's bin directory when it is an executable file,
// otherwise name looked up on PATH.
func Path(name string) (string, error) {
	if dir := PinnedDir(); dir != "" {
		exe := name
		if runtime.GOOS == "windows" {
			exe += ".exe"
		}
		if fi, err := os.Stat(filepath.Join(dir, exe)); err == nil && fi.Mode().IsRegular() && (runtime.GOOS == "windows" || fi.Mode()&0o111 != 0) {
			return filepath.Join(dir, exe), nil
		}
	}
	return exec.LookPath(name)
}

// PinnedDir returns the repository's bin directory, found from the working directory of the test
// by walking up to go.mod, or "" when there is none.
func PinnedDir() string {
	dir, err := os.Getwd()
	if err != nil {
		return ""
	}
	for {
		if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
			return filepath.Join(dir, "bin")
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return ""
		}
		dir = parent
	}
}
