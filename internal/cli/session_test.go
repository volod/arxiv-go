package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/archive"
	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/state"
)

// withLockIdentity makes sessions started by run use a fixed host and pid, and treats every pid in
// alive as a live process.
func withLockIdentity(t *testing.T, pid int, alive ...int) {
	t.Helper()
	saved := sessionHooks
	t.Cleanup(func() { sessionHooks = saved })
	sessionHooks = func(cfg *archive.Config) {
		cfg.Lock = state.LockOptions{Host: "cli-test", PID: pid, Alive: func(p int) bool {
			for _, a := range alive {
				if a == p {
					return true
				}
			}
			return false
		}}
	}
}

func writeOwnerLock(t *testing.T, root string, pid int) state.LockInfo {
	t.Helper()
	owner := state.LockInfo{V: 1, RunID: "20260913T090000Z-0badf00d", PID: pid, Host: "cli-test",
		StartedAt: time.Date(2026, 9, 13, 9, 0, 0, 0, time.UTC), Op: "split", Role: state.RoleArchive, Root: root}
	data, _ := json.Marshal(owner)
	if err := os.MkdirAll(state.StateDir(root), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(state.LockPath(root), data, 0o644); err != nil {
		t.Fatal(err)
	}
	return owner
}

func TestSecondRunOnSameRootsExitsLockedNamingOwner(t *testing.T) {
	archive, video := fixture(t)
	withLockIdentity(t, 500, 400)
	owner := writeOwnerLock(t, video, 400)

	var out, errOut bytes.Buffer
	code := run(context.Background(), []string{"split", "--archive", archive, "--video-archive", video}, testEnv(&out, &errOut, noProcessEnv))
	if code != ExitLocked {
		t.Fatalf("exit code = %d, want %d (stderr %s)", code, ExitLocked, errOut.String())
	}
	for _, want := range []string{"is held by run " + owner.RunID, "pid 400", "owner_pid=400", "owner_run=" + owner.RunID} {
		if !strings.Contains(errOut.String(), want) {
			t.Errorf("stderr does not contain %q: %s", want, errOut.String())
		}
	}
	if _, err := os.Stat(state.LockPath(archive)); !os.IsNotExist(err) {
		t.Error("archive lock kept after the video archive lock was refused")
	}
}

func TestStaleLockExitCodesAndForceUnlock(t *testing.T) {
	archive, _ := fixture(t)
	withLockIdentity(t, 500) // no pid is alive
	writeOwnerLock(t, archive, 400)

	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	if code := run(context.Background(), []string{"--archive", archive}, e); code != ExitLocked {
		t.Fatalf("stale lock: exit code = %d, want %d", code, ExitLocked)
	}
	if !strings.Contains(errOut.String(), "--force-unlock") {
		t.Errorf("stale lock message lacks instructions: %s", errOut.String())
	}

	errOut.Reset()
	if code := run(context.Background(), []string{"--archive", archive, "--force-unlock"}, e); code != ExitOK {
		t.Fatalf("--force-unlock: exit code = %d (stderr %s)", code, errOut.String())
	}
	if !strings.Contains(errOut.String(), "took over stale run lock") {
		t.Errorf("takeover not logged: %s", errOut.String())
	}
	id, err := state.ReadCurrent(archive)
	if err != nil || id == "" {
		t.Fatalf("current = %q, %v", id, err)
	}
	runDir := filepath.Join(state.StateDir(archive), "runs", id)
	for _, name := range []string{state.OptionsFile, state.CheckpointFile, state.LogFile, state.ReportFile} {
		if _, err := os.Stat(filepath.Join(runDir, name)); err != nil {
			t.Errorf("run file %s: %v", name, err)
		}
	}
	if _, err := os.Stat(state.LockPath(archive)); !os.IsNotExist(err) {
		t.Error("lock kept after the run finished")
	}
}

func TestDefiningOptionsIgnoreRuntimeFlags(t *testing.T) {
	root, _ := fixture(t)
	withLockIdentity(t, 500)
	var got []archive.Config
	saved := sessionHooks
	sessionHooks = func(cfg *archive.Config) { saved(cfg); got = append(got, *cfg) }

	var out, errOut bytes.Buffer
	e := testEnv(&out, &errOut, noProcessEnv)
	fakeTools(t, &e, media.FFprobe)
	run(context.Background(), []string{"--archive", root}, e)
	run(context.Background(), []string{"--archive", root, "--log-level", "debug", "--checkpoint-every", "7", "--dry-run", "--new-run"}, e)
	run(context.Background(), []string{"--archive", root, "--metadata", "media"}, e)
	if len(got) != 3 {
		t.Fatalf("sessions = %d", len(got))
	}
	enc := func(v any) string { b, _ := json.Marshal(v); return string(b) }
	if enc(got[0].Defining) != enc(got[1].Defining) {
		t.Errorf("runtime flags changed the defining options:\n%s\n%s", enc(got[0].Defining), enc(got[1].Defining))
	}
	if enc(got[0].Defining) == enc(got[2].Defining) {
		t.Error("--metadata did not change the defining options")
	}
	if enc(got[0].Options) == enc(got[1].Options) {
		t.Error("options.json content does not record runtime flags")
	}
}

func TestRecovererForRequiresRunPayload(t *testing.T) {
	archiveDir, video := fixture(t)
	common := Common{Archive: archiveDir, Payload: PayloadVideo, VideoArchive: video}
	split, _ := json.Marshal(SplitOptions{Common: common, Transfer: TransferAuto, Verify: VerifyHash})
	restore, _ := json.Marshal(RestoreOptions{Common: common, Transfer: TransferCopy, Verify: VerifySize, Descriptions: PolicyKeep})
	if r, err := recovererFor(OpSplit, archive.PayloadVideo, split); err != nil {
		t.Fatalf("video split: %v", err)
	} else if sr, ok := r.(archive.SplitResolver); !ok || sr.Descriptions == nil {
		t.Fatalf("video split resolver = %#v", r)
	}
	if r, err := recovererFor(OpRestore, archive.PayloadVideo, restore); err != nil {
		t.Fatalf("video restore: %v", err)
	} else if rr, ok := r.(archive.RestoreResolver); !ok || rr.Payload != archive.PayloadVideo || !rr.KeepSource || !rr.KeepDescriptions {
		t.Fatalf("video restore resolver = %#v", r)
	}
	noPayload, _ := json.Marshal(SplitOptions{Common: Common{Archive: archiveDir, VideoArchive: video}})
	for name, tc := range map[string]struct {
		payload archive.PayloadKind
		raw     json.RawMessage
	}{
		"run without payload":     {"", split},
		"options without payload": {archive.PayloadVideo, noPayload},
		"other payload":           {archive.PayloadCatia, split},
		"scan":                    {archive.PayloadVideo, json.RawMessage(`{"Payload":"video"}`)},
	} {
		op := OpSplit
		if name == "scan" {
			op = OpScan
		}
		if _, err := recovererFor(op, tc.payload, tc.raw); err == nil {
			t.Errorf("%s: resolver rebuilt", name)
		}
	}
}

func TestSplitRunRecordsPayloadAsDefiningOption(t *testing.T) {
	archiveDir, video := fixture(t)
	withLockIdentity(t, 500)
	var out, errOut bytes.Buffer
	if code := run(context.Background(), []string{"split", "--archive", archiveDir, "--video-archive", video}, testEnv(&out, &errOut, noProcessEnv)); code != ExitOK {
		t.Fatalf("split exit = %d: %s", code, errOut.String())
	}
	id, err := state.ReadCurrent(archiveDir)
	if err != nil || id == "" {
		t.Fatalf("current = %q, %v", id, err)
	}
	var o struct {
		Payload      string `json:"payload"`
		VideoArchive string `json:"video_archive"`
		Defining     struct{ Payload string }
	}
	if err := state.ReadJSON(filepath.Join(state.StateDir(archiveDir), "runs", id, state.OptionsFile), &o); err != nil {
		t.Fatal(err)
	}
	if o.Payload != PayloadVideo || o.VideoArchive != video || o.Defining.Payload != PayloadVideo {
		t.Fatalf("options.json payload = %+v", o)
	}
}
