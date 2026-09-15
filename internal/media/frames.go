package media

import (
	"context"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
)

// GenerateFrame extracts one planned PNG frame to job.Output and decodes it before publication.
func (r *Runner) GenerateFrame(ctx context.Context, source string, job PreviewJob) error {
	if err := validateFrameJob(source, job); err != nil {
		return err
	}
	run := func(job PreviewJob) error {
		return r.Run(ctx, PreviewCommand{
			Args: frameArgs(source, job), Output: job.Output,
			Validate: func(_ context.Context, part string) error { return validateFramePNG(part, job.Size) },
		})
	}
	err := run(job)
	if err == nil || job.DurationKnown || job.TimeS == 0 || !errors.Is(err, errEmptyPreview) {
		return err
	}
	// An unknown-duration start seek may be beyond a very short source. The runner already
	// removed its empty part; retry once at the first frame.
	job.TimeS = 0
	if retryErr := run(job); retryErr != nil {
		return errors.Join(err, retryErr)
	}
	return nil
}

func frameArgs(source string, job PreviewJob) []string {
	return []string{
		"-v", "error", "-ss", formatSeconds(job.TimeS), "-i", source,
		"-map", "0:v:0", "-frames:v", "1", "-vf", scaleFilter(job.Size),
		"-compression_level", pngLevel[job.Quality],
	}
}

func validateFrameJob(source string, job PreviewJob) error {
	if job.Kind != PreviewImage || filepath.Ext(job.Output) != ".png" || job.TimeS < 0 || !finite(job.TimeS) {
		return errors.New("invalid image job")
	}
	return job.validate(source)
}

func validateFramePNG(path string, size PreviewSize) error {
	f, err := os.Open(path)
	if err != nil {
		return fmt.Errorf("open frame PNG: %w", err)
	}
	defer f.Close()
	img, err := png.Decode(f)
	if err != nil {
		return fmt.Errorf("decode frame PNG: %w", err)
	}
	if b := img.Bounds(); b.Dx() != size.Width || b.Dy() != size.Height {
		return fmt.Errorf("frame PNG dimensions %dx%d, want %dx%d", b.Dx(), b.Dy(), size.Width, size.Height)
	}
	return nil
}
