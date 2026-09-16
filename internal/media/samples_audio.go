package media

import (
	"context"
	"errors"
	"fmt"
	"os/exec"
	"strconv"
)

// sampleAudio chooses the audio stream a sample encodes: the first source stream that decodes.
// A stream without an FFmpeg decoder, such as Apple positional audio (apac), is passed over;
// iPhone recordings carry an AAC stream as well. Without a decodable stream the sample is written
// without sound. A test decode is used rather than comparing codec and decoder names, which differ
// for some codecs (amr_nb and amrnb).
func (r *Runner) sampleAudio(ctx context.Context, source string, job PreviewJob) (PreviewJob, error) {
	if !job.HasAudio {
		return job, nil
	}
	for stream := range max(job.AudioStreams, 1) {
		ok, err := r.decodesAudio(ctx, source, stream)
		if err != nil {
			return job, err
		}
		if ok {
			if stream > 0 && r.Log != nil {
				r.Log.Warn("sample uses a later audio stream; earlier ones do not decode", "source", source, "audio_stream", stream)
			}
			job.AudioStream = stream
			return job, nil
		}
	}
	if r.Log != nil {
		r.Log.Warn("no audio stream decodes; sample written without sound", "source", source, "audio_streams", job.AudioStreams)
	}
	job.HasAudio, job.AudioStream = false, 0
	return job, nil
}

// decodesAudio decodes one second of an audio stream to a null output. A non-zero exit means the
// stream is missing or cannot be decoded; a canceled context or an unavailable ffmpeg is an error.
func (r *Runner) decodesAudio(ctx context.Context, source string, stream int) (bool, error) {
	path := r.Tools.Path(FFmpeg)
	if path == "" {
		return false, errors.New("ffmpeg unavailable")
	}
	runCtx, cancel := context.WithTimeout(ctx, DefaultPreviewTimeout)
	defer cancel()
	cmd := exec.CommandContext(runCtx, path, "-hide_banner", "-nostdin", "-v", "error", "-i", source,
		"-map", "0:a:"+strconv.Itoa(stream), "-t", "1", "-f", "null", "-")
	stderr := &tailBuffer{limit: ffmpegStderrLimit}
	cmd.Stderr = stderr
	err := runManaged(cmd)
	if runCtx.Err() != nil {
		return false, fmt.Errorf("ffmpeg audio decode check: %w", runCtx.Err())
	}
	var exit *exec.ExitError
	if errors.As(err, &exit) {
		return false, nil
	}
	if err != nil {
		return false, fmt.Errorf("ffmpeg audio decode check: %w", err)
	}
	return true, nil
}
