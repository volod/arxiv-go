package cli

import (
	"bytes"
	"strings"
	"testing"
)

func TestRunDispatch(t *testing.T) {
	cases := []struct {
		name     string
		args     []string
		wantCode int
		wantOut  string
		wantErr  string
	}{
		{"version", []string{"version"}, ExitOK, "arxgo dev", ""},
		{"help", []string{"help"}, ExitOK, "Usage:", ""},
		{"default operation is scan", []string{"--archive", "x"}, ExitNotImplemented, "", `"scan"`},
		{"split recognized", []string{"split"}, ExitNotImplemented, "", `"split"`},
		{"restore recognized", []string{"restore"}, ExitNotImplemented, "", `"restore"`},
		{"unknown operation", []string{"shuffle"}, ExitUsage, "", "unknown operation"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			var out, errOut bytes.Buffer
			code := Run(tc.args, &out, &errOut)
			if code != tc.wantCode {
				t.Fatalf("exit code = %d, want %d (stderr: %s)", code, tc.wantCode, errOut.String())
			}
			if !strings.Contains(out.String(), tc.wantOut) {
				t.Errorf("stdout %q does not contain %q", out.String(), tc.wantOut)
			}
			if !strings.Contains(errOut.String(), tc.wantErr) {
				t.Errorf("stderr %q does not contain %q", errOut.String(), tc.wantErr)
			}
		})
	}
}
