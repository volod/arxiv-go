package archive

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

func removeRegistryAndStamp(t *testing.T, f viewFixture) {
	t.Helper()
	for _, p := range []string{f.registry, state.RegistryStampPath(f.archive)} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
}

// Without a registry and a stamp, the mirror copies yield the moved rows byte for byte.
func TestFileRegistryRebuildsFromMirrors(t *testing.T) {
	f := newViewFixture(t)
	want := f.splitBoth(t)
	removeRegistryAndStamp(t, f)
	res, rec := f.scan(t, nil)
	if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
		t.Fatalf("rebuilt registry differs\n got %s\nwant %s", got, want)
	}
	if rep := readReport(t, f.archive, res.RunID); rep.Scan.Registry != state.RegistryWritten {
		t.Errorf("registry outcome %q, want written", rep.Scan.Registry)
	}
	if n := len(rec.messages("moved file row reconstructed from the run history; its mirror copy could not be read")); n != 0 {
		t.Errorf("%d reconstruction warnings with readable mirrors", n)
	}
}

// With the mirrors unreadable, the existing registry keeps the rows (registry-row-kept); without it
// the rows are reconstructed (registry-row-reconstructed). Neither changes the exit code.
func TestFileRegistryWithUnreadableMirrors(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not stop root")
	}
	f := newViewFixture(t)
	want := f.splitBoth(t)
	moved := len(f.videos) + len(f.catia)
	for _, root := range []string{f.video, f.catiaRoots.catia} {
		if err := os.Chmod(root, 0); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	}

	if err := os.Remove(state.RegistryStampPath(f.archive)); err != nil {
		t.Fatal(err)
	}
	_, rec := f.scan(t, nil)
	if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
		t.Errorf("registry with kept rows differs\n got %s\nwant %s", got, want)
	}
	if n := len(rec.messages("moved file keeps its row from the existing registry; its mirror copy could not be read")); n != moved {
		t.Errorf("%d registry-row-kept warnings, want %d", n, moved)
	}

	removeRegistryAndStamp(t, f)
	_, rec = f.scan(t, nil)
	warnings := rec.messages("moved file row reconstructed from the run history; its mirror copy could not be read")
	if len(warnings) != moved || warnings[0]["warning"] != warnRowReconstructed {
		t.Fatalf("reconstruction warnings %v, want %d", warnings, moved)
	}
	rows := f.rows(t)
	for _, rel := range f.videos {
		row := rows[rel]
		if row.Location != report.LocationVideoArchive || !row.IsVideo || !row.IsMedia || !row.IsBinary ||
			row.FileType != "mp4" || row.FileSize == 0 || row.FileMIME != "video/mp4" || row.Metadata.MTime == "" {
			t.Errorf("reconstructed video %s = %+v", rel, row)
		}
	}
	for _, rel := range f.catia {
		if row := rows[rel]; row.Location != report.LocationCatiaArchive || !row.IsCatia || row.IsVideo || row.FileSize == 0 {
			t.Errorf("reconstructed CATIA file %s = %+v", rel, row)
		}
	}
}

// Membership comes from the WAL, so deleted payload registries change nothing.
func TestFileRegistryIgnoresDeletedPayloadRegistries(t *testing.T) {
	f := newViewFixture(t)
	want := f.splitBoth(t)
	mtime := f.modTime(t)
	for _, p := range []string{
		filepath.Join(f.archive, scanner.VideoRegistryName), filepath.Join(f.video, scanner.VideoRegistryName),
		filepath.Join(f.archive, scanner.CatiaRegistryName), filepath.Join(f.catiaRoots.catia, scanner.CatiaRegistryName),
	} {
		if err := os.Remove(p); err != nil {
			t.Fatal(err)
		}
	}
	f.scan(t, nil)
	checkUnchanged(t, "scan without payload registries", f, want, mtime)
	removeRegistryAndStamp(t, f)
	f.scan(t, nil)
	if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
		t.Errorf("rebuilt registry without payload registries differs\n got %s\nwant %s", got, want)
	}
}

// A file put back by hand at a moved path is a present row; --exclude drops a preserved row like a
// present one, also through an excluded ancestor directory.
func TestFileRegistryPresenceWinsAndExcludeApplies(t *testing.T) {
	f := newViewFixture(t)
	f.splitBoth(t)
	rel := "nested/deep/clip two.mp4"
	data := mustRead(t, filepath.Join(f.video, filepath.FromSlash(rel)))
	if err := os.WriteFile(filepath.Join(f.archive, filepath.FromSlash(rel)), data, 0o644); err != nil {
		t.Fatal(err)
	}
	res, _ := f.scan(t, nil)
	rows := f.rows(t)
	if row := rows[rel]; row.Location != report.LocationArchive || row.FileSize != int64(len(data)) {
		t.Errorf("put-back file row = %+v", row)
	}
	lines := bytes.Count(mustRead(t, f.registry), []byte("\n"+rel+","))
	if lines != 1 {
		t.Errorf("put-back file has %d rows", lines)
	}
	if rep := readReport(t, f.archive, res.RunID); rep.Scan.Preserved.Count != int64(len(f.videos)+len(f.catia)-1) {
		t.Errorf("preserved = %+v", rep.Scan.Preserved)
	}

	for _, pattern := range []string{"media/*.mp4", "cad"} {
		f.scan(t, func(sc *ScanConfig) { sc.Exclude = []string{pattern} })
		rows := f.rows(t)
		for rel := range rows {
			key := scanner.KeyOf(rel)
			if g, _ := scanner.CompileGlob(pattern); g.Match(key) || g.Match(key[:1]) {
				t.Errorf("--exclude %s: row %s kept", pattern, rel)
			}
		}
	}
}

// An interrupted scan that has written preserved rows resumes to the same registry.
func TestFileRegistryResumeWithPreservedRows(t *testing.T) {
	f := newViewFixture(t)
	want := f.splitBoth(t)
	removeRegistryAndStamp(t, f)
	full, _ := f.scan(t, nil)
	sum := readReport(t, f.archive, full.RunID).Scan
	entries := sum.Files + sum.Dirs + sum.Symlinks
	for n := int64(1); n <= entries; n++ {
		removeRegistryAndStamp(t, f)
		ctx, cancel := context.WithCancel(context.Background())
		sc := f.scanConfig()
		sc.afterEntry = func(written int64) error {
			if written == n {
				cancel()
			}
			return nil
		}
		cfg := scanSessionConfig(f.roots, newClock(), 1)
		res, _ := runScan(t, ctx, cfg, sc)
		cancel()
		if res.Status != StatusInterrupted {
			t.Fatalf("n %d: status %v (%v)", n, res.Status, res.Err)
		}
		resumed, _ := runScan(t, context.Background(), cfg, f.scanConfig())
		if resumed.Status != StatusCompleted || resumed.RunID != res.RunID {
			t.Fatalf("n %d: resume = %+v", n, resumed)
		}
		if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
			t.Fatalf("n %d: resumed registry differs\n got %s\nwant %s", n, got, want)
		}
	}
}

// A scan that stops after syncing the preserved rows behind the last walked entry, before the
// registry is placed, resumes without writing them twice.
func TestFileRegistryResumeAfterTrailingPreservedRows(t *testing.T) {
	f := newViewFixture(t)
	want := f.splitBoth(t)
	removeRegistryAndStamp(t, f)
	// A non-empty directory at the registry path makes placing the registry fail.
	if err := os.MkdirAll(filepath.Join(f.registry, "blocker"), 0o755); err != nil {
		t.Fatal(err)
	}
	cfg := scanSessionConfig(f.roots, newClock(), 1)
	res, _ := runScan(t, context.Background(), cfg, f.scanConfig())
	if res.Status != StatusFailed {
		t.Fatalf("scan with a blocked registry = %+v", res)
	}
	if cp := readRunCheckpoint(t, f.roots, res.RunID); cp.Scan == nil || cp.Scan.Preserved.Count != int64(len(f.videos)+len(f.catia)) {
		t.Fatalf("checkpoint does not hold every preserved row: %+v", cp.Scan)
	}
	if err := os.RemoveAll(f.registry); err != nil {
		t.Fatal(err)
	}
	resumed, _ := runScan(t, context.Background(), cfg, f.scanConfig())
	if resumed.Status != StatusCompleted || resumed.RunID != res.RunID {
		t.Fatalf("resume = %+v", resumed)
	}
	if got := mustRead(t, f.registry); !bytes.Equal(got, want) {
		t.Fatalf("resumed registry differs\n got %s\nwant %s", got, want)
	}
}

// A description that restore --descriptions keep leaves in place is still owned while its marker
// names its file; once the operator replaces it, it is an ordinary file with a row.
func TestFileRegistryKeptDescriptionStaysOwned(t *testing.T) {
	f := newViewFixture(t)
	f.splitBoth(t)
	rel := "nested/deep/clip two.mp4.md"
	cfg, c := restoreConfig(f.roots, "auto")
	c.KeepDescriptions = true
	attachRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if !exists(filepath.Join(f.archive, filepath.FromSlash(rel))) {
		t.Fatal("kept description missing")
	}
	f.scan(t, nil)
	if _, ok := f.rows(t)[rel]; ok {
		t.Error("kept description has a row")
	}
	writeScanFile(t, f.archive, rel, []byte("operator notes replacing the description\n"))
	f.scan(t, nil)
	if _, ok := f.rows(t)[rel]; !ok {
		t.Error("operator file at a former description path has no row")
	}
}
