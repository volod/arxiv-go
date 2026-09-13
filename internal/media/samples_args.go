package media

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

func sampleEncodeArgs(source string, ranges []PreviewRange, job PreviewJob, video, audio string) []string {
	args := []string{"-v", "error"}
	for _, segment := range ranges {
		args = append(args, "-ss", sampleSeconds(segment.StartS), "-t", sampleSeconds(segment.DurationS), "-i", source)
	}
	filter := sampleScale(job.Size)
	if len(ranges) == 1 {
		args = append(args, "-map", "0:v:0")
		if job.HasAudio {
			args = append(args, "-map", "0:a:0")
		}
		args = append(args, "-vf", filter)
	} else {
		var graph strings.Builder
		for i := range ranges {
			fmt.Fprintf(&graph, "[%d:v:0]setpts=PTS-STARTPTS[v%d];", i, i)
			if job.HasAudio {
				fmt.Fprintf(&graph, "[%d:a:0]asetpts=PTS-STARTPTS[a%d];", i, i)
			}
		}
		for i := range ranges {
			fmt.Fprintf(&graph, "[v%d]", i)
			if job.HasAudio {
				fmt.Fprintf(&graph, "[a%d]", i)
			}
		}
		if job.HasAudio {
			fmt.Fprintf(&graph, "concat=n=%d:v=1:a=1[vcat][a]", len(ranges))
		} else {
			fmt.Fprintf(&graph, "concat=n=%d:v=1:a=0[vcat]", len(ranges))
		}
		fmt.Fprintf(&graph, ";[vcat]%s[v]", filter)
		args = append(args, "-filter_complex", graph.String(), "-map", "[v]")
		if job.HasAudio {
			args = append(args, "-map", "[a]")
		}
	}
	args = append(args, "-c:v", video)
	switch video {
	case "libx264", "libx265":
		crf := map[string]string{"low": "32", "medium": "26", "high": "20"}[job.SampleQuality]
		args = append(args, "-crf", crf, "-preset", "veryfast")
	case "libvpx-vp9":
		crf := map[string]string{"low": "40", "medium": "33", "high": "28"}[job.SampleQuality]
		args = append(args, "-crf", crf, "-b:v", "0", "-deadline", "realtime", "-cpu-used", "5")
	case "mpeg4":
		qscale := map[string]string{"low": "10", "medium": "6", "high": "3"}[job.SampleQuality]
		args = append(args, "-q:v", qscale)
	}
	if job.HasAudio {
		bitrate := map[string]string{"low": "64k", "medium": "96k", "high": "128k"}[job.SampleQuality]
		args = append(args, "-c:a", audio, "-b:a", bitrate)
	} else {
		args = append(args, "-an")
	}
	if isISOExtension(filepath.Ext(job.Output)) {
		args = append(args, "-movflags", "+faststart")
	}
	return args
}

func sampleSeconds(seconds float64) string {
	return strconv.FormatFloat(seconds, 'f', 6, 64)
}

func sampleScale(size PreviewSize) string {
	return fmt.Sprintf("scale=%d:%d:flags=lanczos,setsar=1", size.Width, size.Height)
}

func isISOExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov", ".3gp":
		return true
	}
	return false
}

// sampleChunks encodes at most 50 fragments per ffmpeg process, then gives the
// caller concat-demuxer arguments and cleanup for the private chunk directory.
func (r *Runner) sampleChunks(ctx context.Context, source string, job PreviewJob, video, audio string) ([]string, func(), error) {
	dir, err := os.MkdirTemp("", "arxgo-samples-")
	if err != nil {
		return nil, nil, fmt.Errorf("create sample chunk directory: %w", err)
	}
	cleanup := func() { _ = os.RemoveAll(dir) }
	ext := filepath.Ext(job.Output)
	if !knownSampleExtension(ext) {
		ext = ".mp4"
	}
	var list strings.Builder
	list.WriteString("ffconcat version 1.0\n")
	for start := 0; start < len(job.Ranges); start += sampleChunkLimit {
		end := min(start+sampleChunkLimit, len(job.Ranges))
		name := fmt.Sprintf("chunk%04d%s", start/sampleChunkLimit, ext)
		path := filepath.Join(dir, name)
		var seconds float64
		for _, segment := range job.Ranges[start:end] {
			seconds += segment.DurationS
		}
		args := sampleEncodeArgs(source, job.Ranges[start:end], job, video, audio)
		if err := r.Run(ctx, PreviewCommand{Args: args, Output: path, TotalDuration: time.Duration(seconds * float64(time.Second))}); err != nil {
			cleanup()
			return nil, nil, fmt.Errorf("sample chunk %d: %w", start/sampleChunkLimit+1, err)
		}
		fmt.Fprintf(&list, "file '%s'\n", name)
	}
	listPath := filepath.Join(dir, "chunks.ffconcat")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o600); err != nil {
		cleanup()
		return nil, nil, fmt.Errorf("write sample chunk list: %w", err)
	}
	args := []string{"-v", "error", "-f", "concat", "-safe", "1", "-i", listPath, "-map", "0:v:0"}
	if job.HasAudio {
		args = append(args, "-map", "0:a:0")
	}
	args = append(args, "-c", "copy")
	if isISOExtension(filepath.Ext(job.Output)) {
		args = append(args, "-movflags", "+faststart")
	}
	return args, cleanup, nil
}
