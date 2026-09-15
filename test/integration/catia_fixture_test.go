//go:build integration

package integration

import (
	"archive/zip"
	"bytes"
	"encoding/binary"
	"fmt"
	"math/rand"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

// genCatia describes the synthetic CATIA files added to a generated archive. Names, properties and
// component names are invented; nothing is copied from an experimental tree.
type genCatia struct {
	files     map[string]string // rel_path -> expected catia_kind
	foreignMD string            // a human <catia>.md split must not overwrite
	foreignTx string            // a human <catia>.text.md the sidecar must not overwrite
	product   string            // a V5 product with a known release and component count
}

func (g *genArchive) isCatia(rel string) bool {
	if g.catia == nil {
		return false
	}
	_, ok := g.catia.files[rel]
	return ok
}

// addCatia adds CATIA files of every kind at the root and at deep levels, with Unicode and space
// names, varied extension case, V5 payloads large enough for kills to land inside transactions, and
// collisions with foreign Markdown at the description and sidecar paths.
func (g *genArchive) addCatia(t *testing.T, rng *rand.Rand) {
	t.Helper()
	c := &genCatia{files: map[string]string{}}
	g.catia = c
	dirs := []string{"", "cad/2024", "cad/сборка/узел", "with space/cad, parts", "old/deep/er/cad"}
	kinds := []struct{ ext, kind string }{
		{".CATPart", "CATPart"}, {".CATProduct", "CATProduct"}, {".CATDrawing", "CATDrawing"},
		{".cgr", "cgr"}, {".3dxml", "3dxml"}, {".catpart", "CATPart"}, {".CGR", "cgr"},
	}
	n := 0
	for _, dir := range dirs {
		for i, k := range kinds {
			if (i+len(dir))%2 == 1 && dir != "" {
				continue
			}
			name := fmt.Sprintf("invented-%02d%s", n, k.ext)
			rel := path.Join(dir, name)
			var data []byte
			switch {
			case k.kind == "3dxml" && n%2 == 0:
				data = zip3DXML(t, n)
			case k.kind == "3dxml":
				data = []byte(xml3DXML(n))
			case k.kind == "cgr":
				data = append([]byte("CGR invented geometry "), randomBytes(rng, 64*1024+rng.Intn(256*1024))...)
			default:
				data = v5Bytes(name, 28+n%4, n%3, rng, 256*1024+rng.Intn(1536*1024))
			}
			g.write(t, rel, data)
			c.files[rel] = k.kind
			n++
		}
	}
	c.product = "cad/2024/invented-assembly.CATProduct"
	g.write(t, c.product, v5Bytes("invented-assembly.CATProduct", 30, 5, rng, 2*1024*1024))
	c.files[c.product] = "CATProduct"
	c.foreignMD = "cad/2024/invented-00.CATPart.md"
	c.foreignTx = "cad/2024/invented-00.CATPart.text.md"
	if _, ok := c.files["cad/2024/invented-00.CATPart"]; !ok {
		rel := "cad/2024/invented-00.CATPart"
		g.write(t, rel, v5Bytes("invented-00.CATPart", 29, 1, rng, 300*1024))
		c.files[rel] = "CATPart"
	}
	g.write(t, c.foreignMD, []byte("operator notes about a part\n"))
	g.write(t, c.foreignTx, []byte("operator text notes\n"))
}

// v5Bytes is a synthetic V5 document: magic, a LastSaveVersion property, a component window naming
// the file itself and two invented components, then seeded random payload.
func v5Bytes(self string, release, sp int, rng *rand.Rand, payload int) []byte {
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
	prop("LastSaveVersion", fmt.Sprintf("<Version>5/<Version><Release>%d/<Release><ServicePack>%d/<ServicePack>", release, sp))
	b.WriteString("CATOctetArray")
	for _, name := range []string{self, "invented-bolt.CATPart", "invented-frame.CATProduct"} {
		b.WriteString("\x01;\x01\x04File\x00C:\\cad\\" + name + "Z\"")
	}
	b.WriteString("\x08FINJPL invented assembly note")
	b.Write(randomBytes(rng, payload))
	return b.Bytes()
}

func xml3DXML(n int) string {
	return fmt.Sprintf(`<?xml version="1.0" encoding="utf-8"?><Model_3dxml><Header><SchemaVersion>4.%d</SchemaVersion>`+
		`<Title>invented model %d</Title></Header><ProductStructure><Reference3D name="InventedRoot%d"/>`+
		`<ReferenceRep associatedFile="urn:3DXML:invented-rep-%d.3dxml"/></ProductStructure></Model_3dxml>`, n%4, n, n, n)
}

func zip3DXML(t *testing.T, n int) []byte {
	t.Helper()
	var buf bytes.Buffer
	zw := zip.NewWriter(&buf)
	for _, m := range []struct{ name, body string }{
		{"Manifest.xml", "<Manifest><Root>main.3dxml</Root></Manifest>"},
		{"main.3dxml", xml3DXML(n)},
	} {
		w, err := zw.Create(m.name)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := w.Write([]byte(m.body)); err != nil {
			t.Fatal(err)
		}
	}
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

// checkCatiaSplitOutputs validates a completed CATIA split with text sidecars: CATIA files only in
// the CATIA archive with their original bytes, owned descriptions with a catia line, owned sidecars,
// identical arxgo-catia.csv copies naming exactly the CATIA files, and foreign Markdown untouched.
func checkCatiaSplitOutputs(t *testing.T, g *genArchive, catia string, before manifest) {
	t.Helper()
	c := g.catia
	data := readFile(t, filepath.Join(g.root, scanner.CatiaRegistryName))
	if !bytes.Equal(data, readFile(t, filepath.Join(catia, scanner.CatiaRegistryName))) {
		t.Error("arxgo-catia.csv differs between the archive and the CATIA archive")
	}
	rows, err := report.LoadCatiaCSV(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("CATIA registry: %v", err)
	}
	if len(rows) != len(c.files) {
		t.Errorf("CATIA registry has %d rows, want %d", len(rows), len(c.files))
	}
	for _, row := range rows {
		rel := row.RelPath
		want := before[rel]
		kind, ok := c.files[rel]
		switch {
		case !ok:
			t.Errorf("CATIA registry row for non-CATIA %s", rel)
			continue
		case row.Status != report.StatusMoved || row.Transfer != "copy":
			t.Errorf("%s: status=%s transfer=%s", rel, row.Status, row.Transfer)
		case row.FileSize != want.Size || row.SHA256 != want.SHA256 || row.Kind != kind:
			t.Errorf("%s: size=%d sha256=%s kind=%s, want %d %s %s", rel, row.FileSize, row.SHA256, row.Kind, want.Size, want.SHA256, kind)
		case !runIDPattern.MatchString(row.RunID) || row.TextRelPath == "":
			t.Errorf("%s: run_id=%q text_rel_path=%q", rel, row.RunID, row.TextRelPath)
		}
		if rel == c.product && (row.Format != "V5_CFV2" || row.Release != "V5R30 SP5" || row.Components != 2) {
			t.Errorf("%s: format=%s release=%s components=%d", rel, row.Format, row.Release, row.Components)
		}
		if _, err := os.Lstat(filepath.Join(g.root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s still in the archive after CATIA split (%v)", rel, err)
		}
		if got, err := fileSHA256(filepath.Join(catia, filepath.FromSlash(rel))); err != nil || got != want.SHA256 {
			t.Errorf("%s in the CATIA archive: sha256 %s (%v), want %s", rel, got, err, want.SHA256)
		}
		wantDescription, wantSidecar := rel+".md", rel+".text.md"
		if wantDescription == c.foreignMD {
			wantDescription, wantSidecar = rel+".arxgo.md", rel+".arxgo.text.md"
		}
		if row.DescriptionRelPath != wantDescription || row.TextRelPath != wantSidecar {
			t.Errorf("%s: description %q sidecar %q, want %q %q", rel, row.DescriptionRelPath, row.TextRelPath, wantDescription, wantSidecar)
		}
		description, err := report.ReadDescriptionFile(filepath.Join(g.root, filepath.FromSlash(wantDescription)))
		if err != nil || description[report.DescriptionMarker] != rel || !strings.HasPrefix(description["catia"], kind+" | ") ||
			description["video"] != "" || description["sha256"] != want.SHA256 {
			t.Errorf("%s: description %v (%v)", rel, description, err)
		}
		sidecar := readFile(t, filepath.Join(g.root, filepath.FromSlash(wantSidecar)))
		if !bytes.HasPrefix(sidecar, []byte("arxgo-text: "+rel+"\n")) {
			t.Errorf("%s: sidecar starts %q", rel, sidecar[:min(len(sidecar), 80)])
		}
	}
	for rel, body := range map[string]string{c.foreignMD: "operator notes about a part\n", c.foreignTx: "operator text notes\n"} {
		if got := readFile(t, filepath.Join(g.root, filepath.FromSlash(rel))); string(got) != body {
			t.Errorf("foreign %s was changed: %q", rel, got)
		}
	}
}

// checkMirrorPayloads fails when a mirror root holds a file of the other payload or the other
// payload's registry.
func checkMirrorPayloads(t *testing.T, g *genArchive, video, catia string) {
	t.Helper()
	for rel, e := range takeManifest(t, video) {
		if !e.Dir && !g.videos[rel] {
			t.Errorf("video archive holds non-video %s", rel)
		}
	}
	for rel, e := range takeManifest(t, catia) {
		if !e.Dir && !g.isCatia(rel) {
			t.Errorf("CATIA archive holds non-CATIA %s", rel)
		}
	}
	for root, name := range map[string]string{video: scanner.CatiaRegistryName, catia: scanner.VideoRegistryName} {
		if matches, _ := filepath.Glob(filepath.Join(root, strings.TrimSuffix(name, ".csv")+"*")); len(matches) > 0 {
			t.Errorf("%s holds the other payload's registry %v", root, matches)
		}
	}
	rows, err := report.LoadVideoFile(filepath.Join(g.root, scanner.VideoRegistryName))
	if err != nil {
		t.Fatalf("video registry: %v", err)
	}
	for _, row := range rows {
		if g.isCatia(row.RelPath) {
			t.Errorf("video registry row for CATIA file %s", row.RelPath)
		}
	}
}

// checkCatiaRestoreOutputs validates a completed CATIA restore with --descriptions delete: both
// registry copies retired with every row restored and the CATIA archive empty.
func checkCatiaRestoreOutputs(t *testing.T, g *genArchive, catia, runID string) {
	t.Helper()
	retired := "arxgo-catia.restored-" + runID + ".csv"
	data := readFile(t, filepath.Join(g.root, retired))
	if !bytes.Equal(data, readFile(t, filepath.Join(catia, retired))) {
		t.Errorf("%s differs between the roots", retired)
	}
	rows, err := report.LoadCatiaCSV(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("retired CATIA registry: %v", err)
	}
	if len(rows) != len(g.catia.files) {
		t.Errorf("retired CATIA registry has %d rows, want %d", len(rows), len(g.catia.files))
	}
	for _, row := range rows {
		if row.Status != report.StatusRestored || !runIDPattern.MatchString(row.RunID) || row.TextRelPath != "" {
			t.Errorf("%s: status=%s run_id=%s text_rel_path=%q", row.RelPath, row.Status, row.RunID, row.TextRelPath)
		}
	}
	for _, root := range []string{g.root, catia} {
		if _, err := os.Stat(filepath.Join(root, scanner.CatiaRegistryName)); !os.IsNotExist(err) {
			t.Errorf("live arxgo-catia.csv still in %s (%v)", root, err)
		}
	}
	if left := takeManifest(t, catia); len(left) != 0 {
		t.Errorf("CATIA archive not empty after restore: %v", left)
	}
}
