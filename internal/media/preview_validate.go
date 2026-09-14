package media

import (
	"context"
	"fmt"
)

// ValidatePublishedPreview checks a final file left after a crash between publication and the
// preview_done event. It applies the same content checks used before publication.
func (r *Runner) ValidatePublishedPreview(ctx context.Context, path string, job PreviewJob) error {
	switch job.Kind {
	case "sample":
		return r.validateSample(ctx, path, job, sumRanges(job.Ranges), path)
	case "image":
		return validateFramePNG(path, job.Size)
	default:
		return fmt.Errorf("unknown preview kind %q", job.Kind)
	}
}
