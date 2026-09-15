# Media Previews

Accepted work: [0026 FFmpeg runner](../records/0026-preview-implement-ffmpeg-runner.md),
[0027 preview planning](../records/0027-preview-implement-preview-planning.md),
[0028 video samples](../records/0028-preview-implement-video-samples.md),
[0029 frame images](../records/0029-preview-implement-frame-images.md),
[0030 split/restore integration](../records/0030-preview-integrate-previews-into-split-and-restore.md),
[0032 release bundle](../records/0032-preview-implement-release-bundle-with-ffmpeg.md),
[0033 stage-2 review](../records/0033-preview-review-stage-2-previews.md),
[0034 stage-2 repairs](../records/0034-preview-repair-stage-2-preview-defects.md),
[0035 FFmpeg 9.0.1 approval](../records/0035-preview-approve-ffmpeg-9-distribution.md),
[0036 FFmpeg 9.0.1 upgrade](../records/0036-preview-upgrade-bundled-ffmpeg-to-9.md) and
[0037 undecodable sample audio](../records/0037-preview-handle-undecodable-sample-audio.md).
Specification: [previews](../../openspec/stage-2-previews/previews.md). The capability is shipped:
`split --sample` and `--image` generate previews next to the descriptions, and `restore --previews
delete` removes the recorded previews of restored videos.

## Planning (`internal/media`)

`PlanPreviews` is pure: from `MediaInfo`, the source name, `PreviewOptions` and a name-occupancy
predicate it returns one sample job (a series combines its ranges) and one image job per frame,
each with its output name, display-oriented even size, quality and, for samples, the encoders.
The source extension selects the sample container: MP4, M4V, MOV, 3GP, 3G2, F4V, FLV, MKV, TS,
M2TS and MTS keep their extension with H.264/AAC, WebM uses VP9/Opus and AVI MPEG-4/MP3; every
other extension (MPEG-PS, VOB, Ogg, MXF, DV, WMV, ASF, RealMedia, `.qt`, GoPro `.LRV`, custom
`--video-extensions`) gets an `.mp4` sample and a warning. The choice is made at planning, so every
run derives the same name. A taken name gets `-arxgo`; if that is also taken the plan fails.

Start previews work without a known duration; other modes skip with a warning, and so does a source
without a video stream. The plan also returns series-cap warnings and the specified space estimate.
The CLI validates the settings (defaults: 5 s samples, 5 min spacing, `sd`, `medium`, cap 100) and
passes them to split.

## FFmpeg runner (`internal/media`)

`Runner.Run` executes ffmpeg with an argument vector, `-progress pipe:1 -nostats`, a per-preview
timeout of `max(60s, 4 x output duration)` and the last 64 KiB of stderr in errors. It reserves
`<stem>.arxgo-part.<ext>` exclusively, refuses an existing output or part, validates the finished
part (nonempty, then the caller's content check), and publishes it with a no-replace durable
rename; any failure removes only its own part. On Linux cancellation kills the process group (and a
descendant still holding a pipe after the one-second wait delay); on Windows a kill-on-close Job
Object owns the tree (cross-compiled only, [W7a](../../guide/windows-verification.md#scenario)).
`Encoders` parses `ffmpeg -encoders` once per runner; a failed probe can be retried.

`GenerateSample` first decodes one second of each audio stream in order and encodes the first that
decodes; a stream without an FFmpeg decoder (Apple positional audio `apac`) is passed over with a
warning, and without any decodable stream the sample is silent with a warning. It encodes the first
video stream and that audio stream with input seeking per range and the concat filter for series, in chunks of at most 50 ranges joined by the concat demuxer from a private
temporary directory. Missing `libx264` falls back to `mpeg4`, missing `libmp3lame` to `aac`. The
part must probe with the planned size, audio when the source has it, and a duration within
`max(0.5 s, 5%)` (shorter allowed for an unknown-duration start clip). `GenerateFrame` extracts one
PNG with compression 9/6/3 for low/medium/high and decodes it with `image/png` before publication;
an empty result of an unknown-duration start seek retries once at the first frame.

## Split integration (`internal/archive`)

Before the scan, split replays the preview events of every run (`previews_index.go`): completed
previews with sizes, and generations or deletions without an outcome. Their paths are excluded
from the scan, and part files of unfinished generations are removed (not on a dry run). Paths are
resolved against the roots recorded in each run's `options.json`, so a mounted or renamed archive
keeps its previews.

Planning covers new candidates and videos earlier runs moved (`previews_plan.go`). Media metadata
comes from the file registry or the video registry and is probed with ffprobe only when incomplete.
The estimate of previews not yet published joins the preflight. One worker goroutine
(`previews_exec.go`) receives each video after its commit, videos moved earlier first, and reads it
from the video archive. Each output is logged as `preview_begin`, then `preview_done` with its size
or `preview_failed` with the reason. A published preview with its recorded size is skipped, so a
rerun after success generates nothing. A file left at a planned path by an unfinished generation is
adopted only after the normal content checks; any other existing file is a failed preview. After
its jobs the worker refreshes the marked preview section of the owned description, writing only when it
changed. Cancellation leaves the event unfinished and exits 130. A failed preview is an issue,
counted in `previews_done`/`previews_failed` (never `videos_failed`), recorded as a run issue,
and makes the run exit 6; the move stays committed. A fatal error
(for example a WAL write) stops the worker and then the split loop at the next transaction.

Previews are identified by name. A later split with other modes keeps existing previews of the
same name and generates the missing ones; deleting a preview file makes the next split with preview
options generate it again with the current settings.

## Restore integration (`internal/archive`)

Restore keeps previews by default. Before executing it removes unfinished generation parts and
finishes interrupted deletions (`previews_restore.go`). With `--previews delete` it deletes the
recorded previews of each video it restores, and first those of videos an interrupted process of
the same run already restored. A preview is deleted only when it is still a regular file with the
recorded size, below real directories of the archive; `preview_delete` is durable before the
removal and `preview_deleted` after it. A changed file is kept and reported as skipped (exit 6);
unrecorded files are never touched. A kept description loses the deleted links, and its section when none
remain.

## Real media support

A read-only survey of an operator drone archive (627 video files, 287 GiB) and a split, rerun and
restore of a 43-file copy covering every codec and container class
([0033](../records/0033-preview-review-stage-2-previews.md#declared-run-on-real-media)) found: H.264
(Baseline to High, `yuvj420p`), HEVC Main and Main 10, MPEG-1, WMV3; AAC, AC-3 5.1, PCM, WMA; MP4,
MOV, MTS with PGS subtitles, raw MPEG-1 `.mpg`, WMV and GoPro `.LRV` all produce samples and frames
(first with FFmpeg 6.1.1, again on the drone copy with the pinned 9.0.1). GoPro and phone data
tracks (`gpmd` telemetry, `fdsc` SOS, `tmcd` timecode,
`mebx`/`mett` metadata) are moved byte for byte and ignored by previews; desktop players that report
missing "codecs" for them (GStreamer `meta/x-gst-fourcc-gpmd`) do not affect arxgo. Portrait videos
with display rotation produce upright previews.

No FFmpeg version decodes Apple positional audio (`apac`): FFmpeg 9.0.1 recognizes it as
`apple_apac`, and Apple's decoder exists only on its own platforms. Samples therefore use the first
decodable audio stream (the AAC track iPhones record as well) and are silent when there is none;
the real iPhone files, a remux with `apac` first and an `apac`-only remux all produce samples
([0037](../records/0037-preview-handle-undecodable-sample-audio.md)). FFmpeg 6.1.1 and 7.1.1 could
not build a series sample from mono MPEG-1 Layer II audio after seeking; the pinned 9.0.1 can
([0036](../records/0036-preview-upgrade-bundled-ffmpeg-to-9.md)).

## Release bundles

`make dist` builds both static arxgo executables, fetches the pinned FFmpeg 9.0.1 GPL v3 tools
when needed and verifies them before packaging. Each archive in `dist/` contains the platform's
executable and tools, `.env.example`, the matching practical manual, GPL v3 text, the FFmpeg
source notice, arxgo's MIT licence and `SHA256SUMS`; a build-host `bin/.env` is never included, and
`dist/SHA256SUMS` covers both archives. The tag release workflow runs the required checks, then
builds, verifies and publishes the bundles. The Windows bundle was cross-built and inspected on
Linux; its runtime check is [W8](../../guide/windows-verification.md#scenario).

## Verification

Pure planner tests cover positions, short, unknown and non-finite durations, caps, clamps, names and
collisions, container substitution and estimates. Test-binary helpers cover runner progress,
timeouts, process trees, stderr tails, output conflicts and the encoder cache. Live tests (skipped
without ffmpeg, mandatory with `ARXGO_TEST_REQUIRE_TOOLS=1`) generate media at run time and cover
every mode, MOV/MKV/WebM/M4V/3GP/AVI and substituted MPG/VOB/OGV containers (with mono MP2), a
52-range series, rotation, encoder fallback, an undecodable first or only audio stream, and
invalid-output rejection. They use the pinned tools in `bin/` when present. Archive tests cover split, registry and
description links, rerun without regeneration, substituted containers across reruns, failure counters and
catch-up, crash and cancellation during a preview, adoption checks, name collisions, archive
relocation, restore deletion with changed and unrecorded files, crashes around the restore commit
and deletion events, a kept description, and split-restore-split description links. `TestPreviewSplitRestoreRoundTrip` drives
the built binary through split and restore.
