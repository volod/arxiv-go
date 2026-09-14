package media

import (
	"context"
	"errors"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
)

const sampleChunkLimit = 50

// GenerateSample encodes a planned sample to job.Output. Series with more than 50 ranges are
// encoded in chunks inside a private temporary directory, removed when this call returns, then
// joined with the concat demuxer. The part file is probed before publication, so an invalid clip
// never becomes a published preview.
func (r *Runner) GenerateSample(ctx context.Context, source string, job PreviewJob) error {
	if err := validateSampleJob(source, job); err != nil {
		return err
	}
	job, err := r.sampleAudio(ctx, source, job)
	if err != nil {
		return err
	}
	installed, err := r.Encoders(ctx)
	if err != nil {
		return err
	}
	video, audio, err := availableEncoders(job, installed)
	if err != nil {
		return err
	}
	args := sampleEncodeArgs(source, job.Ranges, job, video, audio)
	if len(job.Ranges) > sampleChunkLimit {
		dir, err := os.MkdirTemp("", "arxgo-samples-")
		if err != nil {
			return fmt.Errorf("create sample chunk directory: %w", err)
		}
		defer os.RemoveAll(dir)
		if args, err = r.encodeSampleChunks(ctx, dir, source, job, video, audio); err != nil {
			return err
		}
	}
	return r.Run(ctx, PreviewCommand{
		Args: args, Output: job.Output, TotalDuration: seconds(job.totalSeconds()),
		Validate: func(ctx context.Context, part string) error { return r.validateSample(ctx, part, job) },
	})
}

func validateSampleJob(source string, job PreviewJob) error {
	if job.Kind != PreviewSample || len(job.Ranges) == 0 {
		return errors.New("invalid sample job")
	}
	if err := job.validate(source); err != nil {
		return err
	}
	for _, r := range job.Ranges {
		if !finite(r.StartS) || !finite(r.DurationS) || r.StartS < 0 || r.DurationS <= 0 {
			return errors.New("invalid sample range")
		}
	}
	return nil
}

func finite(v float64) bool { return !math.IsNaN(v) && !math.IsInf(v, 0) }

func seconds(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }

// validateSample requires a probed video stream at the planned size, audio when the source has
// it, and a duration close to the plan. An unknown-duration start clip may be shorter.
func (r *Runner) validateSample(ctx context.Context, path string, job PreviewJob) error {
	info := r.Probe(ctx, path, "")
	if info.Error != "" {
		return fmt.Errorf("ffprobe: %s", info.Error)
	}
	if info.VideoStreams == 0 || info.Width != job.Size.Width || info.Height != job.Size.Height {
		return fmt.Errorf("sample video size %dx%d, want %dx%d", info.Width, info.Height, job.Size.Width, job.Size.Height)
	}
	if job.HasAudio && !info.HasAudio {
		return errors.New("sample audio is missing")
	}
	total := job.totalSeconds()
	tolerance := max(0.5, 0.05*total)
	if info.DurationS <= 0 || info.DurationS > total+tolerance || job.DurationKnown && info.DurationS < total-tolerance {
		return fmt.Errorf("sample duration %.3fs, want %.3fs", info.DurationS, total)
	}
	return nil
}

// encodeSampleChunks encodes at most 50 ranges per ffmpeg process into dir and returns the
// concat-demuxer arguments that join them.
func (r *Runner) encodeSampleChunks(ctx context.Context, dir, source string, job PreviewJob, video, audio string) ([]string, error) {
	ext := filepath.Ext(job.Output)
	var list strings.Builder
	list.WriteString("ffconcat version 1.0\n")
	for n, start := 0, 0; start < len(job.Ranges); n, start = n+1, start+sampleChunkLimit {
		chunk := job
		chunk.Ranges = job.Ranges[start:min(start+sampleChunkLimit, len(job.Ranges))]
		name := fmt.Sprintf("chunk%04d%s", n, ext)
		err := r.Run(ctx, PreviewCommand{
			Args:   sampleEncodeArgs(source, chunk.Ranges, chunk, video, audio),
			Output: filepath.Join(dir, name), TotalDuration: seconds(chunk.totalSeconds()),
		})
		if err != nil {
			return nil, fmt.Errorf("sample chunk %d: %w", n+1, err)
		}
		fmt.Fprintf(&list, "file '%s'\n", name)
	}
	listPath := filepath.Join(dir, "chunks.ffconcat")
	if err := os.WriteFile(listPath, []byte(list.String()), 0o600); err != nil {
		return nil, fmt.Errorf("write sample chunk list: %w", err)
	}
	args := []string{"-v", "error", "-f", "concat", "-safe", "1", "-i", listPath, "-map", "0:v:0"}
	if job.HasAudio {
		args = append(args, "-map", "0:a:0")
	}
	args = append(args, "-c", "copy")
	return append(args, containerArgs(ext)...), nil
}
