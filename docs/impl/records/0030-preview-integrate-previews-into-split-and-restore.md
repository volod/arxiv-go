# Task Record

## Task and scope

- Id / capability / checkpoint: `integrate-previews-into-split-and-restore` / `media-previews` / `review-stage-2-previews`.
- State: accepted.
- Source: plan task at `2748dff`; clean worktree at start.
- Plan counts at start: 12 open tasks (9 agent, 3 human); next eligible `integrate-previews-into-split-and-restore`.
- Accepted task:

````markdown
#### integrate-previews-into-split-and-restore

Generate previews after each committed move and clean them up on restore.

- Serves: `media-previews` -- [Transactions and failures](../openspec/stage-2-previews/previews.md#transactions-and-failures)
- Agent status: CLEAR
- Dependencies: [Video samples](records/0028-preview-implement-video-samples.md); [Frame images](records/0029-preview-implement-frame-images.md).
- User-visible outcome: `arxgo split --sample ... --image ...` leaves previews next to stubs, stubs
  embed them, registries list them, failed previews do not affect moves, and `restore --previews
  delete` removes them.
- Scope boundary: WAL preview records and recovery, preflight estimate, worker pool, stub/registry
  preview sections, scan exclusion of recorded previews, restore policy, missing-preview catch-up on
  rerun, exit 6 on preview failure. While changing the registry reader and writer: an
  `arxgo-videos.csv` that no longer parses gives an operator-actionable error instead of exit 1 on
  every rerun, and archive bytes written count stubs and previews
  (`AUD-review-stage-1-integrity-3`).
- Data and artifact paths: `internal/archive/split.go`, `internal/archive/restore.go`,
  `internal/report/markdown.go`, `internal/state/`.
- Execution path: Extend stage-1 integration test with preview flags when ffmpeg is present.
- Acceptance gates: Crash during preview resumes only missing previews; previews never become split
  candidates; restore deletes only recorded previews with matching size; failure injection yields
  moved video plus exit 6.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.
````

- Amendments: none.

## Implementation

`internal/archive` now plans samples and frames for new candidates and previously moved videos,
probes missing duration/dimensions before preflight, and estimates space only for jobs whose
recorded output is absent. A bounded one-worker queue reads the committed video in the video
archive. It logs a `preview_begin` before each ffmpeg output, then `preview_done` with the exact
size or `preview_failed` with a reason. These are version-2 WAL events; stage-1 transaction
records remain version 1. All preview events use the existing record fields. Recovery removes
part files named by unfinished events and retries missing jobs; it can adopt a completed final
file left between publication and the done event only after normal content validation. WAL replay owns scan exclusions and registry
preview lists. The preview worker refreshes a marked block in the existing owned stub, embedding
PNG frames inline and linking samples while preserving front matter and following operator
notes. Output bytes count the initial stub and
preview/stub refresh writes.

Restore defaults to keeping previews. `--previews delete` verifies each WAL-recorded path is a
regular file below the archive with the recorded size, logs a deletion intent, removes it, then
logs completion. A size mismatch leaves the file and makes the run partial; unrecorded files
are untouched. An interrupted deletion is retried. Kept stubs have their preview links updated.
The registry parser now returns an operator-actionable state error (exit 5) before a move when
`arxgo-videos.csv` is malformed. Failed preview generation stays a partial split (exit 6) and is
listed in `arxgo-videos.md`. The planner reserves names across videos sharing a stem. The
[current page](../current/media-previews.md) describes the behavior. No dependency was added;
the portable software encoders are used on this CUDA host.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Committed move, previews, stub/registry links, scan exclusion and restore deletion | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -count=1 -run 'Test.*Preview.*|TestCorruptVideoRegistryStopsBeforeMove' ./internal/archive` | pass on Linux with generated two-second MP4/MOV and local ffmpeg/ffprobe; rerun does not move or regenerate a recorded preview |
| Preview failure and crash recovery | `TestPreviewFailureAndCrashResumeLive` in the preceding run | pass: ffmpeg nonzero gives moved video and partial status; a crash after durable `preview_begin` resumes with one video commit and generates missing previews |
| Recorded-size deletion and unrecorded protection | `TestSplitPreviewAndRestoreLive`, `TestRestoreKeepsChangedPreviewLive` in the preceding run | pass: matching previews deleted, an unrecorded file and modified recorded PNG retained; mismatch exits partial |
| Unfinished preview with occupied final path | `TestPendingPreviewRejectsInvalidFinalLive` in the preceding run | pass: invalid nonempty final is preserved and refused, rather than adopted as a preview |
| Stub notes preserved | `GOCACHE=/tmp/arxgo-go-cache go test -count=1 -run TestReplacePreviewSectionPreservesFollowingNotes ./internal/report` | pass: first and subsequent preview updates leave following operator notes intact |
| Binary-level split/restore | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -count=1 -run TestStage2Previews -tags integration ./test/integration` | pass on Linux; generated video split with both preview flags, rerun, then restore with deletion |
| WAL compatibility | `GOCACHE=/tmp/arxgo-go-cache go test -count=1 -run TestPreviewEventsSurviveWALReopen ./internal/state` | pass: version-1 move transaction and version-2 preview events reopen without another move |
| Required CI and Windows cross-checks | `GOCACHE=/tmp/arxgo-go-cache make ci` | pass on Linux after the final code and documentation changes: vet, all package tests, Linux/Windows static builds and both doc lints; Windows runtime not run |
| Plan and links | `GOCACHE=/tmp/arxgo-go-cache make lint-spec-plan`; `GOCACHE=/tmp/arxgo-go-cache make lint-doc-links` | pass individually after the final record update |
| Full generated-archive integration | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache make test-integration` | pass on Linux after final changes, including the new binary-level preview test and the existing stage-1 proof |
| Preview integration race check | `GOCACHE=/tmp/arxgo-go-cache go test -race -count=1 -run 'TestSplitPreviewAndRestoreLive|TestPreviewFailureAndCrashResumeLive|TestRestoreKeepsChangedPreviewLive|TestPreviewNamesAcrossVideosLive' ./internal/archive` | pass on Linux |

## Audit handoff

No blocking concern identified in the split/restore preview path, WAL replay, affected report
formats and tests. Windows is cross-compiled and vetted only; runtime behavior remains deferred
to [W7a](../../guide/windows-verification.md#scenario). The stage-2 checkpoint owns a broader
review of preview WAL and registry invariants.

## Close or resume

The record index and current-state page link this record. The task block was removed and the
dependent plan references use this record link. Plan counts after: 11 open (8 agent, 3 human);
next eligible `implement-release-bundle-with-ffmpeg`. `media-previews` remains planned. Final
CI and documentation lint results are recorded above; no temporary repository files remain.
