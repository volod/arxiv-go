package archive

import (
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

func catiaRestoreConfig(r catiaRoots, mode string) (Config, RestoreConfig) {
	cfg, c := restoreConfig(r.roots, mode)
	cfg.Payload = Payload{Kind: PayloadCatia, Root: r.catia}
	c.Scan.Root, c.Scan.SkipPaths = r.catia, []string{r.archive}
	attachCatiaRestoreRecoverer(&cfg, &c, nil)
	return cfg, c
}

func attachCatiaRestoreRecoverer(cfg *Config, c *RestoreConfig, crash state.CrashHook) {
	attachRestoreRecoverer(cfg, c, crash)
	rs := cfg.Recoverer.(RestoreResolver)
	rs.Payload = PayloadCatia
	cfg.Recoverer = rs
}

// fileState is what a round trip must preserve for one regular file.
type fileState struct {
	size  int64
	mtime time.Time
	sum   string
}

// treeState maps the slash rel_path of every regular file below root, outside the state directory
// and except the names in skip, to its size, mtime and SHA-256.
func treeState(t *testing.T, root string, skip ...string) map[string]fileState {
	t.Helper()
	out := map[string]fileState{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel := filepath.ToSlash(strings.TrimPrefix(strings.TrimPrefix(p, root), string(filepath.Separator)))
		if d.IsDir() {
			if rel == state.DirName {
				return filepath.SkipDir
			}
			return nil
		}
		for _, s := range skip {
			if matched, _ := filepath.Match(s, rel); matched {
				return nil
			}
		}
		fi, err := d.Info()
		if err != nil {
			return err
		}
		out[rel] = fileState{fi.Size(), fi.ModTime(), fileSum(t, p)}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func sameTree(t *testing.T, label string, got, want map[string]fileState) {
	t.Helper()
	for rel, w := range want {
		g, ok := got[rel]
		switch {
		case !ok:
			t.Errorf("%s: %s missing", label, rel)
		case g.size != w.size || !g.mtime.Equal(w.mtime) || g.sum != w.sum:
			t.Errorf("%s: %s = %+v, want %+v", label, rel, g, w)
		}
	}
	for rel := range got {
		if _, ok := want[rel]; !ok {
			t.Errorf("%s: unexpected %s", label, rel)
		}
	}
}

// catiaRoundTripFixture builds the mixed CATIA fixture with a foreign text sidecar and fixed
// mtimes, splits its videos into the video archive and returns the archive state to restore.
func catiaRoundTripFixture(t *testing.T, fsys fsops.Ops) (catiaRoots, map[string][]byte, map[string]fileState) {
	t.Helper()
	r, files := catiaFixture(t)
	writeScanFile(t, r.archive, "cad/deep/fixture.CATPart.text.md", []byte("foreign text notes\n"))
	at := time.Date(2023, 3, 4, 5, 6, 7, 0, time.UTC)
	for rel := range files {
		at = at.Add(time.Hour)
		if err := os.Chtimes(filepath.Join(r.archive, filepath.FromSlash(rel)), at, at); err != nil {
			t.Fatal(err)
		}
	}
	vcfg, vc := splitConfig(r.roots, "auto")
	if res := runSplit(t, vcfg, vc); res.Status != StatusCompleted {
		t.Fatalf("video split = %+v", res)
	}
	want := treeState(t, r.archive, "arxgo-registry.csv")
	cfg, c := catiaTextConfig(r, "auto")
	cfg.FS = fsys
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("catia split = %+v", res)
	}
	checkCatiaMoved(t, r, files)
	return r, files, want
}
