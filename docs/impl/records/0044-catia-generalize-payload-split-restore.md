# Payload split and restore executor

## Task and scope

- Id / capability / checkpoint: `generalize-payload-split-restore` / `catia-archive` / `review-stage-4-catia`
- State: accepted.
- Source: plan task `generalize-payload-split-restore`; code revision `4e831f1` with the uncommitted
  [0043](0043-catia-implement-catia-classification.md) tree, committed as `2fb7686` during the task;
  this task's changes are uncommitted on top of `2fb7686`.
- Plan counts at start: 16 open tasks (13 agent, 3 human); next eligible
  `generalize-payload-split-restore`; also eligible `implement-catia-extraction`.
- Accepted task:

```markdown
#### generalize-payload-split-restore

The split and restore executor is video-named end to end and replays every run's WAL, so CATIA would
otherwise copy the pipeline or leak into the video registry.

- Serves: `catia-archive` -- [Run history by payload](../openspec/stage-4-catia/split-restore.md#run-history-by-payload)
- Agent status: CLEAR
- Task kind: refactor
- Dependencies: [Stage-2 review](records/0033-preview-review-stage-2-previews.md).
- User-visible outcome: Video split and restore behave as today except for the new
  `arxgo-videos.csv` column order; the executor takes a payload (kind plus mirror root, only `video`
  reachable) that selects candidates, payload registry name and columns, counters, history filter,
  description renderer and post-commit sidecar hook without a second copy of transfer or recovery.
- Scope boundary: Payload type; executor, lock, preflight and history code name the mirror root
  generically instead of video archive; required `payload` in `options.json` and in the rebuilt
  recovery resolver; `readHistory` consumers filtered by payload (video registry, preview index,
  restore candidates, directory cleanup); `ScanConfig` candidate predicate replacing the `IsVideo`
  check; the shared payload registry columns 1-10 in the
  [video registry order](../openspec/stage-1-core/contracts.md#video-registry-csv); the preview WAL
  begin/finish/part-file mechanism made reusable for another event family. No CLI flags, no CATIA
  behavior, no compatibility with run directories or registries written before this task.
- Data and artifact paths: `internal/archive/`, `internal/state/`, `internal/report/`,
  `internal/cli/session.go`, `test/integration/`.
- Execution path: Existing video unit and integration tests; a new test that a run directory whose
  `options.json` names another payload is excluded from video registry replay.
- Acceptance gates: Video tests pass with only column-order expectations updated; foreign-payload
  history is ignored by video replay; `make ci` and `make test-integration` pass.
- Documentation target: `docs/impl/current/catia-archive.md`; column references in
  `docs/impl/current/video-split.md` and the operator manuals.
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none.

## Implementation

**Payload type** (`internal/archive/payload.go`, `payload_video.go`). `Payload{Kind, Root}` replaces
`Config.VideoArchive`/`CreateVideoArchive` (now `Payload.Root`/`CreateMirror`). A `payloadSpec` per
kind holds the noun, preflight role, registry name and row loader, the candidate predicate, counter
accessors (live atomics and snapshot totals) and two hook factories. `splitHooks` are prepare (video
registry, history, preview index, part cleanup, skip paths), plan (preview plans and preflight
bytes), start (post-commit sidecar: the preview queue with catch-up) and writeRegistry.
`restoreHooks` are rows (payload registry rows by `rel_path`), include (moved rows as extra
candidates), beforeExecute (unfinished preview parts and deletions), committed (`--previews delete`)
and updateRegistry. `Split` and `Restore` are now payload-neutral loops over these hooks. Only
`video` is registered; `Start` refuses split/restore without a known kind or mirror root, or scan
with a payload, before any lock or state is written. `PayloadCatia` exists as a name so run history
can exclude CATIA runs; it has no spec.

**Generic naming.** Lock, session start log (`payload`, `mirror`), mirror directory creation,
transfer mode, destination parents, restore directory cleanup, preflight (`PreflightOptions.Payload`
selects the mirror role and need names), report roots, progress and the partial status read the
payload instead of the video archive. `placeVideo` became `placePayload`, `skipVideo`
`skipCandidate`, `videoRowsByVideoPath` `payloadRowsByPath`. Preview-only helpers moved out of
`split.go`/`restore.go` into `previews_exec.go`/`previews_restore.go`.

**Run history by payload** (`history.go`). `readHistory(root, kind)` reads only runs whose
`options.json` names the kind; each run keeps its recorded archive and mirror root
(`runHistory.mirror`, `mirrorRel`). Runs without readable options, scans and runs of another payload
are skipped. Consumers: video registry replay, preview index (split and restore), mirror directory
cleanup; restore description hints read the payload registry named by `RestoreResolver.Payload`.

**Options and recovery.** `state.RunOptions` gains `payload` and `catia_archive`;
`setRunPayload`/`runPayload` map the kind to its root field. `cli.Common.Payload` is `video` for
split and restore (empty for scan) and part of the defining options. `Config.RecovererFor` now takes
`(op, payload, options)`; `cli.recovererFor` requires a payload, rejects a kind without an executor
and a mismatch with the stored options. Replacing an interrupted run of another payload or mirror
root exits 5 naming that kind's mirror flag (`--catia-archive`); an interrupted run without a
payload is `ErrStateCorrupt`.

**Candidates.** `ScanConfig.Candidate func(rel, FileType) bool` replaces the `IsVideo` check and
`Include`. Split uses the payload predicate; restore adds registry rows. Nil writes no candidates, so
`scan` no longer writes video candidates into its run directory (not an operator output; the scan
unit tests set the video predicate to keep checking the selection).

**Registry columns** (`internal/report/videos.go`). `PayloadHeader`/`PayloadRegistryKeep` (10) and
`PayloadRow` with cell encode/decode are the shared columns 1-10 in the contract order;
`VideoHeader` is those, `previews`, then metadata. `VideoRow.Payload()` projects a row. Video
registries in the previous order no longer load (exit 5 on split and restore).

**Sidecar event families** (`internal/state/wal_event.go`, renamed from `wal_preview.go`).
`EventFamily{Name, Begin, Done, Failed, Delete, Deleted}` with `PreviewEvents` registered.
`WAL.BeginEvent`/`FinishEvent` replace `BeginPreview`/`FinishPreview`; open events are tracked per
txid across families, a finish must match its family and begin kind, and replay rejects a mixed
family as corrupt. `EventWALVersion` (2) replaces the preview-only constant (kept as an alias).
`archive.eventIndex` (alias `previewIndex`) replays one family with its part-path rule.

Rejected: a second executor file per payload (spec forbids it); embedding `PayloadRow` in
`VideoRow` (would have rewritten every row literal in tests beyond column order); keeping a
video-history fallback for runs without `payload` (scope excludes compatibility).

Compatibility: registries in the earlier column order and run directories without `payload` are not
read, as scoped; the Linux and Windows manuals say so. Current-state pages:
[CATIA archive](../current/catia-archive.md), [video split](../current/video-split.md),
[crash safety](../current/crash-safety.md), [media previews](../current/media-previews.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Video tests pass with only column-order expectations updated | `go test ./...`; test edits: status/url indexes in `split_registry_test.go`, `restore_integrity_test.go`, `resume_replaced_test.go`; mechanical renames for the new config fields and `RecovererFor` signature; `Payload: PayloadVideo` in preflight and CLI option expectations; video predicate in the scan test helper | pass, Linux. No behavior expectation other than column positions changed |
| Foreign-payload history ignored by video replay | `TestForeignPayloadRunsAreExcludedFromVideoReplay` (a planted CATIA run with a committed CATIA split, a committed restore of the video's `rel_path` and an unfinished preview event with a part file; a later video split and restore keep the row `moved`, add no CATIA row, leave the part file, and the round trip completes). Mutation check: with the payload filter removed the test fails (part file removed) | pass, Linux |
| Payload in `options.json` and the rebuilt resolver | `TestRunOptionsRecordPayloadAndMirrorRoot`, `TestStartRequiresKnownPayload`, `TestReplacedRunOfAnotherPayloadNeedsOperator`, `TestRecovererForRequiresRunPayload`, `TestSplitRunRecordsPayloadAsDefiningOption` | pass, Linux |
| Shared payload columns | `TestPayloadRegistryColumnOrder` (header, exact CSV, round trip, previous order rejected) | pass, Linux |
| Reusable event mechanism | `TestEventFamiliesShareMechanismAndStayApart` (a test-registered second family, cross-family outcomes rejected, reopen keeps an open delete, mixed family corrupt on open); `TestPreviewEventsSurviveWALReopen` | pass, Linux |
| `make ci` | `make ci` (fmt-check, vet, vet-windows, `go test ./...`, `make build-all`, lint-spec-plan, lint-doc-links) | pass, Linux; Windows cross-compiled only |
| `make test-integration` | `make test-integration` | pass, Linux |
| Seeded kill/resume sweep (declared extra run) | `ARXGO_TEST_SEED=<1..30,222> ARXGO_TEST_VIDEO_PARENT=/dev/shm ARXGO_TEST_REQUIRE_TOOLS=1 go test -count=1 -tags integration -run 'TestArchiveSplitRestoreRoundTrip\|TestPreviewSplitRestoreRoundTrip' ./test/integration/` | 31/31 pass, Linux, tmpfs video archive, FFmpeg required |
| Real-process run on generated media (declared extra run) | Scratch script on the dev host: two NVENC clips (1080p H.264 MP4 with AAC, 720p HEVC MOV with a Cyrillic name), a synthetic CATPart and a text file; `split --transfer copy --verify hash --image start --sample start` to `/dev/shm` killed with SIGKILL during previews, resumed with `--force-unlock`; a foreign CATIA run planted; `split --new-run`; `restore --previews delete` | pass: `options.json` has `payload` `video`, `video_archive` and defining `Payload`; header starts `rel_path,file_name,status,url,description_rel_path,...`; copies equal; CATPart stays; resume generated the missing preview; foreign run changed no row; SHA-256 manifest identical after restore; retired registry has no CATIA row. Evidence kept outside the repository |

## Audit handoff

- `AUD-generalize-payload-split-restore-1`: counters and the finish summary still use the video
  fields (`videos_*`, `video_bytes`, `video_archive_bytes_*`, `state.Counters`). The payload spec
  selects them through `payloadCounters`/`payloadTotals`, so CATIA needs new fields and a spec entry,
  and the finish log keys must follow the payload. Nonblocking; owner `implement-catia-split`.
- `AUD-generalize-payload-split-restore-2`: replacing an interrupted run of another payload exits 5
  (`ErrUnrecoveredRun`) because this process does not hold that payload's mirror lock. The stage-4
  spec requires a video split to roll an interrupted CATIA split forward, which needs the other
  mirror root and its lock. Nonblocking; owner `implement-catia-split` (its acceptance gate covers it).
- `AUD-generalize-payload-split-restore-3`: `Progress.payloadOf` reads video totals when a reporter
  has no payload (scan and progress unit tests). Scan phases have no item totals, so no output
  changes; the checkpoint should confirm no payload phase runs without the session's spec.
  Nonblocking; owner `review-stage-4-catia`.
- `AUD-generalize-payload-split-restore-4`: runs without `payload` are silently excluded from
  history. A split after deleting an earlier-order registry would drop rows of those runs instead of
  failing. Accepted by the task's no-compatibility scope and documented in the manuals; the
  checkpoint should confirm this is acceptable for the operator archive copy. Nonblocking; owner
  `review-stage-4-catia`.

Reviewed scope: executor, lock, preflight, history, recovery, candidates, registry columns, event
families, CLI option mapping and every current test touching them.

## Close or resume

All gates passed. Plan task removed and its dependency in `implement-catia-split` replaced with this
record; current-state pages and the records index updated; `catia-archive` stays planned. Plan counts
after: 15 open tasks (12 agent, 3 human); next eligible `implement-catia-extraction`.
