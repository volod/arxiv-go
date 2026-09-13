package archive

import (
	"errors"
	"testing"

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
