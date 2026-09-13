package media

import (
	"context"
	"encoding/binary"
	"io"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/media/testmp4"
)

func fixtureFile(t *testing.T, name string, data []byte) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), name)
	if err := os.WriteFile(p, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return p
}

func TestISOTracksTagsAndDuration(t *testing.T) {
	p := fixtureFile(t, "clip.mp4", testmp4.File(testmp4.Options{
		Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 1920, Height: 1080}, {Kind: "soun", Codec: "mp4a"}},
		Title:  "Interview", MoovAtEnd: true,
	}))
	m := ReadISO(context.Background(), p, "video/mp4")
	if m.Error != "" {
		t.Fatalf("parse: %s", m.Error)
	}
	if m.Container != "mp4" || m.Source != "go-mp4" || m.DurationS != 5 || m.Width != 1920 || m.Height != 1080 || m.VideoCodec != "h264" || m.AudioCodec != "aac" || !m.HasAudio || m.VideoStreams != 1 || m.AudioStreams != 1 || m.FrameRate != "25/1" || m.Tags["title"] != "Interview" || m.BitRate <= 0 {
		t.Fatalf("metadata: %+v", m)
	}
}

func TestISOAudioOnlyAndRotation(t *testing.T) {
	for _, tc := range []struct {
		name, mime                   string
		track                        testmp4.Track
		video, audio, w, h, rotation int
	}{
		{"song.mp4", "video/mp4", testmp4.Track{Kind: "soun", Codec: "mp4a"}, 0, 1, 0, 0, 0},
		{"song.m4a", "audio/x-m4a", testmp4.Track{Kind: "soun", Codec: "alac"}, 0, 1, 0, 0, 0},
		{"turn.mov", "video/quicktime", testmp4.Track{Kind: "vide", Codec: "hvc1", Width: 1920, Height: 1080, Rotation: 90}, 1, 0, 1080, 1920, 90},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixtureFile(t, tc.name, testmp4.File(testmp4.Options{Tracks: []testmp4.Track{tc.track}}))
			m := ReadISO(context.Background(), p, tc.mime)
			if m.Error != "" || m.VideoStreams != tc.video || m.AudioStreams != tc.audio || m.Width != tc.w || m.Height != tc.h || m.Rotation != tc.rotation {
				t.Fatalf("metadata: %+v", m)
			}
		})
	}
}

func TestISOFragmentedMehd(t *testing.T) {
	p := fixtureFile(t, "fragmented.mp4", testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "av01", Width: 640, Height: 360}}, Fragmented: true}))
	m := ReadISO(context.Background(), p, "video/mp4")
	if m.Error != "" || m.DurationS != 5 || m.VideoCodec != "av1" {
		t.Fatalf("metadata: %+v", m)
	}
}

func TestISOFragmentedSummedWithLateMoov(t *testing.T) {
	for _, useTrex := range []bool{false, true} {
		p := fixtureFile(t, "late.mp4", testmp4.File(testmp4.Options{
			Tracks:     []testmp4.Track{{Kind: "vide", Codec: "avc1", Width: 640, Height: 360}},
			Fragmented: true, NoMehd: true, Fragments: 2, MoovAtEnd: true, UseTrex: useTrex,
		}))
		m := ReadISO(context.Background(), p, "video/mp4")
		if m.Error != "" || m.DurationS != 10 || m.VideoStreams != 1 {
			t.Fatalf("trex=%t metadata: %+v", useTrex, m)
		}
	}
}

func TestISOCorruptIsNonFatal(t *testing.T) {
	base := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}})
	oversized := append([]byte(nil), base...)
	binary.BigEndian.PutUint32(oversized[0:4], uint32(len(oversized)+100))
	for _, tc := range []struct {
		name string
		data []byte
	}{
		{"oversized", oversized},
		{"truncated", base[:len(base)-12]},
	} {
		t.Run(tc.name, func(t *testing.T) {
			p := fixtureFile(t, "bad.mp4", tc.data)
			m := ReadISO(context.Background(), p, "video/mp4")
			if m.Error == "" || m.VideoStreams != 0 {
				t.Fatalf("metadata: %+v", m)
			}
		})
	}
}

func TestISOSkipsLargeMdat(t *testing.T) {
	data := testmp4.File(testmp4.Options{Tracks: []testmp4.Track{{Kind: "vide", Codec: "avc1"}}})
	// Replace the final empty mdat header with a 1 GiB extended-size mdat. The sparse
	// fixture would be impractical if the parser read media payloads.
	data = data[:len(data)-8]
	head := make([]byte, 16)
	binary.BigEndian.PutUint32(head[:4], 1)
	copy(head[4:8], "mdat")
	binary.BigEndian.PutUint64(head[8:], 1<<30)
	p := fixtureFile(t, "large.mp4", append(data, head...))
	f, err := os.OpenFile(p, os.O_RDWR, 0)
	if err != nil {
		t.Fatal(err)
	}
	if err := f.Truncate(int64(len(data)) + (1 << 30)); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	m := ReadISO(context.Background(), p, "video/mp4")
	if m.Error != "" || m.VideoStreams != 1 {
		t.Fatalf("metadata: %+v", m)
	}
	f, err = os.Open(p)
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	counted := &countingSeek{ReadSeeker: f}
	if err := parseISO(context.Background(), counted, int64(len(data))+(1<<30), &MediaInfo{}); err != nil {
		t.Fatal(err)
	}
	if counted.bytes > 4096 {
		t.Fatalf("read %d bytes from sparse mdat", counted.bytes)
	}
}

func TestISOLiveFFmpeg(t *testing.T) {
	ffmpeg, err := exec.LookPath("ffmpeg")
	if err != nil {
		t.Skip("ffmpeg unavailable: live ISO BMFF fixture skipped")
	}
	p := filepath.Join(t.TempDir(), "live.mp4")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, ffmpeg, "-v", "error", "-f", "lavfi", "-i", "color=c=black:s=320x240:r=25", "-f", "lavfi", "-i", "sine=frequency=440:sample_rate=44100", "-t", "1", "-c:v", "mpeg4", "-c:a", "aac", "-shortest", "-y", p)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("ffmpeg fixture: %v: %s", err, out)
	}
	m := ReadISO(context.Background(), p, "video/mp4")
	if m.Error != "" || m.VideoStreams != 1 || m.AudioStreams != 1 || m.VideoCodec != "mpeg4" || m.AudioCodec != "aac" || m.Width != 320 || m.Height != 240 || m.DurationS < 0.9 || m.DurationS > 1.1 {
		t.Fatalf("live metadata: %+v", m)
	}
}

func TestRationalRateDoesNotOverflow(t *testing.T) {
	if got := rationalRate(math.MaxUint64, 2, math.MaxUint64); got != "2/1" {
		t.Fatalf("frame rate = %s", got)
	}
}

type countingSeek struct {
	io.ReadSeeker
	bytes int64
}

func (r *countingSeek) Read(p []byte) (int, error) {
	n, err := r.ReadSeeker.Read(p)
	r.bytes += int64(n)
	return n, err
}
