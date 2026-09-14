package media

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/test/fixtures/testmp4"
	"github.com/volod/arxiv-go/test/fixtures/tooltest"
)

func TestFFprobeCapturedFormats(t *testing.T) {
	for _, tc := range []struct {
		file, path, mime, container, video, audio   string
		duration, width, height, streamsV, streamsA int
	}{
		{"mkv.json", "sample.mkv", "video/x-matroska", "matroska", "mpeg4", "aac", 1, 320, 240, 1, 1},
		{"webm.json", "sample.webm", "video/webm", "webm", "vp8", "opus", 1, 320, 240, 1, 1},
		{"avi.json", "live.avi", "video/x-msvideo", "avi", "mpeg4", "pcm_s16le", 1, 320, 240, 1, 1},
		{"ts.json", "sample.ts", "video/mp2t", "mpegts", "mpeg2video", "mp2", 1, 320, 240, 1, 1},
		{"ogg.json", "sample.ogg", "audio/ogg", "ogg", "", "opus", 1, 0, 0, 0, 1},
		{"missing-duration.json", "missing-duration.mkv", "video/x-matroska", "matroska", "mpeg4", "aac", 0, 320, 240, 1, 1},
		{"rotated.json", "rotated.mp4", "video/mp4", "mp4", "mpeg4", "", 1, 240, 320, 1, 0},
	} {
		t.Run(tc.file, func(t *testing.T) {
			data, err := os.ReadFile(filepath.Join("..", "..", "test", "testdata", "ffprobe", tc.file))
			if err != nil {
				t.Fatal(err)
			}
			m, err := parseFFprobe(data, tc.path, tc.mime)
			if err != nil {
				t.Fatal(err)
			}
			if m.Source != "ffprobe" || m.Container != tc.container || m.VideoCodec != tc.video || m.AudioCodec != tc.audio || m.Width != tc.width || m.Height != tc.height || m.VideoStreams != tc.streamsV || m.AudioStreams != tc.streamsA || m.HasAudio != (tc.streamsA > 0) || int(m.DurationS) != tc.duration || m.BitRate <= 0 {
				t.Fatalf("metadata: %+v", m)
			}
			if tc.streamsV > 0 && m.FrameRate != "25/1" {
				t.Fatalf("frame rate = %q", m.FrameRate)
			}
			if tc.file == "rotated.json" && m.Rotation != 90 {
				t.Fatalf("rotation = %d", m.Rotation)
			}
		})
	}
}

func TestFFprobeNormalizationCorners(t *testing.T) {
	tags := map[string]string{"TITLE": strings.Repeat("a", 255) + "\u00e9", "creation_time": "2024-05-01T12:51:00+03:00"}
	doc := probeDocument{Format: probeFormat{Name: "matroska,webm", Duration: "N/A", BitRate: "N/A", Tags: tags}}
	doc.Streams = []probeStream{{CodecType: "video", CodecName: "h264", AvgFrameRate: "0/0", RFrameRate: "30000/1001", Duration: "2.5555"}, {CodecType: "subtitle", CodecName: "subrip"}}
	cover := probeStream{CodecType: "video", CodecName: "mjpeg"}
	cover.Disposition.AttachedPic = 1
	doc.Streams = append(doc.Streams, cover)
	doc.Streams[0].Tags = map[string]string{"rotate": "-90"}
	data, err := json.Marshal(doc)
	if err != nil {
		t.Fatal(err)
	}
	m, err := parseFFprobe(data, "clip.mkv", "video/x-matroska")
	if err != nil {
		t.Fatal(err)
	}
	if m.DurationS != 2.556 || m.FrameRate != "30000/1001" || m.Rotation != 270 || m.VideoStreams != 1 || m.SubtitleStreams != 1 || m.BitRate != 0 || m.CreationTime != "2024-05-01T09:51:00Z" || len(m.Tags["title"]) != 255 {
		t.Fatalf("normalized: %+v", m)
	}
	for _, raw := range []string{"{}", "{", `{"format":{"format_name":"avi"}}`, `{"format":{"format_name":"avi"},"streams":[{"codec_type":"audio"}]} {}`} {
		if _, err := parseFFprobe([]byte(raw), "x.avi", "video/x-msvideo"); err == nil {
			t.Fatalf("accepted invalid JSON: %q", raw)
		}
	}
}

func TestFFprobeReaderProcessLimits(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	// Only the hung helper gets the short timeout: a race-instrumented helper can take longer than
	// that just to start, which is not the behavior under test.
	for _, tc := range []struct {
		mode, want string
		timeout    time.Duration
	}{
		{"valid", "", 30 * time.Second},
		{"oversize", "output limit exceeded", 30 * time.Second},
		{"nonzero", "exit status 7", 30 * time.Second},
		{"sleep", "deadline exceeded", 250 * time.Millisecond},
	} {
		t.Run(tc.mode, func(t *testing.T) {
			t.Setenv(ffprobeHelperEnv, tc.mode)
			reader := FFprobeReader{Path: exe, Timeout: tc.timeout}
			start := time.Now()
			m := reader.Read(context.Background(), "clip.avi", "video/x-msvideo")
			if tc.want == "" && (m.Error != "" || m.VideoCodec != "mpeg4") || tc.want != "" && !strings.Contains(m.Error, tc.want) {
				t.Fatalf("metadata: %+v", m)
			}
			if tc.mode == "sleep" && time.Since(start) > 3*time.Second {
				t.Fatalf("process did not stop promptly: %s", time.Since(start))
			}
		})
	}
}

func TestReadMetadataISOFallback(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	t.Setenv(ffprobeHelperEnv, "valid")
	data := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}})
	p := fixtureFile(t, "good.mp4", data)
	if m := ReadMetadata(context.Background(), p, "video/mp4", FFprobeReader{Path: exe}); m.Source != "go-mp4" || m.Error != "" {
		t.Fatalf("ISO result: %+v", m)
	}
	bad := append([]byte(nil), data...)
	binary.BigEndian.PutUint32(bad[20:24], uint32(len(bad)+100))
	p = fixtureFile(t, "bad.mp4", bad)
	if m := ReadMetadata(context.Background(), p, "video/mp4", FFprobeReader{Path: exe}); m.Source != "ffprobe" || m.Error != "" || m.VideoCodec != "mpeg4" {
		t.Fatalf("fallback result: %+v", m)
	}
	if m := ReadMetadata(context.Background(), p, "video/mp4", FFprobeReader{}); m.Source != "go-mp4" || m.Error == "" {
		t.Fatalf("no fallback result: %+v", m)
	}
}

func TestFFprobeLive(t *testing.T) {
	if os.Getenv("CI") != "" {
		t.Skip("live ffprobe fixture runs only outside CI")
	}
	ffmpeg := tooltest.LookPath(t, "ffmpeg")
	ffprobe := tooltest.LookPath(t, "ffprobe")
	p := filepath.Join(t.TempDir(), "live.avi")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "testsrc2=size=160x120:rate=25", "-t", "1", "-c:v", "mpeg4", "-y", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture: %v: %s", err, out)
	}
	m := (FFprobeReader{Path: ffprobe}).Read(ctx, p, "video/x-msvideo")
	if m.Error != "" || m.Container != "avi" || m.VideoCodec != "mpeg4" || m.Width != 160 || m.Height != 120 || m.DurationS != 1 || m.VideoStreams != 1 {
		t.Fatalf("live result: %+v", m)
	}
}
