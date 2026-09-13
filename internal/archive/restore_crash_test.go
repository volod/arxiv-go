package archive

import (
	"bytes"
	"errors"
	"reflect"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

func TestRestoreCrashPointsConverge(t *testing.T) {
	cases := []struct {
		mode   string
		points []string
	}{
		{"auto", crashtest.RestoreRenamePoints},
		{"copy", []string{
			"wal:begin", "fs:copy", "wal:copied", "wal:verified", "fs:place",
			"wal:placed", "fs:stub", "wal:stub_removed", "wal:commit",
		}},
	}
	for _, tc := range cases {
		for _, point := range tc.points {
			t.Run(tc.mode+"/"+point, func(t *testing.T) {
				r, src, dst := splitFixture(t)
				splitThen(t, r, src, dst)
				h := &crashtest.Hook{FailAt: point}
				cfg, c := restoreConfig(r, tc.mode)
				attachRestoreRecoverer(&cfg, &c, h.Func())
				res := runRestore(t, cfg, c)
				if !errors.Is(res.Err, crashtest.ErrCrash) {
					t.Fatalf("point %s was not reached: %+v", point, res)
				}
				h.FailAt = ""
				cfg, c = restoreConfig(r, tc.mode)
				cfg.Lock.PID = 200
				attachRestoreRecoverer(&cfg, &c, nil)
				res = runRestore(t, cfg, c)
				if res.Status != StatusCompleted {
					t.Fatalf("resume = %+v", res)
				}
				if !exists(src) {
					t.Fatal("video missing after resume")
				}
				if tc.mode != "copy" && exists(dst) {
					t.Fatal("source remains after auto restore")
				}
				if exists(src + ".md") {
					t.Fatal("stub remains after resume")
				}
			})
		}
	}
}

// otherDevices reports the archive and the video archive on different devices with plenty of space.
func otherDevices(r roots) *fakeFS {
	return &fakeFS{
		device: func(p string) string {
			if strings.HasPrefix(p, r.video) {
				return "video"
			}
			return "archive"
		},
		space: map[string]fsops.Space{"archive": {Total: 1 << 40, Available: 1 << 40}, "video": {Total: 1 << 40, Available: 1 << 40}},
	}
}

func TestRestoreCrossDeviceCrashPointsConverge(t *testing.T) {
	for _, point := range crashtest.RestoreCopyPoints {
		t.Run(point, func(t *testing.T) {
			r, src, dst := splitFixture(t)
			splitThen(t, r, src, dst)
			h := &crashtest.Hook{FailAt: point}
			cfg, c := restoreConfig(r, "auto")
			cfg.FS = otherDevices(r)
			attachRestoreRecoverer(&cfg, &c, h.Func())
			if res := runRestore(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
				t.Fatalf("point %s was not reached: %+v", point, res)
			}
			cfg, c = restoreConfig(r, "auto")
			cfg.FS = otherDevices(r)
			cfg.Lock.PID = 200
			attachRestoreRecoverer(&cfg, &c, nil)
			if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("resume = %+v", res)
			}
			if !bytes.Equal(mustRead(t, src), videoFixture) {
				t.Fatal("restored bytes differ")
			}
			if exists(dst) || exists(fsops.PartPath(src)) || exists(src+".md") {
				t.Fatalf("source %v, part %v or stub %v remains", exists(dst), exists(fsops.PartPath(src)), exists(src+".md"))
			}
		})
	}
}

func TestRestorePreflightRefusesWithoutMutation(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	before := treeManifest(t, r.video)
	cfg, c := restoreConfig(r, "auto")
	f := otherDevices(r)
	f.space["archive"] = fsops.Space{Total: 1 << 40, Available: uint64(len(videoFixture)) - 1}
	cfg.FS = f
	attachRestoreRecoverer(&cfg, &c, nil)
	res := runRestore(t, cfg, c)
	if res.Status != StatusInsufficientSpace {
		t.Fatalf("restore = %+v", res)
	}
	if exists(src) || !exists(src+".md") {
		t.Fatal("refused restore changed the archive")
	}
	if after := treeManifest(t, r.video); !reflect.DeepEqual(before, after) {
		t.Fatalf("refused restore changed the video archive:\n%v\n%v", before, after)
	}
	if exists(state.LockPath(r.archive)) || exists(state.LockPath(r.video)) {
		t.Fatal("refused restore kept a lock")
	}
}
