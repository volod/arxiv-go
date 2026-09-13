//go:build integration

package integration

import (
	"bytes"
	"encoding/json"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

var runIDPattern = regexp.MustCompile(`^\d{8}T\d{6}Z-[0-9a-f]{8}$`)

// checkFileRegistry validates arxgo-registry.csv against the contract and the manifest taken
// before any operation: one row per file, sizes, second-precision mtimes, flags and candidates.
func checkFileRegistry(t *testing.T, g *genArchive, before manifest) map[string]report.RegistryRow {
	t.Helper()
	rows, err := report.LoadRegistry(filepath.Join(g.root, "arxgo-registry.csv"))
	if err != nil {
		t.Fatalf("file registry: %v", err)
	}
	if len(rows) != before.files() {
		t.Fatalf("file registry has %d rows, archive has %d files", len(rows), before.files())
	}
	byPath := report.RegistryByPath(rows)
	for rel, e := range before {
		if e.Dir {
			continue
		}
		row, ok := byPath[rel]
		if !ok {
			t.Errorf("file registry: no row for %s", rel)
			continue
		}
		wantMTime := time.Unix(0, e.MTime).UTC().Truncate(time.Second).Format(time.RFC3339)
		switch {
		case row.FileName != path.Base(rel):
			t.Errorf("%s: file_name %q", rel, row.FileName)
		case row.FileSize != e.Size:
			t.Errorf("%s: file_size %d, want %d", rel, row.FileSize, e.Size)
		case row.Metadata.V != report.MetadataVersion || row.Metadata.MTime != wantMTime:
			t.Errorf("%s: metadata %+v, want v=1 mtime=%s", rel, row.Metadata, wantMTime)
		case len(row.Metadata.Mode) != 4:
			t.Errorf("%s: mode %q is not four octal digits", rel, row.Metadata.Mode)
		case row.IsVideo != g.videos[rel]:
			t.Errorf("%s: is_video=%v, want %v", rel, row.IsVideo, g.videos[rel])
		case row.IsVideo && !row.IsMedia:
			t.Errorf("%s: video row is not media", rel)
		case row.IsLarge:
			t.Errorf("%s: is_large below the default threshold", rel)
		}
	}
	return byPath
}

// checkSplitOutputs validates the state after a completed split: videos only in the video archive
// with their original bytes, one owned stub per video, identical video registries in both roots.
func checkSplitOutputs(t *testing.T, g *genArchive, video string, before manifest) {
	t.Helper()
	archiveCSV := readFile(t, filepath.Join(g.root, "arxgo-videos.csv"))
	if !bytes.Equal(archiveCSV, readFile(t, filepath.Join(video, "arxgo-videos.csv"))) {
		t.Error("arxgo-videos.csv differs between the archive and the video archive")
	}
	for _, root := range []string{g.root, video} {
		if _, err := os.Stat(filepath.Join(root, "arxgo-videos.md")); err != nil {
			t.Errorf("summary: %v", err)
		}
	}
	rows, err := report.LoadVideoCSV(bytes.NewReader(archiveCSV))
	if err != nil {
		t.Fatalf("video registry: %v", err)
	}
	checkRowSet(t, "video registry", g, rows)
	for _, row := range rows {
		rel := row.RelPath
		want := before[rel]
		switch {
		case !g.videos[rel]:
			t.Errorf("video registry row for non-video %s", rel)
			continue
		case row.Status != report.StatusMoved || row.VideoRelPath != rel || row.Transfer != "copy":
			t.Errorf("%s: status=%s video_rel_path=%s transfer=%s", rel, row.Status, row.VideoRelPath, row.Transfer)
		case row.FileSize != want.Size || row.SHA256 != want.SHA256:
			t.Errorf("%s: size=%d sha256=%s, want %d %s", rel, row.FileSize, row.SHA256, want.Size, want.SHA256)
		case !runIDPattern.MatchString(row.RunID):
			t.Errorf("%s: run_id %q", rel, row.RunID)
		}
		if _, err := os.Lstat(filepath.Join(g.root, filepath.FromSlash(rel))); !os.IsNotExist(err) {
			t.Errorf("%s still in the archive after split (%v)", rel, err)
		}
		got, err := fileSHA256(filepath.Join(video, filepath.FromSlash(rel)))
		if err != nil || got != want.SHA256 {
			t.Errorf("%s in the video archive: sha256 %s (%v), want %s", rel, got, err, want.SHA256)
		}
		checkStub(t, g, row)
	}
	if got := readFile(t, filepath.Join(g.root, filepath.FromSlash(g.foreignMD))); string(got) != "human notes about clip 00\n" {
		t.Errorf("foreign %s was changed: %q", g.foreignMD, got)
	}
}

// checkRowSet fails unless rows name every generated video exactly once.
func checkRowSet(t *testing.T, name string, g *genArchive, rows []report.VideoRow) {
	t.Helper()
	seen := map[string]int{}
	for _, row := range rows {
		seen[row.RelPath]++
	}
	for rel := range g.videos {
		if seen[rel] != 1 {
			t.Errorf("%s: %d rows for %s, want 1", name, seen[rel], rel)
		}
	}
	if len(rows) != len(g.videos) {
		t.Errorf("%s has %d rows, want %d", name, len(rows), len(g.videos))
	}
}

func checkStub(t *testing.T, g *genArchive, row report.VideoRow) {
	t.Helper()
	wantStub := row.RelPath + ".md"
	if row.RelPath+".md" == g.foreignMD || row.RelPath+".md" == g.dirAtStub {
		wantStub = row.RelPath + ".arxgo.md"
	}
	if row.StubRelPath != wantStub {
		t.Errorf("%s: stub_rel_path %q, want %q", row.RelPath, row.StubRelPath, wantStub)
	}
	fm, err := report.ReadFrontMatterFile(filepath.Join(g.root, filepath.FromSlash(row.StubRelPath)))
	if err != nil {
		t.Errorf("%s: stub: %v", row.RelPath, err)
		return
	}
	checks := map[string]string{
		report.StubMarker: report.StubMarkerValue, "rel_path": row.RelPath, "sha256": row.SHA256,
		"run_id": row.RunID,
	}
	for k, v := range checks {
		if fm[k] != v {
			t.Errorf("%s: stub %s=%q, want %q", row.RelPath, k, fm[k], v)
		}
	}
	if fm["file_size"] == "" || !strings.HasSuffix(filepath.ToSlash(fm["video_archive_path"]), "/"+row.RelPath) {
		t.Errorf("%s: stub front matter %v", row.RelPath, fm)
	}
}

// checkRestoreOutputs validates the state after a completed restore: the video registries are
// retired in both roots with every row restored, and the video archive holds no files or
// directories besides arxgo's own outputs.
func checkRestoreOutputs(t *testing.T, g *genArchive, video, runID string) {
	t.Helper()
	retired := "arxgo-videos.restored-" + runID + ".csv"
	data := readFile(t, filepath.Join(g.root, retired))
	if !bytes.Equal(data, readFile(t, filepath.Join(video, retired))) {
		t.Errorf("%s differs between the roots", retired)
	}
	rows, err := report.LoadVideoCSV(bytes.NewReader(data))
	if err != nil {
		t.Fatalf("retired video registry: %v", err)
	}
	checkRowSet(t, "retired video registry", g, rows)
	for _, row := range rows {
		if row.Status != report.StatusRestored || row.RunID != runID {
			t.Errorf("%s: status=%s run_id=%s after restore run %s", row.RelPath, row.Status, row.RunID, runID)
		}
	}
	for _, root := range []string{g.root, video} {
		if _, err := os.Stat(filepath.Join(root, "arxgo-videos.csv")); !os.IsNotExist(err) {
			t.Errorf("live arxgo-videos.csv still in %s (%v)", root, err)
		}
	}
	if left := takeManifest(t, video); len(left) != 0 {
		t.Errorf("video archive not empty after restore: %v", left)
	}
}

// checkNoLeftovers fails on part files and locks in either root.
func checkNoLeftovers(t *testing.T, roots ...string) {
	t.Helper()
	for _, root := range roots {
		if _, err := os.Stat(filepath.Join(root, ".arxgo", "lock")); !os.IsNotExist(err) {
			t.Errorf("lock left in %s (%v)", root, err)
		}
		_ = filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
			if err == nil && strings.HasSuffix(d.Name(), ".arxgo-part") {
				t.Errorf("part file left: %s", p)
			}
			return nil
		})
	}
}

// runReport reads report.json of a run and checks the contract fields every report carries.
func runReport(t *testing.T, archive, runID, op string) state.Report {
	t.Helper()
	var r state.Report
	if err := json.Unmarshal(readFile(t, filepath.Join(archive, ".arxgo", "runs", runID, "report.json")), &r); err != nil {
		t.Fatalf("report of %s: %v", runID, err)
	}
	if r.V != 1 || r.RunID != runID || r.Op != op {
		t.Errorf("report of %s: v=%d run_id=%s op=%s", runID, r.V, r.RunID, r.Op)
	}
	return r
}

func currentRunID(t *testing.T, archive string) string {
	t.Helper()
	id := strings.TrimSpace(string(readFile(t, filepath.Join(archive, ".arxgo", "current"))))
	if !runIDPattern.MatchString(id) {
		t.Fatalf("current run id %q", id)
	}
	return id
}

func readFile(t *testing.T, p string) []byte {
	t.Helper()
	data, err := os.ReadFile(p)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
