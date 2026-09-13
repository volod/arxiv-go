package archive

import (
	"errors"
	"testing"

	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

func TestSplitCrashPointsConverge(t *testing.T) {
	for _, mode := range []string{"auto", "copy"} {
		points := crashtest.RenamePoints
		if mode == "copy" {
			points = crashtest.CopyPoints
		}
		for _, point := range append([]string{"fs:mkdir"}, points...) {
			t.Run(mode+"/"+point, func(t *testing.T) {
				r, src, dst := splitFixture(t)
				h := &crashtest.Hook{FailAt: point}
				cfg, c := splitConfig(r, mode)
				cfg.Crash = h.Func()
				cfg.Recoverer = NewSplitResolver(nil, c.Verify, h.Func())
				res := runSplit(t, cfg, c)
				if !errors.Is(res.Err, crashtest.ErrCrash) {
					t.Fatalf("point %s was not reached: %+v", point, res)
				}
				h.FailAt = ""
				cfg, c = splitConfig(r, mode)
				cfg.Lock.PID = 200
				res = runSplit(t, cfg, c)
				if res.Status != StatusCompleted {
					t.Fatalf("resume = %+v", res)
				}
				checkSplit(t, src, dst)
			})
		}
	}
}
