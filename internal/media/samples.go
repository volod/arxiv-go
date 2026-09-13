package media

import (
	"context"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"time"
)

const sampleChunkLimit = 50

// GenerateSample encodes a planned sample. The caller must own the preview WAL
// transaction before passing an output in the archive. Chunk files live in a
// private system temporary directory and are removed when this call returns.
// The returned path can differ from job.Output for an unsupported extension.
func (r *Runner) GenerateSample(ctx context.Context, source string, job PreviewJob) (string, error) {
	if err := validateSampleJob(source, job); err != nil {
		return "", err
	}
	encoders, err := r.Encoders(ctx)
	if err != nil {
		return "", err
	}
	video, audio, err := sampleEncoders(job, encoders)
	if err != nil {
		return "", err
	}
	var total float64
	for _, segment := range job.Ranges {
		total += segment.DurationS
	}
	output := job.Output
	args := sampleEncodeArgs(source, job.Ranges, job, video, audio)
	if len(job.Ranges) > sampleChunkLimit {
		var cleanup func()
		args, cleanup, err = r.sampleChunks(ctx, source, job, video, audio)
		if err != nil {
			return "", err
		}
		defer cleanup()
	}
	// Run refuses an occupied output or part. Validation happens before its
	// no-replace rename, so an invalid clip never becomes a published preview.
	run := func(path string) error {
		return r.Run(ctx, PreviewCommand{
			Args: args, Output: path, TotalDuration: time.Duration(total * float64(time.Second)),
			Validate: func(ctx context.Context, part string) error {
				return r.validateSample(ctx, part, job, total, path)
			},
		})
	}
	if err = run(output); err != nil {
		if knownSampleExtension(filepath.Ext(output)) || !unsupportedSampleMuxer(err) {
			return "", err
		}
		fallback := strings.TrimSuffix(output, filepath.Ext(output)) + ".mp4"
		if fallback == output {
			return "", err
		}
		if r.Log != nil {
			r.Log.Warn("sample container unsupported; trying mp4", "output", output, "fallback", fallback, "error", err)
		}
		if fallbackErr := run(fallback); fallbackErr != nil {
			return "", errors.Join(err, fallbackErr)
		}
		output = fallback
	}
	return output, nil
}

func validateSampleJob(source string, job PreviewJob) error {
	if source == "" || job.Kind != "sample" || job.Output == "" || filepath.Ext(job.Output) == "" ||
		job.Size.Width < 2 || job.Size.Height < 2 || job.Size.Width%2 != 0 || job.Size.Height%2 != 0 ||
		len(job.Ranges) == 0 || !sampleQualityValid(job.SampleQuality) {
		return errors.New("invalid sample job")
	}
	if source == job.Output || PreviewPartPath(job.Output) == source {
		return errors.New("sample output conflicts with source")
	}
	for _, segment := range job.Ranges {
		if segment.StartS < 0 || segment.DurationS <= 0 || math.IsNaN(segment.StartS) ||
			math.IsNaN(segment.DurationS) || math.IsInf(segment.StartS, 0) || math.IsInf(segment.DurationS, 0) {
			return errors.New("invalid sample range")
		}
	}
	return nil
}

func sampleQualityValid(q string) bool { return q == "low" || q == "medium" || q == "high" }

func knownSampleExtension(ext string) bool {
	switch strings.ToLower(ext) {
	case ".mp4", ".m4v", ".mov", ".3gp", ".mkv", ".webm", ".avi":
		return true
	}
	return false
}

func unsupportedSampleMuxer(err error) bool {
	return strings.Contains(err.Error(), "Unable to find a suitable output format") ||
		strings.Contains(err.Error(), "Unable to choose an output format")
}

func sampleEncoders(job PreviewJob, available map[string]bool) (video, audio string, err error) {
	video, audio = job.VideoEncoder, job.AudioEncoder
	if !available[video] {
		switch video {
		case "libx264":
			video = "mpeg4"
		default:
			return "", "", fmt.Errorf("required video encoder %s is unavailable", video)
		}
		if !available[video] {
			return "", "", fmt.Errorf("required video encoders %s and %s are unavailable", job.VideoEncoder, video)
		}
	}
	if job.HasAudio {
		if !available[audio] && audio == "libmp3lame" {
			audio = "aac"
		}
		if !available[audio] {
			return "", "", fmt.Errorf("required audio encoder %s is unavailable", audio)
		}
	}
	return video, audio, nil
}

func (r *Runner) validateSample(ctx context.Context, path string, job PreviewJob, total float64, output string) error {
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
	wantContainer := job.Container
	if strings.EqualFold(filepath.Ext(output), ".mp4") &&
		!knownSampleExtension(filepath.Ext(job.Output)) {
		wantContainer = "mp4"
	}
	if wantContainer != "" && info.Container != wantContainer {
		return fmt.Errorf("sample container %s, want %s", info.Container, wantContainer)
	}
	tolerance := max(0.5, 0.05*total)
	if info.DurationS <= 0 || info.DurationS > total+tolerance ||
		job.DurationKnown && info.DurationS < total-tolerance {
		return fmt.Errorf("sample duration %.3fs, want %.3fs", info.DurationS, total)
	}
	return nil
}
