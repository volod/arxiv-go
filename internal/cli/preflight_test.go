package cli

import (
	"bytes"
	"context"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/internal/fsops"
)

// lowSpaceFS reports every path on one device with the given caller-available bytes.
type lowSpaceFS struct {
	fsops.System
	avail uint64
}

func (lowSpaceFS) SameDevice(string, string) (bool, error) { return true, nil }

func (f lowSpaceFS) FreeSpace(string) (fsops.Space, error) {
	return fsops.Space{Total: 1 << 40, Free: f.avail, Available: f.avail}, nil
}

func TestPreflightOptionsReachTheSession(t *testing.T) {
	arc, video := fixture(t)
	registry := filepath.Join(t.TempDir(), "reg.csv")
	cases := []struct {
		args []string
		want archive.PreflightOptions
		reg  string
	}{
		{[]string{"split", "--archive", arc, "--video-archive", video, "--transfer", "copy", "--min-free", "2GiB"},
			archive.PreflightOptions{Transfer: "copy", MinFree: 2 << 30}, ""},
		{[]string{"restore", "--archive", arc, "--video-archive", video},
			archive.PreflightOptions{Transfer: "auto", MinFree: 1 << 30}, ""},
		{[]string{"scan", "--archive", arc, "--metadata", "media", "--registry", registry, "--min-free", "0"},
			archive.PreflightOptions{Metadata: "media"}, registry},
	}
	for _, tc := range cases {
		t.Run(tc.args[0], func(t *testing.T) {
			withLockIdentity(t, 500)
			var got archive.Config
			lock := sessionHooks
			sessionHooks = func(cfg *archive.Config) { lock(cfg); got = *cfg }
			var out, errOut bytes.Buffer
			if code := run(context.Background(), tc.args, testEnv(&out, &errOut, noProcessEnv)); code != ExitNotImplemented {
				t.Fatalf("exit code = %d: %s", code, errOut.String())
			}
			if got.Preflight != tc.want || got.Registry != tc.reg {
				t.Fatalf("preflight = %+v registry %q, want %+v %q", got.Preflight, got.Registry, tc.want, tc.reg)
			}
		})
	}
}

func TestInsufficientSpaceExitsFour(t *testing.T) {
	arc, video := fixture(t)
	withLockIdentity(t, 500)
	c := Common{Archive: arc, VideoArchive: video, MinFree: 1 << 30, ProgressInterval: 1 << 40,
		CheckpointEvery: 100, CheckpointInterval: 1 << 40, LogLevel: slog.LevelInfo}
	var errOut bytes.Buffer
	log := slog.New(slog.NewTextHandler(&errOut, nil))
	body := func(ctx context.Context, s *archive.Session) error {
		_, err := s.Preflight(ctx, archive.Candidates{Count: 2, Bytes: 5 << 30, Largest: 3 << 30})
		return err
	}
	for _, tc := range []struct {
		avail uint64
		code  int
	}{{1 << 30, ExitInsufficientDisk}, {8 << 30, ExitOK}} {
		errOut.Reset()
		cfg := sessionConfig(OpSplit, c, c, c)
		cfg.Preflight.Transfer, cfg.FS = TransferCopy, lowSpaceFS{avail: tc.avail}
		if code := runSession(context.Background(), cfg, log, body); code != tc.code {
			t.Fatalf("avail %d: exit code = %d, want %d: %s", tc.avail, code, tc.code, errOut.String())
		}
	}
	if code := exitCode(archive.StatusInsufficientSpace); code != 4 {
		t.Fatalf("exitCode = %d", code)
	}
}

func TestInsufficientSpaceReportNamesDevices(t *testing.T) {
	arc, video := fixture(t)
	withLockIdentity(t, 500)
	c := Common{Archive: arc, VideoArchive: video, MinFree: 0, ProgressInterval: 1 << 40,
		CheckpointEvery: 100, CheckpointInterval: 1 << 40, LogLevel: slog.LevelInfo, DryRun: true}
	var errOut bytes.Buffer
	cfg := sessionConfig(OpRestore, c, c, c)
	cfg.Preflight.Transfer, cfg.FS = TransferCopy, lowSpaceFS{avail: 1 << 20}
	code := runSession(context.Background(), cfg, slog.New(slog.NewTextHandler(&errOut, nil)), func(ctx context.Context, s *archive.Session) error {
		_, err := s.Preflight(ctx, archive.Candidates{Count: 1, Bytes: 2 << 20, Largest: 2 << 20})
		return err
	})
	if code != ExitInsufficientDisk {
		t.Fatalf("dry run exit code = %d", code)
	}
	for _, want := range []string{"roles=archive,video_archive", "required=2.0MiB", "available=1.0MiB", "shortfall=1.0MiB", "status=insufficient_space"} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("output lacks %q:\n%s", want, errOut.String())
		}
	}
}
