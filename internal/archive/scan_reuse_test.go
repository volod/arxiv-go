package archive

import (
	"bytes"
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// countingDetector records the files a scan opens for type detection.
type countingDetector struct {
	root   string
	mu     sync.Mutex
	opened []string
}

func (d *countingDetector) detect(path string, size int64, opts scanner.DetectOptions) (scanner.FileType, error) {
	rel, _ := filepath.Rel(d.root, path)
	d.mu.Lock()
	d.opened = append(d.opened, filepath.ToSlash(rel))
	d.mu.Unlock()
	return scanner.Detect(path, size, opts)
}

// take returns the sorted opened paths and forgets them.
func (d *countingDetector) take() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	out := d.opened
	d.opened = nil
	slices.Sort(out)
	return out
}

// reuseFixture is the registry edge-case tree plus CATIA and AppleDouble names, scanned with a
// counting detector.
type reuseFixture struct {
	r        roots
	registry string
	det      *countingDetector
	version  string
	clock    *fakeClock
}

func newReuseFixture(t *testing.T) *reuseFixture {
	t.Helper()
	r := newRoots(t)
	buildEdgeCaseTree(t, r.archive)
	writeScanFile(t, r.archive, "cad/part.CATPart", []byte("V5_CFV2\x00 part"))
	writeScanFile(t, r.archive, "cad/._part.CATPart", append([]byte{0x00, 0x05, 0x16, 0x07}, make([]byte, 60)...))
	return &reuseFixture{r: r, registry: filepath.Join(r.archive, "arxgo-registry.csv"),
		det: &countingDetector{root: r.archive}, version: "test",
		// Scans start after the real time the tree (and its symlink, whose time is not set) was
		// written, one minute apart.
		clock: &fakeClock{t: time.Now().UTC().Add(time.Hour)}}
}

// scan runs a completed scan and returns its report summary, the files it opened and the registry.
func (f *reuseFixture) scan(t *testing.T, edit func(*ScanConfig)) (*state.ScanSummary, []string, []byte) {
	t.Helper()
	sc := testScanConfig(f.r)
	sc.detectFile = f.det.detect
	if edit != nil {
		edit(&sc)
	}
	f.clock.Advance(time.Minute)
	cfg := scanSessionConfig(f.r, f.clock, 5)
	cfg.Version = f.version
	res, _ := runScan(t, context.Background(), cfg, sc)
	if res.Status != StatusCompleted {
		t.Fatalf("scan = %v (%v)", res.Status, res.Err)
	}
	return readReport(t, f.r.archive, res.RunID).Scan, f.det.take(), mustRead(t, f.registry)
}

// redetect scans with --redetect and checks it writes want byte for byte.
func (f *reuseFixture) redetect(t *testing.T, stage string, want []byte, edit func(*ScanConfig)) {
	t.Helper()
	sum, opened, got := f.scan(t, func(sc *ScanConfig) {
		sc.Redetect = true
		if edit != nil {
			edit(sc)
		}
	})
	if !bytes.Equal(got, want) {
		t.Fatalf("%s: registry differs from the --redetect registry\n--- reused\n%s\n--- redetect\n%s", stage, want, got)
	}
	if sum.Reused != 0 || int64(len(opened)) != sum.Files {
		t.Errorf("%s: --redetect reused %d rows and opened %d of %d files", stage, sum.Reused, len(opened), sum.Files)
	}
}

func (f *reuseFixture) touch(t *testing.T, rel string, data []byte, mtime time.Time) {
	t.Helper()
	p := filepath.Join(f.r.archive, filepath.FromSlash(rel))
	if data != nil {
		if err := os.WriteFile(p, data, 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chtimes(p, mtime, mtime); err != nil {
		t.Fatal(err)
	}
}

func wantOpened(t *testing.T, stage string, got []string, want ...string) {
	t.Helper()
	if !slices.Equal(got, want) {
		t.Errorf("%s: opened %q, want %q", stage, got, want)
	}
}

// A second scan of an unchanged tree opens no file, reuses every present row and leaves the
// registry untouched; only the stamp is written again.
func TestScanReusesUnchangedRows(t *testing.T) {
	t.Parallel()
	f := newReuseFixture(t)
	first, opened, want := f.scan(t, nil)
	if first.Reused != 0 || int64(len(opened)) != first.Files {
		t.Fatalf("first scan reused %d rows, opened %d of %d files", first.Reused, len(opened), first.Files)
	}
	info, _ := os.Stat(f.registry)
	stamp := readStamp(t, f.r.archive)

	sum, opened, got := f.scan(t, nil)
	wantOpened(t, "second scan", opened)
	if !bytes.Equal(got, want) || sum.Registry != state.RegistryUnchanged {
		t.Errorf("second scan: registry %q, bytes equal %v", sum.Registry, bytes.Equal(got, want))
	}
	if again, _ := os.Stat(f.registry); !again.ModTime().Equal(info.ModTime()) {
		t.Error("second scan rewrote the registry")
	}
	if sum.Reused != first.Files+first.Symlinks || sum.Files != first.Files {
		t.Errorf("second scan reused %d rows, want %d", sum.Reused, first.Files+first.Symlinks)
	}
	if st := readStamp(t, f.r.archive); st.RunID == stamp.RunID || !st.ScanStartedAt.After(stamp.ScanStartedAt) {
		t.Errorf("stamp not renewed: %+v after %+v", st, stamp)
	}
	f.redetect(t, "unchanged tree", want, nil)
}

// Every change the reuse rule can see leads to detection, and the registry equals the --redetect
// registry after each one.
func TestScanReuseDetectsChanges(t *testing.T) {
	t.Parallel()
	f := newReuseFixture(t)
	f.scan(t, nil)
	base := readStamp(t, f.r.archive).ScanStartedAt
	// Same size, content that detects differently: text becomes binary.
	binary := bytes.Repeat([]byte{0x00, 0xFF}, len("this is text, not a video\n")/2)

	f.touch(t, "lies/text-named.mp4", binary, base.Add(500*time.Millisecond))
	_, opened, got := f.scan(t, nil)
	wantOpened(t, "same size, mtime inside the base scan start second", opened, "lies/text-named.mp4")
	if !strings.Contains(string(got), "lies/text-named.mp4,text-named.mp4,mp4,26,false,application/octet-stream,true,true,false,true") {
		t.Errorf("changed file not detected again:\n%s", got)
	}
	f.redetect(t, "mtime inside the base scan start second", got, nil)

	base = readStamp(t, f.r.archive).ScanStartedAt
	f.touch(t, "notes.txt", []byte("PLAIN TEXT NOTES\n"), base.Add(-time.Hour))
	_, opened, got = f.scan(t, nil)
	wantOpened(t, "same size, new mtime before the base scan start", opened, "notes.txt")
	f.redetect(t, "new mtime before the base scan start", got, nil)

	future := f.clock.Now().AddDate(1, 0, 0)
	f.touch(t, "a/b", []byte("WALK ORDER A/B\n"), future)
	for i := range 2 {
		_, opened, got = f.scan(t, nil)
		wantOpened(t, "future mtime", opened, "a/b")
		if i == 0 {
			f.redetect(t, "future mtime", got, nil)
			f.det.take()
		}
	}

	writeScanFile(t, f.r.archive, "added/new.mp4", mustRead(t, filepath.Join(f.r.archive, "deep/l1/l2/l3/l4/deep.mp4")))
	if err := os.Remove(filepath.Join(f.r.archive, "notes.txt")); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(f.r.archive, "links", "notes-link")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("../a/b", filepath.Join(f.r.archive, "links", "notes-link")); err != nil {
		t.Fatal(err)
	}
	sum, opened, got := f.scan(t, nil)
	wantOpened(t, "added file and changed symlink", opened, "a/b", "added/new.mp4")
	if sum.Reused != sum.Files-2 {
		t.Errorf("reused %d rows of %d files and a changed symlink", sum.Reused, sum.Files)
	}
	f.redetect(t, "added, removed and relinked", got, nil)

	// The documented limit: a same-size change that keeps the old modification time is reused
	// until --redetect.
	f.touch(t, "a-b/x", make([]byte, len("walk order a-b/x\n")), fixtureTime)
	_, opened, got = f.scan(t, nil)
	wantOpened(t, "same size and modification time", opened, "a/b") // a/b still has a future mtime
	_, _, fixed := f.scan(t, func(sc *ScanConfig) { sc.Redetect = true })
	if bytes.Equal(got, fixed) || !strings.Contains(string(fixed), "a-b/x,x,,17,false,application/octet-stream,true") {
		t.Errorf("--redetect did not correct the hidden change:\n%s", fixed)
	}
}

// A detection setting that differs from the stamp's detects every file again; --large-threshold is
// not one, because is_large is recomputed on reused rows.
func TestScanReuseFollowsDetectionSettings(t *testing.T) {
	t.Parallel()
	f := newReuseFixture(t)
	first, _, _ := f.scan(t, nil)
	ffprobe, _ := exec.LookPath("ffprobe")
	for _, tc := range []struct {
		name string
		edit func(*ScanConfig)
	}{
		{"metadata media", func(sc *ScanConfig) { sc.Metadata, sc.FFprobePath = "media", ffprobe }},
		{"metadata file", func(sc *ScanConfig) { sc.Metadata = "file" }},
		{"video extensions", func(sc *ScanConfig) { sc.VideoExtensions = []string{".DAT", ".bin"} }},
		{"version", nil},
	} {
		if tc.edit == nil {
			f.version = "test-next"
		}
		sum, opened, got := f.scan(t, tc.edit)
		if sum.Reused != 0 || int64(len(opened)) != first.Files {
			t.Errorf("%s: reused %d rows, opened %d of %d files", tc.name, sum.Reused, len(opened), first.Files)
		}
		if _, opened, again := f.scan(t, tc.edit); len(opened) != 0 || !bytes.Equal(again, got) {
			t.Errorf("%s: rescan with the same settings opened %q", tc.name, opened)
		}
		f.redetect(t, tc.name, got, tc.edit)
	}

	threshold := func(sc *ScanConfig) { sc.LargeThreshold = 20 }
	sum, opened, got := f.scan(t, threshold)
	wantOpened(t, "large threshold", opened)
	if sum.Large.Count <= first.Large.Count {
		t.Errorf("large rows %d with a lower threshold, %d before", sum.Large.Count, first.Large.Count)
	}
	f.redetect(t, "large threshold", got, threshold)
}
