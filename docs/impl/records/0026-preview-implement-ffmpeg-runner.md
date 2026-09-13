# FFmpeg runner

## Task and scope

- Id / capability / checkpoint: `implement-ffmpeg-runner` / `media-previews` / `review-stage-2-previews`.
- State: accepted.
- Source: plan task; HEAD `9749081`, worktree clean at start.
- Plan counts at start: 16 open tasks (13 agent, 3 human); next eligible `implement-ffmpeg-runner`.
- Accepted task:

````markdown
#### implement-ffmpeg-runner

Run ffmpeg safely with progress, timeouts and error capture.

- Serves: `media-previews` -- [ffmpeg invocation](../openspec/stage-2-previews/previews.md#ffmpeg-invocation)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Preview generation reports progress and fails cleanly with a readable reason
  instead of hanging or leaving partial files.
- Scope boundary: `media.Runner` for ffmpeg/ffprobe, `-progress pipe:1` parsing, stderr ring buffer,
  timeout, cancellation, part-file naming and rename, encoder list probe. No preview planning.
- Data and artifact paths: `internal/media/ffmpeg.go`.
- Execution path: Test-binary helper process (the pattern of [shell-free discovery tests](records/0014-metadata-remove-shell-scripts-from-discovery-tests.md),
  no shell scripts) for progress, exit-code and timeout tests; live test on a `lavfi` input.
- Acceptance gates: Progress parsed; timeout kills the process tree; non-zero exit removes the part
  file and returns the stderr tail; encoder list parsed from captured output.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.
````

- Amendments: none.

## Implementation

`internal/media/ffmpeg.go` adds `Runner`, `PreviewCommand` and `PreviewPartPath`. The runner
uses validated `Toolset` paths, forwards ffprobe metadata reads to the existing bounded
`FFprobeReader`, and runs ffmpeg with argument vectors through `exec.CommandContext`.
`-progress pipe:1 -nostats` reports parsed `out_time_us` and `speed`; the default per-preview
timeout is `max(60s, 4 x total output duration)`. Stderr retains the last 64 KiB. A successful
encoder list probe is cached per runner; cancellation or failure can be retried.

The output part name retains the extension. The runner reserves it exclusively, refuses an
existing final output, removes its own part on failure, verifies a nonempty regular output,
flushes it, and uses `fsops.Rename` for no-replace durable publication. The later split task
owns WAL records around this call; no archive invocation exists yet. The pre-existing
`FFprobeReader` remains the bounded ffprobe implementation.

Linux commands run in a new process group; cancellation kills the group. If a descendant holds
an output pipe after the parent exits, the one-second `WaitDelay` ends the wait and cleanup
kills that group. A self-review narrowed post-Wait group killing to this case: a normal
reaped group ID could be reused before an unconditional kill. Windows commands are assigned
to a Job Object with kill-on-close. Updated [architecture](../../openspec/architecture.md),
[preview specification](../../openspec/stage-2-previews/previews.md),
[current state](../current/media-previews.md), the current-state index and the deferred Windows
scenario. No dependencies were added. CUDA/NVENC is outside this task; the local live test uses
ffmpeg's software `mpeg4` encoder and requires no GPU driver.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Progress parsed | `TestRunnerProgressAndPublish`, `TestProgressCollectorIgnoresInvalidValues` | pass: fragmented lines, out-time, speed, final marker and invalid values |
| Timeout kills process tree | `TestRunnerTimeoutKillsProcessTree`, `TestRunnerClosesOrphanChild`, `TestRunnerCancellation` | pass on Linux: child heartbeat stops after timeout and orphaned-pipe wait, no output or part; Windows runtime deferred to W7a |
| Failed output cleaned and stderr tail returned | `TestRunnerFailureAndConflicts` | pass: exit 17 leaves no file or part and includes the tail marker; empty output and existing-file conflicts handled |
| Encoder list parsed | `TestRunnerEncoders`, `TestRunnerEncoderProbeRetryAfterTimeout` | pass: captured list parsed, cached result copied, timeout retried |
| Live lavfi run | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -count=1 -v -run 'TestRunner|TestPreviewPartPathAndTimeout' ./internal/media` | pass: local ffmpeg 6.1.1 generated nonempty AVI, final progress and `mpeg4` encoder |
| Race-enabled runner tests | `GOCACHE=/tmp/arxgo-go-cache go test -race -count=1 -run 'TestRunner|TestProgressCollector' ./internal/media` | pass; first run exposed helper startup timing, fixed test deadline and reran |
| Linux package and Windows compile | `GOCACHE=/tmp/arxgo-go-cache make fmt-check vet vet-windows test build-all` | pass; Windows cross-vetted and cross-built only |
| Required CI and docs gates | `GOCACHE=/tmp/arxgo-go-cache make ci` (includes spec-plan and doc-link lints) | pass on Linux after plan transition |

## Audit handoff

`AUD-implement-ffmpeg-runner-1`: Windows Job Object process-tree cancellation and no-replace
publication are cross-compiled/vetted but not run on a Windows host. Nonblocking runtime check
is owned by [Windows verification W7a](../../guide/windows-verification.md#scenario). The
Linux helper child, local ffmpeg run, file conflicts and publication paths were reviewed.

## Close or resume

All acceptance gates passed. The [current-state page](../current/media-previews.md) and record
index are updated; references in the forward plan now point to this record and the completed
task block is removed. Plan counts after: 15 open (12 agent, 3 human); next eligible
`implement-preview-planning`. `media-previews` remains planned until its remaining tasks are
accepted. No temporary repository files remain.
