package archive

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

func TestCatiaRoundTripRestoresFilesAndDeletesOwnedMarkdown(t *testing.T) {
	for _, mode := range []string{"same-device", "cross-device"} {
		t.Run(mode, func(t *testing.T) {
			var fsys fsops.Ops = fsops.System{}
			if mode == "cross-device" {
				fsys = forcedOtherDevice{fsops.System{}}
			}
			r, files, want := catiaRoundTripFixture(t, fsys)
			videoTree := treeState(t, r.video)
			videoRegistry := mustRead(t, filepath.Join(r.archive, scanner.VideoRegistryName))
			videoDescription := mustRead(t, filepath.Join(r.archive, "media", "clip.mp4.md"))
			for _, owned := range []string{"cad/deep/fixture.CATPart.arxgo.text.md", "fixture-product.CATProduct.text.md"} {
				if !exists(filepath.Join(r.archive, filepath.FromSlash(owned))) {
					t.Fatalf("owned sidecar %s was not written", owned)
				}
			}

			cfg, c := catiaRestoreConfig(r, "auto")
			cfg.FS = fsys
			res := runRestore(t, cfg, c)
			if res.Status != StatusCompleted {
				t.Fatalf("restore = %+v", res)
			}
			sameTree(t, "archive", treeState(t, r.archive, "arxgo-registry.csv", "arxgo-catia.restored-*.csv"), want)
			sameTree(t, "video archive", treeState(t, r.video), videoTree)
			if !bytes.Equal(mustRead(t, filepath.Join(r.archive, scanner.VideoRegistryName)), videoRegistry) ||
				!bytes.Equal(mustRead(t, filepath.Join(r.archive, "media", "clip.mp4.md")), videoDescription) {
				t.Fatal("video registry or description changed")
			}
			if mirror := treeState(t, r.catia); len(mirror) != 1 {
				t.Fatalf("CATIA archive after restore: %v", mirror)
			} else if _, ok := mirror["arxgo-catia.restored-"+res.RunID+".csv"]; !ok {
				t.Fatalf("CATIA archive after restore: %v", mirror)
			}
			if entries, err := os.ReadDir(r.catia); err != nil || len(entries) != 2 {
				t.Fatalf("CATIA archive directories left after restore: %v %v", entries, err)
			}
			for _, root := range []string{r.archive, r.catia} {
				if exists(filepath.Join(root, scanner.CatiaRegistryName)) {
					t.Fatalf("%s: active CATIA registry left after full restore", root)
				}
				rows, err := report.LoadCatiaFile(filepath.Join(root, "arxgo-catia.restored-"+res.RunID+".csv"))
				if err != nil || len(rows) != len(files) {
					t.Fatalf("%s: retired registry %+v %v", root, rows, err)
				}
				for _, row := range rows {
					if row.Status != report.StatusRestored || row.RunID != res.RunID || row.TextRelPath != "" || row.Kind == "" ||
						!strings.Contains(row.URL, "/archive/") {
						t.Errorf("retired row %+v", row)
					}
				}
			}
			rep := readReport(t, r.archive, res.RunID)
			if rep.Counters.CatiaDone != int64(len(files)) || rep.Counters.VideosDone != 0 {
				t.Fatalf("restore counters %+v", rep.Counters)
			}

			cfg, c = catiaRestoreConfig(r, "auto")
			cfg.FS, cfg.Lock.PID = fsys, 300
			if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("rerun = %+v", res)
			} else if rep := readReport(t, r.archive, res.RunID); rep.Counters.CatiaDone != 0 {
				t.Fatalf("rerun counters %+v", rep.Counters)
			}
			sameTree(t, "archive after rerun", treeState(t, r.archive, "arxgo-registry.csv", "arxgo-catia.restored-*.csv"), want)
			sameTree(t, "video archive after rerun", treeState(t, r.video), videoTree)
		})
	}
}

func TestCatiaRestoreKeepDescriptionsKeepsMarkdownAndRegistry(t *testing.T) {
	r, files, _ := catiaRoundTripFixture(t, fsops.System{})
	cfg, c := catiaRestoreConfig(r, "auto")
	c.KeepDescriptions = true
	attachCatiaRestoreRecoverer(&cfg, &c, nil)
	res := runRestore(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	for rel, data := range files {
		src := filepath.Join(r.archive, filepath.FromSlash(rel))
		if !bytes.Equal(mustRead(t, src), data) {
			t.Errorf("%s not restored", rel)
		}
		description, sidecar := src+".md", src+".text.md"
		if rel == "cad/deep/fixture.CATPart" {
			description, sidecar = src+".arxgo.md", src+".arxgo.text.md"
		}
		if !exists(description) || !exists(sidecar) {
			t.Errorf("%s: kept description or sidecar missing", rel)
		}
	}
	rows := readCatiaRegistry(t, r.archive)
	if mirror := readCatiaRegistry(t, r.catia); len(mirror) != len(rows) {
		t.Fatal("registry copies differ")
	}
	for _, row := range rows {
		if row.Status != report.StatusRestored || !strings.HasSuffix(row.TextRelPath, ".text.md") {
			t.Errorf("row %+v", row)
		}
	}

	// The filesystem is the source of truth: a CATIA file back in the mirror while its row says
	// restored is restored again.
	rel := "cad/view.3dxml"
	back := filepath.Join(r.catia, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(back), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(filepath.Join(r.archive, filepath.FromSlash(rel)), back); err != nil {
		t.Fatal(err)
	}
	cfg, c = catiaRestoreConfig(r, "auto")
	c.KeepDescriptions, cfg.Lock.PID = true, 300
	attachCatiaRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("second restore = %+v", res)
	}
	if exists(back) || !bytes.Equal(mustRead(t, filepath.Join(r.archive, filepath.FromSlash(rel))), files[rel]) {
		t.Fatal("restored-row file in the mirror was not restored again")
	}
}

func TestCatiaRestoreKeepsChangedSidecarsAndForeignMarkdown(t *testing.T) {
	r, files, _ := catiaRoundTripFixture(t, fsops.System{})
	sameSize := textSidecar(r.archive, "fixture-product.CATProduct")
	owned := mustRead(t, sameSize)
	foreign := append([]byte("operator-text"), owned[len("operator-text"):]...)
	if err := os.WriteFile(sameSize, foreign, 0o644); err != nil {
		t.Fatal(err)
	}
	grown := textSidecar(r.archive, "cad/view.3dxml")
	grownData := append(mustRead(t, grown), "operator addition\n"...)
	if err := os.WriteFile(grown, grownData, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := catiaRestoreConfig(r, "auto")
	res := runRestore(t, cfg, c)
	if res.Status != StatusPartial {
		t.Fatalf("restore = %+v", res)
	}
	for rel, data := range files {
		if !bytes.Equal(mustRead(t, filepath.Join(r.archive, filepath.FromSlash(rel))), data) {
			t.Errorf("%s not restored", rel)
		}
	}
	if !bytes.Equal(mustRead(t, sameSize), foreign) || !bytes.Equal(mustRead(t, grown), grownData) {
		t.Fatal("restore deleted or altered a changed sidecar")
	}
	if got := string(mustRead(t, filepath.Join(r.archive, "cad", "deep", "fixture.CATPart.md"))); got != "operator notes\n" {
		t.Fatalf("foreign description changed: %q", got)
	}
	if got := string(mustRead(t, filepath.Join(r.archive, "cad", "deep", "fixture.CATPart.text.md"))); got != "foreign text notes\n" {
		t.Fatalf("foreign sidecar changed: %q", got)
	}
	if exists(filepath.Join(r.archive, "cad", "deep", "fixture.CATPart.arxgo.text.md")) {
		t.Fatal("unchanged owned sidecar kept")
	}
	// Kept sidecars keep text_rel_path; with no moved row left the registry is still retired.
	rows, err := report.LoadCatiaFile(filepath.Join(r.archive, "arxgo-catia.restored-"+res.RunID+".csv"))
	if err != nil || len(rows) != len(files) {
		t.Fatalf("retired registry %+v %v", rows, err)
	}
	for _, row := range rows {
		keep := row.RelPath == "fixture-product.CATProduct" || row.RelPath == "cad/view.3dxml"
		if row.Status != report.StatusRestored || (row.TextRelPath != "") != keep {
			t.Errorf("row %+v", row)
		}
	}
	rep := readReport(t, r.archive, res.RunID)
	if rep.Counters.CatiaDone != int64(len(files)) || len(rep.Issues) != 2 {
		t.Fatalf("report counters %+v issues %+v", rep.Counters, rep.Issues)
	}
}

func TestCatiaRestoreDirectoryAndConflictPolicies(t *testing.T) {
	r, files, _ := catiaRoundTripFixture(t, fsops.System{})
	deep := "cad/deep/fixture.CATPart"
	if err := os.RemoveAll(filepath.Join(r.archive, "cad", "deep")); err != nil {
		t.Fatal(err)
	}
	conflict := "fixture-product.CATProduct"
	other := []byte("different product at the destination")
	writeScanFile(t, r.archive, conflict, other)

	cfg, c := catiaRestoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("restore without policies = %+v", res)
	}
	if exists(filepath.Join(r.archive, filepath.FromSlash(deep))) || !bytes.Equal(mustRead(t, filepath.Join(r.archive, conflict)), other) ||
		!exists(filepath.Join(r.catia, filepath.FromSlash(deep))) || !exists(filepath.Join(r.catia, conflict)) {
		t.Fatal("skipped CATIA files were moved or the conflict was replaced")
	}
	if !bytes.Equal(mustRead(t, filepath.Join(r.archive, "cad", "view.3dxml")), files["cad/view.3dxml"]) {
		t.Fatal("unaffected CATIA file not restored")
	}
	for _, row := range readCatiaRegistry(t, r.archive) {
		if want := map[bool]string{true: report.StatusMoved, false: report.StatusRestored}[row.RelPath == deep || strings.HasSuffix(row.RelPath, "чертеж-fixture.CATDrawing") || row.RelPath == conflict]; row.Status != want {
			t.Errorf("%s status %s, want %s", row.RelPath, row.Status, want)
		}
	}

	cfg, c = catiaRestoreConfig(r, "auto")
	c.CreateDirs, c.Overwrite, cfg.Lock.PID = true, true, 300
	attachCatiaRestoreRecoverer(&cfg, &c, nil)
	res := runRestore(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("restore with --create-dirs --overwrite = %+v", res)
	}
	for rel, data := range files {
		if !bytes.Equal(mustRead(t, filepath.Join(r.archive, filepath.FromSlash(rel))), data) {
			t.Errorf("%s not restored", rel)
		}
	}
	if exists(filepath.Join(r.catia, "cad")) {
		t.Fatal("empty CATIA archive directories left")
	}
	if !exists(filepath.Join(r.archive, "arxgo-catia.restored-"+res.RunID+".csv")) {
		t.Fatal("registry not retired after the last moved row was restored")
	}
}

// A file the CATIA registry records as moved is restored even when its name is not a CATIA
// extension.
func TestCatiaRestoreIncludesRegistryRowsWithoutCatiaExtension(t *testing.T) {
	r, _, _ := catiaRoundTripFixture(t, fsops.System{})
	rel := "cad/renamed-part.bin"
	data := []byte("V5_CFV2\x00 renamed part")
	writeScanFile(t, r.catia, rel, data)
	rows := readCatiaRegistry(t, r.archive)
	rows = append(rows, report.CatiaRow{PayloadRow: report.PayloadRow{RelPath: rel, FileName: "renamed-part.bin",
		Status: report.StatusMoved, FileSize: int64(len(data))}})
	var b bytes.Buffer
	if err := report.WriteCatiaCSV(&b, report.MergeCatiaRows(nil, rows)); err != nil {
		t.Fatal(err)
	}
	for _, root := range []string{r.archive, r.catia} {
		writeScanFile(t, root, scanner.CatiaRegistryName, b.Bytes())
	}
	cfg, c := catiaRestoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(r.archive, filepath.FromSlash(rel))), data) {
		t.Fatal("registry-only candidate not restored")
	}
}
