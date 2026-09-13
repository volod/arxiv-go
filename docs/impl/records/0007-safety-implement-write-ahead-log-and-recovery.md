# Implement Write-Ahead Log and Recovery

## Task and scope

- Id / capability / checkpoint: `implement-write-ahead-log-and-recovery` / `crash-safety` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending, see audit handoff)
- Source: plan task `implement-write-ahead-log-and-recovery`, operator request on 2026-09-13
  ("implement, run, fix, improve implementation and then update documentation and plan.md").
  Branch `ag-01-stage-1` at `cd2eea0`.
- Plan counts at start: 29 open (26 agent, 3 human); next agent task
  `implement-write-ahead-log-and-recovery`; also eligible `implement-disk-space-preflight`,
  `implement-directory-walker`, `implement-file-type-detection`, `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-write-ahead-log-and-recovery

Journal each file transaction and recover interrupted ones by rolling forward or back.

- Serves: `crash-safety` -- [Recovery](../openspec/stage-1-core/integrity.md#recovery)
- Agent status: CLEAR
- Dependencies: [Run lock and checkpoint](records/0006-safety-implement-run-lock-and-checkpoint.md).
- User-visible outcome: Killing `arxgo` at any moment and rerunning the same command leaves every
  file in exactly one complete location or with its original intact.
- Scope boundary: WAL writer (append, fsync policy per step), reader with torn-tail truncation and
  mid-file corruption detection, committed-set index, generic recovery engine that applies the
  specified per-step table through an operation-supplied resolver, crash-injection hook interface
  for tests. Split/restore resolvers are implemented by their own tasks against fake operations
  here.
- Data and artifact paths: `internal/state/wal.go`, `internal/state/recovery.go`,
  `internal/state/crashtest/` (test helper).
- Execution path: JSON Lines records per [contracts](../openspec/stage-1-core/contracts.md#wal-record);
  recovery iterates open transactions in `seq` order and writes `aborted` or forward steps.
- Acceptance gates: Every row of the recovery table is exercised with a fake operation; torn final
  line truncated; corrupt middle line returns the exit-5 error; recovery is idempotent (running it
  twice changes nothing); committed-set lookup scales to 1e6 entries within the test budget.
- Documentation target: `docs/impl/current/crash-safety.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Spec clarifications made while implementing it:
  [integrity](../../openspec/stage-1-core/integrity.md) now names the fsync policy (`copied` /
  `verified` unsynced; other steps fsynced), `txid`/`seq` shape, torn-tail truncation on open,
  begin/copied lost-file corruption, copied-with-destination-already-placed roll-forward,
  `stub_removed`, and idempotent recovery;
  [contracts](../../openspec/stage-1-core/contracts.md#wal-record) now define `txid` as
  `{run-id}-{6-digit}` and `aborted` reason `unplaced`;
  [architecture](../../openspec/architecture.md) lists `wal.go`, `recovery.go` and `crashtest/`.

## Implementation

`internal/state` WAL and recovery; `internal/state/crashtest` for crash injection; `internal/archive`
opens an existing `wal.jsonl` on resume and recovers when `Config.Recoverer` is set. No new module
dependencies.

- `wal.go`, `wal_read.go`: JSON Lines `O_APPEND` writer; compact records without HTML escaping;
  fsync after every step except `copied` and `verified`; torn last line truncated; middle-line or
  version errors wrap `ErrStateCorrupt`. `txid` is `{run-id}-{6-digit}`; `seq` is monotonic.
- `committed.go`: hash set of committed `rel_path` values, mutex-safe, used to skip candidates on
  resume.
- `recovery.go`: generic engine plus `FSResolver`. Open transactions in begin-seq order. Unplaced
  work is aborted (delete part); at or after `placed` the destination is kept and the rest rolls
  forward; missing/wrong-size destination after `placed` is operator-needed. Restore uses
  `stub_removed`.
- `crashtest/`: `Hook` (error or panic after a named point), fake split/restore copy and rename
  operations, and `CrashEach` covering every WAL step and filesystem effect.
- `archive`: `Session.OpenWAL`, `recoverWAL` on resume, checkpoint `wal_offset`, close on Finish.
  Production `cli` still does not import `state`.

Run, fix and improve:

- Crash hook originally ran while the WAL mutex was held; it now runs after unlock so a hook cannot
  deadlock on the WAL.
- `CommittedSet` lookups are mutex-safe for later concurrent resume use.
- `copied`/`verified` with the destination already in place (rename of the part file before `placed`
  was logged) rolls forward instead of aborting, which would otherwise leave two complete copies.
- `begin` with both source and destination missing is corruption (lost file), not an abort.
- Scratch mutations: no-op `truncate` failed `TestWALTornFinalLineIsTruncated`; treating a missing
  destination after `placed` as abort failed `TestRecoveryTable/placed_dst_missing_is_corrupt`.
  Sources restored.

Decisions and rejected alternatives:

- Split and restore still own their resolvers; tests and session recovery use `FSResolver` (marker
  stub body). The engine must not know Markdown layout.
- Crash injection is a hook after each durable WAL record and each filesystem effect, not a killed
  PID: the next process is simulated by closing and reopening the WAL.
- The 1e6 committed-set gate is an in-memory index (the structure resume uses). Writing a million
  fsynced WAL lines is not required by the gate and would dominate the test budget.
- Rejected: fsyncing every record (the spec's per-step policy keeps copy intermediates cheap);
  treating a last-line unsupported version as torn (it decoded, so it is corruption).

Current state: [crash safety](../current/crash-safety.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Every recovery table row with a fake operation | `TestRecoveryTable` subtests (begin abort, begin rename roll-forward, begin lost file, copied abort, verified abort, copied-already-placed, placed roll-forward, placed missing, placed wrong size, stubbed, source_removed, stub_removed) | pass, Linux |
| Torn final line truncated | `TestWALTornFinalLineIsTruncated`, `TestWALTornFinalLineWithNewlineIsTruncated` | pass, Linux |
| Corrupt middle line is exit-5 error | `TestWALCorruptMiddleLineIsStateCorrupt` (`ErrStateCorrupt`); `TestCorruptWALOnResumeNeedsOperator` (`StatusNeedsOperator` / exit 5) | pass, Linux |
| Recovery is idempotent | `TestRecoveryIsIdempotent`; each `TestCrashEachConvergesToUninterruptedState` subtest recovers twice after resume | pass, Linux |
| Committed-set lookup scales to 1e6 | `TestCommittedSetLookupScalesTo1e6` (1e6 in-memory paths, 10000 lookups) | pass, Linux; not a million-line WAL file |
| Crash after every step converges | `TestCrashEachConvergesToUninterruptedState` (split/restore x copy/rename, every WAL and FS point); `TestSessionRecoversInterruptedTransaction` | pass, Linux |
| Tests detect regressions | Scratch mutations: no-op truncate; placed-missing aborts instead of corrupt | each mutation failed the named test; sources restored |
| Race and repeat | `CGO_ENABLED=1 go test -race -count=3 ./internal/state/ ./internal/state/crashtest/ ./internal/archive/` | pass, Linux |
| Windows build compiles | `GOOS=windows go vet` for `state`, `crashtest`, `archive`, `cli`, `fsops`; `GOOS=windows go test -c` for `state`, `crashtest`, `archive` | pass (cross-compiled only) |
| Windows tests pass in CI | `.github/workflows/ci.yml` `windows` job (`go test ./...`) | not-run: changes are not pushed |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

## Audit handoff

- `AUD-implement-write-ahead-log-and-recovery-1`: nonblocking. Windows WAL open/truncate and
  `FSResolver` atomic stub writes are cross-compiled only; the Windows CI job has not run. Next
  check: the Windows CI result after push. Owner: `review-stage-1-integrity`.
- `AUD-implement-write-ahead-log-and-recovery-2`: nonblocking. Crash injection returns an error (or
  panics) from a hook in-process; it does not SIGKILL the helper. Power loss of an unsynced
  `copied`/`verified` record is covered by the fsync policy plus begin/copied abort, not by dropping
  kernel page cache. Next check: stage-1 proof crash injection through the split transaction.
  Owner: `review-stage-1-integrity`.
- `AUD-implement-write-ahead-log-and-recovery-3`: nonblocking. `FSResolver` writes a marker stub;
  split/restore must replace it so recovery does not leave a non-contract Markdown file. Next check:
  `implement-video-split-transactions` resolver. Owner: `implement-video-split-transactions`.
- `AUD-implement-run-lock-and-checkpoint-1`, `-2`, `-3` and earlier filesystem/CLI notes: not
  changed by this task; still owned by `review-stage-1-integrity`.

## Close or resume

All Linux gates pass. The Windows CI gate stays pending and is owned by `review-stage-1-integrity`.
The task was removed from the plan, and its dependent `implement-video-split-transactions` now
links this record. The crash-safety current page, the current index, README and the records index
were updated. Capability `crash-safety` stays `planned` (preflight and the integrity checkpoint
remain). Plan counts after: 28 open (25 agent, 3 human); next agent task
`implement-disk-space-preflight`.
