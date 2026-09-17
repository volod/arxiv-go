package archive

import (
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
)

// headerFields reads the leading "key: value" lines of a description or sidecar, unquoted, and
// the order of their keys.
func headerFields(t *testing.T, path string) (map[string]string, []string) {
	t.Helper()
	fields := map[string]string{}
	var keys []string
	for _, line := range strings.Split(string(mustRead(t, path)), "\n") {
		key, value, ok := strings.Cut(line, ": ")
		if !ok || strings.HasPrefix(line, "- ") {
			break
		}
		if strings.HasPrefix(value, `"`) {
			d, err := report.ParseDescription(strings.NewReader("arxgo: " + value + "\n"))
			if err != nil {
				t.Fatalf("%s: %s: %v", path, line, err)
			}
			value = d[report.DescriptionMarker]
		}
		fields[key] = value
		keys = append(keys, key)
	}
	return fields, keys
}

// checkSidecarIdentity asserts that every moved CATIA file's description and sidecar name the
// archive on their second line and share the description's identity values.
func checkSidecarIdentity(t *testing.T, r catiaRoots, files map[string][]byte) {
	t.Helper()
	for rel := range files {
		src := filepath.Join(r.archive, filepath.FromSlash(rel))
		description, sidecar := src+".md", src+".text.md"
		if rel == "cad/deep/fixture.CATPart" {
			description = src + ".arxgo.md"
		}
		d, dKeys := headerFields(t, description)
		s, sKeys := headerFields(t, sidecar)
		if len(dKeys) < 2 || dKeys[1] != "archive" || len(sKeys) < 2 || sKeys[1] != "archive" {
			t.Errorf("%s: archive is not the second line: %v / %v", rel, dKeys, sKeys)
		}
		if d["archive"] != r.archive || s["archive"] != r.archive {
			t.Errorf("%s: archive %q / %q, want %q", rel, d["archive"], s["archive"], r.archive)
		}
		sum, err := hashFile(filepath.Join(r.catia, filepath.FromSlash(rel)))
		if err != nil {
			t.Fatal(err)
		}
		if d["sha256"] != hex.EncodeToString(sum[:]) {
			t.Errorf("%s: description sha256 %q", rel, d["sha256"])
		}
		for _, key := range []string{"file_size", "file_mime", "sha256", "modified", "catia", "moved_to"} {
			if d[key] == "" || s[key] != d[key] {
				t.Errorf("%s: %s sidecar %q, description %q", rel, key, s[key], d[key])
			}
		}
		if _, ok := s["moved_at"]; ok {
			t.Errorf("%s: sidecar repeats moved_at", rel)
		}
		wantDescription, _ := filepath.Rel(r.archive, description)
		if s["file_name"] != filepath.Base(src) || s["description"] != filepath.ToSlash(wantDescription) {
			t.Errorf("%s: file_name %q description %q", rel, s["file_name"], s["description"])
		}
		want := []string{"arxgo-text", "archive", "file_name", "file_size", "file_mime", "sha256", "modified",
			"catia", "moved_to", "description", "extracted_at", "truncated"}
		if strings.Join(sKeys[:min(len(sKeys), len(want))], ",") != strings.Join(want, ",") {
			t.Errorf("%s: sidecar field order %v", rel, sKeys)
		}
	}
}

func TestCatiaTextSidecarRepeatsDescriptionIdentity(t *testing.T) {
	r, files := catiaFixture(t)
	cfg, c := catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	checkSidecarIdentity(t, r, files)

	cfg, rc := catiaRestoreConfig(r, "auto")
	if res := runRestore(t, cfg, rc); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	for rel := range files {
		src := filepath.Join(r.archive, filepath.FromSlash(rel))
		for _, p := range []string{src + ".md", src + ".arxgo.md", src + ".text.md", src + ".arxgo.text.md"} {
			if exists(p) && !(rel == "cad/deep/fixture.CATPart" && p == src+".md") {
				t.Errorf("%s: %s left after restore --descriptions delete", rel, filepath.Base(p))
			}
		}
	}
}

func TestCatiaTextCatchUpReadsEarlierDescription(t *testing.T) {
	r, files := catiaFixture(t)
	moved := filepath.Join(r.archive, "fixture-product.CATProduct")
	for _, name := range []string{".md", ".arxgo.md"} {
		if err := os.WriteFile(moved+name, []byte("operator notes\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	gap := filepath.Join(r.archive, "fixture-product-1.CATProduct.md")
	if err := os.WriteFile(gap, []byte("operator notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := catiaSplitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	// Removing the note before it leaves the WAL-recorded description as the only way to find the
	// owned one: the naming rule would now take the freed name.
	if err := os.Remove(gap); err != nil {
		t.Fatal(err)
	}
	cfg, c = catiaTextConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("catch-up = %+v", res)
	}
	if rep := readReport(t, r.archive, res.RunID); rep.Counters.TextsDone != int64(len(files)) {
		t.Fatalf("counters %+v", rep.Counters)
	}
	s, _ := headerFields(t, moved+".text.md")
	d, _ := headerFields(t, filepath.Join(r.archive, "fixture-product-2.CATProduct.md"))
	if s["description"] != "fixture-product-2.CATProduct.md" || s["catia"] != d["catia"] || s["sha256"] != d["sha256"] {
		t.Fatalf("catch-up identity %v, description %v", s, d)
	}
}

func TestCatiaTextWithoutDescriptionKeepsFileName(t *testing.T) {
	r, _ := catiaFixture(t)
	cfg, c := catiaSplitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	moved := filepath.Join(r.archive, "fixture-product.CATProduct")
	if err := os.Remove(moved + ".md"); err != nil {
		t.Fatal(err)
	}
	cfg, c = catiaTextConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("catch-up = %+v", res)
	}
	s, keys := headerFields(t, moved+".text.md")
	if strings.Join(keys, ",") != "arxgo-text,archive,file_name,extracted_at,truncated" ||
		s["file_name"] != "fixture-product.CATProduct" {
		t.Fatalf("sidecar without description: %v %v", keys, s)
	}
}

func TestVideoDescriptionNamesArchive(t *testing.T) {
	r := newRoots(t)
	writeScanFile(t, r.archive, "media/clip.mp4", videoFixture)
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	d, keys := headerFields(t, filepath.Join(r.archive, "media", "clip.mp4.md"))
	if len(keys) < 2 || keys[0] != report.DescriptionMarker || keys[1] != "archive" || d["archive"] != r.archive {
		t.Fatalf("video description fields %v %v", keys, d)
	}
}

func TestTextNeedReservesIdentityWithinCap(t *testing.T) {
	for size, want := range map[int64]int64{
		0: 0, -1: 0, 1: 1 + textIdentityReserve, 100 << 10: 100<<10 + textIdentityReserve,
		catiaTextNeedCap - textIdentityReserve: catiaTextNeedCap, catiaTextNeedCap: catiaTextNeedCap, 1 << 62: catiaTextNeedCap,
	} {
		if got := textNeed(size); got != want {
			t.Errorf("textNeed(%d) = %d, want %d", size, got, want)
		}
	}
}
