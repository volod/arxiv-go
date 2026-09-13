# Implement Tool Discovery

## Task and scope

- Id / capability / checkpoint: `implement-tool-discovery` / `media-metadata` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; Windows is cross-compiled and vetted only, see audit handoff)
- Source: plan task `implement-tool-discovery`, operator request on 2026-09-13 ("implement, run,
  fix, improve implementation and then update documentation and plan.md"; the host has CUDA and
  ffmpeg/ffprobe 6.1.1 in `/usr/bin`). Branch `ag-01-stage-1` at `575c2e9`, clean tree.
- Plan counts at start: 24 open (21 agent, 3 human); next agent task `implement-tool-discovery`;
  also eligible `implement-iso-bmff-metadata`, `implement-video-split-transactions`.
- Accepted task:

```markdown
#### implement-tool-discovery

Find `ffprobe`/`ffmpeg` next to the executable or on `PATH`, and fail fast with download guidance.

- Serves: `media-metadata` -- [Tool discovery](../openspec/stage-1-core/metadata.md#tool-discovery)
- Agent status: CLEAR
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md).
- User-visible outcome: Requesting `--metadata media` without ffprobe logs the unavailable options
  and the platform download link and exits 3 before any lock or write.
- Scope boundary: Lookup order, `-version` validation with timeout, requirement computation from
  options, platform link table, injectable search path. Stage-2 requirements are declared but only
  enforced when those flags become available.
- Data and artifact paths: `internal/media/tools.go`, `internal/cli/`.
- Execution path: `os.Executable` + `filepath.EvalSymlinks`, then `exec.LookPath`; fake `.sh` tool
  scripts generated in tests. Windows `.exe` names are implemented; the `.bat` fake-tool check is
  step W6 of the Windows scenario.
- Acceptance gates: On Linux: next-to-executable beats PATH; PATH-only found; missing -> exit 3
  with the `GOOS/GOARCH` link (table-tested for `linux/amd64` and `windows/amd64`); non-zero
  `-version` treated as missing; `--metadata file` needs no tool.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Specification changes made while implementing it (all
  clarifications of existing behavior; no format change):
  - [tool discovery](../../openspec/stage-1-core/metadata.md#tool-discovery): empty and relative
    `PATH` entries ignored and duplicates tried once; candidate must be an executable file; a
    failing, timed-out or silent candidate is logged and the next candidate tried; 64 KiB output
    cap; `--metadata file` and `restore` run no discovery; discovery after validation and before
    the lock, root creation or any write, also for `--dry-run`; the error line format; interrupt
    exits 130; tool paths not stored in `options.json`; the guidance text.
  - [architecture](../../openspec/architecture.md#dependency-direction): new `cli --> media` edge
    (discovery only, before the session); `media` file list; phase 1 reads required tools.

## Implementation

Packages `internal/media` (new code) and `internal/cli`; no new dependencies.

- `media/tools.go`: `Tool` (`FFprobe`, `FFmpeg`), `ExecutableName(tool, goos)`, `Needs` and
  `Requirements` (options -> tools, each with the option spellings that need it; stage-2 modes
  declared), `Finder` (injectable `Executable`, `SearchPath`, `GOOS`, `Timeout`, `Log`) with
  `Candidates`, `Find` and `Discover`, `Found`/`Toolset`, `ErrToolNotFound`. `-version` runs via
  `exec.CommandContext` with a bounded stdout buffer and `WaitDelay`.
- `media/guidance.go`: `DownloadLinks(goos, goarch)` table and `Guidance(missing, goos, goarch)`.
- `cli/tools.go`: `scanNeeds`, `requireTools` (error line per missing tool, guidance on stderr,
  exit 3; exit 130 when interrupted), `env.platform`. `cli/root.go` calls it for `scan` and
  `split` between logger setup and the handler; `env` gains `finder` and a test-only `goarch`.
  `Common.Tools` (`json:"-"`) carries the found tools to the operation.
- Tests: `media/tools_test.go`, `media/guidance_test.go`, `cli/tools_test.go`. The CLI test
  environment now has no tools by default; the two existing tests that pass `--metadata media`
  install a fake `ffprobe`.

Run, fix and improve:

- Ran the built binary with a scrubbed environment (`env -i`) on a scratch archive: PATH without
  ffprobe -> exit 3, the error line and the `linux/amd64` links, archive untouched (no `.arxgo/`,
  no registry); `--metadata file` with the same PATH -> exit 0; PATH `/usr/bin:/bin` -> `tool
  found path=/usr/bin/ffprobe version="ffprobe version 6.1.1-3ubuntu5+esm13 ..."`; ffprobe
  symlinked next to `arxgo` with a failing fake first on PATH -> next-to-executable chosen; a
  failing fake next to `arxgo` -> `tool candidate rejected ... exit status 1`, then `/usr/bin/ffprobe`;
  `arxgo` started through a symlink in another directory -> the tool next to the link target.
  Discovery adds about 30-90 ms to startup with the real ffprobe.
- Fixed during implementation: the first draft read `-version` output from `StdoutPipe` before
  `Wait`; a fake tool whose background child kept stdout open blocked the read past the timeout
  because `WaitDelay` only applies inside `Wait`. Output now goes to a bounded buffer through
  `cmd.Stdout`, so `Run` enforces `WaitDelay`; `TestFindVersionTimeoutKillsTool` covers it.
- Improved: a candidate that fails validation no longer ends the search (the spec left this open);
  a broken tool next to `arxgo` falls back to a working one on PATH, with a warning naming it.
- Improved: relative and empty PATH entries are ignored, so a tool in the current directory is
  never picked up (the same concern as Go's `exec.ErrDot`).
- Scratch mutations (each restored), all failing named tests: PATH before the executable
  directory; `-version` exit status ignored; relative PATH entries allowed; no `WaitDelay`
  (timeout test exceeds its 3 s bound); `split` skipping discovery; executable symlink not
  resolved.

Decisions and rejected alternatives:

- Discovery lives in `run` rather than in the handlers or the session, so it happens before the
  run lock and root creation for every tool-backed operation and handler tests stay unaffected.
- Tool paths are runtime facts, not options: `json:"-"` keeps them out of `options.json` and the
  resume comparison, so a resumed run may use a tool from a different location.
- Rejected: `exec.LookPath("ffprobe")` over the process PATH (not injectable and cannot express
  the next-to-executable precedence without a second search); killing the process group on
  timeout (needs build-tagged code outside `fsops`; real `-version` spawns no children; the
  stage-2 runner owns process-tree kills, audit note 2).

Current state: [media metadata](../current/media-metadata.md#tool-discovery-internalmedia).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Next-to-executable beats PATH | `TestFindPrefersExecutableDirectoryOverPath`, `TestFindResolvesSymlinkedExecutable`; binary run with ffprobe next to `arxgo` | pass, Linux, generated `.sh` fakes and real ffprobe 6.1.1 |
| PATH-only found | `TestFindOnPathOnly`, `TestFoundToolsReachTheHandler`, `TestFindRealFFprobe`; binary run with PATH `/usr/bin:/bin` | pass, Linux |
| Missing -> exit 3 with the `GOOS/GOARCH` link, table-tested for `linux/amd64` and `windows/amd64` | `TestMissingToolExitsThreeBeforeLockOrWrite` (scan and split, both platforms, asserts no `.arxgo/` and empty roots), `TestGuidanceByPlatform` (also `darwin/arm64`, `linux/arm64` fallback), `TestFindMissing`; binary run | pass, Linux; Windows names and links table-tested only |
| Non-zero `-version` treated as missing | `TestFindTreatsFailedVersionAsMissing` (non-zero exit, killed, no output, blank first line, exec failure), `TestFindVersionTimeoutKillsTool`, `TestFindFallsBackToPathWhenExecutableDirectoryToolFails` | pass, Linux |
| `--metadata file` needs no tool | `TestFileMetadataNeedsNoTool` (scan, split, restore; discovery must not run); binary run | pass, Linux |
| Interrupt during discovery | `TestToolDiscoveryInterrupted`, `TestFindStopsWhenContextEnds` | pass, Linux |
| Race | `CGO_ENABLED=1 go test -race -count=1 ./internal/media ./internal/cli` | pass, Linux |
| Windows build compiles | `make build-all`, `make vet-windows` (in `make ci`) | pass (cross-compiled only) |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

Coverage (diagnostic): `internal/media` 99.2 %.

## Audit handoff

- `AUD-implement-tool-discovery-1`: nonblocking, Windows only. `exec.LookPath` on
  `<dir>\ffprobe.exe`, `-version` through `CreateProcess`, and timeout kill with `WaitDelay` are
  cross-compiled only. Discovery tries only `ffprobe.exe`, so a `ffprobe.bat` fake as W6 describes
  is never a candidate; W6 needs fake `.exe` tools (for example a small Go helper built for the
  test) or a copied real `ffprobe.exe` next to a failing one. Next check: shell-free discovery tests
  were added by [0014 Shell-free discovery tests](0014-metadata-remove-shell-scripts-from-discovery-tests.md)
  (renamed from `add-portable-fake-tools`, which planned copied `.exe` fakes); the Windows run itself is step W6 of the deferred
  [Windows verification scenario](../../guide/windows-verification.md#deferred-items). Owner:
  `review-stage-1-integrity` (disposition: deferred).
- `AUD-implement-tool-discovery-2`: nonblocking. The `-version` timeout kills only the direct child;
  a grandchild that ignores the closed pipe survives until it exits. Real ffprobe/ffmpeg do not
  fork for `-version`. Next check: process-tree termination in the ffmpeg runner. Owner:
  `implement-ffmpeg-runner`.
- `AUD-implement-tool-discovery-3`: nonblocking. The ffprobe parser must take the path from
  `Common.Tools.Path(media.FFprobe)` (mapped through `scanConfig` into `archive.ScanConfig`) rather
  than searching again, and must not run ffprobe when `Metadata` is `file`. Owner:
  `implement-ffprobe-metadata`.

## Close or resume

All Linux gates pass. Windows host checks are not a gate. The task was removed from the plan;
`implement-ffprobe-metadata` links this record. `media-metadata` stays `planned` (ISO BMFF and
ffprobe parsing remain). New current page [media metadata](../current/media-metadata.md), linked
from the current index; archive-registry and project-foundation pages, README status, the
architecture, the metadata specification, the records index and the Windows deferred items were
updated.
Plan counts after: 23 open (20 agent, 3 human); next agent task `implement-iso-bmff-metadata`, also
eligible `implement-video-split-transactions`.
