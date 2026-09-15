package media

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const probeHelperEnv = "ARXGO_TEST_PROBE_HELPER"
const ffprobeHelperEnv = "ARXGO_TEST_FFPROBE_HELPER"

func TestMain(m *testing.M) {
	if mode := os.Getenv(ffmpegHelperEnv); mode != "" {
		runFFmpegHelper(mode)
		os.Exit(0)
	}
	if mode := os.Getenv(ffprobeHelperEnv); mode != "" {
		if len(os.Args) != 10 || os.Args[1] != "-v" || os.Args[2] != "error" || os.Args[3] != "-hide_banner" || os.Args[4] != "-print_format" || os.Args[5] != "json" || os.Args[6] != "-show_format" || os.Args[7] != "-show_streams" || os.Args[8] != "--" {
			os.Exit(18)
		}
		switch mode {
		case "valid":
			data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ffprobe", "avi.json"))
			if err != nil {
				os.Exit(16)
			}
			_, _ = os.Stdout.Write(data)
		case "oversize":
			_, _ = os.Stdout.Write([]byte(strings.Repeat("A", ffprobeOutputLimit+1)))
		case "sleep":
			time.Sleep(5 * time.Second)
		case "nonzero":
			fmt.Fprintln(os.Stderr, "invalid media")
			os.Exit(7)
		default:
			os.Exit(17)
		}
		os.Exit(0)
	}
	if mode := os.Getenv(probeHelperEnv); mode != "" {
		// Finder invokes this test binary with -version. Dispatch before testing parses flags.
		if len(os.Args) != 2 || os.Args[1] != "-version" {
			os.Exit(12)
		}
		switch mode {
		case "version":
			fmt.Print("ffprobe version 9.9-helper\nsecond line\n")
		case "nonzero":
			fmt.Print("ffprobe version rejected\n")
			os.Exit(9)
		case "empty":
		case "blank":
			fmt.Print("\nsecond line\n")
		case "large":
			fmt.Print(strings.Repeat("A", versionOutputLimit+1024), "\nsecond line\n")
		case "sleep":
			time.Sleep(5 * time.Second)
		case "child":
			exe, err := os.Executable()
			if err != nil {
				os.Exit(13)
			}
			child := exec.Command(exe, "-version")
			child.Env = append(os.Environ(), probeHelperEnv+"=hold")
			child.Stdout = os.Stdout
			if child.Start() != nil {
				os.Exit(14)
			}
			time.Sleep(5 * time.Second)
		case "hold":
			time.Sleep(5 * time.Second)
		default:
			os.Exit(15)
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestRealVersionProbe(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name, mode, want string
		fail             bool
	}{
		{"version", "version", "ffprobe version 9.9-helper", false},
		{"nonzero", "nonzero", "", true},
		{"empty", "empty", "", true},
		{"blank first line", "blank", "", true},
		{"output cap", "large", strings.Repeat("A", versionOutputLimit), false},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Setenv(probeHelperEnv, tc.mode)
			got, err := (Finder{}).version(context.Background(), exe)
			if tc.fail && err == nil || !tc.fail && (err != nil || got != tc.want) {
				t.Fatalf("version = %q, %v; want %q, fail=%v", got, err, tc.want, tc.fail)
			}
		})
	}
}

func TestRealVersionProbeTimeout(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"sleep", "child"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv(probeHelperEnv, mode)
			f := Finder{Timeout: 500 * time.Millisecond}
			start := time.Now()
			_, err := f.version(context.Background(), exe)
			if err == nil || !strings.Contains(err.Error(), "did not finish") {
				t.Fatalf("version error = %v", err)
			}
			if elapsed := time.Since(start); elapsed > 3*time.Second {
				t.Fatalf("timeout took %s", elapsed)
			}
		})
	}
}
