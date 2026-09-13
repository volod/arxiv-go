# Media metadata

Owner: `media-metadata`. Consumers: registry `metadata` column, video registry, Markdown stubs,
stage-2 preview planning.

## Operator problem

A document-only archive must still describe the videos that left it: duration, resolution, codecs
and whether sound is present. That information must be readable without the video archive being
online, and collecting it must not require installing anything for the common MP4/MOV case.

## Metadata modes

| `--metadata` | Sources | External tool |
| --- | --- | --- |
| `file` (default) | `os.Lstat`: size, mtime, permission bits | none |
| `media` | `file` fields + container/stream fields below | `ffprobe` for non-ISO-BMFF media only |

## Normalized media fields

Both parsers produce the same Go struct and JSON object (`metadata.media`, see
[contracts](contracts.md#metadata-json)):

| Field | Type | Notes |
| --- | --- | --- |
| `container` | string | `mp4`, `mov`, `m4a`, `3gp`, `matroska`, `webm`, `avi`, ... |
| `duration_s` | float | Seconds, 3 decimals |
| `bit_rate` | int | bits/s, container level when known |
| `width`, `height` | int | First video stream, display dimensions after rotation |
| `rotation` | int | Degrees from the display matrix or side data |
| `frame_rate` | string | Rational, e.g. `30000/1001` |
| `video_codec` | string | e.g. `h264`, `hevc`, `av1`, `mpeg4` |
| `audio_codec` | string | Empty when no audio |
| `has_audio` | bool | |
| `video_streams`, `audio_streams`, `subtitle_streams` | int | |
| `creation_time` | string | RFC 3339 when present |
| `tags` | object | Container tags (`title`, `comment`, `encoder`, ...), values truncated to 256 bytes |
| `source` | string | `go-mp4` or `ffprobe` |
| `error` | string | Present instead of stream fields when parsing failed |

## ISO BMFF parser

- Applies to files whose detected MIME is `video/mp4`, `video/quicktime`, `video/3gpp`,
  `video/3gpp2`, `video/x-m4v` or `audio/mp4`.
- Uses `github.com/abema/go-mp4` `ReadBoxStructure`/`Probe` over an `io.ReadSeeker`; it reads only
  box headers and `moov` payloads, never media data, so cost is independent of file size.
- Handles `moov` at the end of the file, fragmented MP4 (`moof`, duration from `mehd` or summed
  fragments), and `udta`/`meta` `ilst` tags.
- Codec names map from sample entry four-character codes (`avc1`/`avc3` -> `h264`, `hvc1`/`hev1`
  -> `hevc`, `av01` -> `av1`, `mp4a` -> `aac`, ...); unknown codes are reported verbatim.
- A parse error produces `error` and does not fail the scan. When `ffprobe` is available the file
  falls back to ffprobe before reporting the error.

## ffprobe parser

- Applies to media files the ISO BMFF parser does not handle, when `--metadata media` is set.
- Command (argument vector, no shell):
  `ffprobe -v error -hide_banner -print_format json -show_format -show_streams -- <path>`.
- Runs with a per-file timeout (default 60 s) through `exec.CommandContext`; stdout is limited to
  16 MiB; stderr is captured to the log at `debug` level.
- The JSON is decoded into typed structs and normalized into the fields above; unknown fields are
  ignored.

## Tool discovery

Shared with stage 2 (`ffmpeg`).

1. Candidate directories, in order: the directory of `os.Executable()` (symlinks resolved), then
   each `PATH` entry via `exec.LookPath`.
2. Executable name: `ffprobe`/`ffmpeg` on Linux, `ffprobe.exe`/`ffmpeg.exe` on Windows.
3. A candidate is accepted when `<tool> -version` exits 0 within 10 s; the first line is logged
   (`ffprobe version 7.1 ...`).
4. Requirements are computed from the validated options:
   - `--metadata media` -> `ffprobe` (checked at startup even if the archive turns out to contain
     only MP4 files, so a run never fails halfway);
   - stage 2 `--sample` or `--image` other than `none` -> `ffmpeg` and `ffprobe`.
5. When a required tool is missing, `arxgo` logs one `error` line per tool listing the options that
   are unavailable, prints the download guidance below, and exits 3 before taking the run lock.

Download guidance by platform:

| `GOOS/GOARCH` | Link printed |
| --- | --- |
| `linux/amd64` | `https://johnvansickle.com/ffmpeg/` (static builds) and `https://ffmpeg.org/download.html#build-linux` |
| `windows/amd64` | `https://www.gyan.dev/ffmpeg/builds/` and `https://github.com/BtbN/FFmpeg-Builds/releases` |
| other | `https://ffmpeg.org/download.html` |

The message also states the expected location: "place ffprobe(.exe) next to arxgo(.exe) or add it
to PATH". The tool search path is injectable for tests.

## Acceptance

- Test helpers build minimal ISO BMFF files in memory (ftyp + moov with one video and one audio
  track, audio-only, fragmented, `moov` at end, corrupt box size) and the parser returns the
  expected fields or a non-fatal `error`.
- ffprobe normalization is tested against committed JSON documents captured from ffprobe output
  (text fixtures, no media). A live ffprobe test runs only locally when the tool is found and is
  skipped with a logged reason otherwise, including on GitHub CI.
- Discovery tests cover: tool next to executable wins over PATH; tool only on PATH; missing tool
  exits 3 with the correct platform link; `-version` failure treated as missing.
