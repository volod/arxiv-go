# Remove Shell Scripts from Discovery Tests

## Task and scope

- Id / capability / checkpoint: `remove-shell-scripts-from-discovery-tests` / `media-metadata` / `review-stage-1-integrity`
- State: accepted (Linux gates pass; Windows cross-compiled and vetted only)
- Source: plan task; clean tree at start.
- Plan counts at start: 24 open (21 agent, 3 human); next agent task `remove-shell-scripts-from-discovery-tests`.
- Accepted task:

```markdown
#### remove-shell-scripts-from-discovery-tests

Discovery tests write POSIX shell scripts as fake `ffprobe`/`ffmpeg` and skip on Windows. A
portable replacement that plants fake tools named per platform (`ffprobe.exe`) would still be
confusing, so discovery must be testable in pure Go with no fake tool on disk.

- Serves: `media-metadata` -- [Acceptance](../openspec/stage-1-core/metadata.md#acceptance)
- Agent status: CLEAR
- Dependencies: [Tool discovery](records/0013-metadata-implement-tool-discovery.md).
- User-visible outcome: Tool discovery (next-to-executable precedence, fallback after a failing or
  hanging `-version`, exit 3 guidance) is tested by the same Go code on every platform without a
  shell, so step W6 on a Windows host is a plain `go test` run.
- Scope boundary: Unexported seams on `media.Finder` for the candidate check and the `-version`
  probe, with defaults that keep today's `exec.LookPath` and `exec.CommandContext` behavior.
  Migrate the discovery-logic tests in `internal/media` and `internal/cli` to in-memory fakes
  keyed by candidate path. Test the real probe against the test binary as a helper process that
  `TestMain` dispatches from an environment variable. Test the real candidate check on a generated
  directory and non-executable file. Excluded: any change to discovery behavior, log lines or exit
  codes; copying or renaming binaries as fake tools; sidecar behavior files; `go build` in tests;
  converting stage-2 runner tests (see `implement-ffmpeg-runner`).
- Data and artifact paths: `internal/media/tools.go`, `internal/media/tools_test.go`,
  `internal/media/probe_test.go` (helper process and `TestMain`), `internal/cli/tools_test.go`,
  `docs/guide/windows-verification.md`.
- Execution path: `go test ./internal/media ./internal/cli`; the `cli` tests reach the seams through
  an unexported test hook or by injecting a probe through `media.Finder` fields. The real-probe
  tests start `os.Executable()` with `-version`, using `t.Setenv` to pass the behavior (version
  line, exit code, no output, blank first line, output over 64 KiB, sleep past the timeout, child
  holding stdout). `TestFindRealFFprobe` stays as the optional live check.
- Acceptance gates: On Linux: every gate in
  [the tool discovery record](records/0013-metadata-implement-tool-discovery.md#acceptance-evidence)
  still passes with the migrated tests. `grep -rn '#!/bin/sh\|\.bat' internal/media internal/cli`
  finds nothing, and no test writes an executable named after a tool. The timeout case with a
  stdout-holding child finishes within its bound. The six scratch mutations listed in that record,
  plus dropping the output cap, each fail a named test. `make vet-windows` passes. No test in these
  packages skips on Windows except the executable-bit case.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

`internal/media.Finder` now has private candidate-check and version-probe seams. Their nil defaults
still use `exec.LookPath` and the existing `exec.CommandContext` probe. Discovery tests supply
in-memory results keyed by candidate path, covering precedence, PATH-only discovery, symlink
resolution, rejection and fallback after nonzero or timed-out probes, missing requirements,
relative PATH entries and context cancellation. The real candidate check rejects a generated
directory on every platform and a non-executable file on Linux. The symlink test uses a real link
where creation is available; on hosts that disallow symlinks, it logs that limit and still checks
the executable-directory path.

`internal/media/probe_test.go` uses `TestMain` to dispatch the test binary when invoked with
`-version`. It checks first-line handling, nonzero exit, silence, blank first line, 64 KiB cap,
timeout and a child holding stdout open. No build, copied binary or fake tool is installed.
`internal/cli` uses a private discovery hook for in-memory results keyed by candidate path; the
scan/split, session and preflight tests no longer write POSIX scripts or skip on Windows. No
runtime behavior, log message or exit code was changed. No dependency was added.

Current state: [media metadata](../current/media-metadata.md#verification). Windows runtime
step W6 remains deferred in the [Windows verification scenario](../../guide/windows-verification.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Existing discovery acceptance: executable-directory precedence, PATH-only, symlink resolution, fallback | `TestFindPrefersExecutableDirectoryOverPath`, `TestFindOnPathOnly`, `TestFindResolvesSymlinkedExecutable`, `TestFindFallsBackToPathWhenExecutableDirectoryToolFails` | pass, Linux; no fake tools on disk |
| Missing tool exits 3 with links before writes; file metadata needs none; interrupt | `TestMissingToolExitsThreeBeforeLockOrWrite`, `TestFileMetadataNeedsNoTool`, `TestToolDiscoveryInterrupted`, `TestFindStopsWhenContextEnds`, `TestGuidanceByPlatform` | pass, Linux; both platform link tables tested |
| Real candidate check and real `-version` probe | `TestFindIgnoresRelativeAndDirectoryCandidates`, `TestFindRejectsNonExecutableFile`, `TestRealVersionProbe`, `TestRealVersionProbeTimeout` | pass, Linux; test-binary helper, no tool executable written; stdout-holding child completes under 3 s |
| Live installed ffprobe | `TestFindRealFFprobe` in `go test -count=1 ./internal/media ./internal/cli` | pass on this Linux host; optional on hosts without ffprobe |
| No shell scripts or fake executable tools | `rg -n '#!/bin/sh|\.bat' internal/media internal/cli` returned no matches; review of all `os.WriteFile`/`exec.Command` calls in discovery tests | pass; only generated non-executable file, symlink target and test binary helper |
| Six prior scratch mutations plus output cap | `/tmp/arxgo_mutations.py` applied one mutation at a time, ran named `go test -count=1 -run` cases and restored each source; all seven reported `KILLED` | pass, Linux: PATH order, exit status, relative PATH, WaitDelay, split discovery, symlink resolution, output cap |
| Focused and race tests | `GOCACHE=/tmp/arxgo-go-cache go test -count=1 ./internal/media ./internal/cli`; `CGO_ENABLED=1 GOCACHE=/tmp/arxgo-go-cache go test -race -count=1 ./internal/media ./internal/cli` | pass, Linux |
| Windows static build and vet | `make build-all`, `make vet-windows` through `make ci` | pass, cross-compiled/vetted only; W6 runtime deferred |
| Required CI and doc gates | `GOCACHE=/tmp/arxgo-go-cache make ci`; `make lint-spec-plan`; `make lint-doc-links` | pass, Linux; includes `make build-all` and `make vet-windows` |

Mutation-to-test mapping: PATH before executable directory ->
`TestFindPrefersExecutableDirectoryOverPath`; ignored `-version` exit status ->
`TestRealVersionProbe/nonzero`; relative PATH accepted ->
`TestFindIgnoresRelativeAndDirectoryCandidates`; removed `WaitDelay` ->
`TestRealVersionProbeTimeout/child`; split skipping discovery ->
`TestMissingToolExitsThreeBeforeLockOrWrite/*/split`; symlink resolution removed ->
`TestFindResolvesSymlinkedExecutable`; increased output cap ->
`TestRealVersionProbe/output_cap`. Each mutation was restored before the next test.

Among migrated discovery tests, only the Unix executable-bit case is Windows-specific.
`TestFindRealFFprobe` remains the explicitly optional live check and skips where ffprobe is absent;
pre-existing unrelated CLI signal and root tests retain their platform-specific skips.

The first `go test` attempt failed because this sandbox's default Go build cache is read-only;
setting `GOCACHE` to `/tmp/arxgo-go-cache` resolved it. The first `make ci` passed vet, Windows vet,
all unit tests and both builds, then stopped at `lint-spec-plan` because this active record still
had its task in the plan. The final rerun passed after the completed transition.

## Audit handoff

No new audit notes from the reviewed discovery and CLI changes. The real Windows runtime check
remains deferred as `AUD-implement-tool-discovery-1` in W6; cross-compilation is not runtime
evidence. Process-tree behavior remains owned by `implement-ffmpeg-runner` per
`AUD-implement-tool-discovery-2`.

## Close or resume

Final CI and documentation gates passed. This record and the current-state page are linked,
the task is removed from the plan, and the stage-2 helper-process reference points here. The
`media-metadata` capability remains planned because parser tasks remain. Plan counts after: 23 open
(20 agent, 3 human); next eligible agent task `implement-iso-bmff-metadata`, also eligible
`implement-video-split-transactions`.
