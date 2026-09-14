package archive

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// treeManifest lists every path under root except arxgo's run state, with file contents.
func treeManifest(t *testing.T, root string) map[string]string {
	t.Helper()
	m := map[string]string{}
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		rel, _ := filepath.Rel(root, p)
		if d.IsDir() && rel == state.DirName {
			return filepath.SkipDir
		}
		if !d.IsDir() {
			m[rel] = string(mustRead(t, p))
		} else {
			m[rel+"/"] = ""
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return m
}

// spaceFS puts both roots on one device with the given caller-available bytes.
func spaceFS(avail int64) *fakeFS {
	return &fakeFS{
		device: func(string) string { return "disk" },
		space:  map[string]fsops.Space{"disk": {Total: 1 << 50, Available: uint64(avail)}},
	}
}

func preflightConfig(t *testing.T, avail int64) (roots, Config, *recorder) {
	t.Helper()
	r := newRoots(t)
	if err := os.WriteFile(filepath.Join(r.archive, "clip.mp4"), []byte("video"), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg := testConfig(r, newClock(), 100)
	rec := &recorder{}
	cfg.Console, cfg.FS = rec, spaceFS(avail)
	cfg.Preflight = PreflightOptions{Transfer: "copy", MinFree: gib, Op: "ignored"}
	return r, cfg, rec
}

// One 10GiB video copied on a shared device: descriptions 4KiB + registries 2KiB + wal 2KiB + largest.
var (
	oneVideo       = Candidates{Count: 1, Bytes: 10 * gib, Largest: 10 * gib}
	oneVideoNeeded = 10*gib + 8*kib + gib
)

func TestSessionPreflightRefusesWithoutMutation(t *testing.T) {
	r, cfg, rec := preflightConfig(t, oneVideoNeeded-1)
	before := [2]map[string]string{treeManifest(t, r.archive), treeManifest(t, r.video)}
	s := start(t, cfg)
	req, err := s.Preflight(context.Background(), oneVideo)
	var short *InsufficientSpaceError
	if !errors.As(err, &short) || !errors.Is(err, ErrInsufficientSpace) || req.Shortfall() != 1 || req.Op != "split" {
		t.Fatalf("Preflight = %+v, %v", req, err)
	}
	res := s.Finish(context.Background(), err)
	if res.Status != StatusInsufficientSpace {
		t.Fatalf("status = %v", res.Status)
	}
	after := [2]map[string]string{treeManifest(t, r.archive), treeManifest(t, r.video)}
	if !reflect.DeepEqual(before, after) {
		t.Fatalf("roots changed:\nbefore %v\nafter  %v", before, after)
	}
	if exists(s.Run.File(state.ReportFile)) || exists(state.LockPath(r.archive)) || exists(state.LockPath(r.video)) {
		t.Error("refused run wrote a report or kept a lock; it must stay resumable and unlocked")
	}
	lines := rec.messages("preflight device")
	if len(lines) != 1 || lines[0]["shortfall_bytes"] != "1" || lines[0]["roles"] != "archive,video_archive" {
		t.Fatalf("preflight lines = %v", lines)
	}
	if len(rec.messages("preflight failed: insufficient free space")) != 1 {
		t.Error("no failure summary logged")
	}
	if cp := readCheckpoint(t, s); cp.Phase != "preflight" {
		t.Errorf("checkpoint phase = %q", cp.Phase)
	}
	// The run log holds the computation too.
	if !strings.Contains(string(mustRead(t, s.Run.File(state.LogFile))), `"shortfall_bytes":1`) {
		t.Error("run log lacks the preflight computation")
	}

	// After space is freed the same command resumes the run and passes at exactly the threshold.
	cfg.FS = spaceFS(oneVideoNeeded)
	s2 := start(t, cfg)
	if !s2.Resumed || s2.Run.ID != s.Run.ID {
		t.Fatalf("second process did not resume run %s", s.Run.ID)
	}
	if _, err := s2.Preflight(context.Background(), oneVideo); err != nil {
		t.Fatalf("at threshold: %v", err)
	}
	if res := s2.Finish(context.Background(), nil); res.Status != StatusCompleted {
		t.Fatalf("status = %v", res.Status)
	}
}

func TestDryRunPreflightRefusalWritesReport(t *testing.T) {
	_, cfg, _ := preflightConfig(t, 0)
	cfg.DryRun = true
	s := start(t, cfg)
	_, err := s.Preflight(context.Background(), oneVideo)
	if res := s.Finish(context.Background(), err); res.Status != StatusInsufficientSpace {
		t.Fatalf("status = %v (%v)", res.Status, err)
	}
	var rep state.Report
	if err := state.ReadJSON(s.Run.File(state.ReportFile), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Status != "insufficient_space" || !rep.DryRun {
		t.Fatalf("report = %+v", rep)
	}
}

func TestSessionPreflightProbeErrorAndCancel(t *testing.T) {
	_, cfg, _ := preflightConfig(t, 0)
	cfg.FS.(*fakeFS).err = errors.New("statfs: input/output error")
	s := start(t, cfg)
	_, err := s.Preflight(context.Background(), oneVideo)
	if err == nil || errors.Is(err, ErrInsufficientSpace) {
		t.Fatalf("probe error = %v", err)
	}
	if res := s.Finish(context.Background(), err); res.Status != StatusFailed {
		t.Fatalf("status = %v", res.Status)
	}

	ctx, cancel := context.WithCancel(context.Background())
	s = start(t, cfg)
	cancel()
	if _, err := s.Preflight(ctx, oneVideo); !errors.Is(err, context.Canceled) {
		t.Fatalf("canceled preflight = %v", err)
	}
	s.Finish(ctx, context.Canceled)
}

func TestSessionPreflightRealFilesystemRefusesImpossibleCopy(t *testing.T) {
	r, cfg, _ := preflightConfig(t, 0)
	cfg.FS = nil // real statfs / GetDiskFreeSpaceEx
	cfg.CreateVideoArchive, cfg.VideoArchive = true, filepath.Join(r.video, "new-root")
	cfg.DryRun = true
	before := treeManifest(t, r.video)
	s := start(t, cfg)
	// No real volume has 4 EiB free: the requirement always exceeds it.
	req, err := s.Preflight(context.Background(), Candidates{Count: 1, Bytes: 1 << 62, Largest: 1 << 62})
	if !errors.Is(err, ErrInsufficientSpace) || len(req.Devices) != 1 || !req.Devices[0].Known {
		t.Fatalf("Preflight = %+v, %v", req, err)
	}
	if res := s.Finish(context.Background(), err); res.Status != StatusInsufficientSpace {
		t.Fatalf("status = %v", res.Status)
	}
	if !reflect.DeepEqual(before, treeManifest(t, r.video)) {
		t.Error("dry run created the video archive root")
	}
}
