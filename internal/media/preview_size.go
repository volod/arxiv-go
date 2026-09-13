package media

import "math"

// ClampPreviewSize fits the display-oriented source in the requested box, without
// upscaling, then rounds both dimensions down to even encoder-compatible values.
func ClampPreviewSize(info MediaInfo, resolution string) PreviewSize {
	// Both metadata readers already report display-oriented dimensions. FFmpeg
	// applies the rotation while decoding, so a second swap would stretch clips.
	w, h := info.Width, info.Height
	if w <= 0 || h <= 0 {
		return PreviewSize{}
	}
	box := map[string]PreviewSize{"sd": {640, 360}, "hd": {1920, 1080}, "4k": {3840, 2160}}[resolution]
	if box.Width == 0 {
		return PreviewSize{}
	}
	if h > w {
		box.Width, box.Height = box.Height, box.Width
	}
	scale := math.Min(1, math.Min(float64(box.Width)/float64(w), float64(box.Height)/float64(h)))
	width, height := int(math.Floor(float64(w)*scale)), int(math.Floor(float64(h)*scale))
	width -= width % 2
	height -= height % 2
	if width < 2 || height < 2 {
		return PreviewSize{}
	}
	return PreviewSize{width, height}
}
