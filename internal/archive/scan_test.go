package archive

import (
	"bytes"
	"context"
	"errors"
	"flag"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

var updateGolden = flag.Bool("update", false, "rewrite golden files")

const symlinkMTimeMark = "{{SYMLINK_MTIME}}"

// edgeCaseRoots builds the edge-case tree, adding an unreadable file (the process must not be
// root, which bypasses permission checks).
func edgeCaseRoots(t *testing.T) roots {
	t.Helper()
	if os.Geteuid() == 0 {
		t.Skip("running as root bypasses permission checks; the unreadable file fixture needs a user")
	}
	r := newRoots(t)
	buildEdgeCaseTree(t, r.archive)
	writeScanFile(t, r.archive, "locked/secret.txt", []byte("secret\n"))
	locked := filepath.Join(r.archive, "locked", "secret.txt")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o644) })
	return r
}

func TestScanGoldenRegistry(t *testing.T) {
	r := edgeCaseRoots(t)
	before := scanManifest(t, r.archive, state.DirName, "arxgo-registry.csv")
	res, rec := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), testScanConfig(r))
	if res.Status != StatusPartial {
		t.Fatalf("status = %v (%v), want partial for the unreadable file", res.Status, res.Err)
	}

	got := mustRead(t, filepath.Join(r.archive, "arxgo-registry.csv"))
	info, err := os.Lstat(filepath.Join(r.archive, "links", "notes-link"))
	if err != nil {
		t.Fatal(err)
	}
	mtime := info.ModTime().UTC().Format("2006-01-02T15:04:05Z07:00")
	golden := filepath.Join("..", "..", "test", "testdata", "archive", "scan-registry.golden.csv")
	if *updateGolden {
		if err := os.WriteFile(golden, bytes.ReplaceAll(got, []byte(mtime), []byte(symlinkMTimeMark)), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	want := bytes.ReplaceAll(mustRead(t, golden), []byte(symlinkMTimeMark), []byte(mtime))
	if !bytes.Equal(got, want) {
		t.Errorf("registry differs from %s (run with -update after checking)\n--- got\n%s\n--- want\n%s", golden, got, want)
	}

	if after := scanManifest(t, r.archive, state.DirName, "arxgo-registry.csv"); !reflect.DeepEqual(before, after) {
		t.Errorf("scan changed the archive outside the registry and .arxgo:\nbefore %v\nafter  %v", sortedKeys(before), sortedKeys(after))
	}
	if exists(fsops.PartPath(filepath.Join(r.archive, "arxgo-registry.csv"))) {
		t.Error("registry part file left behind")
	}

	var cands []string
	if err := ReadCandidates(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.CandidatesFile), func(c Candidate) error {
		cands = append(cands, c.RelPath)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	wantCands := []string{"deep/l1/l2/l3/l4/deep.mp4", "lies/video-named.txt", "media/Clip.MTS", "media/audio-only.mp4", "media/tape.dat"}
	if !reflect.DeepEqual(cands, wantCands) {
		t.Errorf("candidates = %q, want %q", cands, wantCands)
	}

	rep := readReport(t, r.archive, res.RunID)
	sum := rep.Scan
	if sum == nil || sum.Files != 24 || sum.Symlinks != 1 || sum.Dirs != 13 || sum.Skipped["unreadable"] != 1 ||
		sum.Video.Count != 5 || sum.Media.Count != 6 || sum.Large.Count != 4 || sum.Binary.Count != 8 ||
		len(sum.TopMIME) != 5 || sum.TopMIME[0].MIME != "application/octet-stream" || sum.TopMIME[0].Bytes != 267 {
		t.Errorf("report scan summary = %+v", sum)
	}
	if len(rep.Issues) != 1 || rep.Issues[0].RelPath != "locked/secret.txt" || !strings.HasPrefix(rep.Issues[0].Reason, "unreadable: ") {
		t.Errorf("issues = %+v", rep.Issues)
	}
	if len(rec.messages("scan summary")) != 1 || len(rec.messages("skipped entry")) != 1 {
		t.Error("scan summary or skipped-entry warning not logged exactly once")
	}
}

func TestScanEmptyArchiveWritesHeaderOnly(t *testing.T) {
	r := newRoots(t)
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), testScanConfig(r))
	if res.Status != StatusCompleted {
		t.Fatalf("status = %v (%v)", res.Status, res.Err)
	}
	got := string(mustRead(t, filepath.Join(r.archive, "arxgo-registry.csv")))
	if got != "rel_path,file_name,file_size,file_type,file_mime,is_binary,is_media,is_picture,is_video,is_large,metadata\n" {
		t.Errorf("registry = %q", got)
	}
}

func TestScanDryRunWritesNoRegistry(t *testing.T) {
	r := newRoots(t)
	writeScanFile(t, r.archive, "a.txt", []byte("x\n"))
	cfg := scanSessionConfig(r, newClock(), 5)
	cfg.DryRun = true
	res, _ := runScan(t, context.Background(), cfg, testScanConfig(r))
	if res.Status != StatusCompleted {
		t.Fatalf("status = %v (%v)", res.Status, res.Err)
	}
	reg := filepath.Join(r.archive, "arxgo-registry.csv")
	if exists(reg) || exists(fsops.PartPath(reg)) {
		t.Error("dry run wrote the registry or its part file")
	}
	if sum := readReport(t, r.archive, res.RunID).Scan; sum == nil || sum.Files != 1 {
		t.Errorf("dry run summary = %+v", sum)
	}
}

// lowSpaceAfterFS reports plenty of free space for the first calls and then none.
type lowSpaceAfterFS struct {
	fsops.System
	calls, after int
}

func (f *lowSpaceAfterFS) SameDevice(string, string) (bool, error) { return true, nil }

func (f *lowSpaceAfterFS) FreeSpace(string) (fsops.Space, error) {
	f.calls++
	if f.calls > f.after {
		return fsops.Space{Total: 1 << 40, Available: 10}, nil
	}
	return fsops.Space{Total: 1 << 40, Available: 1 << 39}, nil
}

func TestScanStopsWhenFreeSpaceFallsBelowMinFreeAndResumes(t *testing.T) {
	r := newRoots(t)
	buildEdgeCaseTree(t, r.archive)
	reference := referenceRegistry(t, r)

	cfg := scanSessionConfig(r, newClock(), 5)
	cfg.Preflight.MinFree = 1 << 20
	cfg.FS = &lowSpaceAfterFS{after: 3} // preflight and two checkpoints pass
	res, rec := runScan(t, context.Background(), cfg, testScanConfig(r))
	if res.Status != StatusInsufficientSpace {
		t.Fatalf("status = %v (%v), want insufficient space", res.Status, res.Err)
	}
	reg := filepath.Join(r.archive, "arxgo-registry.csv")
	if !exists(fsops.PartPath(reg)) || exists(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.ReportFile)) {
		t.Error("stopped scan must keep its part file and write no report")
	}
	if len(rec.messages("free space fell below --min-free during scan; free space and rerun to resume")) != 1 {
		t.Errorf("low free space not logged: %v", rec.records)
	}
	cfg.FS = nil
	if res, _ := runScan(t, context.Background(), cfg, testScanConfig(r)); res.Status != StatusCompleted {
		t.Fatalf("resumed status = %v (%v)", res.Status, res.Err)
	}
	if got := mustRead(t, reg); !bytes.Equal(got, reference) {
		t.Errorf("resumed registry differs from the uninterrupted one\n--- got\n%s\n--- want\n%s", got, reference)
	}
}

// referenceRegistry scans r.archive once without interruption, returns the registry and puts the
// fixture's original registry content back.
func referenceRegistry(t *testing.T, r roots) []byte {
	t.Helper()
	reg := filepath.Join(r.archive, "arxgo-registry.csv")
	original := mustRead(t, reg)
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 1000), testScanConfig(r))
	if res.Status != StatusCompleted && res.Status != StatusPartial {
		t.Fatalf("reference scan: %v (%v)", res.Status, res.Err)
	}
	out := mustRead(t, reg)
	if err := os.WriteFile(reg, original, 0o644); err != nil {
		t.Fatal(err)
	}
	return out
}

var errTestCrash = errors.New("simulated crash")

func TestScanExplicitRegistryInsideArchiveIsNotRegistered(t *testing.T) {
	r := newRoots(t)
	writeScanFile(t, r.archive, "reports/old.txt", []byte("x\n"))
	cfg := scanSessionConfig(r, newClock(), 1)
	cfg.Registry = filepath.Join(r.archive, "reports", "inventory.csv")
	sc := testScanConfig(r)
	sc.Registry = cfg.Registry
	for range 2 { // the second scan sees the first one's registry in the tree
		res, _ := runScan(t, context.Background(), cfg, sc)
		if res.Status != StatusCompleted {
			t.Fatalf("status = %v (%v)", res.Status, res.Err)
		}
		got := string(mustRead(t, cfg.Registry))
		if strings.Contains(got, "inventory.csv") || !strings.Contains(got, "reports/old.txt,") {
			t.Fatalf("registry = %q", got)
		}
	}
}
