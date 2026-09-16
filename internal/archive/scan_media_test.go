package archive

import (
	"bytes"
	"context"
	"encoding/binary"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
	"github.com/volod/arxiv-go/test/fixtures/testmp4"
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
	parsed, err := report.LoadRegistry(filepath.Join(r.archive, "arxgo-registry.csv"))
	if err != nil {
		t.Fatal(err)
	}
	byParsed := report.RegistryByPath(parsed)
	for name, wantVideo := range map[string]bool{"video.mp4": true, "audio-only.mp4": false, "broken.mp4": true} {
		row, ok := byParsed[name]
		if !ok || row.IsVideo != wantVideo {
			t.Fatalf("%s is_video = %v, want %v", name, row.IsVideo, wantVideo)
		}
		if name == "audio-only.mp4" && row.FileMIME != "video/mp4" {
			t.Fatalf("audio-only MIME = %q; fixture must exercise video flag refinement", row.FileMIME)
		}
		m := row.Metadata
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

func TestScanFileModeCollectsISOWithoutFFprobe(t *testing.T) {
	r := newRoots(t)
	video := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 640, Height: 360}, {Kind: "soun", Codec: "mp4a"}}})
	writeScanFile(t, r.archive, "clip.mp4", video)
	writeScanFile(t, r.archive, "movie.avi", []byte{'R', 'I', 'F', 'F', 4, 0, 0, 0, 'A', 'V', 'I', ' ', 'L', 'I', 'S', 'T'})
	writeScanFile(t, r.archive, "tape.mts", bytes.Repeat([]byte{0x47, 0x40, 0x00, 0x10}, 20))
	sc := testScanConfig(r)
	sc.Metadata, sc.FFprobePath = "file", "must-not-run"
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), sc)
	if res.Status != StatusCompleted {
		t.Fatalf("scan: %v: %v", res.Status, res.Err)
	}
	parsed, err := report.LoadRegistry(filepath.Join(r.archive, "arxgo-registry.csv"))
	if err != nil {
		t.Fatal(err)
	}
	byParsed := report.RegistryByPath(parsed)
	clip := byParsed["clip.mp4"].Metadata.Media
	if clip == nil || clip.Source != "go-mp4" || clip.Error != "" || clip.VideoCodec != "h264" || clip.Width != 640 || clip.Height != 360 || !clip.HasAudio {
		t.Fatalf("clip media = %+v", clip)
	}
	for _, name := range []string{"movie.avi", "tape.mts"} {
		if byParsed[name].Metadata.Media != nil {
			t.Fatalf("%s media = %+v", name, byParsed[name].Metadata.Media)
		}
		if !byParsed[name].IsVideo {
			t.Fatalf("%s is_video = false", name)
		}
	}
}

func TestScanFFprobeMetadataAndISOFallback(t *testing.T) {
	t.Setenv(ffprobeScanHelperEnv, "1")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	r := newRoots(t)
	avi := []byte{'R', 'I', 'F', 'F', 4, 0, 0, 0, 'A', 'V', 'I', ' ', 'L', 'I', 'S', 'T'}
	writeScanFile(t, r.archive, "movie.avi", avi)
	writeScanFile(t, r.archive, "audio-only.avi", avi)
	writeScanFile(t, r.archive, "damaged.avi", avi)
	broken := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}})
	binary.BigEndian.PutUint32(broken[20:24], uint32(len(broken)+100))
	writeScanFile(t, r.archive, "broken.mp4", broken)
	sc := testScanConfig(r)
	sc.Metadata, sc.FFprobePath = "media", exe
	res, _ := runScan(t, context.Background(), scanSessionConfig(r, newClock(), 5), sc)
	if res.Status != StatusCompleted {
		t.Fatalf("scan: %v: %v", res.Status, res.Err)
	}
	parsed, err := report.LoadRegistry(filepath.Join(r.archive, "arxgo-registry.csv"))
	if err != nil {
		t.Fatal(err)
	}
	byParsed := report.RegistryByPath(parsed)
	for rel, row := range byParsed {
		m := row.Metadata
		if m.Media == nil || m.Media.Source != "ffprobe" || m.Media.Error != "" {
			t.Fatalf("%s media = %+v", rel, m.Media)
		}
		switch rel {
		case "damaged.avi":
			// A misprobed file with neither audio nor video streams keeps its extension-based flag.
			if !row.IsVideo || m.Media.Container != "lrc" || m.Media.VideoStreams != 0 {
				t.Fatalf("damaged row = %+v media = %+v", row, m.Media)
			}
		case "audio-only.avi":
			if row.IsVideo || m.Media.VideoStreams != 0 || !m.Media.HasAudio {
				t.Fatalf("audio-only row = %+v media = %+v", row, m.Media)
			}
		default:
			if !row.IsVideo || m.Media.VideoCodec != "mpeg4" {
				t.Fatalf("video row = %+v media = %+v", row, m.Media)
			}
		}
	}
	var candidates []string
	if err := ReadCandidates(filepath.Join(state.StateDir(r.archive), "runs", res.RunID, state.CandidatesFile), func(c Candidate) error { candidates = append(candidates, c.RelPath); return nil }); err != nil {
		t.Fatal(err)
	}
	if strings.Join(candidates, ",") != "broken.mp4,damaged.avi,movie.avi" {
		t.Fatalf("candidates = %v", candidates)
	}
}
