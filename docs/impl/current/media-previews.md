# Media Previews

Accepted work: [0026 FFmpeg runner](../records/0026-preview-implement-ffmpeg-runner.md),
[0027 preview planning](../records/0027-preview-implement-preview-planning.md),
[0028 video samples](../records/0028-preview-implement-video-samples.md),
[0029 frame images](../records/0029-preview-implement-frame-images.md), and
[0030 split/restore integration](../records/0030-preview-integrate-previews-into-split-and-restore.md),
and [0032 release bundle](../records/0032-preview-implement-release-bundle-with-ffmpeg.md).
Specification: [previews](../../openspec/stage-2-previews/previews.md). Active `split --sample`
and `--image` modes generate previews; `restore --previews delete` removes recorded previews of
restored videos when their byte size still matches.

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
Split owns the preview WAL records around this call.

On Linux, cancellation kills the ffmpeg process group, including descendants. A child that
holds an output pipe after its parent exits is killed when the one-second wait delay expires;
normal exits do not send a post-reap group signal. On Windows, the runner assigns ffmpeg to
a Job Object configured to terminate its process tree on close or timeout. Windows behavior is
cross-compiled and vetted on Linux; its runtime check is deferred to
[W7a](../../guide/windows-verification.md#scenario).

## Video sample executor (`internal/media`)

`Runner.GenerateSample` accepts a planned sample job and a video source path, then returns the
published output path. Split logs a preview begin event before passing an archive
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
seek yields no frame, it retries once at the first frame. Split logs a preview begin event before
passing an archive output. PNG extraction uses FFmpeg's software path; no CUDA
encoder is needed.

## Archive integration

Split preflights estimated bytes for the still missing preview jobs. It reads duration and
dimensions from the scan registry or ffprobe, moves each video transactionally, then submits its
preview jobs to a bounded one-worker ffmpeg queue. A failed preview leaves the committed move
intact, records an issue and returns exit 6. Later split runs catch up missing previews for
videos already marked moved. Completed and interrupted preview paths come from WAL events and are
excluded from split scans, so a sample cannot become a new video candidate.

`preview_begin`, `preview_done` and `preview_failed` are durable version-2 WAL events. A resume
removes the recorded unfinished part file and retries missing output; if the final output was
published before a crash, the run adopts it only after the normal format and dimension checks.
Registry `previews` entries are
replayed from all WAL runs. Each successful preview updates a marked block in the owned Markdown
stub, rendering PNGs inline and samples as links while preserving its front matter and any notes
after the preview section. Archive bytes written
count stubs and preview outputs.

Restore keeps previews by default. With `--previews delete`, it removes only WAL-recorded regular
files whose byte size matches the completed preview event. It logs a delete begin and completion,
so an interrupted cleanup resumes safely. Changed files and unrecorded files stay in place;
changed files make the restore partial. The stub is refreshed if it was kept. A malformed
`arxgo-videos.csv` stops split or restore with exit 5 and names the repair action before any move.

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
Archive tests on Linux generate a two-second MP4 with FFmpeg, then cover split, registry and stub
links, preview exclusion on rerun, catch-up after an ffmpeg failure, resume after a preview WAL
crash, size-checked deletion, and preservation of unrecorded and changed files. A binary-level
integration test exercises `split --sample start --image start` and `restore --previews delete`.
Windows behavior is cross-compiled and vetted; runtime checks remain in
[W7a](../../guide/windows-verification.md#scenario).

## Release bundles

`make dist` builds both static arxgo executables, fetches the pinned FFmpeg 6.1.1 GPL v3
tools when needed, and verifies the tool hashes before packaging. It writes a Linux `.tar.gz`
and Windows `.zip` to `dist/`. Each archive contains its platform's executable and tools,
`.env.example`, the matching practical manual, GPL v3 text, FFmpeg build/source notice,
arxgo's MIT licence and `SHA256SUMS`. The packager copies only these named files, so a
build-host `bin/.env` is excluded. `dist/SHA256SUMS` covers both archives. The tag release
workflow runs the required checks, builds and verifies these artifacts, then publishes them.
Linux bundle extraction, checksums and preview generation were exercised on a generated
video; the Windows bundle was inspected and cross-built on Linux. Windows runtime verification
remains [W8](../../guide/windows-verification.md#scenario).
