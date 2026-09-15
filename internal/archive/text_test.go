package archive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/catia"
	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

func catiaTextConfig(r catiaRoots, mode string) (Config, SplitConfig) {
	cfg, c := catiaSplitConfig(r, mode)
	c.CatiaText = true
	return cfg, c
}

func textSidecar(archive, rel string) string {
	return filepath.Join(archive, filepath.FromSlash(rel)) + ".text.md"
}

func TestCatiaTextSidecarsMatchContractAndRerunIsNoop(t *testing.T) {
	r, files := catiaFixture(t)
	cfg, c := catiaTextConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	checkCatiaMoved(t, r, files)
	product := string(mustRead(t, textSidecar(r.archive, "fixture-product.CATProduct")))
	if !strings.HasPrefix(product, "arxgo-text: fixture-product.CATProduct\n") ||
		!strings.Contains(product, "\ntruncated: false\n") ||
		!strings.Contains(product, "\nproperties:\n- release: V5R30 SP5\n") ||
		!strings.Contains(product, "- fixture-part.CATPart\n") ||
		!strings.Contains(product, "- fixture-sub.CATProduct\n") {
		t.Fatalf("product sidecar:\n%s", product)
	}
	drawing := "cad/deep/чертеж-fixture.CATDrawing"
	if got := string(mustRead(t, textSidecar(r.archive, drawing))); !strings.HasPrefix(got, "arxgo-text: "+drawing+"\n") {
		t.Fatalf("unicode sidecar:\n%s", got)
	}
	for rel := range files {
		if exists(fsops.PartPath(textSidecar(r.archive, rel))) {
			t.Errorf("%s: part file remains", rel)
		}
	}
	var found string
	for _, row := range readCatiaRegistry(t, r.archive) {
		if row.TextRelPath == "" || !strings.HasSuffix(row.TextRelPath, ".text.md") {
			t.Errorf("text_rel_path %q for %s", row.TextRelPath, row.RelPath)
		}
		if row.RelPath == "fixture-product.CATProduct" {
			found = row.TextRelPath
		}
	}
	if found != "fixture-product.CATProduct.text.md" {
		t.Fatalf("product text_rel_path %q", found)
	}
	rep := readReport(t, r.archive, res.RunID)
	if rep.Counters.TextsDone != int64(len(files)) || rep.Counters.TextsFailed != 0 || rep.Counters.CatiaDone != int64(len(files)) {
		t.Fatalf("counters %+v", rep.Counters)
	}

	before := mustRead(t, textSidecar(r.archive, "fixture-product.CATProduct"))
	cfg, c = catiaTextConfig(r, "auto")
	res = runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("rerun = %+v", res)
	}
	rep = readReport(t, r.archive, res.RunID)
	if rep.Counters.CatiaDone != 0 || rep.Counters.TextsDone != 0 {
		t.Fatalf("rerun counters %+v", rep.Counters)
	}
	if !bytes.Equal(before, mustRead(t, textSidecar(r.archive, "fixture-product.CATProduct"))) {
		t.Fatal("rerun rewrote a sidecar")
	}
}

func TestCatiaTextSidecarKeepsForeignName(t *testing.T) {
	r, _ := catiaFixture(t)
	foreign := textSidecar(r.archive, "fixture-product.CATProduct")
	if err := os.WriteFile(foreign, []byte("operator notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if got := string(mustRead(t, foreign)); got != "operator notes\n" {
		t.Fatalf("foreign sidecar changed: %q", got)
	}
	owned := filepath.Join(r.archive, "fixture-product.CATProduct.arxgo.text.md")
	if got := string(mustRead(t, owned)); !strings.HasPrefix(got, "arxgo-text: fixture-product.CATProduct\n") {
		t.Fatalf("fallback sidecar:\n%s", got)
	}
	for _, row := range readCatiaRegistry(t, r.archive) {
		if row.RelPath == "fixture-product.CATProduct" && row.TextRelPath != "fixture-product.CATProduct.arxgo.text.md" {
			t.Fatalf("text_rel_path %q", row.TextRelPath)
		}
	}
}

func TestCatiaTextFailureLeavesFileMoved(t *testing.T) {
	r, files := catiaFixture(t)
	old := extractCatia
	extractCatia = func(ctx context.Context, path string) catia.Info {
		info := catia.ExtractPath(ctx, path)
		info.TextFailed = true
		info.ErrorKind = catia.ErrorKindRead
		info.Err = errors.New("injected")
		return info
	}
	t.Cleanup(func() { extractCatia = old })
	cfg, c := catiaTextConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusPartial {
		t.Fatalf("split = %+v", res)
	}
	checkCatiaMoved(t, r, files)
	for rel := range files {
		if exists(textSidecar(r.archive, rel)) {
			t.Errorf("%s: sidecar written after extraction failure", rel)
		}
	}
	rep := readReport(t, r.archive, res.RunID)
	if rep.Counters.TextsFailed != int64(len(files)) || rep.Counters.CatiaDone != int64(len(files)) ||
		rep.Counters.CatiaFailed != 0 || len(rep.Issues) == 0 {
		t.Fatalf("counters %+v issues %+v", rep.Counters, rep.Issues)
	}

	extractCatia = old
	cfg, c = catiaTextConfig(r, "auto")
	if res = runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("catch-up = %+v", res)
	}
	if !exists(textSidecar(r.archive, "fixture-product.CATProduct")) {
		t.Fatal("catch-up did not write the missing sidecar")
	}
}

func TestCatiaTextCrashRetriesWithoutPartFile(t *testing.T) {
	for _, point := range []string{"wal:text_begin", "fs:text_part", "fs:text_sidecar"} {
		t.Run(point, func(t *testing.T) {
			r, files := catiaFixture(t)
			fired := false
			crash := func(p string) error {
				if p == point && !fired {
					fired = true
					return errors.New("injected text crash")
				}
				return nil
			}
			cfg, c := catiaTextConfig(r, "auto")
			cfg.Crash = crash
			attachRecoverer(&cfg, c, crash)
			if res := runSplit(t, cfg, c); res.Status != StatusFailed {
				t.Fatalf("crash = %+v", res)
			}
			cfg, c = catiaTextConfig(r, "auto")
			cfg.Lock.PID = 200
			if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("resume = %+v", res)
			}
			checkCatiaMoved(t, r, files)
			for rel := range files {
				if !exists(textSidecar(r.archive, rel)) && !exists(filepath.Join(r.archive, filepath.FromSlash(rel))+".arxgo.text.md") {
					t.Errorf("missing sidecar for %s", rel)
				}
				if exists(fsops.PartPath(textSidecar(r.archive, rel))) {
					t.Errorf("part file remains for %s", rel)
				}
			}
		})
	}
}

func TestCatiaTextRegistryUsesLastDoneSidecar(t *testing.T) {
	r, _ := catiaFixture(t)
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	rel := "fixture-product.CATProduct"
	primary := textSidecar(r.archive, rel)
	if err := os.WriteFile(primary, []byte("operator notes after split\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c = catiaTextConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("catch-up = %+v", res)
	}
	fallback := filepath.Join(r.archive, rel+".arxgo.text.md")
	if !exists(fallback) {
		t.Fatal("expected fallback sidecar after the primary was replaced")
	}
	if got := string(mustRead(t, primary)); got != "operator notes after split\n" {
		t.Fatalf("foreign primary changed: %q", got)
	}
	var textRel string
	for _, row := range readCatiaRegistry(t, r.archive) {
		if row.RelPath == rel {
			textRel = row.TextRelPath
		}
	}
	if textRel != rel+".arxgo.text.md" {
		t.Fatalf("text_rel_path = %q, want last text_done fallback", textRel)
	}
	rep := readReport(t, r.archive, res.RunID)
	if rep.Counters.CatiaDone != 0 || rep.Counters.TextsDone != 1 {
		t.Fatalf("catch-up counters %+v", rep.Counters)
	}
}

func TestCatiaSplitThenTextCatchUpMovesNothing(t *testing.T) {
	r, files := catiaFixture(t)
	cfg, c := catiaSplitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if exists(textSidecar(r.archive, "fixture-product.CATProduct")) {
		t.Fatal("sidecar written without --catia-text")
	}
	cfg, c = catiaTextConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("text catch-up = %+v", res)
	}
	rep := readReport(t, r.archive, res.RunID)
	if rep.Counters.CatiaDone != 0 || rep.Counters.TextsDone != int64(len(files)) {
		t.Fatalf("catch-up counters %+v", rep.Counters)
	}
	if !exists(textSidecar(r.archive, "fixture-product.CATProduct")) {
		t.Fatal("catch-up did not write sidecars")
	}
	checkCatiaMoved(t, r, files)
}

func TestCanceledTextSidecarIsRetriedNotFailed(t *testing.T) {
	r, _ := catiaFixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	cfg, c := catiaTextConfig(r, "auto")
	cfg.Crash = func(point string) error {
		if point == "wal:text_begin" {
			cancel()
		}
		return nil
	}
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	res := s.Finish(ctx, Split(ctx, s, c))
	if res.Status != StatusInterrupted {
		t.Fatalf("canceled split: %+v", res)
	}
	recs, err := state.ReadWALRecords(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.WALFile))
	if err != nil {
		t.Fatal(err)
	}
	for _, rec := range recs {
		if rec.Step == state.StepTextFailed {
			t.Fatalf("cancellation recorded text_failed: %+v", rec)
		}
	}
	cfg.Crash = nil
	if got := runSplit(t, cfg, c); got.Status != StatusCompleted || !exists(textSidecar(r.archive, "fixture-product.CATProduct")) {
		t.Fatalf("resume after cancel: %+v", got)
	}
}

func TestBroken3DXMLTextFailureDoesNotRollBackMove(t *testing.T) {
	r, files := catiaFixture(t)
	rel := "cad/view.3dxml"
	if err := os.WriteFile(filepath.Join(r.archive, filepath.FromSlash(rel)), []byte("<<<"), 0o644); err != nil {
		t.Fatal(err)
	}
	files[rel] = []byte("<<<")
	cfg, c := catiaTextConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusPartial {
		t.Fatalf("split = %+v", res)
	}
	if exists(filepath.Join(r.archive, filepath.FromSlash(rel))) || !exists(filepath.Join(r.catia, filepath.FromSlash(rel))) {
		t.Fatal("broken 3dxml was not moved")
	}
	if exists(textSidecar(r.archive, rel)) {
		t.Fatal("sidecar written for a failed 3dxml extract")
	}
	rep := readReport(t, r.archive, res.RunID)
	if rep.Counters.TextsFailed < 1 || rep.Counters.CatiaDone != int64(len(files)) {
		t.Fatalf("counters %+v", rep.Counters)
	}
}

// Report counters are cumulative for a resumed run. A text sidecar whose text_done is durable but
// whose process died before the counter was updated or checkpointed still counts (regression: a
// killed and resumed CATIA split reported fewer texts_done than sidecars written).
func TestResumedSplitCountsDurableTextDone(t *testing.T) {
	r, files := catiaFixture(t)
	fired := false
	crash := func(p string) error {
		if p == "wal:text_done" && !fired {
			fired = true
			return errors.New("injected crash after text_done")
		}
		return nil
	}
	cfg, c := catiaTextConfig(r, "auto")
	cfg.Crash = crash
	attachRecoverer(&cfg, c, crash)
	res := runSplit(t, cfg, c)
	if res.Status != StatusFailed || !fired {
		t.Fatalf("crash = %+v", res)
	}
	cfg, c = catiaTextConfig(r, "auto")
	cfg.Lock.PID = 200
	resumed := runSplit(t, cfg, c)
	if resumed.Status != StatusCompleted || resumed.RunID != res.RunID {
		t.Fatalf("resume = %+v (crashed run %s)", resumed, res.RunID)
	}
	if rep := readReport(t, r.archive, resumed.RunID); rep.Counters.TextsDone != int64(len(files)) {
		t.Fatalf("texts_done = %d, want %d", rep.Counters.TextsDone, len(files))
	}
}
