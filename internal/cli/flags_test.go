package cli

import (
	"log/slog"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"
)

// fixture creates an archive and a sibling video archive in a temporary directory.
func fixture(t *testing.T) (archive, video string) {
	t.Helper()
	dir := t.TempDir()
	archive, video = filepath.Join(dir, "archive"), filepath.Join(dir, "video")
	for _, d := range []string{archive, video} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return archive, video
}

func noProcessEnv(string) (string, bool) { return "", false }

func noEnv(string) (string, string, bool) { return "", "", false }

// mapEnv is a process-environment lookup over m.
func mapEnv(m map[string]string) lookupFunc {
	return layeredLookup(func(k string) (string, bool) {
		v, ok := m[k]
		return v, ok
	}, nil, "")
}

func parseScan(t *testing.T, args []string, lookup lookupFunc) (ScanOptions, error) {
	t.Helper()
	s, err := parseFlags(OpScan, args, lookup)
	if err != nil {
		return ScanOptions{}, err
	}
	return buildScanOptions(s, osRootFS())
}

func defaultCommon(archive, video string) Common {
	return Common{
		Archive: archive, VideoArchive: video, LogLevel: slog.LevelInfo, LogFormat: LogText,
		ProgressInterval: 10 * time.Second, CheckpointEvery: 500, CheckpointInterval: 30 * time.Second,
		MinFree: 1 << 30,
	}
}

func TestDefaults(t *testing.T) {
	archive, video := fixture(t)
	defaultScan := ScanSettings{
		LargeThreshold: 1 << 30, Registry: filepath.Join(archive, DefaultRegistryName), Metadata: MetadataFile,
	}

	s, err := parseFlags(OpScan, []string{"--archive", archive}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	scan, err := buildScanOptions(s, osRootFS())
	if err != nil {
		t.Fatal(err)
	}
	if want := (ScanOptions{Common: defaultCommon(archive, ""), ScanSettings: defaultScan}); !reflect.DeepEqual(scan, want) {
		t.Errorf("scan defaults\n got %+v\nwant %+v", scan, want)
	}

	s, err = parseFlags(OpSplit, []string{"--archive", archive, "--video-archive", video}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	split, err := buildSplitOptions(s, osRootFS())
	if err != nil {
		t.Fatal(err)
	}
	wantSplit := SplitOptions{
		Common: defaultCommon(archive, video), ScanSettings: defaultScan,
		Transfer: TransferAuto, Verify: VerifySize,
	}
	if !reflect.DeepEqual(split, wantSplit) {
		t.Errorf("split defaults\n got %+v\nwant %+v", split, wantSplit)
	}

	s, err = parseFlags(OpRestore, []string{"--archive", archive, "--video-archive", video}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	restore, err := buildRestoreOptions(s, osRootFS())
	if err != nil {
		t.Fatal(err)
	}
	wantRestore := RestoreOptions{
		Common: defaultCommon(archive, video), Transfer: TransferAuto, Verify: VerifySize,
		Stubs: StubsDelete, RegistryUpdate: true,
	}
	if !reflect.DeepEqual(restore, wantRestore) {
		t.Errorf("restore defaults\n got %+v\nwant %+v", restore, wantRestore)
	}
}

func TestEveryFlagParses(t *testing.T) {
	archive, video := fixture(t)
	registry := filepath.Join(t.TempDir(), "reg.csv")
	s, err := parseFlags(OpSplit, []string{
		"--archive", archive, "--video-archive=" + video, "--log-level", "debug", "--log-format", "json",
		"--progress-interval", "1m", "--checkpoint-every", "7", "--checkpoint-interval", "90s",
		"--dry-run", "--min-free", "10GB", "--new-run", "--force-unlock=true",
		"--large-threshold", "512MiB", "--registry", registry, "--metadata", "media",
		"--exclude", "*.tmp", "--exclude", "cache/**", "--follow-symlinks=false",
		"--transfer", "copy", "--verify", "hash", "--base-url", "https://cdn.example.com/v/",
		"--video-extensions", "MTS,.m2ts",
	}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	got, err := buildSplitOptions(s, osRootFS())
	if err != nil {
		t.Fatal(err)
	}
	want := SplitOptions{
		Common: Common{
			Archive: archive, VideoArchive: video, LogLevel: slog.LevelDebug, LogFormat: LogJSON,
			ProgressInterval: time.Minute, CheckpointEvery: 7, CheckpointInterval: 90 * time.Second,
			DryRun: true, MinFree: 10_000_000_000, NewRun: true, ForceUnlock: true,
		},
		ScanSettings: ScanSettings{
			LargeThreshold: 512 << 20, Registry: registry, Metadata: MetadataMedia,
			Exclude: []string{"*.tmp", "cache/**"},
		},
		Transfer: TransferCopy, Verify: VerifyHash, BaseURL: "https://cdn.example.com/v",
		VideoExtensions: []string{".mts", ".m2ts"},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("split options\n got %+v\nwant %+v", got, want)
	}

	s, err = parseFlags(OpRestore, []string{
		"--archive", archive, "--video-archive", video, "--transfer", "copy", "--verify", "hash",
		"--stubs", "keep", "--create-dirs", "--overwrite", "--registry-update=false",
	}, noEnv)
	if err != nil {
		t.Fatal(err)
	}
	restore, err := buildRestoreOptions(s, osRootFS())
	if err != nil {
		t.Fatal(err)
	}
	wantRestore := RestoreOptions{
		Common: defaultCommon(archive, video), Transfer: TransferCopy, Verify: VerifyHash,
		Stubs: StubsKeep, CreateDirs: true, Overwrite: true, RegistryUpdate: false,
	}
	if !reflect.DeepEqual(restore, wantRestore) {
		t.Errorf("restore options\n got %+v\nwant %+v", restore, wantRestore)
	}
}

func TestParseErrors(t *testing.T) {
	archive, _ := fixture(t)
	cases := []struct {
		op   string
		args []string
		want string
	}{
		{OpScan, []string{"--log-level", "loud"}, "invalid value \"loud\" for flag --log-level: must be one of debug, info, warn, error"},
		{OpScan, []string{"--log-format", "xml"}, "--log-format"},
		{OpScan, []string{"--metadata", "full"}, "--metadata"},
		{OpSplit, []string{"--transfer", "move"}, "--transfer"},
		{OpSplit, []string{"--verify", "md5"}, "--verify"},
		{OpRestore, []string{"--stubs", "archive"}, "--stubs"},
		{OpScan, []string{"--min-free", "1XB"}, "--min-free"},
		{OpScan, []string{"--large-threshold", "-1"}, "--large-threshold"},
		{OpScan, []string{"--progress-interval", "10"}, "--progress-interval"},
		{OpScan, []string{"--checkpoint-every", "many"}, "--checkpoint-every"},
		{OpScan, []string{"--no-such-flag"}, "flag provided but not defined: --no-such-flag"},
		{OpScan, []string{"--stubs", "keep"}, "not defined: --stubs"},
		{OpRestore, []string{"--metadata", "file"}, "not defined: --metadata"},
		{OpScan, []string{"--archive"}, "flag needs an argument"},
		{OpScan, []string{"--archive", archive, "extra"}, `unexpected argument "extra"`},
		{OpScan, []string{"--archive", archive, "split"}, "operation must come before flags"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			_, err := parseFlags(tc.op, tc.args, noEnv)
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}
}

func TestValueValidation(t *testing.T) {
	archive, video := fixture(t)
	cases := []struct {
		op   string
		args []string
		want string
	}{
		{OpScan, []string{"--progress-interval", "0s"}, "--progress-interval must be positive"},
		{OpScan, []string{"--checkpoint-interval", "-1s"}, "--checkpoint-interval must be positive"},
		{OpScan, []string{"--checkpoint-every", "0"}, "--checkpoint-every must be at least 1"},
		{OpScan, []string{"--large-threshold", "0"}, "--large-threshold must be positive"},
		{OpScan, []string{"--exclude", "/abs"}, "--exclude \"/abs\""},
		{OpScan, []string{"--registry", archive}, "is a directory"},
		{OpScan, []string{"--registry", filepath.Join(archive, "missing", "r.csv")}, "parent directory does not exist"},
		{OpSplit, []string{"--base-url", "ftp://x/y"}, "--base-url"},
		{OpSplit, []string{"--base-url", "https://example.com/v?token=1"}, "query"},
		{OpSplit, []string{"--video-extensions", "mp4,,ts"}, "--video-extensions"},
	}
	for _, tc := range cases {
		t.Run(strings.Join(tc.args, " "), func(t *testing.T) {
			args := append([]string{"--archive", archive, "--video-archive", video}, tc.args...)
			if tc.op == OpScan {
				args = append([]string{"--archive", archive}, tc.args...)
			}
			s, err := parseFlags(tc.op, args, noEnv)
			if err != nil {
				t.Fatalf("parse: %v", err)
			}
			switch tc.op {
			case OpScan:
				_, err = buildScanOptions(s, osRootFS())
			case OpSplit:
				_, err = buildSplitOptions(s, osRootFS())
			}
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want containing %q", err, tc.want)
			}
		})
	}

	t.Run("relative registry becomes absolute", func(t *testing.T) {
		t.Chdir(archive)
		got, err := parseScan(t, []string{"--archive", ".", "--registry", "out.csv"}, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		if !filepath.IsAbs(got.Registry) || filepath.Base(got.Registry) != "out.csv" || !filepath.IsAbs(got.Archive) {
			t.Errorf("paths not absolute: archive %q registry %q", got.Archive, got.Registry)
		}
	})

	t.Run("all errors reported together", func(t *testing.T) {
		s, err := parseFlags(OpSplit, []string{"--checkpoint-every", "0", "--base-url", "nope"}, noEnv)
		if err != nil {
			t.Fatal(err)
		}
		_, err = buildSplitOptions(s, osRootFS())
		for _, want := range []string{"--checkpoint-every", "--archive is required", "--video-archive is required", "--base-url"} {
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Errorf("error %v does not mention %q", err, want)
			}
		}
	})
}
