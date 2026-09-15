# Previews: video samples and frame images

Owner: `media-previews`. Flags: [CLI split flags](../stage-1-core/cli.md#split-flags).

## Operator problem

A video description says a video existed, but not what it shows. Short samples and a few frames stored
next to the description make the document archive browsable while the full video lives elsewhere.

## Placement and naming

Previews are written into the **main archive**, next to the description, because they exist for quick
local viewing. For `projects/2024/interview.mp4`:

| Kind | Name | Example |
| --- | --- | --- |
| Video sample | `<stem>-smpl<NN>.<ext>` | `interview-smpl01.mp4` |
| Frame image | `<stem>-img<NN>.png` | `interview-img01.png`, `interview-img02.png` |

- `<stem>` is the file name without its last extension. `<ext>` is the original extension when its
  container holds the sample encoders ([encoding by container](#encoding-by-container)), so the
  sample uses the source's container format; otherwise it is `.mp4`. The choice is made when the
  preview is planned, so every run derives the same name.
- `<NN>` is the 1-based series index, zero-padded to at least two digits (three when a series has
  more than 99 items).
- A name collision with a file not created by arxgo is resolved by inserting `-arxgo` before the
  suffix (`interview-arxgo-smpl01.mp4`) and logged.
- Every preview path is recorded by the preview events of the WAL and listed in the `previews`
  column of the video registry. Split excludes the WAL-recorded paths from later scans and restore
  deletes only WAL-recorded previews; an edited registry column changes neither.
- The video description describes the original video; previews only add lines to it. The owned
  description lists the completed previews as `- ` link lines after its fields (PNG frames
  embedded, samples linked; [description contract](../stage-1-core/contracts.md#video-description)). Split
  refreshes them for every video it plans previews for, and restore updates them in a kept description;
  other text is never changed.

## Position modes

Let `D` be the video duration from [metadata](../stage-1-core/metadata.md) (probed with ffprobe
when `--metadata file` did not collect it), `L` the sample duration, `E` the series spacing.

| Mode | Video sample | Frame image |
| --- | --- | --- |
| `none` | nothing (default) | nothing (default) |
| `start` | one clip `[0, min(L, D))` -> `smpl01` | one frame at `min(1s, D/2)` (avoids black first frames) -> `img01` |
| `middle` | one clip `[max(0, D/2 - L/2), +L)` -> `smpl01` | one frame at `D/2` -> `img01` |
| `end` | one clip `[max(0, D - L), D)` -> `smpl01` | one frame at `max(0, D - 1s)` -> `img01` |
| `series` | one clip concatenating fragments of length `L` starting at `0, E, 2E, ...` while `< D` -> `smpl01` | one frame at each `0+1s, E, 2E, ...` while `< D` -> `img01..imgNN` |

- When `D <= L`, sample modes produce a single clip of the whole video.
- `--preview-max-items` (default 100) caps series length; the cap is logged.
- Unknown duration (live streams, broken index): `start` still works; other modes skip the preview
  with a warning.
- A source without a video stream skips previews with a warning; it is not a failure.
- `D` is the presented duration: for ISO BMFF files the movie header, which honors edit lists, not
  the longer media of a trimmed edit.
- Previews are identified by name: a later run with other modes or settings keeps an existing
  preview with the same name and generates only the names that are missing.

## Resolution and quality

Targets are bounding boxes: `sd` 640x360, `hd` 1920x1080, `4k` 3840x2160 (default `sd`).

- The output keeps the source aspect ratio and display rotation.
- If the source fits inside the box, its resolution is kept (never upscaled).
- Otherwise it is scaled down to fit: `scale=w='min(iw,W)':h='min(ih,H)':force_original_aspect_ratio=decrease`,
  then rounded to even dimensions for encoders that require them.
- Portrait videos are fitted against the rotated box (360x640 for `sd`).

| `--sample-quality` | x264/x265 CRF | VP9 CRF | Audio |
| --- | --- | --- | --- |
| `low` | 32 | 40 | 64 kbit/s |
| `medium` | 26 | 33 | 96 kbit/s |
| `high` | 20 | 28 | 128 kbit/s |

| `--image-quality` | PNG `-compression_level` |
| --- | --- |
| `low` | 9 (smallest) |
| `medium` | 6 |
| `high` | 3 |

## Encoding by container

Samples are re-encoded (stream copy cannot cut at exact times or scale). The source extension,
compared case-insensitively, selects a container whose standard codecs the sample uses:

| Source extension | Sample | Video encoder | Audio encoder |
| --- | --- | --- | --- |
| mp4, m4v, mov, 3gp, 3g2, f4v, flv, mkv, ts, m2ts, mts | same extension | `libx264` | `aac` |
| webm | `.webm` | `libvpx-vp9` | `libopus` |
| avi | `.avi` | `mpeg4` | `libmp3lame` |
| any other (mpg, vob, ogv, mxf, dv, wmv, asf, rm, qt, lrv, `--video-extensions` entries) | `.mp4`, logged as a warning | `libx264` | `aac` |

- Missing `libx264` falls back to `mpeg4` and missing `libmp3lame` to `aac`; any other missing
  encoder fails the preview. Encoder availability is read once from `ffmpeg -hide_banner -encoders`.
- Only the first video stream and one audio stream are encoded: the first audio stream that
  decodes, found by decoding one second of each in order. A stream without an FFmpeg decoder, such
  as Apple positional audio (`apac`, decodable only on Apple platforms), is passed over with a
  warning; when no audio stream decodes, the sample is written without sound and a warning is
  logged. Data tracks (for example GoPro telemetry `gpmd`, GoPro SOS `fdsc`, timecode `tmcd`,
  Apple metadata `mebx`) and subtitles are ignored; the moved video keeps them byte for byte.

## ffmpeg invocation

- All calls go through `media.Runner`: argument vectors, `exec.CommandContext`, per-preview timeout
  (default `max(60s, 4 x total output duration)`), stderr ring buffer for errors, and
  `-progress pipe:1 -nostats` parsed for progress (`out_time_us`, `speed`).
- Output is written to `<stem>.arxgo-part.<ext>` (extension kept so ffmpeg selects the muxer) and
  renamed without replacement when ffmpeg exits 0, the file is non-empty and its content validates
  (ffprobe size, audio and duration for samples; `image/png` decode and size for frames). An
  existing output or part file is refused; a failed invocation removes only the part file it
  reserved. Part files are [reserved paths](../spec.md#reserved-paths).
- Single clip: `ffmpeg -hide_banner -y -ss START -t L -i SRC -map 0:v:0 -map 0:a:0? -vf SCALE
  -c:v ENC -crf Q -preset veryfast -c:a AENC -b:a AB -movflags +faststart OUT`.
- Series clip: one ffmpeg call with `-ss/-t` inputs per fragment (input seeking) and the `concat`
  filter, limited to 50 inputs per call; longer series are built in chunks and concatenated with
  the concat demuxer.
- Frame: `ffmpeg -hide_banner -y -ss T -i SRC -frames:v 1 -vf SCALE -compression_level C OUT.png`.
- Audio check: `ffmpeg -hide_banner -nostdin -v error -i SRC -map 0:a:N -t 1 -f null -`; exit 0
  means stream `N` decodes. The single-clip and series commands then map `0:a:N`.
- Previews are generated from the **destination** copy in the video archive after the transaction
  commits, so a failure never affects the move; when the video archive is slow (network), the
  operator may accept the extra read cost or skip previews.

## Transactions and failures

- After `commit`, each preview is a WAL sub-record: `preview_begin` (path) then `preview_done`
  (size) or `preview_failed` (reason). The next split or restore deletes part files of unfinished
  previews, and split retries them. A final file published just before a crash is adopted only when
  it passes the same content checks; any other existing file at a planned path is a failed preview.
- A failed preview is logged, counted in `previews_failed` (never in `videos_failed`), listed in
  the summary, and yields exit code 6; the video stays moved. Cancellation (Ctrl+C) leaves the
  preview unfinished, not failed, and exits 130.
- Rerunning split for a video that is already moved but lacks requested previews generates only the
  missing ones; a rerun after success generates nothing and exits 0.
- WAL paths are absolute paths of the roots recorded in the run's `options.json`; previews stay
  owned when the archive root is later mounted or renamed elsewhere.

## Space estimate

For preflight: sample bytes = total sample seconds x bitrate estimate (`sd` 1 Mbit/s, `hd`
5 Mbit/s, `4k` 20 Mbit/s, times 0.7/1.0/1.5 for low/medium/high); image bytes = count x
(`sd` 0.5 MiB, `hd` 3 MiB, `4k` 12 MiB). Durations come from scan metadata or a quick ffprobe pass.

## Restore

- `--previews keep` (default): previews stay in the archive next to the restored video.
- `--previews delete`: preview files recorded for each video this run restores, including videos an
  interrupted process of the same run restored, are deleted if their size matches the recorded
  size; unrecorded files are never deleted. The WAL logs `preview_delete` before removal and
  `preview_deleted` after it; an interrupted delete is finished by the next restore. A changed file
  is kept and reported as a skipped item (exit 6).
- Previews of videos restored by an earlier interrupted restore that another run replaced follow
  [replaced restores](../stage-4-catia/split-restore.md#replaced-restores): the next restore with
  `--previews delete` deletes them when that earlier run's `options.json` records
  `sidecar_cleanup: true`.
- The description is updated or deleted according to `--descriptions`.

## Release bundle

`make dist` produces per-platform archives:

```text
arxgo-<version>-linux-amd64.tar.gz    arxgo, ffmpeg, ffprobe, .env.example, manual-linux.md, LICENSES/, SHA256SUMS
arxgo-<version>-windows-amd64.zip     arxgo.exe, ffmpeg.exe, ffprobe.exe, .env.example, manual-windows.md, LICENSES\, SHA256SUMS
```

- ffmpeg builds are downloaded from pinned sources with pinned SHA-256 values in
  `packaging/ffmpeg.lock` (`make ffmpeg` fetches and verifies them into `bin/`); binaries are
  never committed.
- Approved builds (decision records for the
  [sources and licence](../../impl/records/0002-preview-approve-ffmpeg-distribution.md) and
  [version 9.0.1](../../impl/records/0035-preview-approve-ffmpeg-9-distribution.md)): FFmpeg
  9.0.1, GPL v3 variants, statically linked.

  | Platform | Build |
  | --- | --- |
  | `linux/amd64` | `mwader/static-ffmpeg:9.0.1` image layer, fetched from Docker Hub by digest |
  | `windows/amd64` | gyan.dev `ffmpeg-9.0.1-essentials_build.zip` from `GyanD/codexffmpeg` |

- Every bundle includes the GPL v3 licence text, the build's provenance (source commit or recipe)
  and a written offer or link to the corresponding FFmpeg source of the pinned version.
- The Linux and Windows bundles include the matching practical operator manual from
  [docs/guide/](../../guide/README.md) next to the executable. They include the environment
  template but never an operator's `.env` file.
- `arxgo` never downloads tools at runtime.

## Acceptance

- Test videos are generated at test time with `ffmpeg -f lavfi -i testsrc2=...:duration=N -f lavfi
  -i sine=...` in `t.TempDir()`: landscape and portrait, below and above each resolution box,
  with and without audio, in mp4, mov, mkv and webm. Tests skip with a logged reason when ffmpeg is
  absent. GitHub CI does not install ffmpeg; live ffmpeg tests run only locally.
- For each mode: file count and names, output duration within +/-0.5 s per fragment (via ffprobe),
  dimensions equal to the clamp rule, container matches source, PNG decodes with `image/png`.
- Planning (positions, counts, names, resolution clamp) is pure Go and unit-tested without ffmpeg.
- Failure injection: ffmpeg exits non-zero -> no preview file, video still moved, exit 6.
- Restore `--previews delete` removes recorded previews only, also after a crash between the restore
  commit and the deletion, including when another run replaced the crashed restore.
- A split, restore with kept previews, split sequence links the kept previews in the new description.
