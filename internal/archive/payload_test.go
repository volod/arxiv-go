package archive

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// foreignRun writes a completed run directory of another payload into the archive state: a
// committed CATIA split, a committed CATIA restore of the video's rel_path and an unfinished
// preview event whose part file exists in the archive.
func foreignRun(t *testing.T, r roots, catia string, created time.Time) (runID, partFile string) {
	t.Helper()
	rd, err := state.CreateRunDir(r.archive, created, bytes.NewReader([]byte{9, 9, 9, 9}))
	if err != nil {
		t.Fatal(err)
	}
	ro := state.RunOptions{V: 1, RunID: rd.ID, Op: opSplit, Version: "test", CreatedAt: created,
		Archive: r.archive, Payload: string(PayloadCatia), CatiaArchive: catia,
		Defining: json.RawMessage(`{}`), Options: json.RawMessage(`{}`)}
	if err := state.WriteJSON(rd.File(state.OptionsFile), ro); err != nil {
		t.Fatal(err)
	}
	w, err := state.OpenWAL(rd.File(state.WALFile), rd.ID, state.WALOptions{Now: func() time.Time { return created }})
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	commit := func(op, rel, src, dst string) {
		begin, err := w.Begin(state.Begin{Op: op, RelPath: rel, Src: src, Dst: dst, Size: 4, Mtime: created, Transfer: state.TransferRename})
		if err != nil {
			t.Fatal(err)
		}
		for _, step := range []state.Step{state.StepPlaced, state.StepCommit} {
			if _, err := w.Append(begin.TxID, step, state.Record{}); err != nil {
				t.Fatal(err)
			}
		}
	}
	part := "cad/fixture-part-img01.png"
	commit(opSplit, "cad/fixture-part.CATPart", filepath.Join(r.archive, "cad", "fixture-part.CATPart"), filepath.Join(catia, "cad", "fixture-part.CATPart"))
	commit(opRestore, "nested/clip.mp4", filepath.Join(catia, "nested", "clip.mp4"), filepath.Join(r.archive, "nested", "clip.mp4"))
	if _, err := w.BeginEvent(state.PreviewEvents, "cad/fixture-part.CATPart", filepath.Join(r.archive, filepath.FromSlash(part)), 0, false); err != nil {
		t.Fatal(err)
	}
	partFile = filepath.Join(r.archive, "cad", "fixture-part-img01.arxgo-part.png")
	if err := os.MkdirAll(filepath.Dir(partFile), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(partFile, []byte("foreign"), 0o644); err != nil {
		t.Fatal(err)
	}
	return rd.ID, partFile
}

func TestForeignPayloadRunsAreExcludedFromVideoReplay(t *testing.T) {
	r, src, dst := splitFixture(t)
	catia := filepath.Join(filepath.Dir(r.archive), "catia")
	cfg, c := splitConfig(r, "auto")
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	foreign, partFile := foreignRun(t, r, catia, newClock().Now().Add(time.Hour))

	history, err := readHistory(r.archive, PayloadVideo)
	if err != nil || len(history) != 1 || history[0].id == foreign || history[0].mirror != r.video {
		t.Fatalf("video history = %+v, %v", history, err)
	}
	if other, err := readHistory(r.archive, PayloadCatia); err != nil || len(other) != 1 || other[0].id != foreign || other[0].mirror != catia {
		t.Fatalf("catia history = %+v, %v", other, err)
	}

	writeVideo(t, r.archive, "later/new.mp4")
	cfg, c = splitConfig(r, "auto")
	cfg.NewRun = true
	if res := runSplit(t, cfg, c); res.Status != StatusCompleted {
		t.Fatalf("second split = %+v", res)
	}
	checkSplit(t, src, dst)
	if !exists(partFile) {
		t.Fatal("video split removed a part file recorded by another payload")
	}
	for _, root := range []string{r.archive, r.video} {
		rows := readVideoCSV(t, filepath.Join(root, scanner.VideoRegistryName))
		if len(rows) != 3 {
			t.Fatalf("%s rows = %q", root, rows)
		}
		if row := videoRow(t, root, "nested/clip.mp4"); row == nil || row[2] != report.StatusMoved {
			t.Fatalf("%s: a foreign restore changed the video row: %q", root, row)
		}
		if row := videoRow(t, root, "cad/fixture-part.CATPart"); row != nil {
			t.Fatalf("%s: foreign payload row in the video registry: %q", root, row)
		}
	}

	rcfg, rc := restoreConfig(r, "auto")
	if res := runRestore(t, rcfg, rc); res.Status != StatusCompleted {
		t.Fatalf("restore = %+v", res)
	}
	if !exists(src) || exists(dst) || !exists(partFile) {
		t.Fatalf("restore: src=%v dst=%v foreign part=%v", exists(src), exists(dst), exists(partFile))
	}
	retired, _ := filepath.Glob(filepath.Join(r.archive, "arxgo-videos.restored-*.csv"))
	if len(retired) != 1 || strings.Contains(string(mustRead(t, retired[0])), "CATPart") {
		t.Fatalf("retired registry = %v", retired)
	}
}

func TestRunOptionsRecordPayloadAndMirrorRoot(t *testing.T) {
	r, _, _ := splitFixture(t)
	cfg, c := splitConfig(r, "auto")
	res := runSplit(t, cfg, c)
	if res.Status != StatusCompleted {
		t.Fatalf("split = %+v", res)
	}
	var raw map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.OptionsFile)), &raw); err != nil {
		t.Fatal(err)
	}
	if raw["payload"] != "video" || raw["video_archive"] != r.video || raw["catia_archive"] != nil {
		t.Fatalf("split options.json = %v", raw)
	}

	scan := runScanOptions(t, newRoots(t))
	if _, ok := scan["payload"]; ok || scan["video_archive"] != nil {
		t.Fatalf("scan options.json = %v", scan)
	}
}

func runScanOptions(t *testing.T, r roots) map[string]any {
	t.Helper()
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 100), testScanConfig(r))
	if res.Status != StatusCompleted {
		t.Fatalf("scan = %+v", res)
	}
	var raw map[string]any
	if err := json.Unmarshal(mustRead(t, filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.OptionsFile)), &raw); err != nil {
		t.Fatal(err)
	}
	return raw
}

func TestStartRequiresKnownPayload(t *testing.T) {
	for name, mutate := range map[string]func(*Config){
		"split without payload": func(cfg *Config) { cfg.Payload = Payload{} },
		"split without mirror":  func(cfg *Config) { cfg.Payload.Root = "" },
		"catia not executable":  func(cfg *Config) { cfg.Payload.Kind = PayloadCatia },
		"scan with payload":     func(cfg *Config) { cfg.Op = opScan },
	} {
		t.Run(name, func(t *testing.T) {
			r := newRoots(t)
			cfg := testConfig(r, newClock(), 100)
			mutate(&cfg)
			if s, err := Start(context.Background(), cfg); err == nil {
				s.Finish(context.Background(), nil)
				t.Fatal("Start accepted the payload")
			}
			if exists(state.StateDir(r.archive)) || exists(state.StateDir(r.video)) {
				t.Fatal("a rejected payload wrote run state")
			}
		})
	}
}

func TestReplacedRunOfAnotherPayloadNeedsOperator(t *testing.T) {
	for name, payload := range map[string]string{"catia": "catia", "missing": ""} {
		t.Run(name, func(t *testing.T) {
			r, _, dst := splitFixture(t)
			prev := crashSplit(t, r, "auto", "wal:placed")
			path := filepath.Join(state.StateDir(r.archive), "runs", prev, state.OptionsFile)
			var o state.RunOptions
			if err := state.ReadJSON(path, &o); err != nil {
				t.Fatal(err)
			}
			o.Payload, o.VideoArchive, o.CatiaArchive = payload, "", r.video
			if err := state.WriteJSON(path, o); err != nil {
				t.Fatal(err)
			}
			cfg, _ := splitConfig(r, "auto")
			cfg.RecovererFor = splitRecovererFor(t, r)
			cfg.NewRun, cfg.Lock.PID = true, 200
			_, err := Start(context.Background(), cfg)
			switch {
			case payload == "" && !errors.Is(err, state.ErrStateCorrupt):
				t.Fatalf("run without payload: Start = %v", err)
			case payload != "" && (!errors.Is(err, ErrUnrecoveredRun) || !strings.Contains(err.Error(), "--catia-archive")):
				t.Fatalf("run of another payload: Start = %v", err)
			}
			if id := currentRunID(t, r.archive); id != prev || !exists(dst) {
				t.Fatalf("current = %s (want %s), placed destination kept %v", id, prev, exists(dst))
			}
		})
	}
}
