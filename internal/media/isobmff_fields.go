package media

import (
	"errors"
	"math"
	"math/big"
	"os"
	"strings"
	"time"
	"unicode/utf8"

	mp4 "github.com/abema/go-mp4"
)

func readSmall(h *mp4.ReadHandle) (mp4.IBox, error) {
	if h.BoxInfo.Size-h.BoxInfo.HeaderSize > isoBoxLimit {
		return nil, errors.New("metadata box too large")
	}
	box, _, err := h.ReadPayload()
	return box, err
}

func (s *isoScan) tag(h *mp4.ReadHandle) error {
	if len(h.Path) < 2 || h.BoxInfo.Size-h.BoxInfo.HeaderSize > 4096 {
		return nil
	}
	name := h.Path[len(h.Path)-2]
	key := tagName(name)
	if key == "" {
		return nil
	}
	box, err := readSmall(h)
	if err != nil {
		return err
	}
	v := box.(*mp4.Data)
	if v.DataType != mp4.DataTypeStringUTF8 || !utf8.Valid(v.Data) {
		return nil
	}
	value := strings.TrimSpace(string(v.Data))
	if value == "" {
		return nil
	}
	if len(value) > 256 {
		value = value[:256]
		for !utf8.ValidString(value) {
			value = value[:len(value)-1]
		}
	}
	if s.info.Tags == nil {
		s.info.Tags = make(map[string]string)
	}
	s.info.Tags[key] = value
	return nil
}

func tagName(t mp4.BoxType) string {
	switch t {
	case mp4.BoxType{0xa9, 'n', 'a', 'm'}:
		return "title"
	case mp4.BoxType{0xa9, 'c', 'm', 't'}:
		return "comment"
	case mp4.BoxType{0xa9, 't', 'o', 'o'}:
		return "encoder"
	case mp4.BoxType{0xa9, 'A', 'R', 'T'}:
		return "artist"
	case mp4.BoxType{0xa9, 'a', 'l', 'b'}:
		return "album"
	case mp4.BoxType{0xa9, 'd', 'a', 'y'}:
		return "date"
	case mp4.BoxType{0xa9, 'g', 'e', 'n'}:
		return "genre"
	case mp4.BoxType{0xa9, 'w', 'r', 't'}:
		return "composer"
	case mp4.BoxType{0xa9, 'g', 'r', 'p'}:
		return "grouping"
	case mp4.BoxType{'d', 'e', 's', 'c'}:
		return "description"
	case mp4.BoxType{'c', 'p', 'r', 't'}:
		return "copyright"
	}
	return ""
}

func (s *isoScan) finish(size int64) {
	info := s.info
	// The movie header holds the presented duration, which honors edit lists: a trimmed edit keeps
	// longer media in its tracks. Fragmented files may leave it unset; their tracks sum fragments.
	longest := s.movieSeconds()
	fromTracks := longest == 0 || s.fragment != nil
	for _, t := range s.tracks {
		t.fragmentTicks = s.fragmentTicks[t.id] + s.fragmentPending[t.id]*uint64(s.trexDefaults[t.id])
		switch t.kind {
		case "vide":
			info.VideoStreams++
			if info.VideoStreams == 1 {
				info.VideoCodec, info.Rotation = t.codec, t.rotation
				info.Width, info.Height = t.width, t.height
				if t.rotation == 90 || t.rotation == 270 {
					info.Width, info.Height = info.Height, info.Width
				}
				if t.samples > 0 && t.ticks > 0 && t.timescale > 0 {
					info.FrameRate = rationalRate(t.samples, t.timescale, t.ticks)
				}
			}
		case "soun":
			info.AudioStreams++
			if info.AudioStreams == 1 {
				info.AudioCodec = t.codec
			}
		case "text", "sbtl", "subt", "clcp":
			info.SubtitleStreams++
		}
		if fromTracks && (t.kind == "vide" || t.kind == "soun") && t.timescale > 0 {
			d := t.duration
			if !validTicks(d) {
				d = t.fragmentTicks
			}
			if validTicks(d) {
				longest = max(longest, float64(d)/float64(t.timescale))
			}
		}
	}
	info.HasAudio = info.AudioStreams > 0
	info.DurationS = roundMillis(longest)
	if longest > 0 && size > 0 {
		info.BitRate = int64(math.Round(float64(size) * 8 / longest))
	}
}

func (s *isoScan) movieSeconds() float64 {
	d := s.duration
	if s.mehd > 0 && !validTicks(d) {
		d = s.mehd
	}
	if s.timescale == 0 || !validTicks(d) {
		return 0
	}
	return float64(d) / float64(s.timescale)
}

// validTicks rejects unset and all-ones durations.
func validTicks(d uint64) bool { return d != 0 && d != math.MaxUint32 && d != math.MaxUint64 }

func matrixRotation(m [9]int32) int {
	a, b, c, d := m[0], m[1], m[3], m[4]
	switch {
	case a == 0 && b > 0 && c < 0 && d == 0:
		return 90
	case a < 0 && b == 0 && c == 0 && d < 0:
		return 180
	case a == 0 && b < 0 && c > 0 && d == 0:
		return 270
	}
	return 0
}

func rationalRate(samples uint64, timescale uint32, ticks uint64) string {
	if ticks == 0 {
		return ""
	}
	a, b := new(big.Int).SetUint64(samples), new(big.Int).SetUint64(ticks)
	a.Mul(a, new(big.Int).SetUint64(uint64(timescale)))
	g := new(big.Int).GCD(nil, nil, a, b)
	a.Div(a, g)
	b.Div(b, g)
	return a.String() + "/" + b.String()
}

func mp4Time(v uint64) string {
	if v == 0 || v > math.MaxInt64 {
		return ""
	}
	sec := int64(v) - 2082844800
	if sec < -62135596800 || sec > 253402300799 {
		return ""
	}
	return time.Unix(sec, 0).UTC().Format(time.RFC3339)
}

func codecName(code string) string {
	switch code {
	case "avc1", "avc3":
		return "h264"
	case "hvc1", "hev1":
		return "hevc"
	case "av01":
		return "av1"
	case "mp4v":
		return "mpeg4"
	case "vp09":
		return "vp9"
	case "mp4a":
		return "aac"
	case "ac-3":
		return "ac3"
	case "ec-3":
		return "eac3"
	case "alac":
		return "alac"
	case "Opus":
		return "opus"
	case "fLaC":
		return "flac"
	case "tx3g":
		return "mov_text"
	}
	return code
}

func sanitizeParseError(err error) string {
	var pe *os.PathError
	if errors.As(err, &pe) {
		return pe.Err.Error()
	}
	msg := err.Error()
	if len(msg) > 256 {
		msg = msg[:256]
	}
	return msg
}
