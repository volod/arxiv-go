package archive

import (
	"bytes"
	"maps"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// The file registry describes the archive the operator curated: splits keep a row per moved file
// with its location, owned artifacts never get a row, additions add exactly their rows, restore
// sets the location back, and a scan that finds nothing to change leaves the file untouched.
func TestFileRegistryKeepsArchiveViewAcrossSplitAndRestore(t *testing.T) {
	f := newViewFixture(t)
	f.scan(t, nil)
	scanned := f.rows(t)
	for rel, row := range scanned {
		if row.Location != report.LocationArchive {
			t.Fatalf("scan: %s location %q", rel, row.Location)
		}
	}

	f.splitVideo(t)
	checkLocations(t, "video split", scanned, f.rows(t), locations(f.videos, report.LocationVideoArchive))
	f.splitCatia(t)
	moved := locations(f.videos, report.LocationVideoArchive)
	maps.Copy(moved, locations(f.catia, report.LocationCatiaArchive))
	checkLocations(t, "catia split", scanned, f.rows(t), moved)
	owned := f.ownedArtifacts(t)
	if want := len(f.videos) + 2*len(f.catia); len(owned) < want {
		t.Fatalf("owned artifacts %q, want at least %d", owned, want)
	}
	for _, rel := range owned {
		if _, ok := f.rows(t)[rel]; ok {
			t.Errorf("owned artifact %s has a registry row", rel)
		}
	}
	if st := readStamp(t, f.archive); st.Op != opSplit || st.Registry != f.registry || st.Detect.Metadata != "file" {
		t.Errorf("stamp after split = %+v", st)
	}

	// A scan right after the splits finds nothing to change.
	split, splitTime := mustRead(t, f.registry), f.modTime(t)
	res, _ := f.scan(t, func(sc *ScanConfig) {
		sc.Candidate = func(_ string, ft scanner.FileType) bool { return ft.IsVideo || ft.IsCatia }
	})
	checkUnchanged(t, "scan after split", f, split, splitTime)
	rep := readReport(t, f.archive, res.RunID)
	if rep.Scan.Registry != state.RegistryUnchanged || rep.Scan.Preserved.Count != int64(len(moved)) {
		t.Errorf("scan report: registry %q preserved %+v, want unchanged and %d", rep.Scan.Registry, rep.Scan.Preserved, len(moved))
	}
	if n := countCandidates(t, f.archive, res.RunID); n != 0 {
		t.Errorf("preserved rows entered candidates.jsonl: %d candidates", n)
	}
	if st := readStamp(t, f.archive); st.Op != opScan || st.RunID != res.RunID {
		t.Errorf("stamp after unchanged scan = %+v", st)
	}
	// A split rerun moves nothing and leaves every registry untouched.
	videos := mustRead(t, filepath.Join(f.archive, scanner.VideoRegistryName))
	f.splitVideo(t)
	checkUnchanged(t, "video split rerun", f, split, splitTime)
	if !bytes.Equal(mustRead(t, filepath.Join(f.archive, scanner.VideoRegistryName)), videos) {
		t.Error("video split rerun rewrote arxgo-videos.csv")
	}

	// Additions add exactly their rows.
	f.addVideo(t, "added/new clip.mp4")
	writeScanFile(t, f.archive, "added/new-part.CATPart", []byte("V5_CFV2\x00 added part"))
	f.scan(t, nil)
	added := f.rows(t)
	checkAddedLines(t, split, mustRead(t, f.registry), 2)
	for _, rel := range []string{"added/new clip.mp4", "added/new-part.CATPart"} {
		if row, ok := added[rel]; !ok || row.Location != report.LocationArchive {
			t.Errorf("added %s: %+v (present %v)", rel, row, ok)
		}
	}
	before := f.rows(t)
	f.splitVideo(t)
	f.splitCatia(t)
	checkLocations(t, "split of additions", before, f.rows(t), map[string]string{
		"added/new clip.mp4": report.LocationVideoArchive, "added/new-part.CATPart": report.LocationCatiaArchive})

	// Restore returns the location; the next scan leaves the registry untouched.
	before = f.rows(t)
	rcfg, rc := restoreConfig(f.roots, "auto")
	if res := runRestore(t, rcfg, rc); res.Status != StatusCompleted {
		t.Fatalf("video restore = %+v", res)
	}
	back := locations(append(slices.Clone(f.videos), "added/new clip.mp4"), report.LocationArchive)
	checkLocations(t, "video restore", before, f.rows(t), back)
	checkStampMatches(t, f)
	restored, restoredTime := mustRead(t, f.registry), f.modTime(t)
	f.scan(t, nil)
	checkUnchanged(t, "scan after video restore", f, restored, restoredTime)

	before = f.rows(t)
	ccfg, cc := catiaRestoreConfig(f.catiaRoots, "auto")
	if res := runRestore(t, ccfg, cc); res.Status != StatusCompleted {
		t.Fatalf("catia restore = %+v", res)
	}
	checkLocations(t, "catia restore", before, f.rows(t),
		locations(append(slices.Clone(f.catia), "added/new-part.CATPart"), report.LocationArchive))
	restored, restoredTime = mustRead(t, f.registry), f.modTime(t)
	f.scan(t, nil)
	checkUnchanged(t, "scan after catia restore", f, restored, restoredTime)
	home := locations(slices.Collect(maps.Keys(added)), report.LocationArchive)
	checkLocations(t, "round trip", added, f.rows(t), home)
}

func checkUnchanged(t *testing.T, stage string, f viewFixture, want []byte, mtime time.Time) {
	t.Helper()
	if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
		t.Errorf("%s: registry changed\n got %s\nwant %s", stage, got, want)
	}
	if !mtime.Equal(f.modTime(t)) {
		t.Errorf("%s: registry rewritten (modification time changed)", stage)
	}
}

// checkAddedLines checks that after holds every line of before plus exactly n more.
func checkAddedLines(t *testing.T, before, after []byte, n int) {
	t.Helper()
	b, a := bytes.Split(before, []byte("\n")), bytes.Split(after, []byte("\n"))
	if len(a) != len(b)+n {
		t.Fatalf("registry has %d lines, want %d", len(a), len(b)+n)
	}
	for _, line := range b {
		if !slices.ContainsFunc(a, func(l []byte) bool { return bytes.Equal(l, line) }) {
			t.Errorf("line changed or lost: %s", line)
		}
	}
}

func checkStampMatches(t *testing.T, f viewFixture) {
	t.Helper()
	st := readStamp(t, f.archive)
	data := mustRead(t, f.registry)
	if st.Size != int64(len(data)) || st.SHA256 != fileSum(t, f.registry) {
		t.Errorf("stamp %+v does not match the registry (%d bytes)", st, len(data))
	}
}

func countCandidates(t *testing.T, archive, runID string) int {
	t.Helper()
	n := 0
	err := ReadCandidates(filepath.Join(state.StateDir(archive), "runs", runID, state.CandidatesFile), func(Candidate) error {
		n++
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return n
}
