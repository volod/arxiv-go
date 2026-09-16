package archive

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/testmp4"
)

var videoFixture = testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 320, Height: 240}}, MdatBytes: 2048})

func splitFixture(t *testing.T) (roots, string, string) {
	t.Helper()
	r := newRoots(t)
	src := filepath.Join(r.archive, "nested", "clip.mp4")
	dst := filepath.Join(r.video, "nested", "clip.mp4")
	if err := os.Mkdir(filepath.Dir(src), 0o750); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src, videoFixture, 0o640); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(r.archive, "notes.txt"), []byte("keep"), 0o644); err != nil {
		t.Fatal(err)
	}
	return r, src, dst
}

func splitConfig(r roots, mode string) (Config, SplitConfig) {
	cfg := testConfig(r, newClock(), 100)
	cfg.Preflight.Transfer = mode
	c := SplitConfig{
		Scan: ScanConfig{Root: r.archive, Registry: filepath.Join(r.archive, "arxgo-registry.csv"),
			Metadata: "file", LargeThreshold: 1024, Workers: 2, Window: 4,
			SkipPaths: []string{r.video}},
		Transfer: mode, Verify: fsops.VerifyHash,
	}
	c.Descriptions = NewMarkdownDescription(DescriptionConfig{
		Archive: r.archive, Mirror: r.video, Registry: c.Scan.Registry,
		Version: "test", Verify: c.Verify,
	})
	attachRecoverer(&cfg, c, nil)
	return cfg, c
}

func attachRecoverer(cfg *Config, c SplitConfig, crash state.CrashHook) {
	r := NewSplitResolver(cfg.FS, c.Verify, crash)
	r.Descriptions = c.Descriptions
	if m, ok := c.Descriptions.(*MarkdownDescription); ok && crash != nil {
		m.cfg.Crash = crash
	}
	cfg.Recoverer = r
}

func runSplit(t *testing.T, cfg Config, c SplitConfig) Result {
	t.Helper()
	s, err := Start(context.Background(), cfg)
	if err != nil {
		t.Fatal(err)
	}
	err = Split(context.Background(), s, c)
	return s.Finish(context.Background(), err)
}

func checkSplit(t *testing.T, src, dst string) {
	t.Helper()
	if exists(src) {
		t.Error("source still exists")
	}
	if got := mustRead(t, dst); !bytes.Equal(got, videoFixture) {
		t.Error("destination bytes differ")
	}
	if got := string(mustRead(t, src+".md")); !strings.HasPrefix(got, "arxgo: nested/clip.mp4\n") {
		t.Error("description missing marker or rel_path")
	}
	if exists(fsops.PartPath(dst)) {
		t.Error("part file remains")
	}
}

func TestSplitMovesOnlyVideosAndRerunIsNoop(t *testing.T) {
	for _, mode := range []string{"auto", "copy"} {
		t.Run(mode, func(t *testing.T) {
			r, src, dst := splitFixture(t)
			cfg, c := splitConfig(r, mode)
			if mode == "auto" {
				c.StageCopy = func(context.Context, string, string, fsops.CopyOptions) (fsops.CopyResult, error) {
					return fsops.CopyResult{}, errors.New("same-device auto copied instead of renamed")
				}
			}
			if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("first run = %+v", res)
			}
			checkSplit(t, src, dst)
			if got := string(mustRead(t, filepath.Join(r.archive, "notes.txt"))); got != "keep" {
				t.Errorf("non-video content = %q", got)
			}
			if fi, err := os.Stat(filepath.Dir(dst)); err != nil || fi.Mode().Perm() != 0o750 {
				t.Errorf("mirror directory mode = %v, %v", fi, err)
			}
			if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("second run = %+v", res)
			}
			checkSplit(t, src, dst)
		})
	}
}

func TestSplitAutoUsesCopyOnInjectedOtherDevice(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	cfg.FS = &fakeFS{
		device: func(p string) string {
			if strings.HasPrefix(p, r.video) {
				return "video"
			}
			return "archive"
		},
		space: map[string]fsops.Space{
			"archive": {Total: 1 << 40, Available: 1 << 39},
			"video":   {Total: 1 << 40, Available: 1 << 39},
		},
	}
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	checkSplit(t, src, dst)
	wal, err := os.ReadFile(filepath.Join(state.StateDir(r.archive), "runs", currentRunID(t, r.archive), state.WALFile))
	if err != nil || !strings.Contains(string(wal), `"transfer":"copy"`) {
		t.Fatalf("expected copy transaction: %v, %s", err, wal)
	}
}

func currentRunID(t *testing.T, root string) string {
	t.Helper()
	id, err := state.ReadCurrent(root)
	if err != nil {
		t.Fatal(err)
	}
	return id
}

func TestSplitAdoptsAndConflictsWithoutOverwriting(t *testing.T) {
	for _, conflict := range []bool{false, true} {
		t.Run(map[bool]string{false: "adopt", true: "conflict"}[conflict], func(t *testing.T) {
			r, src, dst := splitFixture(t)
			if err := os.Mkdir(filepath.Dir(dst), 0o755); err != nil {
				t.Fatal(err)
			}
			body := append([]byte(nil), videoFixture...)
			if conflict {
				body[len(body)-1] ^= 1
			}
			if err := os.WriteFile(dst, body, 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, c := splitConfig(r, "auto")
			res := runSplit(t, cfg, c)
			if conflict {
				if res.Status != StatusPartial || !exists(src) || exists(src+".md") {
					t.Fatalf("conflict = %+v", res)
				}
			} else if res.Status != StatusCompleted {
				t.Fatalf("adopt = %+v", res)
			} else {
				checkSplit(t, src, dst)
			}
			if got := mustRead(t, dst); !bytes.Equal(got, body) {
				t.Error("existing destination was overwritten")
			}
		})
	}
}

func TestSplitSizeVerifyAdoptsSameSizeDifferentBytes(t *testing.T) {
	r, src, dst := splitFixture(t)
	if err := os.Mkdir(filepath.Dir(dst), 0o755); err != nil {
		t.Fatal(err)
	}
	body := append([]byte(nil), videoFixture...)
	body[len(body)-1] ^= 1
	if err := os.WriteFile(dst, body, 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "copy")
	c.Verify = fsops.VerifySize
	c.Descriptions = NewMarkdownDescription(DescriptionConfig{
		Archive: r.archive, Mirror: r.video, Registry: c.Scan.Registry,
		Version: "test", Verify: c.Verify,
	})
	attachRecoverer(&cfg, c, nil)
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("size adopt = %+v", res)
	}
	if exists(src) {
		t.Error("source not removed after size adopt")
	}
	if got := mustRead(t, dst); !bytes.Equal(got, body) {
		t.Error("same-size destination was overwritten")
	}
}

func TestSplitDryRunLeavesVideoAndDescriptionUntouched(t *testing.T) {
	r, src, dst := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	cfg.DryRun = true
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("dry run = %+v", res)
	}
	if !exists(src) || exists(dst) || exists(src+".md") || exists(c.Scan.Registry) {
		t.Fatal("dry run mutated archive data")
	}
	if exists(filepath.Join(r.archive, "arxgo-videos.csv")) || exists(filepath.Join(r.archive, "arxgo-videos.md")) {
		t.Fatal("dry run wrote a video registry")
	}
}

func TestSplitDescriptionCollisionUsesFallback(t *testing.T) {
	r, src, dst := splitFixture(t)
	if err := os.WriteFile(src+".md", []byte("user notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	if got := string(mustRead(t, src+".md")); got != "user notes" {
		t.Errorf("foreign description changed: %q", got)
	}
	if !exists(src+".arxgo.md") || !exists(dst) {
		t.Error("fallback description or video missing")
	}
}
