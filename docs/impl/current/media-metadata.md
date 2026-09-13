# Media Metadata

Accepted work: [0013 Tool discovery](../records/0013-metadata-implement-tool-discovery.md),
[0014 Shell-free discovery tests](../records/0014-metadata-remove-shell-scripts-from-discovery-tests.md).
Specification: [media metadata](../../openspec/stage-1-core/metadata.md). The capability is
planned: ISO BMFF and ffprobe parsing remain in the
[plan](../plan.md#media-metadata----media-metadata), so `--metadata media` still writes file
metadata only, but it now requires a working `ffprobe`.

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
