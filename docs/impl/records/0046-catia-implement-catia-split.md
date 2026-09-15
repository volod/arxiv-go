# CATIA split

## Task and scope

- Id / capability / checkpoint: `implement-catia-split` / `catia-archive` / `review-stage-4-catia`
- State: accepted
- Source: plan task `implement-catia-split`; code revision `a9a1806`, clean tree at start.
- Plan counts at start: 14 open tasks (11 agent, 3 human); next eligible `implement-catia-split`.
- Accepted task:

```markdown
#### implement-catia-split

Move CATIA files with the same safety as video and leave Markdown the operator can read.

- Serves: `catia-archive` -- [Split](../openspec/stage-4-catia/split-restore.md#split)
- Agent status: CLEAR
- Dependencies: [CATIA classification](records/0043-catia-implement-catia-classification.md);
  [payload split and restore](records/0044-catia-generalize-payload-split-restore.md);
  [CATIA extraction](records/0045-catia-implement-catia-extraction.md).
- User-visible outcome: `arxgo split --catia --catia-archive PATH` moves CATIA files into the CATIA
  archive, writes CATIA descriptions with the `catia:` line and `arxgo-catia.csv`. Default split
  still moves only videos into `--video-archive`.
- Scope boundary: CLI `--video`, `--catia` and `--catia-archive` for split (and accepted-and-ignored
  on scan) with group environment resolution, payload-root matching, pairwise root nesting checks
  and the exit-2 combinations; CATIA payload in the executor with `catia_*` counters and preflight
  role; description written after `placed` and on recovery roll-forward; the `catia` object on
  `described`; `arxgo-catia.csv` columns 1-10 and 12-16; conflicts and idempotency. No
  `--catia-text`, restore or `--publish`. Update `.env.example` (`ARXGO_CATIA_ARCHIVE`) and the Linux
  and Windows manuals.
- Data and artifact paths: `internal/cli/`, `internal/archive/`, `internal/report/`, `.env.example`,
  `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md`.
- Execution path: Generated CATIA-like files in `t.TempDir()`; same-device and injected cross-device;
  crash after `placed`; an interrupted CATIA split recovered by a video split command.
- Acceptance gates: Byte-identical CATIA files in the CATIA archive; videos, the video archive and
  `arxgo-videos.csv` untouched; recovery by a video command writes a CATIA description;
  `--catia --video`, `--catia --video-archive X`, `--catia-archive X` without `--catia`, nested video
  and CATIA archives, `--catia --sample start` and `--catia --publish gdrive` exit 2 before the lock;
  `ARXGO_VIDEO_ARCHIVE` set in the environment does not affect a CATIA split; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none to the task. Spec amendment in scope of its acceptance gate (recovery by a video
  command): the [integrity](../../openspec/stage-1-core/integrity.md#state-layout) rule that a run
  on another video or CATIA archive exits 5 now applies to the same payload only; a split or restore
  replacing a run of the other payload takes that run's recorded mirror lock for the recovery. The
  [stage-4 page](../../openspec/stage-4-catia/split-restore.md#payload-selection) already required
  that roll-forward (audit note `AUD-generalize-payload-split-restore-2`). The
  [CLI contract](../../openspec/stage-1-core/cli.md) availability note and the stage-4 wording on
  environment roots (compared for nesting, never selected) were aligned with the validation table.

## Implementation

**CLI** (`internal/cli`). Flags `--video`, `--catia` (split) and `--catia-archive` (split, and
accepted-and-ignored by scan) in the flag table; `.env.example` lists `ARXGO_CATIA_ARCHIVE`,
`ARXGO_VIDEO` and `ARXGO_CATIA`. `parseFlags` ignores both payload variables when either payload
flag is on the command line. `selectPayload` rejects both payloads and a command-line mirror flag of
the other payload; `checkRoots` validates the selected root (created later by split) and checks the
archive, the selected root and the other root when set pairwise with `checkApart` (the other root
by path, resolving symlinks when it or its parent exists). Only the selected root is kept in
`Common` (`VideoArchive` or `CatiaArchive`), so it alone is a defining option. `--sample`/`--image`
other than `none` with `--catia` exit 2; `--publish` stays reserved (exit 2). `CreateVideoArchive`
became `CreateMirror`. `payloadOf`, `definingCommon`, the split scan skip path, the description
writer payload and `recovererFor` (accepts CATIA split) follow the payload. Help texts name CATIA.

**Executor** (`internal/archive`). `payload_catia.go` registers `catia`: `is_catia` candidates, role
`catia_archive`, CATIA counters, `arxgo-catia.csv` loader and writer, no sidecar (`noPostCommit`),
no restore (`sessionPayload` refuses a restore of a kind without `newRestore` before any lock).
`splitEvents`/`walAcc.payloadRow` replace the video-only WAL reduction and also carry the
`described` CATIA summary; `finishURL`, `fileRegistryRows` and `rowsFromIssues` are shared by both
registries. `Finish` logs `catia_*` keys for a CATIA run. `tryResume` also requires the recorded
payload and mirror root to match (the CLI already made the payload defining; the archive layer now
enforces it). `recoverReplaced` locks the other payload's recorded mirror root through
`lockOtherMirror` (skips a root it already holds, exit 5 when the root is missing), and gives the
resolver's description writer the run log.

**Descriptions.** `DescriptionConfig.Payload` and `Log`; for `catia`, `MarkdownDescription.Write`
extracts from the placed destination, renders the `catia:` line and remembers a
`state.CatiaSummary`, which `finishSplit` and recovery (`state.DescribedAnnotator`,
`SplitResolver.DescribedCatia`) put on the `described` record. The `SplitDescriptionWriter`
interface gained `Catia(rel)`.

**State and report.** `state.Counters` and `archive.Stats` gain the six CATIA counters (omitempty).
`state.Record.Catia` is written only on `described`. `report.CatiaHeader`, `CatiaRow`,
`WriteCatiaCSV`/`LoadCatiaCSV` (columns 11-16 omitted when empty everywhere, canonical-order subset
on read), `MergeCatiaRows`, `MarkCatiaRestored`, `OverlayPayload`.

Rejected: generic merge helpers over `VideoRow` and `CatiaRow` (video rows would need the embedding
0044 rejected); storing both mirror roots in the options (an environment value would become a
defining option); failing a video split on an interrupted CATIA run (contradicts the stage-4 gate).
`text_rel_path` exists in the header but is always empty until `implement-catia-text-sidecars`.

Compatibility: `SplitOptions.CreateVideoArchive` is now `CreateMirror` in `options.json`; an
interrupted split written by the previous build with a missing video archive root resumes as a new
run after recovery (defining options differ). Current-state pages:
[CATIA archive](../current/catia-archive.md), [crash safety](../current/crash-safety.md),
[project foundation](../current/project-foundation.md), [video split](../current/video-split.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Byte-identical CATIA files in the CATIA archive; descriptions with `catia:`; registry in both roots; rerun moves nothing and leaves the registry byte-identical | `TestCatiaSplitMovesOnlyCatiaFilesAndRerunIsNoop` (same-device rename and injected cross-device copy via `forcedOtherDevice`; root and deep files, invented Cyrillic name, foreign `fixture.CATPart.md` kept with `.arxgo.md` fallback; report counters) | pass, Linux |
| Videos, the video archive and `arxgo-videos.csv` untouched | same test (`checkVideoUntouched`), `TestSplitCatiaCommandIgnoresVideoArchiveEnvironment` | pass, Linux |
| Conflicts and corrupt registry | `TestCatiaSplitConflictIsSkippedAndRegistered` (exit 6 status, `conflict` row), `TestCorruptCatiaRegistryStopsBeforeMoves` | pass, Linux |
| Crash after `placed` (every step) | `TestCatiaSplitCrashPointsConverge` (rename and copy points) | pass, Linux |
| Recovery by a video command writes a CATIA description | `TestInterruptedCatiaSplitRecoveredByVideoSplit` (summary on the recovered `described` record, CATIA lock released, no cross-payload registry); `TestVideoSplitCommandRecoversInterruptedCatiaSplit` (CLI, recovered with the CATIA run's `--base-url`). Mutations: `otherPayload := false` fails the archive test with exit-5 `ErrUnrecoveredRun`; without the payload check in `tryResume` the video split resumed the CATIA run (observed while writing the test); dropping `rec.Catia` fails `TestCatiaSummaryOnlyOnDescribed` and the registry assertions | pass, Linux |
| Exit 2 before the lock: `--catia --video`, `--catia --video-archive X`, `--catia-archive X` without `--catia`, nested video and CATIA archives, `--catia --sample start`, `--catia --publish gdrive` | `TestCatiaUsageErrorsExitBeforeLock` (also missing root, equal roots, CATIA inside archive, `ARXGO_IMAGE` with `--catia`, both payload variables, `--catia` on restore); asserts no `.arxgo` in any root | pass, Linux |
| `ARXGO_VIDEO_ARCHIVE` in the environment does not affect a CATIA split | `TestSplitCatiaCommandIgnoresVideoArchiveEnvironment` (exit 0; `options.json` and defining options hold only `catia_archive`) | pass, Linux |
| Group environment resolution; scan ignores `--catia-archive` | `TestPayloadFlagsResolveAsOneSetting`, `TestScanAcceptsAndIgnoresCatiaArchive`, `TestEnvExampleListsEveryFlag` | pass, Linux |
| Registry columns 1-10 and 12-16 | `TestCatiaRegistryColumnsAndRoundTrip` (exact CSV, omitted empty columns, bad headers), `TestMergeCatiaRowsKeepsMovedAndSummary`; preflight `TestPreflightPlacesCatiaNeedsOnCatiaArchive` | pass, Linux |
| `make ci` | `make ci` (fmt-check, vet, vet-windows, `go test ./...`, `make build-all`, lint-spec-plan, lint-doc-links) | pass, Linux; Windows cross-compiled only |
| `make test-integration` (declared extra, video path unchanged) | `make test-integration` | pass, Linux |
| Real-process kill/recover run (declared extra) | Scratch script outside the repository: generated archive with 120 synthetic CATIA files of all five kinds (two 150 MiB V5 files, Cyrillic directories), a foreign description, two NVENC MP4 clips; CATIA archive on `/dev/shm` (cross-device copy, `--verify hash`); `split --catia` SIGKILLed, then a video `split --force-unlock`, then `split --catia`, then a rerun. 15 passing runs: kills after `placed` (6), between transactions (7), mid-copy of a 150 MiB file at `begin` with a part file (2); six earlier runs failed only on a script assertion that expected no CATIA lock after a between-transactions kill, and were rerun after correcting the script | pass: every SHA-256 matches in its mirror root, the video split logged the recovery and released the CATIA lock, recovered `described` records carry `catia`, no part files, 120 `moved` CATIA rows with kind and hash, 2 video rows, identical registry copies, no cross-payload registry, rerun `catia_done=0`. When the kill lands between transactions the killed process's CATIA lock stays and the next CATIA run needs `--force-unlock` (same as video) |
| Memory bound on a large file (declared extra) | `/usr/bin/time -v arxgo split --catia --verify hash` on one generated 1 GiB V5 file to `/dev/shm` | pass: max RSS 40 MiB, 4.3 s, `catia: CATProduct \| V5_CFV2 \| V5R30 SP5 \| 1 component` |

## Audit handoff

- `AUD-implement-catia-split-1`: description extraction uses `context.Background()`, so Ctrl+C during
  the metadata pass of one large file waits for that pass (1 GiB including copy and hash took 4.3 s
  on tmpfs; slower on a network share). Nonblocking; location `MarkdownDescription.extractCatia`.
  Next check: owner `implement-catia-text-sidecars`, which adds post-commit extraction and should
  pass the run context to both passes.
- `AUD-implement-catia-split-2`: recovery of the other payload's interrupted run now locks that
  run's recorded mirror root (integrity amendment above). Nonblocking; location
  `recoverReplaced`/`lockOtherMirror`. Next check: owner `review-stage-4-catia`, confirm the lock
  and exit-5 behavior against `prove-stage-4-on-generated-archive` kills, including a restore
  replacing a CATIA split once CATIA restore exists.
- `AUD-implement-catia-split-3`: later WAL steps serialize a zero `mtime`
  (`"mtime":"0001-01-01T00:00:00Z"`) because `time.Time` ignores `omitempty`; pre-existing, about 30
  bytes per record, readers ignore it. Nonblocking; location `state.Record`. Next check: owner
  `review-stage-4-catia`, decide whether the WAL contract should omit it.
- Resolved from earlier records: `AUD-generalize-payload-split-restore-1` (CATIA counters and finish
  keys follow the payload) and `AUD-generalize-payload-split-restore-2` (video split rolls an
  interrupted CATIA split forward).

Reviewed scope: CLI parsing and validation, options and recovery mapping, executor payload spec,
description writer and recovery annotation, WAL record, registry replay and CSV, counters, lock
handling for replaced runs, manuals and environment template.

## Close or resume

All gates passed on Linux; Windows cross-compiled only. Plan task removed and dependencies in
`implement-catia-text-sidecars` and `implement-catia-restore` replaced with this record;
current-state pages, operator manuals and the records index updated; `catia-archive` stays planned.
Plan counts after: 13 open tasks (10 agent, 3 human); next eligible `implement-catia-text-sidecars`.
