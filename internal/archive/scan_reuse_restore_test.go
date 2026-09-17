package archive

import (
	"bytes"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/state"
)

// Regression (AUD-reuse-registry-detection-2): restore --registry-update sets location archive on a
// row without detection. When the scan before it reconstructed that row because the mirror was
// unreadable, the next scan reused the reconstructed values for the restored file (here the raw-XML
// .3dxml kept file_type 3dxml and is_binary true where detection writes xml and false). A file a
// restore returned since the base scan started is now detected once, and reused after that.
func TestScanDetectsFilesRestoredSinceBaseScan(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("permission bits do not stop root")
	}
	f := newViewFixture(t)
	backdateTree(t, f.archive, time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC))
	f.scan(t, nil)
	f.splitCatia(t)

	if err := os.Chmod(f.catiaRoots.catia, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(f.catiaRoots.catia, 0o755) })
	removeRegistryAndStamp(t, f)
	_, rec := f.scan(t, nil)
	if n := len(rec.messages("moved file row reconstructed from the run history; its mirror copy could not be read")); n != len(f.catia) {
		t.Fatalf("%d reconstructed rows, want %d", n, len(f.catia))
	}
	if err := os.Chmod(f.catiaRoots.catia, 0o755); err != nil {
		t.Fatal(err)
	}

	cfg, c := catiaRestoreConfig(f.catiaRoots, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("catia restore = %+v", res)
	}
	det := &countingDetector{root: f.archive}
	counted := func(sc *ScanConfig) { sc.detectFile = det.detect }
	res, _ := f.scan(t, counted)
	if got := det.take(); !slices.Equal(got, f.catia) {
		t.Errorf("first scan after the restore opened %q, want the restored files %q", got, f.catia)
	}
	if rep := readReport(t, f.archive, res.RunID); rep.Scan.Registry != state.RegistryWritten {
		t.Errorf("reconstructed rows not corrected: registry %q", rep.Scan.Registry)
	}
	after := mustRead(t, f.registry)
	f.scan(t, counted)
	if got := det.take(); len(got) != 0 {
		t.Errorf("second scan after the restore opened %q", got)
	}
	f.scan(t, func(sc *ScanConfig) { sc.Redetect = true })
	if got := mustRead(t, f.registry); !bytes.Equal(got, after) {
		t.Errorf("registry after the restore differs from --redetect\n got %s\nwant %s", after, got)
	}
}

// backdateTree sets the modification time of every file below root outside run state, so scans on
// the fake clock start after it and may reuse rows.
func backdateTree(t *testing.T, root string, mtime time.Time) {
	t.Helper()
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		switch {
		case err != nil:
			return err
		case d.IsDir() && d.Name() == state.DirName:
			return filepath.SkipDir
		case d.Type().IsRegular():
			return os.Chtimes(p, mtime, mtime)
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
}
