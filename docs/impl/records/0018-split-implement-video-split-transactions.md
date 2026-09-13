# Video split transactions

## Task and scope

- Id / capability / checkpoint: `implement-video-split-transactions` / `video-split` / `review-stage-1-integrity`
- State: accepted.
- Source: plan task; starting revision `20cdad7`, work already started in the worktree.
- Plan counts at start: 21 open (18 agent, 3 human); next eligible: this task.
- Accepted task:

```markdown
#### implement-video-split-transactions

Move every video into the mirrored video archive as a write-ahead-logged transaction.

- Serves: `video-split` -- [Split](../openspec/stage-1-core/split-restore.md#split)
- Agent status: CLEAR
- Dependencies: [Write-ahead log and recovery](records/0007-safety-implement-write-ahead-log-and-recovery.md);
  [Disk-space preflight](records/0009-safety-implement-disk-space-preflight.md);
  [Scan operation and CSV registry](records/0012-registry-implement-scan-operation-and-csv-registry.md).
- User-visible outcome: `arxgo split` moves videos by rename on one device or copy+verify+delete
  across devices, honors `--dry-run`, never overwrites, and resumes after any crash.
- Scope boundary: Split phases 1-5, split recovery resolver, destination-exists and source-changed
  rules, cross-device fallback, directory creation, exit 6 accounting. Stubs and registries are
  written through a minimal interface completed by `implement-stubs-and-video-registry`; this task
  writes a placeholder stub that satisfies the WAL `stubbed` step.
- Data and artifact paths: `internal/archive/split.go`, `internal/archive/split_recovery.go`.
- Execution path: Candidate iterator from the scan run, per-candidate transaction using `fsops` and
  `state`, crash-injection tests through `test/fixtures/crashtest`, cross-device simulated by an injected
  device function. Ensure that if the source archive path ARXGO_ARCHIVE and the target video archive
  path ARXGO_VIDEO_ARCHIVE reside on the same physical device, we use strict move semantics rather
  than copy-and-delete. In this case, we will not overload storage by copying gigabytes of data.
- Acceptance gates: Byte-identical videos at mirrored paths; non-video media untouched; crash
  injection after every step on both transfer paths converges; adopted identical destination;
  conflicting destination skipped with exit 6; source modified mid-copy aborted and retried;
  `--dry-run` mutates nothing; second run is a no-op.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

`internal/archive` split executor and resolver; `fsops.StageCopy` so WAL `copied`/`verified` can be
recorded before the part file is placed. No new module dependencies.

- `split.go`: scan (`Preflight=false`, video archive in `SkipPaths`), remaining-candidate preflight,
  walk-order transactions, same-device classification from the two archive roots, adopt/conflict,
  source-changed retry, exit 6 skips, `--dry-run` stop after the plan.
- `split_transfer.go`: no-replace rename or staged copy, EXDEV fallback that rechecks space for
  remaining copies, durable mkdir of mirrored parents (source permission bits on Linux, parent
  fsync), flush-after-place treated as `placed`.
- `split_recovery.go`: `SplitResolver` plus `PlaceholderStub` (`rel_path` front matter;
  foreign `<source>.md` uses `<source>.arxgo.md`). Recovery uses the same writer. `cli` installs
  the resolver before `Start` and runs `SplitBody`.
- Transfer mode is taken from `SameDevice(archive, video-archive)`, not per-file paths, so a
  same-device layout always renames.

Run, fix and improve:

- Nested destination directories used `filepath.Base` fragments and created `video/b/a` for
  `a/b`. The chain is now the relative prefixes `a`, `a/b`.
- A non-`LinkError` after rename (directory flush failed, destination already visible) continues
  from `placed` instead of failing the transfer.
- WAL `mtime` keeps nanoseconds through JSON; recovery source removal after `placed` uses that
  value. Covered by a copy-path crash at `wal:placed`.
- Real Linux tmpfs: archive in `t.TempDir()`, video archive under `/dev/shm`, `auto` records
  `transfer=copy`.

Decisions and rejected alternatives:

- Full Markdown stubs, `arxgo-videos.csv` and `arxgo-videos.md` stay with
  `implement-stubs-and-video-registry`. The placeholder satisfies `stubbed`.
- Per-file `SameDevice(src, dst)` was rejected for the initial classification: the operator
  requirement is root-to-root move semantics; a surprising EXDEV still falls back to copy.
- `StageCopy` is a `fsops` seam so split can journal `copied`/`verified` before placement; tests
  inject source mutation there.

Current state: [video split](../current/video-split.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Byte-identical videos at mirrored paths; non-video untouched; second run no-op | `TestSplitMovesOnlyVideosAndRerunIsNoop` (auto and copy); `TestSplitMirrorsNestedAndUnicodePaths` | pass, Linux |
| Same-device rename, not copy | `TestSplitMovesOnlyVideosAndRerunIsNoop/auto` (`StageCopy` errors if called) | pass, Linux |
| Cross-device copy (injected) | `TestSplitAutoUsesCopyOnInjectedOtherDevice` | pass, Linux |
| Cross-device copy (real filesystems) | `TestSplitAutoCopiesWhenRootsOnDifferentFilesystems` (`t.TempDir()` vs `/dev/shm`) | pass, Linux; skips if `/dev/shm` is missing or shares the temp device |
| EXDEV fallback rechecks space | `TestSplitRenameEXDEVFallbackRechecksSpace` (shortfall exit 4; enough completes) | pass, Linux, injected rename |
| Crash after every step converges | `TestSplitCrashPointsConverge` (`fs:mkdir` plus `crashtest.RenamePoints` / `CopyPoints`) | pass, Linux |
| Adopt identical destination; conflict skipped without overwrite (exit 6) | `TestSplitAdoptsAndConflictsWithoutOverwriting`; `TestSplitSizeVerifyAdoptsSameSizeDifferentBytes` | pass, Linux |
| Source changed mid-copy retries once, then skips and drops the part | `TestSplitChangedSourceRetries`; `TestSplitChangedSourceTwiceSkipsAndRemovesPart` | pass, Linux |
| Destination appeared during copy skipped | `TestSplitDestinationAppearedDuringCopyIsSkipped` | pass, Linux |
| `--dry-run` mutates nothing | `TestSplitDryRunLeavesVideoAndStubUntouched` | pass, Linux |
| CLI `arxgo split` | `TestSplitCommandMovesVideo`; `TestRunExitCodes/split` | pass, Linux |
| Incoming WAL mtime nanoseconds | `TestSplitCopyRecoversNanosecondMtime` | pass, Linux (ext4 nanosecond mtime) |
| Incoming flush-after-place | `TestSplitFlushErrorAfterRenameContinuesAsPlaced` | pass, Linux |
| Required repository gates | `GOCACHE=/tmp/arxgo-gocache make ci` | pass, Linux (Go 1.27.1); Windows runtime cross-compiled only |
| Race | `CGO_ENABLED=1 go test -race -count=1 ./internal/archive ./internal/cli ./internal/fsops ./internal/state` | pass, Linux |

## Audit handoff

Incoming notes resolved in this task:

- `AUD-implement-filesystem-primitives-3`: WAL begin stores `time.Time` as RFC 3339 with
  nanoseconds; recovery source removal after `placed` succeeds. Evidence:
  `TestSplitCopyRecoversNanosecondMtime`.
- `AUD-implement-filesystem-primitives-4`: new parents are mkdir plus parent `SyncDir`; a
  non-`LinkError` after rename continues as `placed`. Evidence: nested-path test and
  `TestSplitFlushErrorAfterRenameContinuesAsPlaced`.
- `AUD-implement-write-ahead-log-and-recovery-3`: `SplitResolver` / `PlaceholderStub` replace the
  marker stub; recovery records the collision-aware path.
- `AUD-implement-disk-space-preflight-1`: split calls `Session.Preflight` after scan with remaining
  candidates; EXDEV rechecks copy space. Restore still belongs to `implement-video-restore`.
- `AUD-implement-scan-operation-and-csv-registry-4`: split scan uses `Preflight=false` and
  `SkipPaths` for the video archive; remaining work is counted from `candidates.jsonl` minus the
  committed set; a completed scan is not repeated.

New notes:

- `AUD-implement-video-split-transactions-1`: nonblocking. Windows case-fold skip and same-volume
  rename are cross-compiled only. Next check: step W7 of the
  [Windows verification scenario](../../guide/windows-verification.md#deferred-items). Owner:
  `review-stage-1-integrity` (disposition: deferred).

## Close or resume

All Linux gates pass. The task was already removed from the plan during implementation; dependents
link this record. The video-split current page, current index, crash-safety and project-foundation
pages, README status and the records index are updated. Capability `video-split` stays `planned`
(stubs and video registries remain). Plan counts after: 20 open (17 agent, 3 human); next eligible
`implement-stubs-and-video-registry`.
