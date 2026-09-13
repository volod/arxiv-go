# Media Metadata

Accepted work: [0013 Tool discovery](../records/0013-metadata-implement-tool-discovery.md),
[0014 Shell-free discovery tests](../records/0014-metadata-remove-shell-scripts-from-discovery-tests.md),
[0015 ISO BMFF metadata](../records/0015-metadata-implement-iso-bmff-metadata.md),
[0016 ffprobe metadata](../records/0016-metadata-implement-ffprobe-metadata.md);
QuickTime handler fix in [0023](../records/0023-restore-repair-split-restore-round-trip-defects.md);
audio-only refinement narrowed in [0025](../records/0025-restore-prove-stage-1-on-generated-archive.md).
Specification: [media metadata](../../openspec/stage-1-core/metadata.md). The capability is
shipped. `--metadata media` requires a working `ffprobe` at startup, even for a scan containing
only ISO BMFF files; successful ISO BMFF parsing itself does not invoke it.

## ISO BMFF metadata (`internal/media`)

`scan --metadata media` reads detected MP4, MOV, M4A, M4V and 3GP files with `go-mp4` and adds
`metadata.media` to their registry rows. The normalized object contains container, duration,
estimated bit rate, first video stream's display dimensions and rotation, frame rate, codecs,
stream counts, audio presence, creation time and selected text tags. Unknown sample-entry codecs
remain as four-character codes. A track's kind comes from the `hdlr` directly inside `mdia`; the
data handler (`dhlr`) that QuickTime movies also carry inside `minf` is ignored. Before that fix a
QuickTime `.mov` parsed with no streams and `--metadata media` left it in the archive as not video. The box walk skips `mdat`, limits metadata reads to 32 MiB and
individual decoded boxes to 1 MiB, and visits a trailing `moov` by seeking. It reads fragment
duration from `mehd` or summed fragments, including `trex` defaults and fragments before `moov`.

A successful parse with an audio track and no video track clears `is_video` before statistics and
the candidate list are written, including for audio-only `.mp4`. A parse with neither keeps the
MIME and extension decision ([0025](../records/0025-restore-prove-stage-1-on-generated-archive.md)). A malformed or truncated file falls back to ffprobe.
If both parsers fail, its row gets a `metadata.media.error`; scanning continues and retains the
original MIME-based video flag.

## ffprobe metadata (`internal/media`)

For other detected audio/video files, and ISO BMFF parse failures, `scan --metadata media` runs the
validated ffprobe executable with `-show_format -show_streams` JSON output. Each invocation has a
60-second timeout, a 16 MiB stdout cap, a 1-second process wait delay and debug-level stderr
capture. A failed command, timeout, malformed JSON or oversized output becomes a non-fatal media
error; the path is excluded from registry error text.

Typed JSON decoding ignores unknown fields. Normalization fills container, duration (from format
or stream), bit rate, first video codec and display dimensions, frame rate, rotation from side data
or tags, audio codec and presence, stream counts, creation time and container tags. Tag values are
limited to 256 bytes. An attached cover picture does not count as a video stream. A successful
audio-only parse clears `is_video` before statistics and candidates are written. A result with no
audio or video stream keeps the flag: ffprobe 6.1.1 reads some random bytes named `.avi` as LRC
lyrics with one subtitle stream, and before the stage-1 proof such a file silently stayed in the
archive during `split --metadata media`.

## Tool discovery (`internal/media`)

`arxgo scan --metadata media` and `arxgo split --metadata media` look for `ffprobe` before the run
starts. Without it they exit 3 and write nothing:

```text
$ arxgo scan --archive /data/archive --metadata media
time=... level=ERROR msg="required tool not found" tool=ffprobe unavailable="--metadata media"
Download ffmpeg for linux/amd64 (it includes ffprobe):
  https://johnvansickle.com/ffmpeg/
  https://ffmpeg.org/download.html#build-linux
Then place ffprobe next to arxgo or add it to PATH.
```

- `media.Requirements(Needs)` maps options to tools: `--metadata media` -> ffprobe; stage-2
  `Sample`/`Image` modes -> ffprobe and ffmpeg (the CLI passes empty modes until stage 2 enables
  those flags). Each requirement lists the options that need the tool, for the error line.
- `media.Finder` tries `<dir of os.Executable, symlinks resolved>/ffprobe`, then each absolute
  `PATH` directory (relative and empty entries ignored, duplicates tried once), with `.exe` names on
  Windows. `exec.LookPath` rejects absent, non-executable and directory candidates. A candidate is
  accepted when `-version` exits 0 within 10 s with a non-empty first line; otherwise it is logged
  at `warn` and the next one is tried. The accepted path and version line are logged at `info`.
- `-version` runs through `exec.CommandContext` with a 64 KiB stdout buffer and a 1 s
  `WaitDelay`, so a tool whose child keeps stdout open cannot stall startup after the timeout.
- `Finder.Discover` returns a `Toolset` (path and version per tool) and the missing requirements;
  a cancelled context returns its error. Executable, search path, GOOS and timeout are fields for
  tests. Private candidate-check and version-probe seams let unit tests exercise the same search
  logic without creating fake tool files.
- `media.Guidance(missing, goos, goarch)` and `media.DownloadLinks`: `linux/amd64` and
  `windows/amd64` link tables, `https://ffmpeg.org/download.html` for any other platform.

## CLI integration (`internal/cli`)

- `requireTools` runs after option validation and logger setup and before the handler, so usage
  errors still exit 2 and discovery precedes the run lock, `.arxgo/`, video archive root creation
  and registry writes, also with `--dry-run`. Missing tool: exit 3; interrupt: exit 130.
- Found tools are set on `Common.Tools` (`json:"-"`): they reach the operation but are not stored
  in `options.json` or compared on resume. The metadata parsers will read the ffprobe path from
  there.
- `restore` and `--metadata file` compute no requirement and never run discovery.

## Verification

`go test ./internal/media ./internal/cli` covers candidate order, symlink resolution, fallback,
requirements, exit 3 guidance and interrupt with in-memory candidate and probe results keyed by
path. The real `-version` probe is tested by running the test binary as a helper process: successful
output, nonzero exit, empty or blank first line, 64 KiB output cap, timeout and a child holding
stdout open. The real candidate check rejects a directory on every platform and a non-executable
file on Linux. No test writes a fake ffprobe/ffmpeg executable or runs a shell script.

`TestFindRealFFprobe` also validates an installed `ffprobe` when one is on `PATH` and skips with a
reason otherwise. Linux tests and `make ci` pass; Windows behavior is cross-compiled and vetted,
with runtime step W6 still deferred in the
[Windows verification scenario](../../guide/windows-verification.md).

Generated ISO BMFF fixtures cover video plus audio, audio-only `.mp4` and M4A, rotated MOV, late
`moov`, fragmented duration, tags, corrupt boxes and a sparse 1 GiB `mdat` for the read bound. A
live MP4 fixture uses ffmpeg when installed and skips with a reason otherwise. Scan integration
checks the JSON, non-fatal errors, audio-only classification and candidate list.

Captured ffprobe JSON fixtures cover Matroska, WebM, AVI, MPEG-TS and audio-only Ogg; derived
fixtures cover rotated side data and missing duration. The test binary acts as ffprobe to check
timeout, output limit, command failure, ISO fallback and scan integration without a fake tool
installation. A live lavfi AVI probe runs when ffmpeg and ffprobe are installed and skips otherwise.
The Linux CLI scan of generated clips and `make ci` pass. Windows is cross-compiled and vetted;
runtime behavior remains in the [Windows verification scenario](../../guide/windows-verification.md).
