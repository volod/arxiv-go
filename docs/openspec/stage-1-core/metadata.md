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
  `video/3gpp2`, `video/x-m4v`, `audio/mp4` or `audio/x-m4a`.
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
   each `PATH` entry. Empty and relative `PATH` entries are ignored, so the current directory never
   supplies a tool; a directory listed twice is tried once. Each candidate
   `<dir>/<executable name>` must be an executable regular file (`exec.LookPath`).
2. Executable name: `ffprobe`/`ffmpeg` on Linux, `ffprobe.exe`/`ffmpeg.exe` on Windows.
3. A candidate is accepted when `<tool> -version` exits 0 within 10 s and prints a non-empty first
   line; the path and first line are logged at `info` (`ffprobe version 7.1 ...`). A candidate
   that exits non-zero, times out (the process is killed) or prints nothing is logged at `warn` and
   the next candidate is tried; when none is accepted the tool is missing. Output beyond 64 KiB is
   discarded.
4. Requirements are computed from the validated options:
   - `--metadata media` -> `ffprobe` (checked at startup even if the archive turns out to contain
     only MP4 files, so a run never fails halfway);
   - stage 2 `--sample` or `--image` other than `none` -> `ffmpeg` and `ffprobe` (enforced once
     those flags are available);
   - `--metadata file` and `restore` need no tool, and discovery does not run.
5. Discovery runs after option validation (so usage errors still exit 2) and before the run lock,
   the video archive root creation or any other write, also for `--dry-run`. When a required tool
   is missing, `arxgo` logs one `error` line per tool (`required tool not found tool=ffprobe
   unavailable="--metadata media"`), prints the download guidance below to stderr, and exits 3.
   An interrupt during discovery exits 130. The validated tool paths are passed to the operation
   and are not part of `options.json`, so a resumed run may find the tool elsewhere.

Download guidance by platform:

| `GOOS/GOARCH` | Link printed |
| --- | --- |
| `linux/amd64` | `https://johnvansickle.com/ffmpeg/` (static builds) and `https://ffmpeg.org/download.html#build-linux` |
| `windows/amd64` | `https://www.gyan.dev/ffmpeg/builds/` and `https://github.com/BtbN/FFmpeg-Builds/releases` |
| other | `https://ffmpeg.org/download.html` |

The guidance names the platform, lists its links and states the expected location:

```text
Download ffmpeg for linux/amd64 (it includes ffprobe):
  https://johnvansickle.com/ffmpeg/
  https://ffmpeg.org/download.html#build-linux
Then place ffprobe next to arxgo or add it to PATH.
```

On Windows the names are `ffprobe.exe` and `arxgo.exe`. The executable location, search path,
platform and timeout are injectable for tests.

## Acceptance

- Test helpers build minimal ISO BMFF files in memory (ftyp + moov with one video and one audio
  track, audio-only, fragmented, `moov` at end, corrupt box size) and the parser returns the
  expected fields or a non-fatal `error`.
- ffprobe normalization is tested against committed JSON documents captured from ffprobe output
  (text fixtures, no media). A live ffprobe test runs only locally when the tool is found and is
  skipped with a logged reason otherwise, including on GitHub CI.
- Discovery tests cover: tool next to executable wins over PATH; tool only on PATH; missing tool
  exits 3 with the correct platform link before any write; `-version` failure or timeout treated
  as missing; `--metadata file` runs no discovery.
- Fake tools used by discovery tests are real executables, not shell scripts: the test binary is
  copied into the test directory under the tool's platform name (`ffprobe`, `ffprobe.exe`) and,
  when started, acts out a behavior read from a sidecar file next to the copy (print a version
  line, exit with a code, print nothing, sleep past the timeout). The same tests therefore run
  unchanged on Linux and Windows, need no shell or compiler, and the helper is reusable by the
  stage-2 ffmpeg runner tests.
