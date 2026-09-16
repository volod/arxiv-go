package media

import (
	"context"
	"math"
	"os"
	"strings"
	"syscall"
)

// TagKeys is the fixed set of container text tags exposed in the CSV registries.
var TagKeys = []string{
	"title", "comment", "encoder", "artist", "album", "date", "genre",
	"composer", "grouping", "description", "copyright",
}

// MediaInfo is the shared metadata.media representation for the pure-Go and ffprobe parsers.
type MediaInfo struct {
	Container       string            `json:"container,omitempty"`
	DurationS       float64           `json:"duration_s,omitempty"`
	BitRate         int64             `json:"bit_rate,omitempty"`
	Width           int               `json:"width,omitempty"`
	Height          int               `json:"height,omitempty"`
	Rotation        int               `json:"rotation,omitempty"`
	FrameRate       string            `json:"frame_rate,omitempty"`
	VideoCodec      string            `json:"video_codec,omitempty"`
	AudioCodec      string            `json:"audio_codec,omitempty"`
	HasAudio        bool              `json:"has_audio,omitempty"`
	VideoStreams    int               `json:"video_streams,omitempty"`
	AudioStreams    int               `json:"audio_streams,omitempty"`
	SubtitleStreams int               `json:"subtitle_streams,omitempty"`
	CreationTime    string            `json:"creation_time,omitempty"`
	Tags            map[string]string `json:"tags,omitempty"`
	Source          string            `json:"source,omitempty"`
	Error           string            `json:"error,omitempty"`
}

// IsISOBMFF reports whether the detected MIME is handled by the pure-Go parser.
func IsISOBMFF(mime string) bool {
	switch mime {
	case "video/mp4", "video/quicktime", "video/3gpp", "video/3gpp2", "video/x-m4v", "audio/mp4", "audio/x-m4a":
		return true
	}
	return false
}

// ReadMetadata routes detected media to the ISO parser first, then ffprobe for other formats or
// an ISO failure. If ffprobe is unavailable, an ISO failure is kept as the non-fatal result.
func ReadMetadata(ctx context.Context, path, mime string, probe FFprobeReader) *MediaInfo {
	if IsISOBMFF(mime) {
		info := ReadISO(ctx, path, mime)
		if info.Error == "" || probe.Path == "" {
			return info
		}
	}
	return probe.Read(ctx, path, mime)
}

// ReadISO returns a non-fatal media result. A parse failure is recorded in Error; the caller
// still writes the registry row and leaves its original video classification intact.
func ReadISO(ctx context.Context, path, mime string) *MediaInfo {
	info := &MediaInfo{Source: "go-mp4", Container: isoContainer(path, mime)}
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err == nil {
		defer f.Close()
		var stat os.FileInfo
		stat, err = f.Stat()
		if err == nil {
			if !stat.Mode().IsRegular() {
				err = errNotRegular
			} else {
				err = parseISO(ctx, f, stat.Size(), info)
			}
		}
	}
	if err != nil {
		// Do not record absolute local paths from os.PathError in registry metadata.
		info.Error = "ISO BMFF parse failed: " + sanitizeParseError(err)
		info.DurationS, info.BitRate = 0, 0
		info.Width, info.Height, info.Rotation = 0, 0, 0
		info.FrameRate, info.VideoCodec, info.AudioCodec = "", "", ""
		info.HasAudio, info.VideoStreams, info.AudioStreams, info.SubtitleStreams = false, 0, 0, 0
		info.CreationTime, info.Tags = "", nil
	}
	return info
}

func isoContainer(path, mime string) string {
	switch {
	case mime == "video/quicktime" || strings.HasSuffix(strings.ToLower(path), ".mov"):
		return "mov"
	case mime == "video/3gpp":
		return "3gp"
	case strings.HasSuffix(strings.ToLower(path), ".3gp"):
		return "3gp"
	case mime == "video/3gpp2":
		return "3g2"
	case strings.HasSuffix(strings.ToLower(path), ".3g2"):
		return "3g2"
	case mime == "audio/x-m4a" || strings.HasSuffix(strings.ToLower(path), ".m4a"):
		return "m4a"
	default:
		return "mp4"
	}
}

func roundMillis(v float64) float64 { return math.Round(v*1000) / 1000 }
