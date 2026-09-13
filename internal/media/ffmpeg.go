package media

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/fsops"
)

const DefaultPreviewTimeout = 60 * time.Second
const ffmpegStderrLimit = 64 << 10

// Runner runs the validated ffmpeg and ffprobe paths. A Runner may be reused by
// preview workers; the encoder probe is cached after its first successful call.
type Runner struct {
	Tools          Toolset
	Timeout        time.Duration // positive overrides the per-preview default
	ProbeTimeout   time.Duration // passed to FFprobeReader
	EncoderTimeout time.Duration // zero uses 10 seconds
	Log            *slog.Logger

	mu       sync.Mutex
	encoders map[string]bool
}

// PreviewCommand supplies ffmpeg arguments without the output path. Run appends
// a temporary output path with the original extension. Progress is called from
// the stdout reader while ffmpeg runs; the callback must be concurrency-safe.
type PreviewCommand struct {
	Args          []string
	Output        string
	TotalDuration time.Duration
	OnProgress    func(Progress)
}

// PreviewPartPath preserves the output extension for ffmpeg's muxer selection.
func PreviewPartPath(output string) string {
	ext := filepath.Ext(output)
	return strings.TrimSuffix(output, ext) + ".arxgo-part" + ext
}

// Probe delegates ffprobe metadata reads to the existing bounded reader.
func (r *Runner) Probe(ctx context.Context, path, mime string) *MediaInfo {
	return (FFprobeReader{
		Path: r.Tools.Path(FFprobe), Timeout: r.ProbeTimeout, Log: r.Log,
	}).Read(ctx, path, mime)
}

// Run writes one preview to a reserved part path and publishes a nonempty file
// with a no-replace, durable rename. The caller owns the preview WAL transaction.
func (r *Runner) Run(ctx context.Context, req PreviewCommand) (err error) {
	path := r.Tools.Path(FFmpeg)
	if path == "" {
		return errors.New("ffmpeg unavailable")
	}
	if req.Output == "" || filepath.Ext(req.Output) == "" {
		return errors.New("preview output requires a file extension")
	}
	if _, statErr := os.Lstat(req.Output); statErr == nil {
		return fmt.Errorf("preview output already exists: %w", os.ErrExist)
	} else if !errors.Is(statErr, os.ErrNotExist) {
		return fmt.Errorf("check preview output: %w", statErr)
	}
	part := PreviewPartPath(req.Output)
	f, createErr := os.OpenFile(part, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if createErr != nil {
		return fmt.Errorf("reserve preview part: %w", createErr)
	}
	if closeErr := f.Close(); closeErr != nil {
		_ = os.Remove(part)
		return fmt.Errorf("close preview part: %w", closeErr)
	}
	defer func() {
		if removeErr := os.Remove(part); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			err = errors.Join(err, fmt.Errorf("remove preview part: %w", removeErr))
		}
	}()

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = previewTimeout(req.TotalDuration)
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	args := append([]string{"-hide_banner", "-nostdin", "-y", "-progress", "pipe:1", "-nostats"}, req.Args...)
	args = append(args, part)
	cmd := exec.CommandContext(runCtx, path, args...)
	progress := &progressCollector{callback: req.OnProgress}
	stderr := &tailBuffer{limit: ffmpegStderrLimit}
	cmd.Stdout, cmd.Stderr = progress, stderr
	runErr := runManaged(cmd)
	if runCtx.Err() != nil {
		return fmt.Errorf("ffmpeg preview: %w", runCtx.Err())
	}
	if progress.overflow {
		return errors.New("ffmpeg progress line exceeds limit")
	}
	if runErr != nil {
		reason := strings.TrimSpace(stderr.String())
		if reason == "" {
			return fmt.Errorf("ffmpeg preview: %w", runErr)
		}
		return fmt.Errorf("ffmpeg preview: %w: %s", runErr, reason)
	}
	f, err = os.OpenFile(part, os.O_RDWR, 0)
	if err != nil {
		return fmt.Errorf("open preview part: %w", err)
	}
	info, statErr := f.Stat()
	if statErr == nil && (!info.Mode().IsRegular() || info.Size() == 0) {
		statErr = errors.New("ffmpeg produced an empty or non-regular preview")
	}
	if statErr == nil {
		statErr = f.Sync()
	}
	closeErr := f.Close()
	if statErr != nil {
		return fmt.Errorf("validate preview part: %w", statErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close preview part: %w", closeErr)
	}
	if err := fsops.Rename(part, req.Output); err != nil {
		return fmt.Errorf("publish preview: %w", err)
	}
	return nil
}

func previewTimeout(duration time.Duration) time.Duration {
	if duration > time.Duration(math.MaxInt64/4) {
		return time.Duration(math.MaxInt64)
	}
	return max(DefaultPreviewTimeout, 4*duration)
}
