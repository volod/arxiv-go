package archive

import (
	"context"
	"encoding/binary"
	"encoding/csv"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/media/testmp4"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

func TestScanISOMetadataAndAudioOnlyRefinement(t *testing.T) {
	r := newRoots(t)
	video := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 640, Height: 360}, {Kind: "soun", Codec: "mp4a"}}, Title: "Title"})
	audio := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "soun", Codec: "mp4a"}}})
	bad := append([]byte(nil), video...)
	// Keep ftyp recognizable to MIME detection, but corrupt moov's declared size.
	binary.BigEndian.PutUint32(bad[20:24], uint32(len(bad)+100))
	writeScanFile(t, r.archive, "video.mp4", video)
	writeScanFile(t, r.archive, "audio-only.mp4", audio)
	writeScanFile(t, r.archive, "broken.mp4", bad)
	sc := testScanConfig(r)
	sc.Metadata = "media"
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), sc)
	if res.Status != StatusCompleted {
		t.Fatalf("scan: %v: %v", res.Status, res.Err)
	}
	f, err := os.Open(filepath.Join(r.archive, "arxgo-registry.csv"))
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	rows, err := csv.NewReader(f).ReadAll()
	if err != nil {
		t.Fatal(err)
	}
	byName := map[string][]string{}
	for _, row := range rows[1:] {
		byName[row[0]] = row
	}
	for name, wantVideo := range map[string]string{"video.mp4": "true", "audio-only.mp4": "false", "broken.mp4": "true"} {
		row := byName[name]
		if row == nil || row[8] != wantVideo {
			t.Fatalf("%s row = %v", name, row)
		}
		if name == "audio-only.mp4" && row[4] != "video/mp4" {
			t.Fatalf("audio-only MIME = %q; fixture must exercise video flag refinement", row[4])
		}
		var m report.Metadata
		if err := json.Unmarshal([]byte(row[10]), &m); err != nil {
			t.Fatal(err)
		}
		if m.Media == nil || m.Media.Source != "go-mp4" {
			t.Fatalf("%s media = %+v", name, m.Media)
		}
		if name == "video.mp4" && (m.Media.Error != "" || m.Media.VideoCodec != "h264" || m.Media.AudioCodec != "aac" || m.Media.Tags["title"] != "Title") {
			t.Fatalf("video media = %+v", m.Media)
		}
		if name == "audio-only.mp4" && (m.Media.Error != "" || m.Media.VideoStreams != 0 || !m.Media.HasAudio) {
			t.Fatalf("audio media = %+v", m.Media)
		}
		if name == "broken.mp4" && !strings.Contains(m.Media.Error, "parse failed") {
			t.Fatalf("bad media = %+v", m.Media)
		}
	}
	var candidates []string
	if err := ReadCandidates(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.CandidatesFile), func(c Candidate) error { candidates = append(candidates, c.RelPath); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(candidates, ",") != "broken.mp4,video.mp4" {
		t.Fatalf("candidates = %v", candidates)
	}
}
