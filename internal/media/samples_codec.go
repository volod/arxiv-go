package media

import (
	"fmt"
	"strings"
)

type sampleCodec struct{ video, audio string }

var (
	x264AAC  = sampleCodec{"libx264", "aac"}
	vp9Opus  = sampleCodec{"libvpx-vp9", "libopus"}
	mpeg4MP3 = sampleCodec{"mpeg4", "libmp3lame"}
)

// sampleCodecs lists the source extensions whose ffmpeg container holds the sample encoders.
// Other extensions (MPEG-PS, Ogg, MXF, DV, RealMedia, ASF, custom ones) get an .mp4 sample.
var sampleCodecs = map[string]sampleCodec{
	".mp4": x264AAC, ".m4v": x264AAC, ".mov": x264AAC, ".3gp": x264AAC, ".3g2": x264AAC,
	".f4v": x264AAC, ".flv": x264AAC, ".mkv": x264AAC, ".ts": x264AAC, ".m2ts": x264AAC,
	".mts": x264AAC, ".webm": vp9Opus, ".avi": mpeg4MP3,
}

// isoSampleExtensions are written with the moov atom first for progressive playback.
var isoSampleExtensions = map[string]bool{".mp4": true, ".m4v": true, ".mov": true, ".3gp": true, ".3g2": true, ".f4v": true}

// sampleContainer returns the sample encoders and output extension for a source extension. The
// source extension is kept, in its original case, when its container holds the encoders.
func sampleContainer(sourceExt string) (codec sampleCodec, ext string, substituted bool) {
	if codec, ok := sampleCodecs[strings.ToLower(sourceExt)]; ok {
		return codec, sourceExt, false
	}
	return x264AAC, ".mp4", true
}

// availableEncoders applies the specified fallbacks when an encoder is not installed: libx264 ->
// mpeg4 and libmp3lame -> aac.
func availableEncoders(job PreviewJob, installed map[string]bool) (video, audio string, err error) {
	fallback := map[string]string{"libx264": "mpeg4", "libmp3lame": "aac"}
	pick := func(want string) (string, error) {
		if installed[want] {
			return want, nil
		}
		if alt, ok := fallback[want]; ok && installed[alt] {
			return alt, nil
		}
		return "", fmt.Errorf("required encoder %s is unavailable", want)
	}
	if video, err = pick(job.VideoEncoder); err != nil {
		return "", "", err
	}
	if job.HasAudio {
		if audio, err = pick(job.AudioEncoder); err != nil {
			return "", "", err
		}
	}
	return video, audio, nil
}

// Quality settings from the specification; mpeg4 uses its quantizer.
var (
	x264CRF      = map[string]string{"low": "32", "medium": "26", "high": "20"}
	vp9CRF       = map[string]string{"low": "40", "medium": "33", "high": "28"}
	mpeg4Q       = map[string]string{"low": "10", "medium": "6", "high": "3"}
	audioBitrate = map[string]string{"low": "64k", "medium": "96k", "high": "128k"}
	pngLevel     = map[string]string{"low": "9", "medium": "6", "high": "3"}
)

func videoQualityArgs(encoder, quality string) []string {
	switch encoder {
	case "libx264":
		return []string{"-crf", x264CRF[quality], "-preset", "veryfast"}
	case "libvpx-vp9":
		return []string{"-crf", vp9CRF[quality], "-b:v", "0", "-deadline", "realtime", "-cpu-used", "5"}
	case "mpeg4":
		return []string{"-q:v", mpeg4Q[quality]}
	}
	return nil
}
