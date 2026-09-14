//go:build integration

package integration

import (
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

// SeedEnv fixes the seed of the stage-1 proof (archive content and kill points); the seed of every
// run is logged so a failure can be replayed.
const SeedEnv = "ARXGO_TEST_SEED"

// VideoParentEnv optionally names an existing directory, for example on another device such as
// /dev/shm, in which the video archive is created so split and restore cross devices.
const VideoParentEnv = "ARXGO_TEST_VIDEO_PARENT"

// WAL records per video: split on the copy path writes begin, copied, verified, placed, stubbed,
// source_removed and commit; restore with --transfer copy keeps the source and writes one less.
const (
	splitCopyRecords   = 7
	restoreCopyRecords = 6
)

// TestStage1GeneratedArchive proves stage 1 through the built binary: scan, split killed twice
// and resumed, restore killed and replaced by a run with other options, then the round-trip gate
// (path, size, mtime and SHA-256 of every file and every directory) and the exit-code contract.
func TestStage1GeneratedArchive(t *testing.T) {
	seed := time.Now().UnixNano()
	if s := os.Getenv(SeedEnv); s != "" {
		v, err := strconv.ParseInt(s, 10, 64)
		if err != nil {
			t.Fatalf("%s=%q: %v", SeedEnv, s, err)
		}
		seed = v
	}
	t.Logf("seed %d (replay with %s=%d)", seed, SeedEnv, seed)
	rng := rand.New(rand.NewSource(seed))

	bin := buildArxgo(t)
	work := t.TempDir()
	bin.work = work
	archive := filepath.Join(work, "archive")
	video := filepath.Join(work, "video")
	if parent := os.Getenv(VideoParentEnv); parent != "" {
		dir, err := os.MkdirTemp(parent, "arxgo-stage1-")
		if err != nil {
			t.Fatalf("%s: %v", VideoParentEnv, err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		video = filepath.Join(dir, "video")
		t.Logf("video archive under %s", parent)
	}
	g := generateArchive(t, archive, rng)
	before := takeManifest(t, archive)
	t.Logf("generated archive: %d files, %d videos, ffmpeg clips %v", before.files(), len(g.videos), g.ffmpeg)

	common := []string{"--archive", archive, "--min-free", "0", "--log-level", "warn"}
	scanFlags := []string{"--video-extensions", customVideoExt}
	if g.ffmpeg && hasTool("ffprobe") {
		scanFlags = append(scanFlags, "--metadata", "media")
	}
	checkRefusals(t, bin, g, video, before)

	// Scan.
	if r := bin.run(t, append(append([]string{"scan"}, scanFlags...), common...)...); r.code != 0 {
		t.Fatalf("scan exit %d:\n%s", r.code, r.stderr)
	}
	if rep := runReport(t, archive, currentRunID(t, archive), "scan"); rep.Status != "completed" {
		t.Errorf("scan report status %q", rep.Status)
	}
	checkFileRegistry(t, g, before)
	checkNoLeftovers(t, archive)

	// Split, killed at two seeded points, resumed.
	split := append(append([]string{"split", "--video-archive", video, "--transfer", "copy", "--verify", "hash"},
		scanFlags...), common...)
	total := splitCopyRecords * len(g.videos)
	first := bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(total/3)}, split...)
	if r := bin.run(t, split...); r.code != 5 || !strings.Contains(r.stderr, "lock") {
		t.Fatalf("rerun after kill without --force-unlock: exit %d, want 5 naming the lock:\n%s", r.code, r.stderr)
	}
	remaining := total - first.records
	second := bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(remaining/2)}, append(split, "--force-unlock")...)
	if second.runID != first.runID {
		t.Errorf("killed split did not resume: run %s, then %s", first.runID, second.runID)
	}
	if r := bin.run(t, append(split, "--force-unlock")...); r.code != 0 {
		t.Fatalf("resumed split exit %d:\n%s", r.code, r.stderr)
	}
	if rep := runReport(t, archive, first.runID, "split"); rep.Status != "completed" || !rep.Resumed {
		t.Errorf("split report status=%q resumed=%v", rep.Status, rep.Resumed)
	}
	checkSplitOutputs(t, g, video, before)
	checkNoLeftovers(t, archive, video)
	if r := bin.run(t, split...); r.code != 0 {
		t.Fatalf("split rerun after success exit %d:\n%s", r.code, r.stderr)
	}
	checkSplitOutputs(t, g, video, before)

	// Restore, killed at a seeded point, then replaced by a run with other defining options.
	restore := append([]string{"restore", "--video-archive", video}, common...)
	third := bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(restoreCopyRecords*len(g.videos)/2)},
		append(restore, "--transfer", "copy", "--verify", "hash", "--force-unlock")...)
	if r := bin.run(t, append(restore, "--new-run", "--force-unlock")...); r.code != 0 {
		t.Fatalf("restore after kill exit %d:\n%s", r.code, r.stderr)
	}
	restoreID := currentRunID(t, archive)
	if restoreID == third.runID {
		t.Fatalf("--new-run restore resumed the killed run %s", restoreID)
	}
	if rep := runReport(t, archive, restoreID, "restore"); rep.Status != "completed" {
		t.Errorf("restore report status %q", rep.Status)
	}
	checkRestoreOutputs(t, g, video, restoreID)
	checkNoLeftovers(t, archive, video)
	if r := bin.run(t, restore...); r.code != 0 {
		t.Fatalf("restore rerun after success exit %d:\n%s", r.code, r.stderr)
	}

	if diff := diffManifests(before, takeManifest(t, archive)); len(diff) > 0 {
		t.Fatalf("round trip changed the archive (%d differences):\n%s", len(diff), strings.Join(diff, "\n"))
	}
	t.Logf("round trip: %d files and directories equal (path, size, mtime, sha256)", len(before))
}

// checkRefusals checks the refusing exit codes before any operation succeeded: each leaves the
// archive unchanged and no lock behind.
func checkRefusals(t *testing.T, bin *arxgoBin, g *genArchive, video string, before manifest) {
	t.Helper()
	cases := []struct {
		name string
		env  []string
		args []string
		want int
	}{
		{"usage: split without --video-archive", bin.env, []string{"split", "--archive", g.root}, 2},
		{"missing tool: media metadata with an empty tool search path", withPath(bin.env, t.TempDir()),
			[]string{"scan", "--archive", g.root, "--metadata", "media"}, 3},
		{"insufficient space", bin.env, []string{"split", "--archive", g.root, "--video-archive", video,
			"--min-free", "8000000TB", "--log-level", "warn"}, 4},
	}
	for _, c := range cases {
		r := bin.runEnv(t, c.env, c.args...)
		if r.code != c.want {
			t.Errorf("%s: exit %d, want %d:\n%s", c.name, r.code, c.want, r.stderr)
		}
		if c.want == 3 && !strings.Contains(r.stderr, "https://") {
			t.Errorf("%s: no download link:\n%s", c.name, r.stderr)
		}
		if diff := diffManifests(before, takeManifest(t, g.root)); len(diff) > 0 {
			t.Fatalf("%s changed the archive: %v", c.name, diff)
		}
		checkNoLeftovers(t, g.root)
	}
}

func withPath(env []string, dir string) []string {
	out := []string{"PATH=" + dir}
	for _, kv := range env {
		if !strings.HasPrefix(strings.ToUpper(kv), "PATH=") {
			out = append(out, kv)
		}
	}
	return out
}

func hasTool(name string) bool {
	_, err := tooltest.Path(name)
	return err == nil
}
