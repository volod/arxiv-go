package archive

import (
	"encoding/json"
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

// A CATIA split interrupted after placed is rolled forward with a CATIA description by a later
// video split, which takes the CATIA archive lock for the recovery only.
func TestInterruptedCatiaSplitRecoveredByVideoSplit(t *testing.T) {
	r, files := catiaFixture(t)
	h := &crashtest.Hook{FailAt: "wal:placed"}
	cfg, c := catiaSplitConfig(r, "auto")
	cfg.Crash = h.Func()
	attachRecoverer(&cfg, c, h.Func())
	if res := runSplit(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("crash = %+v", res)
	}
	prev := currentRunID(t, r.archive)

	vcfg, vc := splitConfig(r.roots, "auto")
	vcfg.Lock.PID = 200
	var rebuilt []PayloadKind
	vcfg.RecovererFor = func(_ string, payload PayloadKind, _ json.RawMessage) (Resolver, error) {
		rebuilt = append(rebuilt, payload)
		_, cc := catiaSplitConfig(r, "auto")
		rs := NewSplitResolver(nil, cc.Verify, nil)
		rs.Descriptions = cc.Descriptions
		return rs, nil
	}
	if res := runSplit(t, vcfg, vc); res.Status != StatusCompleted {
		t.Fatalf("video split = %+v", res)
	}
	if len(rebuilt) != 1 || rebuilt[0] != PayloadCatia {
		t.Fatalf("recovery resolvers = %v", rebuilt)
	}
	if exists(state.LockPath(r.catia)) {
		t.Fatal("CATIA archive lock kept after recovery")
	}
	var recovered string
	for rel := range files {
		if exists(filepath.Join(r.catia, filepath.FromSlash(rel))) {
			recovered = rel
		}
	}
	if recovered == "" {
		t.Fatal("no CATIA file was placed before the crash")
	}
	src := filepath.Join(r.archive, filepath.FromSlash(recovered))
	description := src + ".md"
	if recovered == "cad/deep/fixture.CATPart" {
		description = src + ".arxgo.md"
	}
	if got := string(mustRead(t, description)); !strings.Contains(got, "\ncatia: ") || exists(src) {
		t.Fatalf("recovered description:\n%s", got)
	}
	records, err := state.ReadWALRecords(filepath.Join(state.StateDir(r.archive), "runs", prev, state.WALFile))
	if err != nil {
		t.Fatal(err)
	}
	var described *state.CatiaSummary
	for _, rec := range records {
		if rec.Step == state.StepDescribed {
			described = rec.Catia
		}
	}
	if described == nil || described.Kind == "" {
		t.Fatalf("recovered described record has no catia object: %+v", records)
	}
	for _, row := range readVideoCSV(t, filepath.Join(r.archive, scanner.VideoRegistryName))[1:] {
		if row[0] != "media/clip.mp4" {
			t.Errorf("video registry row %q", row)
		}
	}
	if !exists(filepath.Join(r.video, "media", "clip.mp4")) || exists(filepath.Join(r.video, "cad")) || exists(filepath.Join(r.video, "fixture-product.CATProduct")) {
		t.Error("video archive holds the wrong payload")
	}

	cfg, c = catiaSplitConfig(r, "auto")
	cfg.Lock.PID = 300
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("CATIA rerun = %+v", res)
	}
	checkCatiaMoved(t, r, files)
	for _, row := range readCatiaRegistry(t, r.archive) {
		if row.Status != report.StatusMoved || row.Kind == "" {
			t.Errorf("row %+v", row)
		}
	}
	if exists(filepath.Join(r.catia, scanner.VideoRegistryName)) || exists(filepath.Join(r.video, scanner.CatiaRegistryName)) {
		t.Fatal("a mirror root received the other payload's registry")
	}
}
