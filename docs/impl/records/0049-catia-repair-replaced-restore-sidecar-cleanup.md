# Replaced restore sidecar cleanup

## Task and scope

- Id / capability / checkpoint: `repair-replaced-restore-sidecar-cleanup` / `catia-archive` /
  `review-stage-4-catia`
- State: accepted
- Source: plan task `repair-replaced-restore-sidecar-cleanup` (routes `AUD-implement-catia-restore-1`
  and `AUD-implement-catia-restore-2` from [0048](0048-catia-implement-catia-restore.md)); code
  revision `0358aee`, clean tree at start.
- Plan counts at start: 12 open tasks (9 agent, 3 human); next eligible
  `repair-replaced-restore-sidecar-cleanup`.
- Accepted task:

```markdown
#### repair-replaced-restore-sidecar-cleanup

A restore interrupted between a commit and its sidecar deletion and then replaced by another run
leaves owned video previews behind, and CATIA text cleanup learns the earlier run's intent by
decoding CLI options through the recovery resolver.

- Serves: `catia-archive` -- [Replaced restores](../openspec/stage-4-catia/split-restore.md#replaced-restores)
- Agent status: CLEAR
- Dependencies: [CATIA restore](records/0048-catia-implement-catia-restore.md).
- User-visible outcome: After any interrupted restore, the next restore with sidecar cleanup leaves
  no owned previews or text sidecars of files that earlier run restored, whichever command
  recovered it, and never deletes sidecars a `keep` restore or a later split left.
- Scope boundary: `sidecar_cleanup` in `state.RunOptions` written by `archive.Start` for restore
  runs from the restore configuration; one payload-generic earlier-restore scan in the shared
  sidecar cleanup used by video (with description link refresh) and CATIA restore; replace
  `catiaRestore.deletedDescriptions` and its `RecovererFor` use; current run's resumed commits keep
  the existing path. Routes `AUD-implement-catia-restore-1` and `AUD-implement-catia-restore-2`. No
  change to WAL events, registries or split.
- Data and artifact paths: `internal/state/`, `internal/archive/`, `internal/cli/`,
  `docs/impl/current/catia-archive.md`, `docs/impl/current/media-previews.md`,
  `docs/impl/current/crash-safety.md`.
- Execution path: Generated trees in `t.TempDir()` with ffmpeg-generated previews (skip without
  ffmpeg) and synthetic CATIA sidecars; crash at `wal:commit` and `wal:placed` of a restore, then
  recovery by the other payload and by `--new-run`; archive tests with `RecovererFor` nil; declared
  extra: the scratch kill driver of record 0048 extended with `--previews delete`.
- Acceptance gates: Video previews and CATIA text sidecars of a replaced restore are deleted by the
  next restore with cleanup and kept by one without; a `keep` restore in between and a later split
  keep them; an earlier run without `sidecar_cleanup` deletes nothing; changed sidecars are logged,
  not reported; results identical with `RecovererFor` nil; rerun changes nothing; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none to the task. The gate "a `keep` restore in between ... keep them" is read as the
  spec states it: the `keep` restore itself keeps them; a later restore with cleanup still deletes
  them, because a `keep` restore that returns nothing does not change the file's last transaction.
  The [replaced restores](../../openspec/stage-4-catia/split-restore.md#replaced-restores)
  acceptance sentence was reworded to say so explicitly.

## Implementation

**State.** `state.RunOptions.SidecarCleanup *bool` (`sidecar_cleanup`, omitted when nil).

**Session.** `archive.Config.SidecarCleanup`; `openRun` writes it for every restore run (`true` or
`false`) and never for scan or split; `sessionPayload` refuses it on a non-restore op before any
lock. `archive.RestoreSidecarCleanup(kind, RestoreConfig)` is the single policy rule (video:
`DeletePreviews`; CATIA: `!KeepDescriptions`); the CLI sets `Config.SidecarCleanup` from it and
`Restore` refuses a body whose policy differs from the recorded one, so options.json cannot drift
from what the run does.

**History.** `runHistory.sidecarCleanup` replaces the raw `op`/`options` fields added in 0048.

**Shared rule** (`previews_restore.go`). `sidecarCleanup.deleteEarlierRestored(history)` reduces the
payload history to each file's last transaction (committed split resets, committed restore sets),
selects files restored by another run with `sidecarCleanup` that still own sidecars, and deletes
them quietly (info log instead of an issue for a changed sidecar), with the family's normal checks,
WAL events and, for video, the description link refresh. `restorePreviewsBeforeExecute` (video,
now given the history `newVideoRestore` already reads) and `catiaRestore.beforeExecute` call it
after the current run's own resumed commits. `catiaRestore.earlierRestored` and
`deletedDescriptions` are removed; nothing in cleanup uses `Config.RecovererFor`.

Rejected: reading the policy from `options` (couples archive to CLI names, the 0048 note); a WAL
intent record (spec keeps WAL events unchanged); deleting on any earlier restore regardless of its
intent (would remove sidecars an operator kept).

Compatibility: restore runs written by earlier builds have no `sidecar_cleanup` and never trigger
the rule. Current-state pages: [CATIA archive](../current/catia-archive.md),
[media previews](../current/media-previews.md), [crash safety](../current/crash-safety.md);
operator manuals updated.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Replaced restore's previews and text sidecars deleted by the next restore with cleanup | `TestReplacedRestoreSidecarCleanup/{video,catia}/new_run_after_commit...`, `.../new_run_after_placed_deletes`, `.../other_payload_recovers_and_next_restore_deletes` (video crash recovered by a CATIA restore, CATIA crash recovered by a video restore); `TestVideoRestoreCommandRecoversInterruptedCatiaRestore` (CLI) | pass, Linux; video cases use ffmpeg-generated previews (ran with `ARXGO_TEST_REQUIRE_TOOLS=1`, not skipped) |
| Kept by a restore without cleanup; a keep restore in between and a later split keep them | `.../keep_restore_keeps,_a_later_cleanup_restore_deletes`, `.../crashed_keep_restore_keeps`, `.../file_split_again_is_excluded` (both payloads) | pass, Linux |
| Earlier run without `sidecar_cleanup` deletes nothing | `.../earlier_build_without_sidecar_cleanup_keeps` (field removed from the crashed run's `options.json`) | pass, Linux |
| Changed sidecars logged, not reported | `.../changed_sidecar_is_logged,_not_reported` (status completed, no report issues, file unchanged) | pass, Linux |
| Results identical with `RecovererFor` nil | every cleanup run in `TestReplacedRestoreSidecarCleanup` has `RecovererFor` nil (only the transaction-recovering run gets one in the `wal:placed` cases); `catiaRestoreConfig` no longer installs one and the 0048 CATIA recovery tests still pass | pass, Linux |
| Rerun changes nothing | `.../new_run_after_commit_deletes_and_rerun_is_a_no-op` (archive manifest equal) | pass, Linux |
| Intent recorded and enforced | `TestRestoreRecordsSidecarCleanup` (restore writes true/false, split omits it, split with it refused, mismatched restore body refused); `TestRestoreRecordsSidecarCleanupPerPayload` (CLI: video default false, `--previews delete` true, CATIA default true, CATIA keep false) | pass, Linux |
| Tests detect the defect | Mutations of `deleteEarlierRestored`: dropping the `sidecarCleanup` check fails the crashed-keep and earlier-build cases for both payloads; dropping the split reset fails both split-again cases; disabling the rule fails the four delete cases for both payloads; `quiet = false` fails both changed-sidecar cases | pass (all mutations detected), Linux |
| `make ci` | `make ci` | pass, Linux; Windows cross-compiled only |
| `make test-integration` | `make test-integration` | pass, Linux |
| Declared extra: kill driver with `--previews delete` | Scratch driver of 0048 extended: video split with `--sample start --image start` (4 previews), CATIA split with `--catia-text` killed and resumed, then CATIA restore and video `restore --previews delete` each SIGKILLed 1-2 times in random order, then both run to completion in random order; archive manifest (path, size, mtime ns, SHA-256) equals the original, no previews or owned sidecars left, mirror roots empty, rerun unchanged. Kills are timing-based. Seeds 4-7 and 200-239 with the final build | pass: 44 of 44 seeds; the new rule fired (run-log line `... an earlier interrupted restore returned`) in 17 seeds, and in 16 the first completing restore of one payload rolled the other payload's interrupted run forward; a few late video kill attempts had already exited 0 |

## Audit handoff

- Resolved `AUD-implement-catia-restore-1` (video previews of a replaced restore) and
  `AUD-implement-catia-restore-2` (cleanup intent read through `RecovererFor`).
- `none identified` in the new work. Reviewed scope: run options field and its enforcement, history
  reduction, shared quiet deletion, video and CATIA hooks, CLI wiring, docs and manuals.

## Close or resume

All gates passed on Linux; Windows cross-compiled only. Plan task removed and the dependency in
`prove-stage-4-on-generated-archive` replaced with this record; current-state pages, manuals and the
records index updated; `catia-archive` stays planned. Plan counts after: 11 open tasks (8 agent,
3 human); next eligible `prove-stage-4-on-generated-archive`.
