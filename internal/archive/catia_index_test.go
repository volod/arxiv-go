package archive

import (
	"bytes"
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// buildIndex writes the CATIA text index of archive to out and returns the document and the log.
func buildIndex(t *testing.T, archive, out string) (string, *recorder) {
	t.Helper()
	rec := &recorder{}
	cfg := CatiaIndexConfig{Archive: archive, Out: out,
		Lock: state.LockOptions{Host: "index-test", PID: 900, Alive: func(int) bool { return false }}}
	if st := CatiaIndex(context.Background(), cfg, slog.New(rec)); st != StatusCompleted {
		t.Fatalf("CatiaIndex = %v; log %v", st, rec.records)
	}
	return string(mustRead(t, out)), rec
}

// indexSections returns the section headings of an index, without the Missing text section.
func indexSections(doc string) []string {
	var out []string
	for _, line := range strings.Split(doc, "\n") {
		if strings.HasPrefix(line, "## ") && line != "## Missing text" {
			out = append(out, strings.TrimPrefix(line, "## "))
		}
	}
	return out
}

// sectionOf returns the lines of the section of rel, up to the next heading.
func sectionOf(t *testing.T, doc, rel string) string {
	t.Helper()
	_, after, ok := strings.Cut(doc, "\n## "+rel+"\n\n")
	if !ok {
		t.Fatalf("no section %s in\n%s", rel, doc)
	}
	body, _, _ := strings.Cut(after, "\n## ")
	return body
}

func movedRows(t *testing.T, root string) int {
	t.Helper()
	n := 0
	for _, row := range readCatiaRegistry(t, root) {
		if row.Status == report.StatusMoved {
			n++
		}
	}
	return n
}

func TestCatiaIndexCoversMovedFilesAndListsMissingText(t *testing.T) {
	r, files := catiaFixture(t)
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if err := os.Remove(textSidecar(r.archive, "cad/view.3dxml")); err != nil {
		t.Fatal(err)
	}
	before := treeState(t, r.archive)
	runs, _ := state.ListRunIDs(r.archive)

	out := filepath.Join(r.archive, scanner.CatiaIndexName)
	doc, rec := buildIndex(t, r.archive, out)

	want := []string{"cad/deep/fixture.CATPart", "cad/deep/Р-fixture.CATDrawing", "cad/view.3dxml", "fixture-product.CATProduct"}
	if got := indexSections(doc); strings.Join(got, "|") != strings.Join(want, "|") || len(got) != movedRows(t, r.archive) || len(got) != len(files) {
		t.Fatalf("sections %q, want walk order %q and %d moved rows", got, want, movedRows(t, r.archive))
	}
	header := "# arxgo CATIA text index\n\narchive: " + r.archive + "\ncatia_archive: " + r.catia + "\nhistory_at: "
	if !strings.HasPrefix(doc, header) || !strings.Contains(doc, "\nfiles: 4\ncomponents: 2\nmissing_text: 1\n") {
		t.Fatalf("header:\n%s", doc)
	}
	if n := strings.Count(doc, "cad/view.3dxml\n"); n != 2 || !strings.HasSuffix(doc, "\n## Missing text\n\n- missing: cad/view.3dxml\n") {
		t.Fatalf("missing sidecar not listed exactly once under Missing text (%d mentions):\n%s", n, doc)
	}
	if len(rec.messages("CATIA files without text sidecar listed under Missing text")) != 1 {
		t.Fatalf("no warning for missing text: %v", rec.records)
	}

	product := sectionOf(t, doc, "fixture-product.CATProduct")
	description, _ := headerFields(t, filepath.Join(r.archive, "fixture-product.CATProduct.md"))
	for _, key := range []string{"file_size", "file_mime", "sha256", "modified", "catia", "moved_to"} {
		if !strings.Contains(product, "\n"+key+": "+description[key]+"\n") {
			t.Errorf("product %s differs from description %q:\n%s", key, description[key], product)
		}
	}
	if !strings.HasPrefix(product, "file_name: fixture-product.CATProduct\n") ||
		!strings.Contains(product, "\ndescription: fixture-product.CATProduct.md\ntext: fixture-product.CATProduct.text.md\ntruncated: false\n") ||
		!strings.HasSuffix(product, "\nproperties:\n- release: V5R30 SP5\n- part_number: FIXTURE-PRODUCT\n- revision: B\n"+
			"- description: invented assembly\ncomponents:\n- fixture-part.CATPart\n- fixture-sub.CATProduct\n"+
			"notes:\n- \"Invented requirement one.\\nInvented requirement two.\"\n") ||
		strings.Contains(product, "moved_at:") || strings.Contains(product, "extracted_at:") {
		t.Fatalf("product section:\n%s", product)
	}
	sidecar := string(mustRead(t, textSidecar(r.archive, "fixture-product.CATProduct")))
	if _, blocks, _ := strings.Cut(sidecar, "\ntruncated: false\n"); !strings.HasSuffix(product, "\ntruncated: false\n"+blocks) {
		t.Fatalf("index blocks differ from the sidecar:\n%s\nsidecar:\n%s", product, sidecar)
	}
	if deep := sectionOf(t, doc, "cad/deep/fixture.CATPart"); !strings.Contains(deep, "\ndescription: cad/deep/fixture.CATPart.arxgo.md\n") {
		t.Fatalf("fallback description not read:\n%s", deep)
	}
	if view := sectionOf(t, doc, "cad/view.3dxml"); strings.Contains(view, "text:") || !strings.Contains(view, "\ncatia: 3dxml | xml | 3DXML 4.3 | 0 components\n") {
		t.Fatalf("section without sidecar:\n%s", view)
	}

	sameTree(t, "archive after index", treeState(t, r.archive, scanner.CatiaIndexName), before)
	if after, _ := state.ListRunIDs(r.archive); len(after) != len(runs) || exists(state.LockPath(r.archive)) {
		t.Fatalf("index started a run or left a lock: %v -> %v", runs, after)
	}

	again, _ := buildIndex(t, r.archive, out)
	if again != doc {
		t.Fatal("rerun is not byte-identical")
	}

	cfg, c = catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("catch-up split = %+v", res)
	}
	if !exists(out) {
		t.Fatal("a split removed the index")
	}
	registry, err := report.LoadRegistry(filepath.Join(r.archive, "arxgo-registry.csv"))
	if err != nil {
		t.Fatal(err)
	}
	for _, row := range registry {
		if row.RelPath == scanner.CatiaIndexName {
			t.Fatal("the reserved index is a file registry row")
		}
	}
	if doc, _ = buildIndex(t, r.archive, out); strings.Contains(doc, "Missing text") || !strings.Contains(doc, "\nmissing_text: 0\n") {
		t.Fatalf("index after catch-up:\n%s", doc)
	}
}

func TestCatiaIndexReadsRecordedOwnershipNotNames(t *testing.T) {
	r, _ := catiaFixture(t)
	product := filepath.Join(r.archive, "fixture-product.CATProduct")
	if err := os.WriteFile(product+".text.md", []byte("operator notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	// A foreign file at the recorded sidecar path and a removed description: the section keeps the
	// identity the sidecar repeated, and the foreign file is listed, not read.
	if err := os.WriteFile(textSidecar(r.archive, "cad/view.3dxml"), []byte("arxgo-text: other.3dxml\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(r.archive, "cad", "deep", "Р-fixture.CATDrawing.md")); err != nil {
		t.Fatal(err)
	}
	doc, rec := buildIndex(t, r.archive, filepath.Join(t.TempDir(), "index.md"))
	if s := sectionOf(t, doc, "fixture-product.CATProduct"); !strings.Contains(s, "\ntext: fixture-product.CATProduct.arxgo.text.md\n") ||
		strings.Contains(doc, "operator notes") {
		t.Fatalf("fallback sidecar name:\n%s", s)
	}
	drawing := sectionOf(t, doc, "cad/deep/Р-fixture.CATDrawing")
	if !strings.Contains(drawing, "\ncatia: CATDrawing | V5_CFV2 | unknown | 0 components\n") || strings.Contains(drawing, "description:") {
		t.Fatalf("identity from sidecar:\n%s", drawing)
	}
	if len(rec.messages("CATIA index: owned description not found")) != 1 {
		t.Fatalf("missing description not logged: %v", rec.records)
	}
	if !strings.HasSuffix(doc, "\n## Missing text\n\n- foreign: cad/view.3dxml\n") {
		t.Fatalf("foreign sidecar:\n%s", doc)
	}
}

func TestCatiaIndexWithoutTextOrAfterRestore(t *testing.T) {
	r, _ := catiaFixture(t)
	out := filepath.Join(t.TempDir(), "index.md")
	if doc, _ := buildIndex(t, r.archive, out); doc != "# arxgo CATIA text index\n\narchive: "+r.archive+"\nfiles: 0\ncomponents: 0\nmissing_text: 0\n" {
		t.Fatalf("index without history:\n%s", doc)
	}
	cfg, c := catiaSplitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	doc, _ := buildIndex(t, r.archive, out)
	if !strings.Contains(doc, "\nfiles: 4\ncomponents: 0\nmissing_text: 4\n") || strings.Count(doc, "\n- not_recorded: ") != 4 {
		t.Fatalf("index without --catia-text:\n%s", doc)
	}
	rcfg, rc := catiaRestoreConfig(r, "auto")
	if res := runRestore(t, rcfg, rc); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if doc, _ = buildIndex(t, r.archive, out); !strings.Contains(doc, "\nfiles: 0\n") || len(indexSections(doc)) != 0 {
		t.Fatalf("index after restore:\n%s", doc)
	}
}

func TestCatiaIndexStopsOnLiveLock(t *testing.T) {
	r, _ := catiaFixture(t)
	lock, err := state.AcquireLock(r.archive, state.LockInfo{RunID: "20260916T100000Z-00000001", Op: "split", Role: state.RoleArchive},
		state.LockOptions{Host: "index-test", PID: 700})
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "index.md")
	for _, tc := range []struct {
		name  string
		lock  state.LockOptions
		want  Status
		wrote bool
	}{
		{"live", state.LockOptions{Host: "index-test", PID: 900, Alive: func(p int) bool { return p == 700 }}, StatusLocked, false},
		{"remote", state.LockOptions{Host: "other-host", PID: 900, Alive: func(int) bool { return true }}, StatusLocked, false},
		{"stale", state.LockOptions{Host: "index-test", PID: 900, Alive: func(int) bool { return false }}, StatusCompleted, true},
	} {
		rec := &recorder{}
		st := CatiaIndex(context.Background(), CatiaIndexConfig{Archive: r.archive, Out: out, Lock: tc.lock}, slog.New(rec))
		if st != tc.want || exists(out) != tc.wrote {
			t.Fatalf("%s: status %v, wrote %v; log %v", tc.name, st, exists(out), rec.records)
		}
	}
	if got, err := state.ReadLock(lock.Path()); err != nil || got.PID != 700 {
		t.Fatalf("index changed the lock: %+v %v", got, err)
	}
}

func TestCatiaIndexCanceledWritesNothing(t *testing.T) {
	r, _ := catiaFixture(t)
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	out := filepath.Join(t.TempDir(), "index.md")
	if err := os.WriteFile(out, []byte("previous\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	st := CatiaIndex(ctx, CatiaIndexConfig{Archive: r.archive, Out: out}, slog.New(&recorder{}))
	if st != StatusInterrupted || !bytes.Equal(mustRead(t, out), []byte("previous\n")) {
		t.Fatalf("canceled index: %v, out %q", st, mustRead(t, out))
	}
	if entries, _ := os.ReadDir(filepath.Dir(out)); len(entries) != 1 {
		t.Fatalf("part file left: %v", entries)
	}
}

func TestCatiaIndexFailures(t *testing.T) {
	r, _ := catiaFixture(t)
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	rec := &recorder{}
	unwritable := filepath.Join(t.TempDir(), "no-such-dir", "index.md")
	if st := CatiaIndex(context.Background(), CatiaIndexConfig{Archive: r.archive, Out: unwritable}, slog.New(rec)); st != StatusFailed ||
		len(rec.messages("CATIA text index not written")) != 1 {
		t.Fatalf("unwritable output: %v %v", st, rec.records)
	}
	runs, err := state.ListRunIDs(r.archive)
	if err != nil || len(runs) == 0 {
		t.Fatal(runs, err)
	}
	wal := filepath.Join(state.StateDir(r.archive), "runs", runs[0], state.WALFile)
	f, err := os.OpenFile(wal, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	// A text event whose sidecar lies outside the archive is corrupt state, not a missing sidecar.
	_, err = f.WriteString(`{"v":2,"txid":"20260916T100000Z-00000001-999999","seq":999999,"step":"text_begin","ts":"2026-09-16T10:00:00Z","rel_path":"fixture-product.CATProduct","dst":"/elsewhere/x.text.md"}` + "\n")
	f.Close()
	if err != nil {
		t.Fatal(err)
	}
	out := filepath.Join(t.TempDir(), "index.md")
	if st := CatiaIndex(context.Background(), CatiaIndexConfig{Archive: r.archive, Out: out}, slog.New(&recorder{})); st != StatusNeedsOperator || exists(out) {
		t.Fatalf("corrupt history: %v, wrote %v", st, exists(out))
	}
}
