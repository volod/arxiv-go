package media

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
)

// sampleEncodeArgs builds one ffmpeg call: input seeking per range, then either a single scaled
// stream or the concat filter over all ranges.
func sampleEncodeArgs(source string, ranges []PreviewRange, job PreviewJob, video, audio string) []string {
	args := []string{"-v", "error"}
	for _, r := range ranges {
		args = append(args, "-ss", formatSeconds(r.StartS), "-t", formatSeconds(r.DurationS), "-i", source)
	}
	scale := scaleFilter(job.Size)
	if len(ranges) == 1 {
		args = append(args, "-map", "0:v:0")
		if job.HasAudio {
			args = append(args, "-map", fmt.Sprintf("0:a:%d", job.AudioStream))
		}
		args = append(args, "-vf", scale)
	} else {
		audioStream := -1
		if job.HasAudio {
			audioStream = job.AudioStream
		}
		args = append(args, "-filter_complex", concatFilter(len(ranges), audioStream, scale), "-map", "[v]")
		if job.HasAudio {
			args = append(args, "-map", "[a]")
		}
	}
	args = append(args, "-c:v", video)
	args = append(args, videoQualityArgs(video, job.Quality)...)
	if job.HasAudio {
		args = append(args, "-c:a", audio, "-b:a", audioBitrate[job.Quality])
	} else {
		args = append(args, "-an")
	}
	return append(args, containerArgs(filepath.Ext(job.Output))...)
}

// concatFilter resets each input's timestamps, concatenates video and, when audio is a stream
// index rather than -1, that audio stream, and scales once.
func concatFilter(n, audio int, scale string) string {
	var g strings.Builder
	for i := range n {
		fmt.Fprintf(&g, "[%d:v:0]setpts=PTS-STARTPTS[v%d];", i, i)
		if audio >= 0 {
			fmt.Fprintf(&g, "[%d:a:%d]asetpts=PTS-STARTPTS[a%d];", i, audio, i)
		}
	}
	for i := range n {
		fmt.Fprintf(&g, "[v%d]", i)
		if audio >= 0 {
			fmt.Fprintf(&g, "[a%d]", i)
		}
	}
	if audio >= 0 {
		fmt.Fprintf(&g, "concat=n=%d:v=1:a=1[vcat][a]", n)
	} else {
		fmt.Fprintf(&g, "concat=n=%d:v=1:a=0[vcat]", n)
	}
	fmt.Fprintf(&g, ";[vcat]%s[v]", scale)
	return g.String()
}

func containerArgs(ext string) []string {
	if isoSampleExtensions[strings.ToLower(ext)] {
		return []string{"-movflags", "+faststart"}
	}
	return nil
}

func formatSeconds(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', 6, 64)
}

func scaleFilter(size PreviewSize) string {
	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", size.Width, size.Height)
}
