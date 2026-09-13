# Implement Run Lock and Checkpoint

## Task and scope

- Id / capability / checkpoint: `implement-run-lock-and-checkpoint` / `crash-safety` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending, see audit handoff)
- Source: plan task `implement-run-lock-and-checkpoint`, operator request on 2026-09-13
  ("implement, run, fix, improve implementation and then update documentation and plan.md").
  Branch `ag-01-stage-1` at `a05b0b0`, dirty with the started implementation.
- Plan counts at start: 30 open (27 agent, 3 human); next agent task
  `implement-run-lock-and-checkpoint`; also eligible `implement-disk-space-preflight`,
  `implement-directory-walker`, `implement-file-type-detection`, `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-run-lock-and-checkpoint

Give every run an owned state directory, a lock that prevents concurrent mutation, periodic atomic
checkpoints and structured run logs with progress.

- Serves: `crash-safety` -- [Integrity](../openspec/stage-1-core/integrity.md#run-lock)
- Agent status: CLEAR
- Dependencies: [Filesystem primitives](records/0005-safety-implement-filesystem-primitives.md).
- User-visible outcome: A second `arxgo` on the same roots exits 5 naming the owner; a stale local
  lock needs `--force-unlock`; each run leaves `options.json`, `checkpoint.json`, `run.log.jsonl`
  and `report.json`; progress lines appear at the configured interval.
- Scope boundary: `.arxgo/` layout, run id, `current` pointer, lock create/verify/release in one or
  two roots, checkpoint write/read with version field, `slog` fan-out handler (console + JSON file),
  progress reporter with rate/ETA, report writer, interrupt checkpoint. No WAL records.
- Data and artifact paths: `internal/state/lock.go`, `internal/state/checkpoint.go`,
  `internal/state/rundir.go`, `internal/archive/progress.go`.
- Execution path: `O_CREATE|O_EXCL` lock files; PID liveness via `os.FindProcess`+signal 0 on Unix
  and `OpenProcess` on Windows; checkpoint through `fsops.AtomicWriteFile`; progress driven by a
  ticker reading atomic counters.
- Acceptance gates: Concurrent lock attempt fails; stale lock detection with a dead PID; remote-host
  lock never taken over; checkpoint round-trips and a torn temp file is ignored; progress throttle
  honors the interval with an injected clock; context cancel writes a final checkpoint.
- Documentation target: `docs/impl/current/crash-safety.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Spec clarifications made while implementing it:
  [integrity](../../openspec/stage-1-core/integrity.md) now states UTC run ids, resume vs `--dry-run`
  vs `--new-run`, lock order and takeover, own-pid staleness, liveness APIs, checkpoint-on-start,
  report/`run.log.jsonl` rules and progress line fields;
  [contracts](../../openspec/stage-1-core/contracts.md) now define lock, `options.json`, extra
  checkpoint counters and `report.json`;
  [architecture](../../openspec/architecture.md) places process liveness in `fsops`, forbids a
  production `cli` -> `state` import, and records that `scan` takes an exclusive archive lock.

## Implementation

`internal/state` (run directory, lock, checkpoint, report, JSON Lines run log) and
`internal/archive` (session lifecycle and progress). `internal/cli` starts every operation inside a
session. Process liveness lives in `internal/fsops` (`process_unix.go` / `process_windows.go`) so
platform files stay in one package. No new module dependencies.

- `rundir.go`: run id `YYYYMMDDTHHMMSSZ-<8 hex>` (UTC); `runs/<id>/`; `current` as a plain file
  (not a symlink). `options.json` stores every validated option plus a `defining` subset used for
  resume. `--dry-run` creates its own directory and never changes `current`.
- `lock.go`, `lock_takeover.go`: `O_CREATE|O_EXCL` + fsync; archive lock then mirror lock; stale =
  same host (case-insensitive) and dead pid, or this process's own pid; remote host never taken
  over, even with `--force-unlock`. Takeover moves the judged bytes aside to
  `lock.<hex>.arxgo-part`, checks them, removes, then creates with `O_EXCL`. Checkpoints call
  `Verify`. A lost lock writes nothing more into the run directory (console only).
- `checkpoint.go`: versioned `checkpoint.json` through `fsops.AtomicWriteFile`; leftover
  `.arxgo-part` ignored. `Throttle` fires on `--checkpoint-every` or `--checkpoint-interval`.
- `runlog.go`: JSON Lines, UTC times, torn last line terminated on resume; `Fanout` to console and
  file with independent levels. The file is `info` (or `debug` when `--log-level debug`) even if
  the console is `warn`/`error`.
- `report.go`: `report.json` marks the run complete (exit 0, 6, 70, and every dry run).
- `archive/session.go`, `resume.go`, `finish.go`: take locks, resume or create, progress goroutine,
  phase/advance checkpoints, issues cap, `Finish` maps outcomes. `scan` locks only the archive
  root. `split` creates a missing video archive after the archive lock, not in `--dry-run`.
- `archive/progress.go`: atomic counters; interval throttle with injected clock; scan lines have no
  ETA; execute lines have rate (window) and ETA (phase average).
- `cli/session.go`: maps options onto `archive.Config` and `archive.Status` onto exit codes.
  Production `cli` does not import `state`. Operations that are not yet implemented still run the
  lifecycle, then exit 70.

Run, fix and improve:

- File log level used `min(console, info)`, so `--log-level warn` dropped info records from
  `run.log.jsonl`. The file is now info unless the console is debug. Regression:
  `TestRunLogKeepsInfoWhenConsoleIsWarn`.
- A lock whose directory fsync failed was left on disk; `createLockFile` now removes it.
- Takeover restore that hits an already-recreated lock file returns `errLockChanged` and drops the
  aside part file instead of failing hard.
- Lost lock: switch to the console handler before Flush/finish logs so the run directory is not
  appended after ownership is gone (`TestLostLockStopsForOperator`).
- `ProcessAlive` falls back to `/proc/<pid>` when `kill(2)` is denied (restricted environments).
- Tests: scan takes only the archive lock; an interrupted dry run writes `report.json` and does not
  become `current`; an unreadable lock that finishes writing during the retry is classified as
  held; eight-way concurrent `--force-unlock` still has one winner; session tests stop the progress
  goroutine if `Finish` is skipped.
- Scratch mutations: dropping the `--force-unlock` requirement failed `TestStaleLockNeedsForceUnlock`;
  wiring the file log to the console level failed `TestRunLogKeepsInfoWhenConsoleIsWarn`. Sources
  restored.

Decisions and rejected alternatives:

- Liveness stays in `fsops`, not `state`, because only `fsops` may have build-tagged files.
- The lock is created with a provisional run id, then `SetRunID` after resume/create, so the lock
  is held before `current` is read.
- `Start` that never opens a usable run (including a corrupt checkpoint) releases the lock so the
  operator can `--new-run` without `--force-unlock`. `Finish` on `StatusNeedsOperator` keeps it.
  See `AUD-implement-run-lock-and-checkpoint-2`.
- Defining options are the validated option structs with runtime `Common` fields zeroed, so
  `options.json` uses those Go field names (durations in nanoseconds, sizes in bytes).
- Rejected: taking over a remote-host lock with `--force-unlock`; a symlink for `current`;
  production `cli` importing `state`.

Current state: [crash safety](../current/crash-safety.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Concurrent lock attempt fails | `TestConcurrentLockAttemptsHaveOneWinner` (32 goroutines), `TestSecondSessionOnSameRootsIsLocked`, `TestSecondRunOnSameRootsExitsLockedNamingOwner` (exit 5, owner in stderr) | pass, Linux |
| Stale lock detection with a dead PID | `TestStaleLockNeedsForceUnlock` (reaped child pid via `ProcessAlive`), `TestLockWithOwnPIDFromEarlierProcessIsStale`, `TestStaleLockNeedsForceUnlockInSession`, `TestStaleLockExitCodesAndForceUnlock` | pass, Linux |
| Remote-host lock never taken over | `TestRemoteHostLockIsNeverTakenOver` (`--force-unlock` still refused; host compare is case-insensitive) | pass, Linux (injected host names, not a live NAS) |
| Checkpoint round-trip; torn temp ignored | `TestCheckpointRoundTrip`, `TestCheckpointTornPartFileIsIgnored`, `TestReadCheckpointErrors` | pass, Linux |
| Progress throttle honors interval with injected clock | `TestProgressThrottleHonorsInterval`, `TestProgressPhaseBoundaryLines`, `TestProgressRunReadsTicks`, `TestCheckpointCadence` | pass, Linux |
| Context cancel writes a final checkpoint | `TestInterruptWritesFinalCheckpointAndResumes` (no `report.json`, locks released, resume restores cursor and elapsed) | pass, Linux |
| Run files and two-root lock | `TestSessionLeavesRunFilesAndReleasesLocks` (`options.json`, `checkpoint.json`, `run.log.jsonl`, `report.json`); scan-only lock `TestScanTakesOnlyArchiveLock` | pass, Linux |
| Unreadable lock | `TestUnreadableLock`, `TestUnreadableLockBecomesReadableDuringRetry` | pass, Linux |
| Tests detect regressions | Scratch mutations: stale lock taken without `--force-unlock`; file log follows console warn | each mutation failed the named test; sources restored |
| Race and repeat | `CGO_ENABLED=1 go test -race -count=5 ./internal/state/ ./internal/archive/ ./internal/fsops/ ./internal/cli/` | pass, Linux |
| Windows build compiles | `GOOS=windows go vet ./...`; `GOOS=windows go test -c` for `state`, `archive`, `fsops`, `cli`; `make build-all` | pass (cross-compiled only) |
| Windows tests pass in CI | `.github/workflows/ci.yml` `windows` job (`go test ./...`) | not-run: changes are not pushed |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

## Audit handoff

- `AUD-implement-run-lock-and-checkpoint-1`: nonblocking. Windows liveness (`OpenProcess` +
  `GetExitCodeProcess`), case-insensitive roots, and `MoveFileEx` during takeover are
  cross-compiled only; the Windows CI job has not run. Next check: the Windows CI result after
  push. Owner: `review-stage-1-integrity`.
- `AUD-implement-run-lock-and-checkpoint-2`: nonblocking. A `Start` that finds corrupt run state
  releases the lock (exit 5) so `--new-run` does not need `--force-unlock`. [integrity](../../openspec/stage-1-core/integrity.md#run-lock)
  lists exit 5 as keep-lock. The keep-lock rule is applied on `Finish` when the lock was lost or
  the run already owned the directory. Next check: confirm this Start-vs-Finish split, or require
  `--force-unlock` after a failed Start. Owner: `review-stage-1-integrity`.
- `AUD-implement-run-lock-and-checkpoint-3`: nonblocking. Remote-host refusal is tested with
  injected host strings, not a live network share. Next check: stage-1 proof against a CIFS/NFS
  lock file if one is available. Owner: `review-stage-1-integrity`.
- `AUD-implement-filesystem-primitives-1` and `-2`, and `AUD-implement-cli-contract-1`: not changed
  by this task; still owned by `review-stage-1-integrity`.

## Close or resume

All Linux gates pass. The Windows CI gate stays pending and is owned by `review-stage-1-integrity`.
The task was removed from the plan, and its dependents (`implement-write-ahead-log-and-recovery`,
`implement-scan-operation-and-csv-registry`) now link this record. The crash-safety current page,
the current index, project-foundation and the records index were updated. Capability
`crash-safety` stays `planned` (WAL, preflight and the integrity checkpoint remain). Plan counts
after: 29 open (26 agent, 3 human); next agent task `implement-write-ahead-log-and-recovery`.
