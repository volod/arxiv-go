package archive

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/crashtest"
)

// replacedHarness drives one payload through a restore interrupted between a commit and its
// sidecar deletion, and later runs that replace it.
type replacedHarness struct {
	archive, mirror string
	pid             int
	// restore returns a fresh restore run configuration with or without sidecar cleanup.
	restore func(cleanup bool) (Config, RestoreConfig)
	// other returns a restore of the other payload on the same archive.
	other func() (Config, RestoreConfig)
	// resplit moves restored files of the payload back into the mirror.
	resplit func(t *testing.T)
	// sidecars lists the owned sidecars of rel.
	sidecars func(rel string) []string
	files    []string
}

func videoReplacedHarness(t *testing.T) *replacedHarness {
	cfg, c, r, _ := previewFixture(t)
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	catia := filepath.Join(filepath.Dir(r.archive), "catia")
	if err := os.Mkdir(catia, 0o755); err != nil {
		t.Fatal(err)
	}
	return &replacedHarness{
		archive: r.archive, mirror: r.video, files: []string{"clip.mp4"},
		restore: func(cleanup bool) (Config, RestoreConfig) {
			rcfg, rc := restoreConfig(r, "auto")
			rc.DeletePreviews = cleanup
			return rcfg, rc
		},
		other: func() (Config, RestoreConfig) { return catiaRestoreConfig(catiaRoots{roots: r, catia: catia}, "auto") },
		resplit: func(t *testing.T) {
			scfg, sc := nextSplit(r, c)
			scfg.Lock.PID = 900
			if res := runSplit(t, scfg, sc); res.Status != StatusCompleted {
				t.Fatalf("second split = %+v", res)
			}
		},
		sidecars: func(string) []string {
			return []string{filepath.Join(r.archive, "clip-img01.png"), filepath.Join(r.archive, "clip-smpl01.mp4")}
		},
	}
}

func catiaReplacedHarness(t *testing.T) *replacedHarness {
	r, files, _ := catiaRoundTripFixture(t, fsops.System{})
	h := &replacedHarness{
		archive: r.archive, mirror: r.catia,
		restore: func(cleanup bool) (Config, RestoreConfig) {
			rcfg, rc := catiaRestoreConfig(r, "auto")
			rc.KeepDescriptions = !cleanup
			attachCatiaRestoreRecoverer(&rcfg, &rc, nil)
			return rcfg, rc
		},
		other: func() (Config, RestoreConfig) { return restoreConfig(r.roots, "auto") },
		resplit: func(t *testing.T) {
			scfg, sc := catiaSplitConfig(r, "auto")
			scfg.Lock.PID = 900
			if res := runSplit(t, scfg, sc); res.Status != StatusCompleted {
				t.Fatalf("second split = %+v", res)
			}
		},
		sidecars: func(rel string) []string {
			src := filepath.Join(r.archive, filepath.FromSlash(rel))
			if rel == "cad/deep/fixture.CATPart" {
				return []string{src + ".arxgo.text.md"}
			}
			return []string{src + ".text.md"}
		},
	}
	for rel := range files {
		h.files = append(h.files, rel)
	}
	return h
}

func (h *replacedHarness) run(t *testing.T, cfg Config, c RestoreConfig) Result {
	t.Helper()
	h.pid++
	cfg.Lock.PID = 300 + h.pid
	cfg.NewRun = true
	return runRestore(t, cfg, c)
}

// crash interrupts a restore at point and returns the files it had restored and its resolver.
func (h *replacedHarness) crash(t *testing.T, point string, cleanup bool) ([]string, Resolver) {
	t.Helper()
	cfg, c := h.restore(cleanup)
	hook := &crashtest.Hook{FailAt: point}
	rs := cfg.Recoverer.(RestoreResolver)
	rs.Crash, cfg.Crash = hook.Func(), hook.Func()
	cfg.Recoverer = rs
	if res := h.run(t, cfg, c); !errors.Is(res.Err, crashtest.ErrCrash) {
		t.Fatalf("crash at %s = %+v", point, res)
	}
	rs.Crash = nil
	var restored []string
	for _, rel := range h.files {
		if !exists(filepath.Join(h.mirror, filepath.FromSlash(rel))) {
			restored = append(restored, rel)
		}
	}
	if len(restored) != 1 {
		t.Fatalf("crashed restore returned %v", restored)
	}
	return restored, rs
}

func (h *replacedHarness) owned(t *testing.T, rels []string, want bool) {
	t.Helper()
	for _, rel := range rels {
		for _, p := range h.sidecars(rel) {
			if exists(p) != want {
				t.Errorf("%s: sidecar %s present=%v, want %v", rel, p, !want, want)
			}
		}
	}
}

func dropSidecarCleanup(t *testing.T, archive, runID string) {
	t.Helper()
	path := filepath.Join(state.StateDir(archive), "runs", runID, state.OptionsFile)
	var raw map[string]json.RawMessage
	if err := state.ReadJSON(path, &raw); err != nil {
		t.Fatal(err)
	}
	if _, ok := raw["sidecar_cleanup"]; !ok {
		t.Fatalf("restore options.json has no sidecar_cleanup: %s", mustRead(t, path))
	}
	delete(raw, "sidecar_cleanup")
	if err := state.WriteJSON(path, raw); err != nil {
		t.Fatal(err)
	}
}

func TestReplacedRestoreSidecarCleanup(t *testing.T) {
	payloads := map[string]func(*testing.T) *replacedHarness{"video": videoReplacedHarness, "catia": catiaReplacedHarness}
	scenarios := map[string]func(t *testing.T, h *replacedHarness){
		"new run after commit deletes and rerun is a no-op": func(t *testing.T, h *replacedHarness) {
			rels, _ := h.crash(t, "wal:commit", true)
			h.owned(t, rels, true)
			cfg, c := h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("replacing restore = %+v", res)
			}
			h.owned(t, rels, false)
			before := treeState(t, h.archive)
			cfg, c = h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("rerun = %+v", res)
			}
			sameTree(t, "archive after rerun", treeState(t, h.archive), before)
		},
		"new run after placed deletes": func(t *testing.T, h *replacedHarness) {
			rels, rs := h.crash(t, "wal:placed", true)
			cfg, c := h.restore(true)
			cfg.RecovererFor = func(string, PayloadKind, json.RawMessage) (Resolver, error) { return rs, nil }
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("replacing restore = %+v", res)
			}
			h.owned(t, rels, false)
		},
		"other payload recovers and next restore deletes": func(t *testing.T, h *replacedHarness) {
			rels, rs := h.crash(t, "wal:placed", true)
			ocfg, oc := h.other()
			ocfg.RecovererFor = func(string, PayloadKind, json.RawMessage) (Resolver, error) { return rs, nil }
			if res := h.run(t, ocfg, oc); res.Status != StatusCompleted {
				t.Fatalf("other payload restore = %+v", res)
			}
			h.owned(t, rels, true)
			cfg, c := h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("restore after recovery = %+v", res)
			}
			h.owned(t, rels, false)
		},
		"keep restore keeps, a later cleanup restore deletes": func(t *testing.T, h *replacedHarness) {
			rels, _ := h.crash(t, "wal:commit", true)
			cfg, c := h.restore(false)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("keep restore = %+v", res)
			}
			h.owned(t, rels, true)
			cfg, c = h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("cleanup restore = %+v", res)
			}
			h.owned(t, rels, false)
		},
		"crashed keep restore keeps": func(t *testing.T, h *replacedHarness) {
			rels, _ := h.crash(t, "wal:commit", false)
			cfg, c := h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("cleanup restore = %+v", res)
			}
			h.owned(t, rels, true)
		},
		"earlier build without sidecar_cleanup keeps": func(t *testing.T, h *replacedHarness) {
			rels, _ := h.crash(t, "wal:commit", true)
			dropSidecarCleanup(t, h.archive, currentRunID(t, h.archive))
			cfg, c := h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusCompleted {
				t.Fatalf("cleanup restore = %+v", res)
			}
			h.owned(t, rels, true)
		},
		"changed sidecar is logged, not reported": func(t *testing.T, h *replacedHarness) {
			rels, _ := h.crash(t, "wal:commit", true)
			changed := h.sidecars(rels[0])[0]
			data := append(mustRead(t, changed), "operator edit\n"...)
			if err := os.WriteFile(changed, data, 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, c := h.restore(true)
			res := h.run(t, cfg, c)
			if res.Status != StatusCompleted {
				t.Fatalf("cleanup restore = %+v", res)
			}
			if rep := readReport(t, h.archive, res.RunID); len(rep.Issues) != 0 {
				t.Fatalf("issues %+v", rep.Issues)
			}
			if got := mustRead(t, changed); string(got) != string(data) {
				t.Fatal("changed sidecar altered")
			}
		},
		"file split again is excluded": func(t *testing.T, h *replacedHarness) {
			rels, _ := h.crash(t, "wal:commit", true)
			h.resplit(t)
			// A different file at the destination makes the next restore skip the file.
			dst := filepath.Join(h.archive, filepath.FromSlash(rels[0]))
			if err := os.WriteFile(dst, []byte("different content"), 0o644); err != nil {
				t.Fatal(err)
			}
			cfg, c := h.restore(true)
			if res := h.run(t, cfg, c); res.Status != StatusPartial {
				t.Fatalf("cleanup restore = %+v", res)
			}
			h.owned(t, rels, true)
		},
	}
	for pname, setup := range payloads {
		for sname, scenario := range scenarios {
			t.Run(pname+"/"+sname, func(t *testing.T) {
				h := setup(t)
				scenario(t, h)
			})
		}
	}
}

func TestRestoreRecordsSidecarCleanup(t *testing.T) {
	for _, cleanup := range []bool{false, true} {
		r, src, dst := splitFixture(t)
		splitThen(t, r, src, dst)
		cfg, c := restoreConfig(r, "auto")
		c.DeletePreviews = cleanup
		res := runRestore(t, cfg, c)
		var o state.RunOptions
		if err := state.ReadJSON(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.OptionsFile), &o); err != nil {
			t.Fatal(err)
		}
		if o.SidecarCleanup == nil || *o.SidecarCleanup != cleanup {
			t.Fatalf("restore sidecar_cleanup = %v, want %v", o.SidecarCleanup, cleanup)
		}
	}
	r, _, _ := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	res := runSplit(t, cfg, c)
	var o state.RunOptions
	if err := state.ReadJSON(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.OptionsFile), &o); err != nil {
		t.Fatal(err)
	}
	if o.SidecarCleanup != nil {
		t.Fatal("split recorded sidecar_cleanup")
	}
	cfg.SidecarCleanup = true
	if _, err := Start(t.Context(), cfg); err == nil {
		t.Fatal("split accepted sidecar cleanup")
	}
	rcfg, rc := restoreConfig(r, "auto")
	s, err := Start(t.Context(), rcfg)
	if err != nil {
		t.Fatal(err)
	}
	rc.DeletePreviews = true
	if res := s.Finish(t.Context(), Restore(t.Context(), s, rc)); res.Err == nil {
		t.Fatal("restore accepted a cleanup policy different from the recorded one")
	}
}
