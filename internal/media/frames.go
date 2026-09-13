package media

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"math"
	"os"
	"path/filepath"
)

// GenerateFrame extracts one planned image. The caller must own the preview WAL
// transaction before passing an output in the archive.
func (r *Runner) GenerateFrame(ctx context.Context, source string, job PreviewJob) error {
	if err := validateFrameJob(source, job); err != nil {
		return err
	}
	run := func(job PreviewJob) error {
		return r.Run(ctx, PreviewCommand{
			Args: frameArgs(source, job), Output: job.Output,
			Validate: func(_ context.Context, part string) error {
				return validateFramePNG(part, job.Size)
			},
		})
	}
	err := run(job)
	if err == nil || job.DurationKnown || job.TimeS == 0 || !errors.Is(err, errEmptyPreview) {
		return err
	}
	// An unknown-duration start seek may be beyond a very short source. The
	// runner already removed its empty part; retry once at the first frame.
	job.TimeS = 0
	if retryErr := run(job); retryErr != nil {
		return errors.Join(err, retryErr)
	}
	return nil
}

func frameArgs(source string, job PreviewJob) []string {
	compression := map[string]string{"low": "9", "medium": "6", "high": "3"}[job.ImageQuality]
	return []string{
		"-v", "error", "-ss", sampleSeconds(job.TimeS), "-i", source,
		"-map", "0:v:0", "-frames:v", "1", "-vf", sampleScale(job.Size),
		"-compression_level", compression,
	}
}

func validateFrameJob(source string, job PreviewJob) error {
	if source == "" || job.Kind != "image" || job.Output == "" ||
		filepath.Ext(job.Output) != ".png" ||
		job.Size.Width < 2 || job.Size.Height < 2 || job.Size.Width%2 != 0 || job.Size.Height%2 != 0 ||
		job.TimeS < 0 || math.IsNaN(job.TimeS) || math.IsInf(job.TimeS, 0) ||
		(job.ImageQuality != "low" && job.ImageQuality != "medium" && job.ImageQuality != "high") {
		return errors.New("invalid frame job")
	}
	if filepath.Clean(source) == filepath.Clean(job.Output) ||
		filepath.Clean(source) == filepath.Clean(PreviewPartPath(job.Output)) {
		return errors.New("frame output conflicts with source")
	}
	return nil
}

func validateFramePNG(path string, size PreviewSize) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open frame PNG: %w", err)
	}
	img, decodeErr := png.Decode(f)
	closeErr := f.Close()
	if decodeErr != nil {
		return fmt.Errorf("decode frame PNG: %w", decodeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close frame PNG: %w", closeErr)
	}
	if bounds := img.Bounds(); bounds.Dx() != size.Width || bounds.Dy() != size.Height {
		return fmt.Errorf("frame PNG dimensions %dx%d, want %dx%d", bounds.Dx(), bounds.Dy(), size.Width, size.Height)
	}
	return nil
}
