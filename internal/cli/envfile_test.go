package cli

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"strings"
	"testing"
)

func writeEnvFile(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), EnvFileName)
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

func TestReadEnvFile(t *testing.T) {
	t.Run("missing file and disabled path yield nothing", func(t *testing.T) {
		for _, path := range []string{"", filepath.Join(t.TempDir(), EnvFileName)} {
			values, err := readEnvFile(path)
			if err != nil || values != nil {
				t.Errorf("readEnvFile(%q) = %v, %v; want nil, nil", path, values, err)
			}
		}
	})

	t.Run("dotenv syntax", func(t *testing.T) {
		path := writeEnvFile(t, strings.Join([]string{
			"# comment",
			"",
			"ARXGO_ARCHIVE=/data/archive # trailing comment",
			"export ARXGO_LOG_LEVEL=debug",
			`ARXGO_VIDEO_ARCHIVE='\\nas\video'`,
			`WIN_UNQUOTED=D:\archive`,
			`ARXGO_BASE_URL="https://example.com/v"`,
			"ROOT=/srv",
			"ARXGO_REGISTRY=${ROOT}/reg.csv",
			"CRLF=yes\r",
			"# ARXGO_DRY_RUN=true",
		}, "\n"))
		values, err := readEnvFile(path)
		if err != nil {
			t.Fatal(err)
		}
		want := map[string]string{
			"ARXGO_ARCHIVE": "/data/archive", "ARXGO_LOG_LEVEL": "debug",
			"ARXGO_VIDEO_ARCHIVE": `\\nas\video`, "WIN_UNQUOTED": `D:\archive`,
			"ARXGO_BASE_URL": "https://example.com/v", "ROOT": "/srv", "ARXGO_REGISTRY": "/srv/reg.csv",
			"CRLF": "yes",
		}
		if !reflect.DeepEqual(values, want) {
			t.Errorf("values\n got %q\nwant %q", values, want)
		}
	})

	for name, content := range map[string]string{
		"unterminated quote":    `ARXGO_ARCHIVE="/data`,
		"line without equals":   "ARXGO_ARCHIVE=/data\nARXGO_DRY_RUN\n",
		"key with space":        "ARXGO ARCHIVE=/data\n",
		"key starts with digit": "1KEY=x\n",
	} {
		t.Run("malformed: "+name, func(t *testing.T) {
			path := writeEnvFile(t, content)
			if _, err := readEnvFile(path); err == nil || !strings.Contains(err.Error(), path) {
				t.Fatalf("error = %v, want one naming %s", err, path)
			}
		})
	}

	t.Run("directory instead of file", func(t *testing.T) {
		dir := filepath.Join(t.TempDir(), EnvFileName)
		if err := os.Mkdir(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if _, err := readEnvFile(dir); err == nil {
			t.Fatal("want error for a directory")
		}
	})
}

func TestLayeredLookupPrecedence(t *testing.T) {
	process := map[string]string{"P_ONLY": "p", "BOTH": "process", "EMPTY_PROCESS": ""}
	file := map[string]string{"F_ONLY": "f", "BOTH": "file", "EMPTY_PROCESS": "from-file", "EMPTY_FILE": ""}
	lookup := layeredLookup(func(k string) (string, bool) { v, ok := process[k]; return v, ok }, file, "/x/.env")
	cases := []struct{ key, value, source string }{
		{"P_ONLY", "p", "P_ONLY"},
		{"F_ONLY", "f", "F_ONLY (from /x/.env)"},
		{"BOTH", "process", "BOTH"},
		{"EMPTY_PROCESS", "from-file", "EMPTY_PROCESS (from /x/.env)"},
	}
	for _, tc := range cases {
		v, source, ok := lookup(tc.key)
		if !ok || v != tc.value || source != tc.source {
			t.Errorf("lookup(%q) = %q, %q, %v; want %q, %q", tc.key, v, source, ok, tc.value, tc.source)
		}
	}
	for _, key := range []string{"EMPTY_FILE", "MISSING"} {
		if _, _, ok := lookup(key); ok {
			t.Errorf("lookup(%q) found a value", key)
		}
	}
}

func TestRunWithEnvFile(t *testing.T) {
	archive, video := fixture(t)
	_, other := fixture(t)
	runWith := func(t *testing.T, content string, process map[string]string, args ...string) (int, SplitOptions, string) {
		t.Helper()
		var out, errOut bytes.Buffer
		e := testEnv(&out, &errOut, func(k string) (string, bool) { v, ok := process[k]; return v, ok })
		e.envFile = writeEnvFile(t, content)
		var got SplitOptions
		e.handlers.Split = func(_ context.Context, o SplitOptions, _ *slog.Logger) int {
			got = o
			return ExitOK
		}
		return run(context.Background(), append([]string{"split"}, args...), e), got, errOut.String()
	}
	base := "ARXGO_ARCHIVE=" + archive + "\nARXGO_VIDEO_ARCHIVE=" + video + "\n"

	t.Run("file supplies mandatory roots", func(t *testing.T) {
		code, got, stderr := runWith(t, base+"ARXGO_VERIFY=hash\n", nil)
		if code != ExitOK || got.Archive != archive || got.VideoArchive != video || got.Verify != VerifyHash {
			t.Fatalf("code %d options %+v stderr %s", code, got, stderr)
		}
	})

	t.Run("process environment beats file, command line beats both", func(t *testing.T) {
		code, got, stderr := runWith(t, base+"ARXGO_VERIFY=hash\nARXGO_TRANSFER=copy\n",
			map[string]string{"ARXGO_VIDEO_ARCHIVE": other, "ARXGO_VERIFY": "size"}, "--transfer", "auto")
		if code != ExitOK || got.VideoArchive != other || got.Verify != VerifySize || got.Transfer != TransferAuto {
			t.Fatalf("code %d options %+v stderr %s", code, got, stderr)
		}
	})

	t.Run("invalid value names the file", func(t *testing.T) {
		code, _, stderr := runWith(t, base+"ARXGO_VERIFY=md5\n", nil)
		if code != ExitUsage || !strings.Contains(stderr, "ARXGO_VERIFY (from ") || !strings.Contains(stderr, EnvFileName) {
			t.Fatalf("code %d stderr %s", code, stderr)
		}
	})

	t.Run("reserved flag in file", func(t *testing.T) {
		code, _, stderr := runWith(t, base+"ARXGO_PUBLISH=gdrive\n", nil)
		if code != ExitUsage || !regexp.MustCompile(`ARXGO_PUBLISH \(from .*\): option not available`).MatchString(stderr) {
			t.Fatalf("code %d stderr %s", code, stderr)
		}
	})

	t.Run("malformed file exits 2", func(t *testing.T) {
		code, _, stderr := runWith(t, base+"NOT A LINE\n", nil)
		if code != ExitUsage || !strings.Contains(stderr, "arxgo: environment file ") {
			t.Fatalf("code %d stderr %s", code, stderr)
		}
	})

	t.Run("unknown ARXGO variable warns, secrets are not logged", func(t *testing.T) {
		code, _, stderr := runWith(t, base+"ARXGO_ARCHVE=typo\nSOME_SECRET=hunter2\n", nil, "--log-level", "debug")
		if code != ExitOK || !strings.Contains(stderr, "variable=ARXGO_ARCHVE") ||
			!strings.Contains(stderr, "environment file loaded") || strings.Contains(stderr, "hunter2") ||
			strings.Contains(stderr, "SOME_SECRET") {
			t.Fatalf("code %d stderr %s", code, stderr)
		}
	})

	t.Run("process environment is not modified", func(t *testing.T) {
		const key = "ARXGO_TEST_ENVFILE_SENTINEL"
		runWith(t, base+key+"=1\n", nil)
		if _, ok := os.LookupEnv(key); ok {
			t.Fatalf("%s leaked into the process environment", key)
		}
	})

	t.Run("help and version work with a broken file", func(t *testing.T) {
		var out, errOut bytes.Buffer
		e := testEnv(&out, &errOut, noProcessEnv)
		e.envFile = writeEnvFile(t, `ARXGO_ARCHIVE="unterminated`)
		for _, args := range [][]string{{"help"}, {"version"}, {"help", "split"}, {"split", "--help"}, {"-h"}} {
			if code := run(context.Background(), args, e); code != ExitOK {
				t.Errorf("%v: exit %d, stderr %s", args, code, errOut.String())
			}
		}
	})
}

// TestEnvExampleListsEveryFlag keeps .env.example in step with the flag table.
func TestEnvExampleListsEveryFlag(t *testing.T) {
	path := filepath.Join("..", "..", ".env.example")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, d := range flagTable {
		name := EnvName(d.name)
		if !regexp.MustCompile(`(?m)^# ` + name + `=`).Match(content) {
			t.Errorf(".env.example has no commented %s line", name)
		}
	}
	values, err := readEnvFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if len(values) != 0 {
		t.Errorf(".env.example must set nothing by default, got %v", values)
	}
}
