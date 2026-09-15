package report

import (
	"fmt"
	"strconv"

	"github.com/volod/arxiv-go/internal/media"
)

const mediaTagStart = 17

// MetadataHeader is shared by both CSV registries. Its fixed order lets a scan stream rows and
// resume at a durable byte offset.
var MetadataHeader = metadataHeader()

func metadataHeader() []string {
	h := []string{
		"mtime", "link_target", "media_source", "media_container", "media_duration_s",
		"media_bit_rate", "media_width", "media_height", "media_rotation", "media_frame_rate",
		"media_video_codec", "media_audio_codec", "media_has_audio", "media_video_streams",
		"media_audio_streams", "media_subtitle_streams", "media_creation_time",
	}
	for _, key := range media.TagKeys {
		h = append(h, "media_tag_"+key)
	}
	return append(h, "media_error")
}

func mediaErrorIndex() int { return mediaTagStart + len(media.TagKeys) }

func HasMetadata(m Metadata) bool { return m.MTime != "" || m.LinkTarget != "" || m.Media != nil }

func MetadataCells(m Metadata) []string {
	c := make([]string, len(MetadataHeader))
	c[0], c[1] = m.MTime, m.LinkTarget
	if m.Media == nil {
		return c
	}
	v := m.Media
	c[2], c[3] = v.Source, v.Container
	c[4] = floatCell(v.DurationS)
	c[5] = intCell(v.BitRate)
	c[6], c[7], c[8] = intCell(int64(v.Width)), intCell(int64(v.Height)), intCell(int64(v.Rotation))
	c[9], c[10], c[11] = v.FrameRate, v.VideoCodec, v.AudioCodec
	c[12] = strconv.FormatBool(v.HasAudio)
	c[13], c[14], c[15] = intCell(int64(v.VideoStreams)), intCell(int64(v.AudioStreams)), intCell(int64(v.SubtitleStreams))
	c[16] = v.CreationTime
	for i, key := range media.TagKeys {
		c[mediaTagStart+i] = v.Tags[key]
	}
	c[mediaErrorIndex()] = v.Error
	return c
}

// parseMetadataHeader maps a subsequence of MetadataHeader names onto the canonical cells.
func parseMetadataHeader(names, values []string) (Metadata, error) {
	cells, err := expandMetadata(names, values)
	if err != nil {
		return Metadata{}, err
	}
	return parseMetadataCells(cells)
}

func expandMetadata(names, values []string) ([]string, error) {
	if len(names) != len(values) {
		return nil, fmt.Errorf("metadata fields: %d names, %d values", len(names), len(values))
	}
	out := make([]string, len(MetadataHeader))
	pos := 0
	for i, name := range names {
		for pos < len(MetadataHeader) && MetadataHeader[pos] != name {
			pos++
		}
		if pos == len(MetadataHeader) {
			return nil, fmt.Errorf("unknown or out-of-order metadata column %q", name)
		}
		out[pos] = values[i]
		pos++
	}
	return out, nil
}

func checkRequiredHeader(got, canonical []string, keep int) (int, error) {
	if keep > len(canonical) {
		return 0, fmt.Errorf("keep %d, canonical %d", keep, len(canonical))
	}
	if len(got) < keep || !equalStrings(got[:keep], canonical[:keep]) {
		return 0, fmt.Errorf("unexpected header %q", got)
	}
	if _, err := expandMetadata(got[keep:], make([]string, len(got)-keep)); err != nil {
		return 0, err
	}
	return keep, nil
}

func intCell(n int64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatInt(n, 10)
}

func floatCell(n float64) string {
	if n == 0 {
		return ""
	}
	return strconv.FormatFloat(n, 'f', -1, 64)
}

func parseMetadataCells(c []string) (Metadata, error) {
	if len(c) != len(MetadataHeader) {
		return Metadata{}, fmt.Errorf("metadata fields: got %d, want %d", len(c), len(MetadataHeader))
	}
	m := Metadata{MTime: c[0], LinkTarget: c[1]}
	mediaPresent := false
	for _, v := range c[2:] {
		if v != "" {
			mediaPresent = true
			break
		}
	}
	if !mediaPresent {
		return m, nil
	}
	v := &media.MediaInfo{Source: c[2], Container: c[3], FrameRate: c[9], VideoCodec: c[10], AudioCodec: c[11], CreationTime: c[16], Error: c[mediaErrorIndex()]}
	var err error
	if v.DurationS, err = parseFloatCell(c[4]); err != nil {
		return Metadata{}, fmt.Errorf("media_duration_s: %w", err)
	}
	if v.BitRate, err = parseIntCell(c[5]); err != nil {
		return Metadata{}, fmt.Errorf("media_bit_rate: %w", err)
	}
	if v.Width, err = parseIntCellInt(c[6]); err != nil {
		return Metadata{}, fmt.Errorf("media_width: %w", err)
	}
	if v.Height, err = parseIntCellInt(c[7]); err != nil {
		return Metadata{}, fmt.Errorf("media_height: %w", err)
	}
	if v.Rotation, err = parseIntCellInt(c[8]); err != nil {
		return Metadata{}, fmt.Errorf("media_rotation: %w", err)
	}
	if c[12] != "" {
		if v.HasAudio, err = strconv.ParseBool(c[12]); err != nil {
			return Metadata{}, fmt.Errorf("media_has_audio: %w", err)
		}
	}
	if v.VideoStreams, err = parseIntCellInt(c[13]); err != nil {
		return Metadata{}, fmt.Errorf("media_video_streams: %w", err)
	}
	if v.AudioStreams, err = parseIntCellInt(c[14]); err != nil {
		return Metadata{}, fmt.Errorf("media_audio_streams: %w", err)
	}
	if v.SubtitleStreams, err = parseIntCellInt(c[15]); err != nil {
		return Metadata{}, fmt.Errorf("media_subtitle_streams: %w", err)
	}
	for i, key := range media.TagKeys {
		if c[mediaTagStart+i] == "" {
			continue
		}
		if v.Tags == nil {
			v.Tags = make(map[string]string)
		}
		v.Tags[key] = c[mediaTagStart+i]
	}
	m.Media = v
	return m, nil
}

func parseIntCell(s string) (int64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseInt(s, 10, 64)
}

func parseIntCellInt(s string) (int, error) {
	v, err := parseIntCell(s)
	return int(v), err
}

func parseFloatCell(s string) (float64, error) {
	if s == "" {
		return 0, nil
	}
	return strconv.ParseFloat(s, 64)
}
