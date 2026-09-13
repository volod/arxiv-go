package media

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

const DefaultFFprobeTimeout = 60 * time.Second
const ffprobeOutputLimit = 16 << 20

// FFprobeReader runs the validated ffprobe path for one file. A failure becomes MediaInfo.Error
// so that the scan can keep the registry row and its original video classification.
type FFprobeReader struct {
	Path    string
	Timeout time.Duration // zero uses DefaultFFprobeTimeout
	Log     *slog.Logger
}

func (r FFprobeReader) Read(ctx context.Context, path, mime string) *MediaInfo {
	info := &MediaInfo{Source: "ffprobe", Container: probeContainer("", path, mime)}
	if r.Path == "" {
		info.Error = "ffprobe unavailable"
		return info
	}
	timeout := r.Timeout
	if timeout <= 0 {
		timeout = DefaultFFprobeTimeout
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, r.Path, "-v", "error", "-hide_banner", "-print_format", "json", "-show_format", "-show_streams", "--", path)
	stdout := &probeOutput{limit: ffprobeOutputLimit}
	stderr := &limitedBuffer{limit: versionOutputLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	cmd.WaitDelay = time.Second
	err := cmd.Run()
	if r.Log != nil && stderr.String() != "" {
		r.Log.Debug("ffprobe stderr", "output", stderr.String())
	}
	switch {
	case ctx.Err() != nil:
		info.Error = "ffprobe failed: " + ctx.Err().Error()
	case stdout.overflow:
		info.Error = "ffprobe output limit exceeded"
	case err != nil:
		// exec errors may embed the archive path. Keep the registry error path-free.
		info.Error = "ffprobe failed: " + probeExecError(err)
	default:
		parsed, parseErr := parseFFprobe(stdout.buf, path, mime)
		if parseErr != nil {
			info.Error = "ffprobe parse failed: " + sanitizeParseError(parseErr)
		} else {
			info = parsed
		}
	}
	return info
}

type probeOutput struct {
	buf      []byte
	limit    int
	overflow bool
}

func (b *probeOutput) Write(p []byte) (int, error) {
	room := b.limit - len(b.buf)
	if room > 0 {
		b.buf = append(b.buf, p[:min(room, len(p))]...)
	}
	if len(p) > room {
		b.overflow = true
	}
	return len(p), nil
}

func probeExecError(err error) string {
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return fmt.Sprintf("exit status %d", exit.ExitCode())
	}
	return "could not start process"
}

type probeDocument struct {
	Streams []probeStream `json:"streams"`
	Format  probeFormat   `json:"format"`
}

type probeStream struct {
	CodecName    string            `json:"codec_name"`
	CodecType    string            `json:"codec_type"`
	Width        int               `json:"width"`
	Height       int               `json:"height"`
	AvgFrameRate string            `json:"avg_frame_rate"`
	RFrameRate   string            `json:"r_frame_rate"`
	Duration     string            `json:"duration"`
	Tags         map[string]string `json:"tags"`
	SideData     []struct {
		Rotation json.Number `json:"rotation"`
	} `json:"side_data_list"`
	Disposition struct {
		AttachedPic int `json:"attached_pic"`
	} `json:"disposition"`
}

type probeFormat struct {
	Name     string            `json:"format_name"`
	Duration string            `json:"duration"`
	BitRate  string            `json:"bit_rate"`
	Tags     map[string]string `json:"tags"`
}

func parseFFprobe(data []byte, path, mime string) (*MediaInfo, error) {
	var doc probeDocument
	dec := json.NewDecoder(bytes.NewReader(data))
	if err := dec.Decode(&doc); err != nil {
		return nil, err
	}
	var extra any
	if err := dec.Decode(&extra); err != io.EOF {
		return nil, errors.New("trailing ffprobe JSON")
	}
	if len(doc.Streams) == 0 {
		return nil, errors.New("no streams")
	}
	info := &MediaInfo{Source: "ffprobe", Container: probeContainer(doc.Format.Name, path, mime)}
	info.DurationS = roundMillis(positiveFloat(doc.Format.Duration))
	info.BitRate = positiveInt(doc.Format.BitRate)
	info.CreationTime = creationTime(doc.Format.Tags)
	info.Tags = cleanTags(doc.Format.Tags)
	var streamDuration float64
	for _, s := range doc.Streams {
		streamDuration = max(streamDuration, positiveFloat(s.Duration))
		switch s.CodecType {
		case "video":
			if s.Disposition.AttachedPic != 0 {
				continue // cover art does not make an audio file a video
			}
			info.VideoStreams++
			if info.VideoStreams == 1 {
				info.VideoCodec = s.CodecName
				info.Width, info.Height = max(s.Width, 0), max(s.Height, 0)
				info.Rotation = streamRotation(s)
				if info.Rotation == 90 || info.Rotation == 270 {
					info.Width, info.Height = info.Height, info.Width
				}
				info.FrameRate = validFrameRate(s.AvgFrameRate)
				if info.FrameRate == "" {
					info.FrameRate = validFrameRate(s.RFrameRate)
				}
			}
		case "audio":
			info.AudioStreams++
			if info.AudioStreams == 1 {
				info.AudioCodec = s.CodecName
			}
		case "subtitle":
			info.SubtitleStreams++
		}
		if info.CreationTime == "" {
			info.CreationTime = creationTime(s.Tags)
		}
	}
	if info.DurationS == 0 {
		info.DurationS = roundMillis(streamDuration)
	}
	info.HasAudio = info.AudioStreams > 0
	return info, nil
}

func positiveFloat(s string) float64 {
	v, err := strconv.ParseFloat(s, 64)
	if err != nil || v <= 0 || v > float64(math.MaxInt64)/1000 || math.IsInf(v, 0) || math.IsNaN(v) {
		return 0
	}
	return v
}

func positiveInt(s string) int64 {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil || v <= 0 {
		return 0
	}
	return v
}

func validFrameRate(s string) string {
	a, b, ok := strings.Cut(s, "/")
	if !ok || positiveInt(a) == 0 || positiveInt(b) == 0 {
		return ""
	}
	return s
}

func streamRotation(s probeStream) int {
	for _, side := range s.SideData {
		if side.Rotation != "" {
			v, err := strconv.ParseFloat(string(side.Rotation), 64)
			if err == nil && !math.IsInf(v, 0) && !math.IsNaN(v) {
				return normalizeRotation(int(math.Round(v)))
			}
		}
	}
	v, _ := strconv.ParseFloat(s.Tags["rotate"], 64)
	if math.IsInf(v, 0) || math.IsNaN(v) {
		return 0
	}
	return normalizeRotation(int(math.Round(v)))
}

func normalizeRotation(v int) int { return (v%360 + 360) % 360 }

func creationTime(tags map[string]string) string {
	for k, v := range tags {
		if strings.EqualFold(k, "creation_time") {
			if parsed, err := time.Parse(time.RFC3339Nano, v); err == nil {
				return parsed.UTC().Format(time.RFC3339)
			}
		}
	}
	return ""
}

func cleanTags(tags map[string]string) map[string]string {
	if len(tags) == 0 {
		return nil
	}
	out := make(map[string]string, len(tags))
	for k, v := range tags {
		k = strings.ToLower(strings.TrimSpace(k))
		if k == "" {
			continue
		}
		if len(v) > 256 {
			v = v[:256]
			for !utf8.ValidString(v) {
				v = v[:len(v)-1]
			}
		}
		out[k] = v
	}
	return out
}

func probeContainer(name, path, mime string) string {
	ext := strings.TrimPrefix(strings.ToLower(filepath.Ext(path)), ".")
	switch name {
	case "matroska,webm":
		if ext == "webm" || mime == "video/webm" || mime == "audio/webm" {
			return "webm"
		}
		return "matroska"
	case "mov,mp4,m4a,3gp,3g2,mj2":
		return isoContainer(path, mime)
	case "mpegts":
		return "mpegts"
	case "":
		if ext != "" {
			return ext
		}
		return ""
	default:
		first, _, _ := strings.Cut(name, ",")
		return first
	}
}
