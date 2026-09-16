package archive

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

// videoRow returns the registry row of rel in root's arxgo-videos.csv, or nil.
func videoRow(t *testing.T, root, rel string) []string {
	t.Helper()
	for _, row := range readVideoCSV(t, filepath.Join(root, scanner.VideoRegistryName))[1:] {
		if row[0] == rel {
			return row
		}
	}
	return nil
}

func TestRestoreReturnsVideoSelectedByExtension(t *testing.T) {
	r := newRoots(t)
	src := filepath.Join(r.archive, "old", "clip.bik")
	body := []byte{0, 1, 2, 3, 250, 251, 252, 253, 0, 0, 7}
	if err := os.MkdirAll(filepath.Dir(src), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "auto")
	c.Scan.VideoExtensions = []string{".bik"}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted || exists(src) {
		t.Fatalf("split = %+v, source still present %v", res, exists(src))
	}
	rcfg, rc := restoreConfig(r, "auto")
	if res := runRestore(t, rcfg, rc); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if got, err := os.ReadFile(src); err != nil || !bytes.Equal(got, body) {
		t.Fatalf("video selected by extension not restored: %v", err)
	}
	if exists(filepath.Join(r.video, "old", "clip.bik")) || exists(src+".md") {
		t.Fatal("video archive copy or description remains")
	}
}

func TestRestoreIgnoresRegistryPathsOutsideArchive(t *testing.T) {
	for name, rel := range map[string]string{"parent": "../escaped.mp4", "reserved": "arxgo-registry.csv"} {
		t.Run(name, func(t *testing.T) {
			r, src, dst := splitFixture(t)
			splitThen(t, r, src, dst)
			for _, root := range []string{r.archive, r.video} {
				p := filepath.Join(root, scanner.VideoRegistryName)
				data := strings.Replace(string(mustRead(t, p)), "\nnested/clip.mp4,", "\n"+rel+",", 1)
				if err := os.WriteFile(p, []byte(data), 0o644); err != nil {
					t.Fatal(err)
				}
			}
			before, err := report.LoadRegistry(filepath.Join(r.archive, "arxgo-registry.csv"))
			if err != nil {
				t.Fatal(err)
			}
			cfg, c := restoreConfig(r, "auto")
			rec := &recorder{}
			cfg.Console = rec
			if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("restore = %+v", res)
			}
			if exists(filepath.Join(filepath.Dir(r.archive), "escaped.mp4")) {
				t.Fatal("restore wrote outside the archive")
			}
			// The file registry is not replaced by a payload row naming it: restore only sets the
			// location of the video it returned.
			after, err := report.LoadRegistry(filepath.Join(r.archive, "arxgo-registry.csv"))
			if err != nil {
				t.Fatalf("file registry replaced: %v", err)
			}
			for i := range before {
				if before[i].RelPath == "nested/clip.mp4" {
					before[i].Location = report.LocationArchive
				}
			}
			if !reflect.DeepEqual(after, before) {
				t.Fatalf("restore changed the file registry beyond location:\n got %+v\nwant %+v", after, before)
			}
			if !bytes.Equal(mustRead(t, src), videoFixture) || exists(dst) {
				t.Fatal("video not restored to its own relative path")
			}
			if len(rec.messages("video registry row ignored: a path is not a local path below its root")) == 0 {
				t.Fatal("ignored row not logged")
			}
		})
	}
}

func TestSplitAfterRestoreKeepsRestoredStatus(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	c.KeepDescriptions = true // keeps the live registry instead of retiring it
	attachRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	restoreRun := currentRunID(t, r.archive)
	// The operator removes the restored video and adds a new one before splitting again.
	if err := os.Remove(src); err != nil {
		t.Fatal(err)
	}
	writeVideo(t, r.archive, "later/new.mp4")
	scfg, sc := splitConfig(r, "auto")
	scfg.Lock.PID = 300
	if res := runSplit(t, scfg, sc); res.Status != StatusCompleted {
		t.Fatalf("second split = %+v", res)
	}
	for _, root := range []string{r.archive, r.video} {
		old := videoRow(t, root, "nested/clip.mp4")
		if old == nil || old[2] != "restored" || old[8] != restoreRun {
			t.Fatalf("%s: restored row = %q, want status restored by %s", root, old, restoreRun)
		}
		wantURL := report.FileURL(filepath.ToSlash(filepath.Join(r.archive, "nested", "clip.mp4")))
		if old[3] != wantURL {
			t.Errorf("%s: restored local URL = %q, want %q", root, old[3], wantURL)
		}
		if row := videoRow(t, root, "later/new.mp4"); row == nil || row[2] != "moved" {
			t.Fatalf("%s: new row = %q", root, row)
		}
	}
}

func TestSplitAfterRetiredRegistryKeepsHistory(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if exists(filepath.Join(r.archive, scanner.VideoRegistryName)) {
		t.Fatal("fixture: registry not retired")
	}
	if err := os.Rename(src, filepath.Join(r.archive, "notes-clip.bin")); err != nil {
		t.Fatal(err)
	}
	writeVideo(t, r.archive, "later/new.mp4")
	scfg, sc := splitConfig(r, "auto")
	if res := runSplit(t, scfg, sc); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if row := videoRow(t, r.archive, "nested/clip.mp4"); row == nil || row[2] != "restored" {
		t.Fatalf("history row = %q", row)
	}
	if row := videoRow(t, r.archive, "later/new.mp4"); row == nil || row[2] != "moved" {
		t.Fatalf("new row = %q", row)
	}
}

func TestRestoreCleanupIsLimitedToRestoredDirectories(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	userEmpty := filepath.Join(r.video, "kept-empty")
	locked := filepath.Join(r.video, "locked")
	for _, d := range []string{userEmpty, locked} {
		if err := os.Mkdir(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	want := StatusPartial // the scan counts the unreadable directory as skipped
	if _, err := os.ReadDir(locked); err == nil {
		want = StatusCompleted // running as a user who can read it anyway
	}
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != want {
		t.Fatalf("restore = %+v, want %v", res, want)
	}
	if exists(filepath.Dir(dst)) {
		t.Fatal("directory of the restored video remains")
	}
	if !exists(userEmpty) || !exists(locked) {
		t.Fatal("restore removed a directory that held no restored video")
	}
	if !bytes.Equal(mustRead(t, src), videoFixture) {
		t.Fatal("video not restored")
	}
	cfg, c = restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != want {
		t.Fatalf("rerun = %+v, want %v", res, want)
	}
}

func TestRestoreRemovesOwnedFallbackDescriptionAndRecordsIt(t *testing.T) {
	r, src, dst := splitFixture(t)
	if err := os.WriteFile(src+".md", []byte("human notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	fallback := src + ".arxgo.md"
	cfg, c := splitConfig(r, "copy")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted || !exists(fallback) {
		t.Fatalf("split = %+v, fallback description %v", res, exists(fallback))
	}
	rcfg, rc := restoreConfig(r, "auto")
	if res := runRestore(t, rcfg, rc); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if exists(fallback) || string(mustRead(t, src+".md")) != "human notes\n" || exists(dst) {
		t.Fatal("owned fallback description kept, foreign description changed, or video not restored")
	}
	recs, err := state.ReadWALRecords(filepath.Join(state.StateDir(r.archive), "runs", currentRunID(t, r.archive), state.WALFile))
	if err != nil {
		t.Fatal(err)
	}
	var description string
	for _, rec := range recs {
		if rec.Step == state.StepDescriptionRemoved {
			description = rec.Description
		}
	}
	if description != fallback {
		t.Fatalf("description_removed records %q, want %q", description, fallback)
	}
}

func TestRestoreCleansDirectoriesOfReplacedInterruptedRestore(t *testing.T) {
	r := newRoots(t)
	writeVideo(t, r.archive, "a/one.mp4")
	writeVideo(t, r.archive, "b/two.mp4")
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	h := &crashtest.Hook{FailAt: "wal:commit"} // after the first restore commits
	rcfg, rc := restoreConfig(r, "auto")
	attachRestoreRecoverer(&rcfg, &rc, h.Func())
	if res := runRestore(t, rcfg, rc); !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("crash = %+v", res)
	}
	rcfg, rc = restoreConfig(r, "auto")
	rcfg.Lock.PID = 200
	rcfg.NewRun = true
	rcfg.RecovererFor = func(string, PayloadKind, json.RawMessage) (Resolver, error) { return rcfg.Recoverer, nil }
	if res := runRestore(t, rcfg, rc); res.Status != StatusCompleted {
		t.Fatalf("new restore run = %+v", res)
	}
	for _, d := range []string{"a", "b"} {
		if exists(filepath.Join(r.video, d)) {
			t.Errorf("video archive directory %s remains", d)
		}
		if !exists(filepath.Join(r.archive, d)) {
			t.Errorf("archive directory %s missing", d)
		}
	}
}
