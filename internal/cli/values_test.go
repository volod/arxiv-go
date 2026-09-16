package cli

import (
	"reflect"
	"strings"
	"testing"
)

func TestParseSize(t *testing.T) {
	cases := []struct {
		in      string
		want    Size
		wantErr string
	}{
		{"0", 0, ""},
		{"1024", 1024, ""},
		{"1B", 1, ""},
		{"1KB", 1000, ""},
		{"1MB", 1000 * 1000, ""},
		{"2GB", 2 * 1000 * 1000 * 1000, ""},
		{"1TB", 1000 * 1000 * 1000 * 1000, ""},
		{"1KiB", 1024, ""},
		{"1MiB", 1 << 20, ""},
		{"1GiB", 1 << 30, ""},
		{"3TiB", 3 << 40, ""},
		{"1gib", 1 << 30, ""},
		{" 5 MiB ", 5 << 20, ""},
		{"1.5KiB", 1536, ""},
		{"0.1KB", 100, ""},
		{"1.0001KB", 1000, ""},
		{"8388607TiB", 8388607 << 40, ""},
		{"8388608TiB", 0, "exceeds"},
		{"9223372036854775808", 0, "exceeds"},
		{"", 0, "want a number"},
		{"GiB", 0, "want a number"},
		{"-1GiB", 0, "want a number"},
		{"1.", 0, "want a number"},
		{".5GiB", 0, "want a number"},
		{"1..5GiB", 0, "want a number"},
		{"1.5", 0, "fractional bytes"},
		{"1.5B", 0, "fractional bytes"},
		{"1XB", 0, "unknown unit"},
		{"1 GiB extra", 0, "unknown unit"},
		{"1K", 0, "unknown unit"},
	}
	for _, tc := range cases {
		t.Run(tc.in, func(t *testing.T) {
			got, err := ParseSize(tc.in)
			if tc.wantErr != "" {
				if err == nil || !strings.Contains(err.Error(), tc.wantErr) {
					t.Fatalf("ParseSize(%q) error = %v, want containing %q", tc.in, err, tc.wantErr)
				}
				return
			}
			if err != nil || got != tc.want {
				t.Fatalf("ParseSize(%q) = %d, %v; want %d", tc.in, got, err, tc.want)
			}
		})
	}
}

func TestSizeStringRoundTrips(t *testing.T) {
	cases := map[Size]string{0: "0B", 1: "1B", 1000: "1000B", 1024: "1KiB", 1 << 30: "1GiB", 1536: "1536B", 5 << 40: "5TiB"}
	for size, want := range cases {
		if got := size.String(); got != want {
			t.Errorf("Size(%d).String() = %q, want %q", int64(size), got, want)
		}
		if back, err := ParseSize(want); err != nil || back != size {
			t.Errorf("ParseSize(%q) = %d, %v; want %d", want, back, err, int64(size))
		}
	}
}

func TestParseExtensions(t *testing.T) {
	cases := []struct {
		in      string
		want    []string
		wantErr bool
	}{
		{"", nil, false},
		{"  ", nil, false},
		{"mp4", []string{".mp4"}, false},
		{".MKV, ts,mkv", []string{".mkv", ".ts"}, false},
		{"m2t_s,x-y", []string{".m2t_s", ".x-y"}, false},
		{"mp4,", nil, true},
		{"mp4,,ts", nil, true},
		{"a/b", nil, true},
		{"my ext", nil, true},
		{"*.mp4", nil, true},
	}
	for _, tc := range cases {
		got, err := parseExtensions(tc.in)
		if (err != nil) != tc.wantErr || !reflect.DeepEqual(got, tc.want) {
			t.Errorf("parseExtensions(%q) = %v, %v; want %v, error %v", tc.in, got, err, tc.want, tc.wantErr)
		}
	}
}

func TestValidateGlob(t *testing.T) {
	valid := []string{"*.tmp", "cache/**", "**/node_modules", "a/**/b/*.iso", "[a-z]?.bak", "Thumbs.db"}
	for _, g := range valid {
		if err := validateGlob(g, false); err != nil {
			t.Errorf("validateGlob(%q) = %v, want nil", g, err)
		}
	}
	invalid := map[string]string{
		"/abs/*":  "relative",
		"a//b":    "empty path segment",
		"a/":      "empty path segment",
		"../x":    "'..'",
		"a/[b":    "malformed",
		"a/\\":    "malformed",
		"x/../..": "'..'",
	}
	for g, want := range invalid {
		if err := validateGlob(g, false); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("validateGlob(%q) = %v, want error containing %q", g, err, want)
		}
	}
	// Windows operators must not get a silently non-matching pattern from a backslash separator.
	for _, g := range []string{`cache\*`, `a/\[b]`} {
		if err := validateGlob(g, true); err == nil || !strings.Contains(err.Error(), `use "/"`) {
			t.Errorf("windows validateGlob(%q) = %v, want separator error", g, err)
		}
	}
	if err := validateGlob("a/[[]b]", true); err != nil {
		t.Errorf("windows validateGlob literal bracket = %v", err)
	}
}

func TestValidateBaseURL(t *testing.T) {
	valid := map[string]string{
		"https://storage.example.com/video":   "https://storage.example.com/video",
		"http://nas.local:8080/":              "http://nas.local:8080",
		"https://example.com/a%20b/videos///": "https://example.com/a%20b/videos",
	}
	for in, want := range valid {
		got, err := validateBaseURL(in)
		if err != nil || got != want {
			t.Errorf("validateBaseURL(%q) = %q, %v; want %q", in, got, err, want)
		}
	}
	invalid := map[string]string{
		"storage.example.com/video":      "scheme",
		"/video":                         "scheme",
		"ftp://example.com/video":        "scheme",
		"file:///srv/video":              "scheme",
		"https://":                       "host",
		"https://:443/x":                 "host",
		"https://user:pw@example.com/x":  "credentials",
		"https://example.com/x?sig=abc":  "query",
		"https://example.com/x?":         "query",
		"https://example.com/x#frag":     "query",
		"https://example.com/%zz":        "valid URL",
		"https://exa mple.com/video":     "valid URL",
		"https://example.com/x#":         "query",
		"HTTP://EXAMPLE.COM/x?list=a%2C": "query",
	}
	for in, want := range invalid {
		if _, err := validateBaseURL(in); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("validateBaseURL(%q) = %v, want error containing %q", in, err, want)
		}
	}
}
