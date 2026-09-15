package archive

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

func TestCatiaRestoreCrashPointsConverge(t *testing.T) {
	points := append(append([]string(nil), crashtest.RestoreRenamePoints...), "wal:text_delete", "wal:text_deleted")
	for _, point := range points {
		t.Run(point, func(t *testing.T) {
			r, files, want := catiaRoundTripFixture(t, fsops.System{})
			h := &crashtest.Hook{FailAt: point}
			cfg, c := catiaRestoreConfig(r, "auto")
			attachCatiaRestoreRecoverer(&cfg, &c, h.Func())
			if res := runRestore(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
				t.Fatalf("point %s was not reached: %+v", point, res)
			}
			cfg, c = catiaRestoreConfig(r, "auto")
			cfg.Lock.PID = 200
			res := runRestore(t, cfg, c)
			if res.Status != StatusCompleted {
				t.Fatalf("resume = %+v", res)
			}
			sameTree(t, "archive", treeState(t, r.archive, "arxgo-registry.csv", "arxgo-catia.restored-*.csv"), want)
			for rel := range files {
				if exists(filepath.Join(r.catia, filepath.FromSlash(rel))) {
					t.Errorf("%s left in the CATIA archive", rel)
				}
			}
		})
	}
}

// A CATIA restore interrupted after placed is rolled forward by a later video restore, which
// removes the owned CATIA description and leaves CATIA text sidecars to the next CATIA restore.
func TestInterruptedCatiaRestoreRecoveredByVideoRestore(t *testing.T) {
	r, files, want := catiaRoundTripFixture(t, fsops.System{})
	h := &crashtest.Hook{FailAt: "wal:placed"}
	cfg, c := catiaRestoreConfig(r, "auto")
	attachCatiaRestoreRecoverer(&cfg, &c, h.Func())
	if res := runRestore(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("crash = %+v", res)
	}
	var placed string
	for rel := range files {
		if exists(filepath.Join(r.archive, filepath.FromSlash(rel))) {
			placed = rel
		}
	}
	if placed == "" {
		t.Fatal("no CATIA file was placed before the crash")
	}

	vcfg, vc := restoreConfig(r.roots, "auto")
	vcfg.Lock.PID = 200
	vcfg.RecovererFor = func(_ string, payload PayloadKind, _ json.RawMessage) (Resolver, error) {
		if payload != PayloadCatia {
			t.Errorf("recovery payload %q", payload)
		}
		rcfg, _ := catiaRestoreConfig(r, "auto")
		return rcfg.Recoverer, nil
	}
	if res := runRestore(t, vcfg, vc); res.Status != StatusCompleted {
		t.Fatalf("video restore = %+v", res)
	}
	if exists(state.LockPath(r.catia)) || exists(filepath.Join(r.catia, filepath.FromSlash(placed))) {
		t.Fatal("CATIA recovery left the lock or the mirror copy")
	}
	src := filepath.Join(r.archive, filepath.FromSlash(placed))
	for _, description := range []string{src + ".md", src + ".arxgo.md"} {
		if occ, err := report.InspectDescription(description, placed); err != nil || occ == report.DescriptionOwned {
			t.Fatalf("recovered CATIA restore kept its owned description %s: %v", description, err)
		}
	}
	if exists(filepath.Join(r.catia, scanner.VideoRegistryName)) || exists(filepath.Join(r.video, scanner.CatiaRegistryName)) {
		t.Fatal("a mirror root received the other payload's registry")
	}

	cfg, c = catiaRestoreConfig(r, "auto")
	cfg.Lock.PID = 300
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("CATIA restore after recovery = %+v", res)
	}
	// The video restore returned the video and retired its registry and description.
	delete(want, "media/clip.mp4.md")
	delete(want, scanner.VideoRegistryName)
	sameTree(t, "archive", treeState(t, r.archive, "arxgo-registry.csv", "arxgo-catia.restored-*.csv", "arxgo-videos.restored-*.csv", "media/clip.mp4"), want)
}
