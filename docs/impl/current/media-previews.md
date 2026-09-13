# Media Previews

Accepted work: [0026 FFmpeg runner](../records/0026-preview-implement-ffmpeg-runner.md),
[0027 preview planning](../records/0027-preview-implement-preview-planning.md).
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

## Verification

Planner table tests cover all position modes, short and unknown duration, caps, landscape and
portrait clamps at each box, rotation, even rounding, collisions, padding, encoder selection and
space estimates. CLI tests cover defaults, explicit and environment settings, invalid values and
the no-mutation guard for active modes. Test-binary helpers cover fragmented progress, invalid values, timeouts, cancellation, a
child process that outlives its parent, stderr-tail capture, failed and empty output cleanup,
pre-existing file conflicts, encoder parsing and cache retry. A local `lavfi` run with ffmpeg
6.1.1 produced a nonempty AVI, a final progress report and an encoder list. The test skips with
a reason when ffmpeg is unavailable; `ARXGO_TEST_REQUIRE_TOOLS=1` makes it mandatory locally.
