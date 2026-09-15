package archive

import (
	"bytes"
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

// catiaRoots are an archive, a video archive and a CATIA archive, siblings in one temp dir.
type catiaRoots struct {
	roots
	catia string
}

// v5Product is a synthetic V5 document with a LastSaveVersion property and a component window
// naming two invented components and itself.
func v5Product(self string) []byte {
	var b bytes.Buffer
	b.WriteString("V5_CFV2\x00")
	b.Write(make([]byte, 32))
	prop := func(key, val string) {
		b.Write(binary.LittleEndian.AppendUint16(nil, uint16(len(key))))
		b.WriteString(key)
		b.Write(binary.LittleEndian.AppendUint32(nil, 0x0e))
		b.Write(binary.LittleEndian.AppendUint32(nil, uint32(len(val))))
		b.WriteString(val)
	}
	prop("LastSaveVersion", "<Version>5/<Version><Release>30/<Release><ServicePack>5/<ServicePack>")
	b.WriteString("CATOctetArray")
	for _, name := range []string{self, "fixture-part.CATPart", "fixture-sub.CATProduct"} {
		b.WriteString("\x01;\x01\x04File\x00C:\\cad\\" + name + "Z\"")
	}
	b.WriteString("\x08FINJPL trailing assembly note")
	return b.Bytes()
}

// catiaFixture builds a mixed archive: CATIA files at the root and deep, a video, a text file, a
// foreign description at a CATIA description path and an invented Unicode name.
func catiaFixture(t *testing.T) (catiaRoots, map[string][]byte) {
	t.Helper()
	r := catiaRoots{roots: newRoots(t)}
	r.catia = filepath.Join(filepath.Dir(r.archive), "catia")
	if err := os.Mkdir(r.catia, 0o755); err != nil {
		t.Fatal(err)
	}
	files := map[string][]byte{
		"fixture-product.CATProduct":         v5Product("fixture-product.CATProduct"),
		"cad/deep/fixture.CATPart":           []byte("V5_CFV2\x00 small part without markers"),
		"cad/deep/чертеж-fixture.CATDrawing": []byte("V5_CFV2\x00 drawing"),
		"cad/view.3dxml":                     []byte(`<?xml version="1.0"?><Model_3dxml><Header><SchemaVersion>4.3</SchemaVersion></Header></Model_3dxml>`),
	}
	for rel, data := range files {
		writeScanFile(t, r.archive, rel, data)
	}
	writeScanFile(t, r.archive, "cad/deep/fixture.CATPart.md", []byte("operator notes\n"))
	writeScanFile(t, r.archive, "media/clip.mp4", videoFixture)
	writeScanFile(t, r.archive, "notes.txt", []byte("keep"))
	return r, files
}

func catiaSplitConfig(r catiaRoots, mode string) (Config, SplitConfig) {
	cfg, c := splitConfig(r.roots, mode)
	cfg.Payload = Payload{Kind: PayloadCatia, Root: r.catia}
	c.Scan.SkipPaths = []string{r.catia}
	c.Descriptions = NewMarkdownDescription(DescriptionConfig{
		Archive: r.archive, Mirror: r.catia, Registry: c.Scan.Registry,
		Version: "test", Payload: PayloadCatia, Verify: c.Verify,
	})
	attachRecoverer(&cfg, c, nil)
	return cfg, c
}

func readCatiaRegistry(t *testing.T, root string) []report.CatiaRow {
	t.Helper()
	rows, err := report.LoadCatiaFile(filepath.Join(root, scanner.CatiaRegistryName))
	if err != nil || rows == nil {
		t.Fatalf("catia registry in %s: %v", root, err)
	}
	return rows
}

func checkCatiaMoved(t *testing.T, r catiaRoots, files map[string][]byte) {
	t.Helper()
	for rel, data := range files {
		src := filepath.Join(r.archive, filepath.FromSlash(rel))
		if exists(src) {
			t.Errorf("%s: source still exists", rel)
		}
		if got := mustRead(t, filepath.Join(r.catia, filepath.FromSlash(rel))); !bytes.Equal(got, data) {
			t.Errorf("%s: CATIA archive copy differs", rel)
		}
		description := src + ".md"
		if rel == "cad/deep/fixture.CATPart" {
			description = src + ".arxgo.md"
		}
		got := string(mustRead(t, description))
		if !strings.HasPrefix(got, "arxgo: "+rel+"\n") || !strings.Contains(got, "\ncatia: ") ||
			strings.Contains(got, "\nvideo: ") || strings.Contains(got, "\ncreated: ") {
			t.Errorf("%s: description:\n%s", rel, got)
		}
		if exists(fsops.PartPath(filepath.Join(r.catia, filepath.FromSlash(rel)))) {
			t.Errorf("%s: part file remains", rel)
		}
	}
}

func checkVideoUntouched(t *testing.T, r catiaRoots) {
	t.Helper()
	if got := mustRead(t, filepath.Join(r.archive, "media", "clip.mp4")); !bytes.Equal(got, videoFixture) {
		t.Error("video changed")
	}
	if exists(filepath.Join(r.archive, "media", "clip.mp4.md")) {
		t.Error("CATIA split described a video")
	}
	if entries, err := os.ReadDir(r.video); err != nil || len(entries) != 0 {
		t.Errorf("video archive touched: %v %v", entries, err)
	}
	if exists(filepath.Join(r.archive, scanner.VideoRegistryName)) {
		t.Error("CATIA split wrote arxgo-videos.csv")
	}
}

func TestCatiaSplitMovesOnlyCatiaFilesAndRerunIsNoop(t *testing.T) {
	for _, mode := range []string{"same-device", "cross-device"} {
		t.Run(mode, func(t *testing.T) {
			r, files := catiaFixture(t)
			cfg, c := catiaSplitConfig(r, "auto")
			wantTransfer := state.TransferRename
			if mode == "cross-device" {
				cfg.FS = forcedOtherDevice{fsops.System{}}
				wantTransfer = state.TransferCopy
			}
			res := runSplit(t, cfg, c)
			if res.Status != StatusCompleted {
				t.Fatalf("split = %+v", res)
			}
			checkCatiaMoved(t, r, files)
			checkVideoUntouched(t, r)
			if got := string(mustRead(t, filepath.Join(r.archive, "cad", "deep", "fixture.CATPart.md"))); got != "operator notes\n" {
				t.Fatalf("foreign description changed: %q", got)
			}
			product := string(mustRead(t, filepath.Join(r.archive, "fixture-product.CATProduct.md")))
			if !strings.Contains(product, "\ncatia: CATProduct | V5_CFV2 | V5R30 SP5 | 2 components\n") {
				t.Fatalf("product description:\n%s", product)
			}
			rows := readCatiaRegistry(t, r.archive)
			if mirror := readCatiaRegistry(t, r.catia); len(mirror) != len(rows) {
				t.Fatalf("registry copies differ: %d vs %d rows", len(mirror), len(rows))
			}
			if len(rows) != len(files) {
				t.Fatalf("registry rows = %+v", rows)
			}
			for _, row := range rows {
				if row.Status != report.StatusMoved || row.Transfer != wantTransfer || row.Kind == "" || row.MTime == "" ||
					!strings.HasPrefix(row.URL, "file://") || !strings.Contains(row.URL, "/catia/") {
					t.Errorf("row %+v", row)
				}
				if row.RelPath == "fixture-product.CATProduct" &&
					(row.Kind != "CATProduct" || row.Format != "V5_CFV2" || row.Release != "V5R30 SP5" || row.Components != 2) {
					t.Errorf("product row %+v", row)
				}
				if row.RelPath == "cad/deep/fixture.CATPart" && row.DescriptionRelPath != "cad/deep/fixture.CATPart.arxgo.md" {
					t.Errorf("collision description path %q", row.DescriptionRelPath)
				}
			}
			var rep state.Report
			if err := state.ReadJSON(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.ReportFile), &rep); err != nil {
				t.Fatal(err)
			}
			if rep.Counters.CatiaDone != int64(len(files)) || rep.Counters.VideosDone != 0 || rep.Counters.CatiaWritten == 0 ||
				len(rep.Roots) != 2 || rep.Roots[1].Root != r.catia {
				t.Fatalf("report counters %+v roots %+v", rep.Counters, rep.Roots)
			}

			registry := mustRead(t, filepath.Join(r.archive, scanner.CatiaRegistryName))
			cfg, c = catiaSplitConfig(r, "auto")
			res = runSplit(t, cfg, c)
			if res.Status != StatusCompleted {
				t.Fatalf("rerun = %+v", res)
			}
			rep = state.Report{}
			if err := state.ReadJSON(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.ReportFile), &rep); err != nil {
				t.Fatal(err)
			}
			if rep.Counters.CatiaDone != 0 {
				t.Fatalf("rerun moved %d files: %+v resumed %v run %s report %s", rep.Counters.CatiaDone, rep.Counters, rep.Resumed, res.RunID, rep.RunID)
			}
			if got := mustRead(t, filepath.Join(r.archive, scanner.CatiaRegistryName)); !bytes.Equal(got, registry) {
				t.Fatalf("rerun changed the registry:\n%s\nwant\n%s", got, registry)
			}
			checkVideoUntouched(t, r)
		})
	}
}

func TestCatiaSplitConflictIsSkippedAndRegistered(t *testing.T) {
	r, files := catiaFixture(t)
	rel := "cad/view.3dxml"
	writeScanFile(t, r.catia, rel, []byte("different content at the destination"))
	cfg, c := catiaSplitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("split = %+v", res)
	}
	if got := mustRead(t, filepath.Join(r.archive, filepath.FromSlash(rel))); !bytes.Equal(got, files[rel]) {
		t.Fatal("conflicting source changed")
	}
	if exists(filepath.Join(r.archive, filepath.FromSlash(rel)) + ".md") {
		t.Fatal("conflict got a description")
	}
	for _, row := range readCatiaRegistry(t, r.archive) {
		want := report.StatusMoved
		if row.RelPath == rel {
			want = report.StatusConflict
		}
		if row.Status != want {
			t.Errorf("%s status %s, want %s", row.RelPath, row.Status, want)
		}
	}
}

func TestCatiaSplitCrashPointsConverge(t *testing.T) {
	for _, mode := range []string{"auto", "copy"} {
		points := crashtest.RenamePoints
		if mode == "copy" {
			points = crashtest.CopyPoints
		}
		for _, point := range points {
			t.Run(mode+"/"+point, func(t *testing.T) {
				r, files := catiaFixture(t)
				h := &crashtest.Hook{FailAt: point}
				cfg, c := catiaSplitConfig(r, mode)
				cfg.Crash = h.Func()
				attachRecoverer(&cfg, c, h.Func())
				if res := runSplit(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
					t.Fatalf("point %s was not reached: %+v", point, res)
				}
				cfg, c = catiaSplitConfig(r, mode)
				cfg.Lock.PID = 200
				if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
					t.Fatalf("resume = %+v", res)
				}
				checkCatiaMoved(t, r, files)
				checkVideoUntouched(t, r)
				for _, row := range readCatiaRegistry(t, r.archive) {
					if row.Status != report.StatusMoved || row.Kind == "" {
						t.Errorf("row after recovery %+v", row)
					}
				}
			})
		}
	}
}

func TestCorruptCatiaRegistryStopsBeforeMoves(t *testing.T) {
	r, files := catiaFixture(t)
	writeScanFile(t, r.archive, scanner.CatiaRegistryName, []byte("broken\n"))
	cfg, c := catiaSplitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusNeedsOperator {
		t.Fatalf("split = %+v", res)
	}
	for rel := range files {
		if !exists(filepath.Join(r.archive, filepath.FromSlash(rel))) {
			t.Fatalf("%s moved despite a corrupt registry", rel)
		}
	}
}

func TestPreflightPlacesCatiaNeedsOnCatiaArchive(t *testing.T) {
	const gib = 1 << 30
	info := DeviceInfo{Devices: []Device{
		{Roles: []Role{RoleArchive}, Path: "/a", Space: fsops.Space{Total: 100 * gib, Available: 100 * gib}},
		{Roles: []Role{RoleCatiaArchive}, Path: "/c", Space: fsops.Space{Total: 100 * gib, Available: 100 * gib}},
	}}
	req := Plan(Candidates{Count: 2, Bytes: 3 * gib, Largest: 2 * gib}, PreflightOptions{Op: opSplit, Payload: PayloadCatia}, info)
	names := func(d DeviceRequirement) string {
		var out []string
		for _, n := range d.Needs {
			out = append(out, n.Name)
		}
		return strings.Join(out, ",")
	}
	if len(req.Devices) != 2 || names(req.Devices[0]) != "descriptions,catia_registry,wal" ||
		names(req.Devices[1]) != "catia_registry,catia" || req.Devices[1].Required < 3*gib {
		t.Fatalf("requirement = %+v", req.Devices)
	}
}
