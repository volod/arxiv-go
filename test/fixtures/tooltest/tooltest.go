// Package tooltest finds the external tools that live media tests run. Production code never
// imports it.
package tooltest

import (
	"os"
	"os/exec"
	"testing"
)

// RequireEnv, set to 1, turns a missing tool into a test failure instead of a skip, so a local
// run meant to exercise ffmpeg and ffprobe cannot pass by skipping every live test.
const RequireEnv = "ARXGO_TEST_REQUIRE_TOOLS"

// LookPath returns the absolute path of name on PATH. When it is missing the test is skipped with
// the reason, or fails when RequireEnv is 1.
func LookPath(t testing.TB, name string) string {
	t.Helper()
	p, err := exec.LookPath(name)
	if err == nil {
		return p
	}
	if os.Getenv(RequireEnv) == "1" {
		t.Fatalf("%s is required by %s=1 but not found: %v", name, RequireEnv, err)
	}
	t.Skipf("%s unavailable, live test skipped (set %s=1 to fail instead): %v", name, RequireEnv, err)
	return ""
}
