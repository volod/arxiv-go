package media

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"maps"
	"os/exec"
	"strings"
	"time"
)

const encoderOutputLimit = 1 << 20
const defaultEncoderTimeout = 10 * time.Second

// Encoders reports available ffmpeg encoder names. A successful probe is
// cached per Runner; failed or canceled probes can be retried.
func (r *Runner) Encoders(ctx context.Context) (map[string]bool, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.encoders != nil {
		return maps.Clone(r.encoders), nil
	}
	path := r.Tools.Path(FFmpeg)
	if path == "" {
		return nil, errors.New("ffmpeg unavailable")
	}
	timeout := r.EncoderTimeout
	if timeout <= 0 {
		timeout = defaultEncoderTimeout
	}
	runCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path, "-hide_banner", "-encoders")
	stdout := &probeOutput{limit: encoderOutputLimit}
	stderr := &tailBuffer{limit: ffmpegStderrLimit}
	cmd.Stdout, cmd.Stderr = stdout, stderr
	err := runManaged(cmd)
	switch {
	case runCtx.Err() != nil:
		return nil, fmt.Errorf("ffmpeg encoder probe: %w", runCtx.Err())
	case stdout.overflow:
		return nil, errors.New("ffmpeg encoder list exceeds limit")
	case err != nil:
		return nil, fmt.Errorf("ffmpeg encoder probe: %w: %s", err, strings.TrimSpace(stderr.String()))
	}
	encoders := parseEncoders(string(stdout.buf))
	if len(encoders) == 0 {
		return nil, errors.New("ffmpeg reported no encoders")
	}
	r.encoders = encoders
	return maps.Clone(encoders), nil
}

func parseEncoders(output string) map[string]bool {
	encoders := make(map[string]bool)
	scanner := bufio.NewScanner(strings.NewReader(output))
	for scanner.Scan() {
		fields := strings.Fields(scanner.Text())
		if len(fields) < 2 || len(fields[0]) != 6 {
			continue
		}
		flags := fields[0]
		if flags[0] != 'V' && flags[0] != 'A' && flags[0] != 'S' {
			continue
		}
		if fields[1] != "=" {
			encoders[fields[1]] = true
		}
	}
	return encoders
}
