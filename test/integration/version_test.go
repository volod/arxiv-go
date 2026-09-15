package integration

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// semver is the Semantic Versioning 2.0.0 grammar (https://semver.org).
var semver = regexp.MustCompile(`^(0|[1-9]\d*)\.(0|[1-9]\d*)\.(0|[1-9]\d*)` +
	`(?:-((?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*)(?:\.(?:0|[1-9]\d*|\d*[a-zA-Z-][0-9a-zA-Z-]*))*))?` +
	`(?:\+([0-9a-zA-Z-]+(?:\.[0-9a-zA-Z-]+)*))?$`)

// TestVersionFileIsSemver guards the hand-edited VERSION file that make stamps into arxgo and the
// release bundle names.
func TestVersionFileIsSemver(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "VERSION"))
	if err != nil {
		t.Fatal(err)
	}
	if v := string(data); !strings.HasSuffix(v, "\n") || strings.Count(v, "\n") != 1 || !semver.MatchString(strings.TrimSuffix(v, "\n")) {
		t.Fatalf("VERSION must hold one Semantic Versioning line such as 0.1.0, got %q", v)
	}
	for _, bad := range []string{"v0.1.0", "0.1", "01.0.0", "0.1.0-", "dcb38ed"} {
		if semver.MatchString(bad) {
			t.Errorf("semver accepted %q", bad)
		}
	}
}
