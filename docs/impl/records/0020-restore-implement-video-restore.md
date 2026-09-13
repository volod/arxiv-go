# Video restore

## Task and scope

- Id / capability / checkpoint: `implement-video-restore` / `video-restore` / `review-stage-1-integrity`
- State: accepted
- Source: plan task; starting revision includes accepted [0019](0019-split-implement-stubs-and-video-registry.md) work in the tree.
- Plan counts at start: 19 open (16 agent, 3 human); next eligible: this task.
- Accepted task:

```markdown
#### implement-video-restore

Return videos from the video archive to the main archive with the specified directory, conflict,
stub and registry policies.

- Serves: `video-restore` -- [Restore](../openspec/stage-1-core/split-restore.md#restore)
- Agent status: CLEAR
- Dependencies: [Stubs and video registry](records/0019-split-implement-stubs-and-video-registry.md).
- User-visible outcome: `arxgo restore` moves or copies videos back, skips or recreates missing
  directories, deletes or keeps matching stubs, updates registries, and resumes after a crash.
- Scope boundary: Restore phases, recovery resolver, registry matching, `--create-dirs`,
  `--overwrite`, `--stubs`, `--registry-update`, empty video-archive directory cleanup. `--previews`
  stays reserved until stage 2.
- Data and artifact paths: `internal/archive/restore.go`, `internal/archive/restore_recovery.go`.
- Execution path: Reuses the split transaction engine with swapped roots and restore steps.
- Acceptance gates: Split-then-restore round trip reproduces paths, sizes, mtimes and SHA-256;
  missing directory skipped by default and recreated with the flag; conflict skipped vs
  overwritten; foreign stub never deleted; `--transfer copy` keeps the video archive copy; crash
  injection converges; rerun is a no-op.
- Documentation target: `docs/impl/current/video-restore.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

`internal/archive` restore executor and resolver; `internal/cli` runs `RestoreBody`. No new
module dependencies. `--previews` stays reserved.

- `restore.go` / `restore_exec.go`: scan the video archive (registry in the run directory),
  remaining-candidate preflight, walk-order transactions, registry matching by `video_rel_path`,
  missing-directory / conflict / overwrite, EXDEV fallback, source-changed retry.
- `restore_recovery.go`: `RestoreResolver` shares inspect/delete-part/remove-source with split;
  `WriteStub` deletes an owned stub or is a no-op when `--stubs keep`; `RemoveSource` is a no-op
  when `--transfer copy`.
- `restore_dirs.go`: `--create-dirs` mkdir with video-archive parent permission bits; prune empty
  video-archive directories after `--transfer auto`.
- `restore_report.go`: mark committed paths `restored` in both CSV copies; retire to
  `arxgo-videos.restored-<run-id>.csv/.md` when no `moved` rows remain and stubs were deleted.
- Placement reuses `placeVideo` (rename or staged copy; `Replace` when overwriting).

Decisions and rejected alternatives:

- `--transfer copy` keeps the video-archive file, so recovery must not delete the source. The
  resolver carries `KeepSource` from the run options rather than inferring it from WAL
  `transfer=copy` (which also means auto+cross-device, where the source is removed).
- Collision stubs are found via `stub_rel_path` in the video registry, then `<rel_path>.md` and
  `<rel_path>.arxgo.md`. Only an owned front-matter `rel_path` is deleted.
- Capability `video-restore` stays planned: checkpoint and stage-1 proof remain.

Current state: [video restore](../current/video-restore.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Split-then-restore round trip (paths, sizes, mtimes, SHA-256) | `TestRestoreRoundTripPreservesBytesMtimeAndHash` (`auto` and `copy`) | pass, Linux |
| Missing directory skipped vs `--create-dirs` | `TestRestoreMissingDirectorySkippedAndCreated` | pass, Linux |
| Conflict skipped vs `--overwrite` | `TestRestoreConflictSkippedVsOverwrite` | pass, Linux |
| Foreign stub never deleted | `TestRestoreNeverDeletesForeignStub` | pass, Linux |
| `--transfer copy` keeps the video archive copy | `TestRestoreRoundTripPreservesBytesMtimeAndHash/copy` | pass, Linux |
| Crash injection converges | `TestRestoreCrashPointsConverge` (rename points; copy-keep without `source_removed`) | pass, Linux |
| Rerun is a no-op | second run in `TestRestoreRoundTripPreservesBytesMtimeAndHash` | pass, Linux |
| `--dry-run` mutates nothing | `TestRestoreDryRunDoesNotMove` | pass, Linux |
| Registry update and retire | `TestRestoreUpdatesAndRetiresVideoRegistry`; `TestRestoreRegistryUpdateFalseLeavesMovedRows` | pass, Linux |
| CLI round trip | `TestRestoreCommandRoundTrip` | pass, Linux |
| Required repository gates | `GOCACHE=/tmp/arxgo-gocache make ci` | pass, Linux (Go 1.27); Windows runtime cross-compiled only |
| Race | `CGO_ENABLED=1 go test -race -count=1 ./internal/archive ./internal/cli ./internal/report ./internal/state` | pass, Linux |

## Audit handoff

none identified. Reviewed: keep-source recovery vs auto copy, foreign stub deletion, overwrite
through part+replace, empty-dir prune skipping `.arxgo` and the video-archive root, Windows
case-fold skip by cross-compile only.

## Close or resume

Linux gates pass, including race on the changed packages. The task is removed from the plan;
`review-stage-1-integrity` depends on this record. Capability `video-restore` stays `planned`.
Plan counts after: 18 open (15 agent, 3 human); next eligible `review-stage-1-integrity`.
