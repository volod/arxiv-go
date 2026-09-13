package archive

import (
	"bytes"
	"context"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

func restoreConfig(r roots, mode string) (Config, RestoreConfig) {
	cfg := testConfig(r, newClock(), 100)
	cfg.Op = opRestore
	cfg.Preflight.Transfer = mode
	keepSource := mode == transferCopy
	c := RestoreConfig{
		Scan: ScanConfig{Root: r.video, Metadata: "file", LargeThreshold: 1024, Workers: 2, Window: 4,
			SkipPaths: []string{r.archive}},
		Transfer: mode, Verify: fsops.VerifyHash, RegistryUpdate: true, KeepSource: keepSource,
	}
	attachRestoreRecoverer(&cfg, &c, nil)
	return cfg, c
}

func attachRestoreRecoverer(cfg *Config, c *RestoreConfig, crash state.CrashHook) {
	r := NewRestoreResolver(RestoreResolver{
		FS: cfg.FS, Verify: c.Verify, Crash: crash,
		KeepStubs: c.KeepStubs, KeepSource: c.KeepSource, Archive: cfg.Archive,
	})
	c.Resolver = &r
	cfg.Recoverer = r
	cfg.Crash = crash
}

func runRestore(t *testing.T, cfg Config, c RestoreConfig) Result {
	t.Helper()
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = Restore(context.Background(), s, c)
	return s.Finish(context.Background(), err)
}

func splitThen(t *testing.T, r roots, src, dst string) {
	t.Helper()
	cfg, c := splitConfig(r, "copy")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	checkSplit(t, src, dst)
}

func fileSum(t *testing.T, path string) string {
	t.Helper()
	sum, err := hashFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return hex.EncodeToString(sum[:])
}

func TestRestoreRoundTripPreservesBytesMtimeAndHash(t *testing.T) {
	for _, mode := range []string{"auto", "copy"} {
		t.Run(mode, func(t *testing.T) {
			r, src, dst := splitFixture(t)
			mtime := time.Date(2024, 5, 1, 10, 22, 3, 123456789, time.UTC)
			if err := os.Chtimes(src, mtime, mtime); err != nil {
				t.Fatal(err)
			}
			wantSum := fileSum(t, src)
			wantInfo, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}
			splitThen(t, r, src, dst)
			cfg, c := restoreConfig(r, mode)
			if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("restore = %+v", res)
			}
			if got := mustRead(t, src); !bytes.Equal(got, videoFixture) {
				t.Fatal("restored bytes differ")
			}
			got, err := os.Stat(src)
			if err != nil {
				t.Fatal(err)
			}
			if got.Size() != wantInfo.Size() || !got.ModTime().Equal(wantInfo.ModTime()) {
				t.Fatalf("size/mtime = %d %s, want %d %s", got.Size(), got.ModTime(), wantInfo.Size(), wantInfo.ModTime())
			}
			if fileSum(t, src) != wantSum {
				t.Fatal("sha256 differs")
			}
			if exists(src + ".md") {
				t.Fatal("owned stub still present")
			}
			if mode == "copy" {
				if got := mustRead(t, dst); !bytes.Equal(got, videoFixture) {
					t.Fatal("video archive copy was removed")
				}
			} else if exists(dst) {
				t.Fatal("video archive source still present")
			}
			if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("rerun = %+v", res)
			}
			if fileSum(t, src) != wantSum {
				t.Fatal("rerun changed bytes")
			}
		})
	}
}

func TestRestoreMissingDirectorySkippedAndCreated(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	if err := os.RemoveAll(filepath.Dir(src)); err != nil {
		t.Fatal(err)
	}
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("skip missing dir = %+v", res)
	}
	if exists(src) || !exists(dst) {
		t.Fatal("skipped restore moved the video")
	}
	cfg, c = restoreConfig(r, "auto")
	c.CreateDirs = true
	attachRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("create-dirs = %+v", res)
	}
	if !bytes.Equal(mustRead(t, src), videoFixture) {
		t.Fatal("created dest bytes")
	}
}

func TestRestoreConflictSkippedVsOverwrite(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	if err := os.WriteFile(src, []byte("other-content"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusPartial {
		t.Fatalf("conflict = %+v", res)
	}
	if string(mustRead(t, src)) != "other-content" || !exists(dst) {
		t.Fatal("conflict mutated files")
	}
	cfg, c = restoreConfig(r, "auto")
	c.Overwrite = true
	attachRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("overwrite = %+v", res)
	}
	if !bytes.Equal(mustRead(t, src), videoFixture) {
		t.Fatal("overwrite did not replace destination")
	}
}

func TestRestoreNeverDeletesForeignStub(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	if err := os.WriteFile(src+".md", []byte("human notes\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if got := string(mustRead(t, src+".md")); got != "human notes\n" {
		t.Fatalf("foreign stub = %q", got)
	}
	if !bytes.Equal(mustRead(t, src), videoFixture) {
		t.Fatal("video not restored beside foreign stub")
	}
}

func TestRestoreKeepStubsLeavesOwnedStub(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	c.KeepStubs = true
	attachRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if !exists(src + ".md") {
		t.Fatal("kept stub was deleted")
	}
}

func TestRestoreDryRunDoesNotMove(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	cfg.DryRun = true
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("dry-run = %+v", res)
	}
	if exists(src) || !exists(dst) || !exists(src+".md") {
		t.Fatal("dry-run mutated the trees")
	}
}

func TestRestoreUpdatesAndRetiresVideoRegistry(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	res := runRestore(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	for _, root := range []string{r.archive, r.video} {
		if exists(filepath.Join(root, scanner.VideoRegistryName)) {
			t.Fatalf("live registry remains in %s", root)
		}
		matches, err := filepath.Glob(filepath.Join(root, "arxgo-videos.restored-*.csv"))
		if err != nil || len(matches) != 1 {
			t.Fatalf("retired csv in %s: %v %v", root, matches, err)
		}
		data := string(mustRead(t, matches[0]))
		if !strings.Contains(data, "restored") || strings.Contains(data, ",moved,") {
			t.Fatalf("retired csv = %s", data)
		}
	}
}

func TestRestoreUnregisteredVideoUsesSamePath(t *testing.T) {
	r := newRoots(t)
	extra := filepath.Join(r.video, "lone.mp4")
	if err := os.WriteFile(extra, videoFixture, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if !bytes.Equal(mustRead(t, filepath.Join(r.archive, "lone.mp4")), videoFixture) {
		t.Fatal("unregistered dest")
	}
	if exists(extra) {
		t.Fatal("unregistered source remains")
	}
}

func TestRestorePrunesEmptyVideoDirs(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if exists(filepath.Dir(dst)) {
		t.Fatal("empty nested video dir remains")
	}
}

func TestRestoreRegistryUpdateFalseLeavesMovedRows(t *testing.T) {
	r, src, dst := splitFixture(t)
	splitThen(t, r, src, dst)
	cfg, c := restoreConfig(r, "auto")
	c.RegistryUpdate = false
	attachRestoreRecoverer(&cfg, &c, nil)
	if res := runRestore(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	data := string(mustRead(t, filepath.Join(r.archive, scanner.VideoRegistryName)))
	if !strings.Contains(data, ",moved,") || strings.Contains(data, ",restored,") {
		t.Fatalf("csv = %s", data)
	}
}
