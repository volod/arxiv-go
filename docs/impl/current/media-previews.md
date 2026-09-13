# Media Previews

Accepted work: [0026 FFmpeg runner](../records/0026-preview-implement-ffmpeg-runner.md),
[0027 preview planning](../records/0027-preview-implement-preview-planning.md),
[0028 video samples](../records/0028-preview-implement-video-samples.md),
[0029 frame images](../records/0029-preview-implement-frame-images.md).
Specification: [previews](../../openspec/stage-2-previews/previews.md). The runner is available
to later preview tasks. Split preview flags now parse and validate, but an active sample or image
mode exits 70 before opening a run until generation is integrated.

## Pure planner (`internal/media`)

`PlanPreviews` takes `MediaInfo`, a source name, validated `PreviewOptions` and a read-only
name-occupancy predicate. It returns one sample job with time ranges (a series combines fragments)
and one image job per frame time. Each job carries its output name, display-oriented even size,
preferred codecs or image quality. It uses the probed container to choose preferred codecs when
available and preserves the source extension in sample names. Existing names get the `-arxgo`
suffix; occupancy of both names is an error. The plan also reports series caps, unknown-duration
skips, collision warnings and the specified preflight byte estimate.

Start previews work without a known duration; other modes skip with a warning. Sample duration
and spacing default to 5 seconds and 5 minutes, image spacing to 5 minutes, resolution to `sd`,
quality to `medium`, and the series cap to 100. CLI parsing accepts these settings from flags,
environment variables and `.env`, validates them, and passes typed options to the split handler.

## FFmpeg runner (`internal/media`)

`media.Runner` uses validated `Toolset` paths. `Run` accepts ffmpeg arguments, a final
preview path, planned output duration and an optional progress callback. It adds
`-progress pipe:1 -nostats`, parses `out_time_us` and `speed` reports as ffmpeg runs, and
applies a per-preview timeout of `max(60s, 4 x output duration)` unless overridden. It keeps
the last 64 KiB of stderr in a failed command's error. `Probe` delegates to the existing
bounded `FFprobeReader`; `Encoders` parses `ffmpeg -hide_banner -encoders` and caches a
successful result per runner.

`Run` reserves `<stem>.arxgo-part.<ext>` without replacing a pre-existing part or final
file. It removes its part on failure. After a successful ffmpeg exit, it requires a nonempty
regular file, flushes it, and publishes it with the no-replace durable rename in `fsops`.
The future split integration owns the preview WAL records around this call.

On Linux, cancellation kills the ffmpeg process group, including descendants. A child that
holds an output pipe after its parent exits is killed when the one-second wait delay expires;
normal exits do not send a post-reap group signal. On Windows, the runner assigns ffmpeg to
a Job Object configured to terminate its process tree on close or timeout. Windows behavior is
cross-compiled and vetted on Linux; its runtime check is deferred to
[W7a](../../guide/windows-verification.md#scenario).

## Video sample executor (`internal/media`)

`Runner.GenerateSample` accepts a planned sample job and a video source path, then returns the
published output path. The owning split transaction must log the preview before passing an archive
output. Single clips use input seeking; series clips concatenate planned ranges. Series longer
than 50 ranges are encoded in groups of at most 50 in a private temporary directory, then joined
with the concat demuxer. Chunk files are removed after the call. The runner probes the part file
before publication and requires video, planned dimensions, source container, expected audio and
duration within a tight tolerance; invalid results leave no published sample.
For an unknown-duration `start` job, a shorter nonempty output is valid because the source may end
before the requested length.

MP4, M4V, MOV, 3GP, MKV, WebM and AVI use the container-specific software encoders in the
specification. Missing `libx264` falls back to `mpeg4`, and missing AVI `libmp3lame` falls back
to `aac`. Quality maps to x264 CRF, VP9 CRF, `mpeg4` quantizer and audio bitrate. If an unknown
source extension has no FFmpeg muxer, the executor retries as MP4 and returns that path. It does
not retry an occupied output or other failure. This CUDA host ran the live tests with the
specified software encoders; GPU encoding is outside the current container policy.

Both metadata readers report display-oriented dimensions, so the clamp now uses those
dimensions directly. FFmpeg applies display rotation when it decodes a rotated source.

## Frame image executor (`internal/media`)

`Runner.GenerateFrame` accepts a planned image job and a video source path. It seeks to the
planned position, selects the first video stream, scales to the planned display-oriented size and
encodes one PNG. Image quality maps to PNG compression levels 9, 6 and 3 for low, medium and high.
The runner decodes the part with `image/png` and checks its dimensions before publishing it. An
invalid or empty PNG leaves no final output. For an unknown-duration start job, if the 1-second
seek yields no frame, it retries once at the first frame. The owning split transaction must log
the preview before passing an archive output. PNG extraction uses FFmpeg's software path; no CUDA
encoder is needed.

## Verification

Planner table tests cover all position modes, short and unknown duration, caps, landscape and
portrait clamps at each box, rotation, even rounding, collisions, padding, encoder selection and
space estimates. CLI tests cover defaults, explicit and environment settings, invalid values and
the no-mutation guard for active modes. Test-binary helpers cover fragmented progress, invalid values, timeouts, cancellation, a
child process that outlives its parent, stderr-tail capture, failed and empty output cleanup,
pre-existing file conflicts, encoder parsing and cache retry. A local `lavfi` run with ffmpeg
6.1.1 produced a nonempty AVI, a final progress report and an encoder list. The test skips with
a reason when ffmpeg is unavailable; `ARXGO_TEST_REQUIRE_TOOLS=1` makes it mandatory locally.
Live sample tests generate `testsrc2` and `sine` sources and verify modes, duration, dimensions,
audio and container across MP4, MOV, MKV, WebM, M4V, 3GP and AVI. They also cover a 52-range
series with and without audio, short videos, landscape and portrait clamping, rotation, encoder
fallback, unknown-extension fallback and ffprobe rejection before publication.
An unknown-duration start job on a short source is also covered.
Frame tests generate clips at run time and check all position modes, frame counts and names,
decoded dimensions, landscape and portrait clamps, display rotation, a late seek in a short video,
unknown-duration start fallback, compression arguments, invalid PNG rejection and occupied-file
preservation.
