# Stage-1 integrity review

## Task and scope

- Id / capability / checkpoint: `review-stage-1-integrity` / `video-restore` / none (this is the
  bounded checkpoint)
- State: accepted.
- Source: plan task; revision `7bf111f`, worktree clean at start. Host: Linux amd64, Go 1.27.1,
  ffmpeg/ffprobe 6.1.1 on `PATH`, NVIDIA RTX 4060 Ti (NVENC used only to generate fixture clips;
  stage 1 has no GPU path).
- Plan counts at start: 18 open tasks (15 agent, 3 human); next eligible
  `review-stage-1-integrity`.
- Accepted task:

```markdown
#### review-stage-1-integrity

Review stage-1 cross-module invariants before the stage proof and before stage 2 builds on them.

- Serves: `video-restore` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md);
  [Disk-space preflight](records/0009-safety-implement-disk-space-preflight.md);
  [Video restore](records/0020-restore-implement-video-restore.md);
  [ffprobe metadata](records/0016-metadata-implement-ffprobe-metadata.md).
- User-visible outcome: Stage 1 is known to be coherent: WAL steps, recovery table, registry
  contracts, lock and preflight agree across scan, split and restore.
- Scope boundary: Read all stage-1 records, code and tests; trace a video through scan, split,
  crash, recover, restore; check contract/spec drift and error accounting; review Windows-specific
  code paths against the spec by reading and cross-compiling (no Windows host). Add missing
  behavior tests at stable seams. No speculative refactor.
- Data and artifact paths: Stage-1 records, `internal/`, `docs/openspec/stage-1-core/`.
- Execution path: Invariant-to-evidence table in the record; targeted tests; audit notes routed to
  one owner each.
- Acceptance gates: Every incoming audit note dispositioned, Windows-only notes as deferred to the
  [Windows verification scenario](../guide/windows-verification.md#deferred-items) (never
  blockers); refactor/no-refactor verdict and `proceed`, `proceed-with-nonblocking-notes` or
  `blocked` recorded; blockers get separate repair tasks before this checkpoint closes; `make ci`
  passes on Linux.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.
```

- Amendments: the checkpoint's dependencies gained its two blocking repair tasks,
  `recover-incomplete-run-before-replacing-it` and `repair-split-restore-round-trip-defects`, as
  the acceptance gate requires ("blockers get separate repair tasks before this checkpoint
  closes"). No other field changed.

## Implementation

### Method

Read records 0001-0020, the stage-1 specification pages, `internal/{state,archive,fsops,report,
scanner,cli,media}` and their tests, and the Windows build-tagged files. Traced one video through
the code: `scan` (walker, detection, ordered writer, `candidates.jsonl`, checkpoint cursor) ->
split preflight -> `begin` -> rename or staged copy (`copied`, `verified`) -> `placed` -> stub
(`stubbed`) -> source removal (`source_removed`) -> `commit` -> video registry report; then a crash
at each step -> `Start` (lock, resume or replace, recovery) -> resume; then restore (scan of the
video archive, registry match, `begin` -> place -> `stub_removed` -> source removal -> `commit`,
directory cleanup, registry update or retirement) and a second split. Suspected defects were
reproduced with throwaway tests (`TestRepro*`, deleted) before any change, then confirmed with
real processes.

### Findings

Blocking (repaired before this checkpoint closed):

1. An incomplete run replaced by a new run (other defining options, another operation, or
   `--new-run`) was never recovered: `current` moved away from its open transactions. A split
   crashed after `placed` and rerun with `--new-run` left the video without a stub or registry row.
   Repaired in [0022](0022-restore-recover-incomplete-run-before-replacing-it.md).
2. Restore missed videos that split selected with `--video-extensions` (restore has no such flag):
   the round trip silently lost them from the archive (exit 0).
3. Restore used registry `rel_path` values unchecked: `../escaped.mp4` wrote outside the archive.
4. Every split report replayed old split commits over newer `restored` rows and counted restore
   commits as `moved`, so the registry claimed restored videos were in the video archive.
5. Restore's empty-directory cleanup walked the whole video archive: one unreadable directory
   failed every run (exit 1), and unrelated empty directories were removed.
6. A directory, symlink or unreadable file at `<video>.md` failed split after placement, on every
   rerun and in recovery.
7. A destination that appeared during a copy removed the part file before the `aborted` record: a
   crash in between rolled the foreign destination forward (wrong stub; with `--verify hash` a
   recovery that fails on every rerun).
8. Restore parsed `arxgo-videos.csv` twice per transaction for stub hints (quadratic: 4000 videos
   97 s) and recorded `<dst>.md` in `stub_removed` for a removed `.arxgo.md` stub.
9. Found by the declared run below: the ISO BMFF parser let QuickTime's data handler `hdlr` in
   `minf` overwrite the track kind, so a real h264+aac `.mov` parsed with zero streams and
   `split --metadata media` left it in the archive as not video.

Findings 2-9 are repaired in [0023](0023-restore-repair-split-restore-round-trip-defects.md).
Each has a regression that fails on `7bf111f`.

Contract and spec drift corrected: WAL `mtime` precision and `stub` field meaning, the video
registry regeneration rule (now a specified replay), restore candidates, registry path validation
and cleanup, recovery of replaced runs, and the exit-5 lock rule (`AUD-implement-run-lock-and-checkpoint-2`).
Error accounting reviewed: skipped entries and videos end with exit 6 (including a skipped entry
from an earlier process), unexpected failures exit 1, operator-needed states exit 5; findings 5 and
6 were the cases where a rerun could never finish.

### Checkpoint-owned tests and tooling

- `TestRestorePreflightRefusesWithoutMutation`: restore across devices below the requirement exits
  4 with both trees unchanged and no locks (restore half of `AUD-implement-disk-space-preflight-1`).
- `TestRestoreCrossDeviceCrashPointsConverge`: every point of `crashtest.RestoreCopyPoints` on the
  auto copy+delete path (previously only rename and copy-keep were crash-tested in `archive`).
- `test/fixtures/tooltest.LookPath` and `ARXGO_TEST_REQUIRE_TOOLS=1` for the live media tests
  (`AUD-bootstrap-repository-and-agent-harness-3`); documented in [test layout](../../../test/README.md)
  and the [development guide](../../guide/development.md).

### Declared run (Linux, generated archive, outside the repository)

`e2e.sh BIN WORKDIR VIDEO_PARENT SEED` in the session scratchpad: builds an 805 MiB, 253-file
archive (four 190 MiB NVENC H.264 MP4s, MKV with a comma in the name, VP9 WebM under a Cyrillic
directory, AVI, MPEG-TS three levels deep, QuickTime MOV, M4A, PNG, 240 text files, a 3 MB
`custom.bik` of random bytes, a foreign `big-1.mp4.md`, a directory named `top.mov.md`, a
nanosecond mtime), records path/size/mtime/SHA-256, then runs the built `bin/arxgo-linux-amd64`
with a scrubbed `ARXGO_*` environment:

1. `split --metadata media --video-extensions bik --verify hash`, `kill -9` at a seeded random
   delay, twice (second with `--force-unlock`, as the stale-lock rule requires);
2. `split ... --verify size --new-run --force-unlock` (other options: recover first), then a rerun;
3. `restore --verify hash`, `kill -9`; `restore --verify size --new-run --force-unlock`; rerun;
4. compare manifests; check stubs, registries in both roots, part files, leftover directories.

| Seed, video archive | Kills landed at | Result |
| --- | --- | --- |
| 4242, `/dev/shm` (cross-device) | split `begin`, split `stubbed`; restore `begin` | recovery logged for both replaced runs; 10/10 videos moved, registries identical; manifests equal (253 files); video archive empty, 0 directories; no part files; all exits 0 |
| 777, same ext4 device | split `placed` (resumed by the second run), restore finished before the kill | 10/10 moved; manifests equal; exits 0 |
| 91, `/dev/shm` | split `stubbed`, split `begin`; restore `stub_removed` | recovery logged; manifests equal; exits 0 |
| 4242 with a `7bf111f` build | split `begin`, `stubbed`; restore `begin` | exits 0 everywhere, but `top.mov` never moved (9 rows), `custom.bik` stayed in the video archive, manifests differ |

The first iteration of the repaired build showed three directories left after restore (those of
the replaced interrupted restore); that became amendment 2 of 0023 and the runs above are after it.

### Measurements

- Restore of N copies of a 482 KiB `.mov` in 50 directories, same device: 1000 videos 14.5 s ->
  10.2 s, 4000 videos 97.1 s -> 38.0 s (linear now; about 10 ms per video, the specified WAL,
  rename and directory fsyncs).
- Scan of 200,000 small files in 3,007 directories: 3.7 s at `--checkpoint-every 500`, 1.2 s at
  5000, 0.8 s at 100000 (`AUD-implement-scan-operation-and-csv-registry-3`).

### Windows review (reading and cross-compilation only)

Read `fsops/{rename,device,space,process}_windows.go`, `report/limits_windows.go`, the Windows
branches in `cli/roots.go`, `split.go` and `restore_exec.go` (case-fold skip), and the new code's
use of `filepath.IsLocal`, `filepath.Rel` and separators. They match the
[cross-platform notes](../../openspec/architecture.md#cross-platform-notes); no Windows-only defect
was found. `make build-all` and `make vet-windows` pass. Runtime checks stay in the deferred
scenario (new note 1 below).

## Invariant-to-evidence table

| Invariant | Code | Evidence |
| --- | --- | --- |
| WAL steps per operation match the spec (split `begin -> [copied -> verified] -> placed -> stubbed -> [source_removed] -> commit`; restore with `stub_removed`) | `archive/split.go`, `restore_exec.go`, `split_transfer.go` | `TestSplitCrashPointsConverge`, `TestRestoreCrashPointsConverge`, `TestRestoreCrossDeviceCrashPointsConverge` (every point listed in `crashtest`) |
| `copied`/`verified` unsynced, all other steps fsynced; losing them equals `begin` | `state/wal.go` `stepNeedsFsync`; `recoverBegin`/`recoverUnplaced` | `state` WAL and recovery tests; `test/integration` `TestCrashEachConvergesToUninterruptedState` |
| Recovery table rows implemented as specified | `state/recovery.go` | `state/recovery_test.go` per row; declared run kills at `begin`, `placed`, `stubbed`, `stub_removed` |
| Recovery runs for `current` before scanning, whether resumed or replaced; `current` never leaves unfinished transactions | `archive/resume.go`, `resume_replaced.go` | 0022 tests; declared run (`recovering the interrupted run` logged, stubs and rows present) |
| Dry runs never resume, recover or change `current` | `resume.go` `openRun` | `TestDryRunNeverBecomesCurrent`, `TestSplitDryRunLeavesVideoAndStubUntouched`, `TestRestoreDryRunDoesNotMove` |
| Archive lock then mirror lock; refused mirror releases the archive lock; checkpoints verify both; exit-5 lock rule | `archive/session.go`, `finish.go`, `state/lock*.go` | `TestSecondSessionOnSameRootsIsLocked`, `TestSecondRunOnSameRootsExitsLockedNamingOwner`, `TestLostLockStopsForOperator`, `TestReplacedRunOutsideLockedRootsNeedsOperator`; declared run stale lock -> exit 5 -> `--force-unlock` |
| Preflight before the first mutation for scan, split and restore; exit 4 changes nothing | `archive/preflight_run.go`, `scan.go`, `split.go`, `restore.go` | `TestSessionPreflightRefusesWithoutMutation`, `TestSplitRenameEXDEVFallbackRechecksSpace`, `TestRestorePreflightRefusesWithoutMutation`, `internal/cli` preflight tests |
| Reserved paths never traversed; restore never writes to a reserved or outside path named by a registry | `scanner/walker.go`, `scanner/relpath.go`, `restore_exec.go` | `scanner` walker tests, `TestLocalRelPath`, `TestRestoreIgnoresRegistryPathsOutsideArchive` |
| Split never overwrites; restore overwrites only with `--overwrite` | `fsops.Rename` no-replace, `StageCopy`, `placeVideo` | `TestSplitAdoptsAndConflictsWithoutOverwriting`, `TestSplitDestinationAppearedDuringCopyIsSkipped`, `TestRestoreConflictSkippedVsOverwrite` |
| A source is removed only after a verified destination and a durable stub; a conflict abort is durable before its part file goes | `split.go` step order, `SplitResolver.RemoveSource`, `abortConflict` | copy-path crash tests, `TestSplitCopyRecoversNanosecondMtime`, `TestConflictAbortIsDurableBeforePartRemoval` |
| Only owned stubs are overwritten or deleted | `report.InspectStub`, `ChooseStubPath`, `RestoreResolver.ownedStub` | `TestInspectStubTreatsNonFilesAsForeign`, `TestSplitDirectoryAtStubPathUsesFallback`, `TestRestoreNeverDeletesForeignStub`, `TestRestoreRemovesOwnedFallbackStubAndRecordsIt` |
| Video registry statuses follow the filesystem across runs; identical copies in both roots | `split_report.go` `replayVideoRows`, `restore_report.go` | `TestSplitRegistryMergesTwoRuns`, `TestSplitAfterRestoreKeepsRestoredStatus`, `TestSplitAfterRetiredRegistryKeepsHistory`, `TestRestoreUpdatesAndRetiresVideoRegistry`; declared run `cmp` of both copies |
| Split then restore reproduces every path, size, mtime and SHA-256 | whole pipeline | `TestRestoreRoundTripPreservesBytesMtimeAndHash`, `TestRestoreReturnsVideoSelectedByExtension`, `TestRestoreCommandRoundTrip`; declared run manifests (3 seeds) |
| The same file is a candidate in scan, split and restore (extensions, media refinement, container parsing) | `scanner/mimetype.go`, `scan_pipeline.go`, `media/isobmff.go` | `TestScanISOMetadataAndAudioOnlyRefinement`, `TestSplitMediaModeStubHasDuration/camera.mov`, `TestISOQuickTimeDataHandlerKeepsTrackKind`, `TestISOLiveFFmpeg` |
| Skips end with exit 6, unexpected failures exit 1, and a rerun can finish | `finish.go` `classify`, `skipSplit`, `summarizeScan` | `TestFailedAndPartialOutcomes`, `TestRestoreCleanupIsLimitedToRestoredDirectories`, `TestSplitReportsFileNamesThatAreNotUTF8` |
| A rerun after success changes nothing and exits 0 | committed set, scan of stubs only | `TestSplitMovesOnlyVideosAndRerunIsNoop`, round-trip rerun; declared run rerun exits 0 |
| A checkpoint never names output that is not durable; the WAL is the authority for placement | `scan.go` `sync`, `split.go` committed counter | `TestScanCrashAfterEveryEntryResumesByteIdentical`, `TestScanCursorNeverMovesBackwards` |
| Package boundaries: `cli` maps options and does not import `state`; only `media` starts processes; no `fmt.Print*` in domain code | `internal/cli`, `internal/media` | `grep` over non-test files at review time: no `state` import in `cli`, `os/exec` only in `media/{tools,ffprobe}.go`, no `fmt.Print` in domain packages |
| Windows specifics implemented in build-tagged files and type-check | `fsops/*_windows.go`, `report/limits_windows.go` | `make build-all`, `make vet-windows` (in `make ci`); runtime deferred |

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Every incoming audit note dispositioned; Windows-only notes deferred | [Audit handoff](#audit-handoff) below; [deferred items](../../guide/windows-verification.md#deferred-items) | pass; no Windows note is a blocker |
| Refactor verdict and decision recorded | [Audit handoff](#audit-handoff) | no refactor; `proceed-with-nonblocking-notes` |
| Blockers repaired by separate tasks before closing | [0022](0022-restore-recover-incomplete-run-before-replacing-it.md), [0023](0023-restore-repair-split-restore-round-trip-defects.md) | pass; regressions fail on `7bf111f` |
| `make ci` passes on Linux | `PATH=/usr/local/go/bin:$PATH make ci` | pass: fmt, vet, Windows vet, all tests, Linux/Windows static builds, spec-plan and doc-link lints |
| Race detector on changed packages | `CGO_ENABLED=1 go test -race -count=1 ./internal/archive ./internal/cli ./internal/state ./internal/report ./internal/scanner` | pass, Linux |
| Live media tests ran, not skipped | `ARXGO_TEST_REQUIRE_TOOLS=1 go test ./internal/media -run 'TestFFprobeLive\|TestISOLiveFFmpeg\|TestFindRealFFprobe' -v` | pass with ffmpeg/ffprobe 6.1.1; with `PATH` lacking them the variable fails the test, unset it skips |
| Declared end-to-end run | `e2e.sh` (table above) | pass on 3 seeds; real SIGKILL; generated data only, never operator data |

## Audit handoff

### Incoming notes

| Note | Disposition |
| --- | --- |
| `AUD-bootstrap-repository-and-agent-harness-1` | Checked: every recorded interpretation is implemented as written (`is_large` column, `<video>.md` stubs, registries in both roots, `--metadata media` needs ffprobe at startup, split never overwrites, restore `--overwrite`/`--create-dirs`, JSONL WAL with atomic checkpoints). Operator confirmation is part of `approve-stage-1-on-operator-archive-copy`; the preview-position interpretation belongs to stage 2. Closed here. |
| `AUD-bootstrap-repository-and-agent-harness-3` | Arrived unresolved (0016 did not record it). Resolved here with `tooltest.LookPath` and `ARXGO_TEST_REQUIRE_TOOLS=1`; evidence in the table above. |
| `AUD-implement-cli-contract-1` | Windows only: deferred to W1-W3 (already listed). |
| `AUD-implement-cli-contract-2` | Resolved: domain packages use `archive.Config`, `ScanConfig`, `SplitConfig`, `RestoreConfig`; `cli` maps options and alone decodes `options.json` back (`recovererFor`); no production `state` import in `cli`. |
| `AUD-implement-filesystem-primitives-1` | Windows only: deferred to W3 (already listed). |
| `AUD-implement-filesystem-primitives-2` | Not run: the host has no NFS or CIFS mount. Nonblocking; routed to `approve-stage-1-on-operator-archive-copy` (trial on a network share when the operator has one; plan text amended). |
| `AUD-implement-run-lock-and-checkpoint-1` | Windows only: deferred to W4 (already listed). |
| `AUD-implement-run-lock-and-checkpoint-2` | Confirmed: a refused start releases its locks, exit 5 after the run started keeps them; the integrity spec now says so. Closed. |
| `AUD-implement-run-lock-and-checkpoint-3` | Not run (no network share); routed with `-2` of filesystem primitives to `approve-stage-1-on-operator-archive-copy` (a second host against the share's lock). |
| `AUD-implement-write-ahead-log-and-recovery-1` | Windows only: deferred to W4 (already listed). |
| `AUD-implement-write-ahead-log-and-recovery-2` | Evidence added: the declared run SIGKILLed split and restore six times across three seeds and converged. The formal gate stays with `prove-stage-1-on-generated-archive`, whose gates already require seeded kills at three points. |
| `AUD-refactor-repository-layout-1` | Still inaccurate (`bash tools/fetch-ffmpeg.sh`; the recipe runs `scripts/fetch-ffmpeg.sh`); left unchanged as the operator asked earlier. Routed to `implement-release-bundle-with-ffmpeg`, which owns `make ffmpeg` and that guide (plan text amended). |
| `AUD-implement-disk-space-preflight-1` (restore half) | Resolved: restore preflights after its scan; `TestRestorePreflightRefusesWithoutMutation`. |
| `AUD-implement-disk-space-preflight-2` | Windows only: deferred to W3 (already listed). |
| `AUD-implement-directory-walker-1` | Windows only: deferred to W5 (already listed). |
| `AUD-implement-directory-walker-4` | Specified behavior; documented for operators on the [archive registry](../current/archive-registry.md) page (next split run moves such a video). Closed. |
| `AUD-implement-file-type-detection-4` | Windows only: deferred to W5 (already listed). |
| `AUD-implement-scan-operation-and-csv-registry-1` | Decided: keep raw bytes in CSV, document the exception in the contracts, and skip such videos with a reason naming the limit (0023). Closed. |
| `AUD-implement-scan-operation-and-csv-registry-3` | Measured (3.7 s vs 0.8 s on 200,000 files); the specified default stays and the tuning hint is documented. Changing the default is a CLI contract decision, routed to `approve-stage-1-on-operator-archive-copy` together with the timings review (plan text amended). |
| `AUD-implement-scan-operation-and-csv-registry-5` | Windows only: deferred to W5 (already listed). |
| `AUD-implement-tool-discovery-1` | Windows only: deferred to W6 (already listed). |
| `AUD-implement-video-split-transactions-1` | Windows only: deferred to W7 (already listed). |

Notes resolved by their owners earlier and verified here with no action: `-cli-contract-3` (0010),
`-bootstrap-...-2` and `-approve-ffmpeg-distribution-3` (0003), `-filesystem-primitives-3/-4`,
`-write-ahead-log-and-recovery-3`, `-scan-operation-...-4` (0018), `-disk-space-preflight-3`,
`-directory-walker-2/-3`, `-file-type-detection-1` (0012), `-file-type-detection-2` (0016; the
no-video-stream refinement applies to every media parser result), `-file-type-detection-3` and
`-scan-operation-...-2` (0015), `-tool-discovery-3` (0016). Not owned here, owners unchanged:
`AUD-add-env-file-and-setup-1` (`review-stage-3-cloud`), `-2` (`implement-cloud-target-interface`),
`AUD-bootstrap-repository-and-agent-harness-4` and `AUD-approve-ffmpeg-distribution-1/-2`
(`implement-release-bundle-with-ffmpeg`), `AUD-implement-tool-discovery-2`
(`implement-ffmpeg-runner`).

### New notes

- `AUD-review-stage-1-integrity-1`: nonblocking, Windows only. New code is cross-compiled only:
  `scanner.LocalRelPath` (`filepath.IsLocal` rejects device names such as `CON`, `\` is rejected),
  the replaced-run root check compares recorded root strings exactly (a case variant of the same
  root exits 5), and cleanup relies on `filepath.Rel` case folding. Next check: step W7 of the
  [Windows verification scenario](../../guide/windows-verification.md#deferred-items). Owner: this
  checkpoint; disposition: deferred.
- `AUD-review-stage-1-integrity-2`: nonblocking. A registry row with status `conflict` or
  `skipped` names a video-archive file that arxgo never moved; restore treats it like any
  candidate, moves it into the archive and, with `--overwrite`, replaces the archive original at
  that path. This follows the specification (unregistered videos are restored too) but may
  surprise an operator. Next check: decide whether restore skips such rows (a spec amendment).
  Owner: `approve-stage-1-on-operator-archive-copy`.
- `AUD-review-stage-1-integrity-3`: nonblocking. An `arxgo-videos.csv` that no longer parses (for
  example re-saved by a spreadsheet with a BOM or reformatted sizes) makes every split report and
  every restore fail with exit 1, although exit 1 promises "recoverable by rerunning". Split's
  `archive_bytes_written` also leaves out stub bytes shown in the report example. Next check: an
  operator-actionable error for an unreadable registry, and archive bytes that count stubs and
  previews, when stage 2 changes the registry reader/writer and writes previews. Owner:
  `integrate-previews-into-split-and-restore` (plan text amended).

### Verdicts

- Refactor: **no refactor.** Repairs stayed at existing seams (session resume, report replay,
  resolvers, a scan include hook, the ISO handler check). `internal/archive/split.go` is at 297
  lines; a stage-2 change there should put preview wiring in its own file.
- Decision: **`proceed-with-nonblocking-notes`.** Both blocking repairs passed; the remaining notes
  are nonblocking and have one owner each.

## Close or resume

All gates pass. The checkpoint and both repair tasks are removed from the plan; references to
`review-stage-1-integrity` in the plan now link this record, and the routed notes were added to
their owners' task text. Current pages updated: [index](../current.md),
[crash safety](../current/crash-safety.md), [archive registry](../current/archive-registry.md),
[media metadata](../current/media-metadata.md), [video split](../current/video-split.md),
[video restore](../current/video-restore.md). Capability `video-restore` stays `planned` until
`prove-stage-1-on-generated-archive` is accepted. Plan counts after: 17 open tasks (14 agent, 3
human); next eligible `prove-stage-1-on-generated-archive`.
