package report

import (
	"fmt"
	"math"
	"strconv"
	"strings"

	"github.com/volod/arxiv-go/internal/media"
)

// Resolution bands used in arxgo-videos.md.
const (
	BandBelowSD = "<SD"
	BandSD      = "SD"
	BandHD      = "HD"
	Band4K      = "4K+"
)

// FormatSize formats a byte count with a space before the binary unit, for example "700.0 MiB".
func FormatSize(n int64) string {
	const units = "KMGTPE"
	if n < 1024 && n > -1024 {
		return fmt.Sprintf("%d B", n)
	}
	v, i := float64(n)/1024, 0
	for (v >= 1024 || v <= -1024) && i < len(units)-1 {
		v /= 1024
		i++
	}
	return fmt.Sprintf("%.1f %ciB", v, units[i])
}

// FormatClock formats a duration in seconds as M:SS or H:MM:SS.
func FormatClock(seconds float64) string {
	if seconds <= 0 || math.IsNaN(seconds) || math.IsInf(seconds, 0) {
		return ""
	}
	s := int(math.Round(seconds))
	h, m, sec := s/3600, (s%3600)/60, s%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, sec)
	}
	return fmt.Sprintf("%d:%02d", m, sec)
}

// MediaLine is the stub duration bullet, omitting missing pieces.
func MediaLine(m *media.MediaInfo) string {
	if m == nil || m.Error != "" {
		return ""
	}
	var parts []string
	if c := FormatClock(m.DurationS); c != "" {
		parts = append(parts, c)
	}
	if m.Width > 0 && m.Height > 0 {
		parts = append(parts, fmt.Sprintf("%dx%d", m.Width, m.Height))
	}
	switch {
	case m.VideoCodec != "" && m.AudioCodec != "":
		parts = append(parts, m.VideoCodec+" + "+m.AudioCodec)
	case m.VideoCodec != "":
		parts = append(parts, m.VideoCodec)
	case m.AudioCodec != "":
		parts = append(parts, m.AudioCodec)
	}
	if fps := formatFPS(m.FrameRate); fps != "" {
		parts = append(parts, fps+" fps")
	}
	return strings.Join(parts, " | ")
}

func formatFPS(rate string) string {
	if rate == "" {
		return ""
	}
	n, d, ok := strings.Cut(rate, "/")
	if !ok {
		return rate
	}
	num, err1 := strconv.ParseFloat(n, 64)
	den, err2 := strconv.ParseFloat(d, 64)
	if err1 != nil || err2 != nil || den == 0 {
		return rate
	}
	// People read 29.97 fps, not the 30000/1001 rational or a variable-rate phone's 12690000/422899.
	return strconv.FormatFloat(math.Round(num/den*100)/100, 'f', -1, 64)
}

// ResolutionBand classifies a frame size by its short side: <SD, SD, HD, 4K+.
func ResolutionBand(width, height int) string {
	if width <= 0 || height <= 0 {
		return ""
	}
	short := width
	if height < width {
		short = height
	}
	switch {
	case short < 480:
		return BandBelowSD
	case short < 720:
		return BandSD
	case short < 2160:
		return BandHD
	default:
		return Band4K
	}
}
