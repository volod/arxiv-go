//go:build integration

package integration

import (
	"bytes"
	"math/rand"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"
)

// WAL records per file on the copy paths: CATIA split writes the seven split steps plus text_begin
// and text_done; CATIA restore with --transfer copy keeps the source (six steps) plus text_delete
// and text_deleted.
const (
	catiaSplitCopyRecords   = splitCopyRecords + 2
	catiaRestoreCopyRecords = restoreCopyRecords + 2
)

// TestCatiaSplitRestoreRoundTrip proves the CATIA payload through the built binary alongside a video
// split of the same archive into a separate video archive: video split killed and resumed, CATIA
// split with --catia-text killed twice and resumed, then restores of both payloads killed so that each
// payload's next command rolls the other's interrupted run forward, and the round-trip gate.
func TestCatiaSplitRestoreRoundTrip(t *testing.T) {
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
	mirrors := work
	if parent := os.Getenv(VideoParentEnv); parent != "" {
		dir, err := os.MkdirTemp(parent, "arxgo-catia-roundtrip-")
		if err != nil {
			t.Fatalf("%s: %v", VideoParentEnv, err)
		}
		t.Cleanup(func() { _ = os.RemoveAll(dir) })
		mirrors = dir
		t.Logf("mirror roots under %s", parent)
	}
	video, catia := filepath.Join(mirrors, "video"), filepath.Join(mirrors, "catia")
	g := generateArchive(t, archive, rng)
	g.addCatia(t, rng)
	before := takeManifest(t, archive)
	t.Logf("generated archive: %d files, %d videos, %d CATIA files, ffmpeg clips %v",
		before.files(), len(g.videos), len(g.catia.files), g.ffmpeg)

	common := []string{"--archive", archive, "--min-free", "0", "--log-level", "warn"}
	scanFlags := []string{"--video-extensions", customVideoExt}
	checkCatiaRefusals(t, bin, g, video, catia, before)

	if r := bin.run(t, append(append([]string{"scan"}, scanFlags...), common...)...); r.code != 0 {
		t.Fatalf("scan exit %d:\n%s", r.code, r.stderr)
	}
	checkFileRegistry(t, g, before)

	// Video split, killed once and resumed.
	vsplit := append(append([]string{"split", "--video-archive", video, "--transfer", "copy", "--verify", "hash"},
		scanFlags...), common...)
	bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(splitCopyRecords*len(g.videos)/3)}, vsplit...)
	if r := bin.run(t, append(vsplit, "--force-unlock")...); r.code != 0 {
		t.Fatalf("resumed video split exit %d:\n%s", r.code, r.stderr)
	}
	checkSplitOutputs(t, g, video, before)
	videoCSV := readFile(t, filepath.Join(archive, "arxgo-videos.csv"))

	// CATIA split with text sidecars, killed at two seeded points and resumed.
	csplit := append(append([]string{"split", "--catia", "--catia-text", "--catia-archive", catia,
		"--transfer", "copy", "--verify", "hash"}, scanFlags...), common...)
	total := catiaSplitCopyRecords * len(g.catia.files)
	first := bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(total/3)}, csplit...)
	second := bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn((total-first.records)/2)},
		append(csplit, "--force-unlock")...)
	if second.runID != first.runID {
		t.Errorf("killed CATIA split did not resume: run %s, then %s", first.runID, second.runID)
	}
	if r := bin.run(t, append(csplit, "--force-unlock")...); r.code != 0 {
		t.Fatalf("resumed CATIA split exit %d:\n%s", r.code, r.stderr)
	}
	if rep := runReport(t, archive, first.runID, "split"); rep.Status != "completed" || !rep.Resumed ||
		rep.Counters.CatiaDone != int64(len(g.catia.files)) || rep.Counters.TextsDone != int64(len(g.catia.files)) ||
		rep.Counters.VideosDone != 0 || rep.Counters.TextsFailed != 0 {
		t.Errorf("CATIA split report status=%q resumed=%v counters=%+v", rep.Status, rep.Resumed, rep.Counters)
	}
	checkCatiaSplitOutputs(t, g, catia, before)
	checkMirrorPayloads(t, g, video, catia)
	checkNoLeftovers(t, archive, video, catia)
	if !bytes.Equal(videoCSV, readFile(t, filepath.Join(archive, "arxgo-videos.csv"))) {
		t.Error("CATIA split changed arxgo-videos.csv")
	}
	catiaCSV := readFile(t, filepath.Join(archive, "arxgo-catia.csv"))
	if r := bin.run(t, csplit...); r.code != 0 {
		t.Fatalf("CATIA split rerun exit %d:\n%s", r.code, r.stderr)
	}
	if rep := runReport(t, archive, currentRunID(t, archive), "split"); rep.Counters.CatiaDone != 0 || rep.Counters.TextsDone != 0 {
		t.Errorf("CATIA split rerun moved or wrote: %+v", rep.Counters)
	}
	if !bytes.Equal(catiaCSV, readFile(t, filepath.Join(archive, "arxgo-catia.csv"))) {
		t.Error("CATIA split rerun changed arxgo-catia.csv")
	}
	checkCatiaIndex(t, bin, archive, work)
	checkSplitOutputs(t, g, video, before)

	// Restores: kill a CATIA restore, then a video restore (whose start rolls the CATIA run forward,
	// possibly killed during that recovery); the next CATIA restore rolls the video run forward and
	// cleans the sidecars of the files the killed CATIA run returned; then the video restore completes.
	crestore := append([]string{"restore", "--catia", "--catia-archive", catia}, common...)
	vrestore := append([]string{"restore", "--video-archive", video}, common...)
	bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(catiaRestoreCopyRecords*len(g.catia.files)/2)},
		append(crestore, "--transfer", "copy", "--verify", "hash", "--force-unlock")...)
	bin.runKilled(t, archive, killPoint{records: 1 + rng.Intn(restoreCopyRecords*len(g.videos)/2)},
		append(vrestore, "--transfer", "copy", "--verify", "hash", "--force-unlock")...)
	r := bin.run(t, append(crestore, "--force-unlock")...)
	if r.code != 0 {
		t.Fatalf("CATIA restore after kills exit %d:\n%s", r.code, r.stderr)
	}
	if !strings.Contains(r.stderr, "recovering the interrupted run") {
		t.Logf("CATIA restore found no open video transaction to recover (kill landed between transactions)")
	}
	catiaRestoreID := currentRunID(t, archive)
	if rep := runReport(t, archive, catiaRestoreID, "restore"); rep.Status != "completed" || rep.Counters.VideosDone != 0 {
		t.Errorf("CATIA restore report status=%q counters=%+v", rep.Status, rep.Counters)
	}
	checkCatiaRestoreOutputs(t, g, catia, catiaRestoreID)
	if r := bin.run(t, append(vrestore, "--force-unlock")...); r.code != 0 {
		t.Fatalf("video restore exit %d:\n%s", r.code, r.stderr)
	}
	videoRestoreID := currentRunID(t, archive)
	checkRestoreOutputs(t, g, video, videoRestoreID)
	checkNoLeftovers(t, archive, video, catia)
	for _, args := range [][]string{crestore, vrestore} {
		if r := bin.run(t, args...); r.code != 0 {
			t.Fatalf("restore rerun after success exit %d:\n%s", r.code, r.stderr)
		}
	}

	if diff := diffManifests(before, takeManifest(t, archive)); len(diff) > 0 {
		t.Fatalf("round trip changed the archive (%d differences):\n%s", len(diff), strings.Join(diff, "\n"))
	}
	t.Logf("round trip: %d files and directories equal (path, size, mtime, sha256)", len(before))
}

// checkCatiaRefusals checks CATIA usage errors (exit 2) before any operation: each leaves the
// archive unchanged, creates no mirror root and leaves no lock.
func checkCatiaRefusals(t *testing.T, bin *arxgoBin, g *genArchive, video, catia string, before manifest) {
	t.Helper()
	other := t.TempDir() // an existing root, so the restore refusal is the payload rule itself
	cases := []struct {
		name, want string
		args       []string
	}{
		{"catia with video archive", "--video-archive cannot be used with --catia",
			[]string{"split", "--catia", "--archive", g.root, "--video-archive", video}},
		{"catia-text without catia", "requires --catia",
			[]string{"split", "--catia-text", "--archive", g.root, "--video-archive", video}},
		{"catia and video", "mutually exclusive",
			[]string{"split", "--catia", "--video", "--archive", g.root, "--catia-archive", catia}},
		{"catia archive inside archive", "is inside --archive",
			[]string{"split", "--catia", "--archive", g.root, "--catia-archive", filepath.Join(g.root, "cad", "mirror")}},
		{"catia restore with previews delete", "--previews delete cannot be used with --catia",
			[]string{"restore", "--catia", "--archive", g.root, "--catia-archive", other, "--previews", "delete"}},
	}
	for _, c := range cases {
		if r := bin.run(t, c.args...); r.code != 2 || !strings.Contains(r.stderr, c.want) {
			t.Errorf("%s: exit %d, want 2 naming %q:\n%s", c.name, r.code, c.want, r.stderr)
		}
		if diff := diffManifests(before, takeManifest(t, g.root)); len(diff) > 0 {
			t.Fatalf("%s changed the archive: %v", c.name, diff)
		}
		for _, root := range []string{video, catia} {
			if _, err := os.Stat(root); !os.IsNotExist(err) {
				t.Fatalf("%s created %s (%v)", c.name, root, err)
			}
		}
		checkNoLeftovers(t, g.root)
	}
}
