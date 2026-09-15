package cli

import (
	"log/slog"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func TestEnvironmentOverrides(t *testing.T) {
	archive, other := fixture(t)
	sep := string(os.PathListSeparator)

	t.Run("environment replaces defaults", func(t *testing.T) {
		got, err := parseScan(t, nil, mapEnv(map[string]string{
			"ARXGO_ARCHIVE": archive, "ARXGO_LOG_LEVEL": "warn", "ARXGO_MIN_FREE": "2GiB",
			"ARXGO_DRY_RUN": "1", "ARXGO_CHECKPOINT_EVERY": "10", "ARXGO_PROGRESS_INTERVAL": "2s",
			"ARXGO_EXCLUDE": "*.tmp" + sep + sep + "a/**",
		}))
		if err != nil {
			t.Fatal(err)
		}
		if got.Archive != archive || got.LogLevel != slog.LevelWarn || got.MinFree != 2<<30 ||
			!got.DryRun || got.CheckpointEvery != 10 || got.ProgressInterval != 2*time.Second ||
			!reflect.DeepEqual(got.Exclude, []string{"*.tmp", "a/**"}) {
			t.Errorf("environment not applied: %+v", got)
		}
	})

	t.Run("command line wins", func(t *testing.T) {
		got, err := parseScan(t, []string{"--archive", archive, "--log-level=error", "--exclude", "x", "--dry-run=false"},
			mapEnv(map[string]string{
				"ARXGO_ARCHIVE": other, "ARXGO_LOG_LEVEL": "debug", "ARXGO_EXCLUDE": "y" + sep + "z",
				"ARXGO_DRY_RUN": "true",
			}))
		if err != nil {
			t.Fatal(err)
		}
		if got.Archive != archive || got.LogLevel != slog.LevelError || got.DryRun ||
			!reflect.DeepEqual(got.Exclude, []string{"x"}) {
			t.Errorf("command line did not win: %+v", got)
		}
	})

	t.Run("empty environment value is ignored", func(t *testing.T) {
		got, err := parseScan(t, []string{"--archive", archive}, mapEnv(map[string]string{"ARXGO_LOG_LEVEL": ""}))
		if err != nil || got.LogLevel != slog.LevelInfo {
			t.Fatalf("got %v, %v", got.LogLevel, err)
		}
	})

	t.Run("environment for another operation's flag is ignored", func(t *testing.T) {
		_, err := parseScan(t, []string{"--archive", archive},
			mapEnv(map[string]string{"ARXGO_DESCRIPTIONS": "bogus", "ARXGO_SAMPLE": "start"}))
		if err != nil {
			t.Fatal(err)
		}
	})

	for _, tc := range []struct{ env, value, want string }{
		{"ARXGO_LOG_LEVEL", "loud", "ARXGO_LOG_LEVEL"},
		{"ARXGO_MIN_FREE", "ten", "ARXGO_MIN_FREE"},
		{"ARXGO_DRY_RUN", "maybe", "ARXGO_DRY_RUN"},
		{"ARXGO_CHECKPOINT_INTERVAL", "soon", "ARXGO_CHECKPOINT_INTERVAL"},
	} {
		t.Run("invalid "+tc.env, func(t *testing.T) {
			_, err := parseFlags(OpScan, []string{"--archive", archive}, mapEnv(map[string]string{tc.env: tc.value}))
			if err == nil || !strings.Contains(err.Error(), tc.want) {
				t.Fatalf("error = %v, want mention of %s", err, tc.want)
			}
		})
	}
}

func TestReservedFlagsRejected(t *testing.T) {
	archive, video := fixture(t)
	for _, d := range flagTable {
		if d.plannedFeature == "" {
			continue
		}
		op := d.ops[0]
		base := []string{"--archive", archive, "--video-archive", video}
		value := "--" + d.name + "=x"
		if d.name == "publish-delete-local" {
			value = "--" + d.name
		}
		t.Run(d.name+" flag", func(t *testing.T) {
			_, err := parseFlags(op, append(base, value), noEnv)
			if err == nil || !strings.Contains(err.Error(), "--"+d.name+": option not available in this build") {
				t.Fatalf("error = %v", err)
			}
		})
		t.Run(d.name+" env", func(t *testing.T) {
			env := EnvName(d.name)
			_, err := parseFlags(op, base, mapEnv(map[string]string{env: "true"}))
			if err == nil || !strings.Contains(err.Error(), env+": option not available in this build") {
				t.Fatalf("error = %v", err)
			}
		})
	}
	if _, err := parseScan(t, []string{"--archive", archive, "--follow-symlinks"}, noEnv); err == nil ||
		!strings.Contains(err.Error(), "--follow-symlinks: option not available") {
		t.Errorf("--follow-symlinks error = %v", err)
	}
}
