# Previews: video samples and frame images

Owner: `media-previews`. Flags: [CLI split flags](../stage-1-core/cli.md#split-flags).

## Operator problem

A Markdown stub says a video existed, but not what it shows. Short samples and a few frames stored
next to the stub make the document archive browsable while the full video lives elsewhere.

## Placement and naming

Previews are written into the **main archive**, next to the stub, because they exist for quick
local viewing. For `projects/2024/interview.mp4`:

| Kind | Name | Example |
| --- | --- | --- |
| Video sample | `<stem>-smpl<NN>.<ext>` | `interview-smpl01.mp4` |
| Frame image | `<stem>-img<NN>.png` | `interview-img01.png`, `interview-img02.png` |

- `<stem>` is the file name without its last extension; `<ext>` is the original extension, so the
  sample uses the same container format as the source.
- `<NN>` is the 1-based series index, zero-padded to at least two digits (three when a series has
  more than 99 items).
- A name collision with a file not created by arxgo is resolved by inserting `-arxgo` before the
  suffix (`interview-arxgo-smpl01.mp4`) and logged.
- Every preview path is recorded in the WAL (`preview` step) and in the `previews` column of the
  video registry, which is how split excludes previews from later scans and how restore finds them.

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

Samples are re-encoded (stream copy cannot cut at exact times or scale). Codec choice keeps the
source container valid:

| Source container | Video encoder | Audio encoder |
| --- | --- | --- |
| mp4, m4v, mov, 3gp | `libx264` (fallback `mpeg4`) | `aac` |
| mkv | `libx264` | `aac` |
| webm | `libvpx-vp9` | `libopus` |
| avi | `mpeg4` | `libmp3lame` (fallback `aac`) |
| other | `libx264` in the same extension if ffmpeg accepts it, otherwise the sample is written as `.mp4` and the substitution is logged |

Encoder availability is read once from `ffmpeg -hide_banner -encoders`.

## ffmpeg invocation

- All calls go through `media.Runner`: argument vectors, `exec.CommandContext`, per-preview timeout
  (default `max(60s, 4 x total output duration)`), stderr ring buffer for errors, and
  `-progress pipe:1 -nostats` parsed for progress (`out_time_us`, `speed`).
- Output is written to `<name>.arxgo-part.<ext>` (extension kept so ffmpeg selects the muxer) and
  renamed when ffmpeg exits 0 and the file is non-empty.
- Single clip: `ffmpeg -hide_banner -y -ss START -t L -i SRC -map 0:v:0 -map 0:a:0? -vf SCALE
  -c:v ENC -crf Q -preset veryfast -c:a AENC -b:a AB -movflags +faststart OUT`.
- Series clip: one ffmpeg call with `-ss/-t` inputs per fragment (input seeking) and the `concat`
  filter, limited to 50 inputs per call; longer series are built in chunks and concatenated with
  the concat demuxer.
- Frame: `ffmpeg -hide_banner -y -ss T -i SRC -frames:v 1 -vf SCALE -compression_level C OUT.png`.
- Previews are generated from the **destination** copy in the video archive after the transaction
  commits, so a failure never affects the move; when the video archive is slow (network), the
  operator may accept the extra read cost or skip previews.

## Transactions and failures

- After `commit`, each preview is a WAL sub-record: `preview_begin` (path) then `preview_done` or
  `preview_failed` (reason). Recovery deletes part files of unfinished previews and retries them
  on resume.
- A failed preview is logged, counted, reflected in the summary, and yields exit code 6; the video
  stays moved.
- Rerunning split for a video that is already moved but lacks requested previews generates only the
  missing ones.

## Space estimate

For preflight: sample bytes = total sample seconds x bitrate estimate (`sd` 1 Mbit/s, `hd`
5 Mbit/s, `4k` 20 Mbit/s, times 0.7/1.0/1.5 for low/medium/high); image bytes = count x
(`sd` 0.5 MiB, `hd` 3 MiB, `4k` 12 MiB). Durations come from scan metadata or a quick ffprobe pass.

## Restore

- `--previews keep` (default): previews stay in the archive next to the restored video.
- `--previews delete`: preview files recorded for the restored video are deleted if their size
  matches the recorded size; unrecorded files are never deleted.
- The stub is updated or deleted according to `--stubs`.

## Release bundle

`make dist` produces per-platform archives:

```text
arxgo-<version>-linux-amd64.tar.gz    arxgo, ffmpeg, ffprobe, LICENSES/, SHA256SUMS
arxgo-<version>-windows-amd64.zip     arxgo.exe, ffmpeg.exe, ffprobe.exe, LICENSES\, SHA256SUMS
```

- ffmpeg builds are downloaded from pinned sources with pinned SHA-256 values in
  `packaging/ffmpeg.lock` (`make ffmpeg` fetches and verifies them into `bin/`); binaries are
  never committed.
- Approved builds ([decision record](../../impl/records/0002-preview-approve-ffmpeg-distribution.md)):
  FFmpeg 6.1.1, GPL v3 variants, statically linked.

  | Platform | Build |
  | --- | --- |
  | `linux/amd64` | `mwader/static-ffmpeg:6.1.1` image layer, fetched from Docker Hub by digest |
  | `windows/amd64` | gyan.dev `ffmpeg-6.1.1-essentials_build.zip` from `GyanD/codexffmpeg` |

- Every bundle includes the GPL v3 licence text, the build's provenance (source commit or recipe)
  and a written offer or link to the corresponding FFmpeg 6.1.1 source.
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
- Restore `--previews delete` removes recorded previews only.
