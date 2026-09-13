# Media Previews

Accepted work: [0026 FFmpeg runner](../records/0026-preview-implement-ffmpeg-runner.md).
Specification: [previews](../../openspec/stage-2-previews/previews.md). The runner is available
to later preview tasks; sample and image CLI options are not yet enabled.

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

Test-binary helpers cover fragmented progress, invalid values, timeouts, cancellation, a
child process that outlives its parent, stderr-tail capture, failed and empty output cleanup,
pre-existing file conflicts, encoder parsing and cache retry. A local `lavfi` run with ffmpeg
6.1.1 produced a nonempty AVI, a final progress report and an encoder list. The test skips with
a reason when ffmpeg is unavailable; `ARXGO_TEST_REQUIRE_TOOLS=1` makes it mandatory locally.
