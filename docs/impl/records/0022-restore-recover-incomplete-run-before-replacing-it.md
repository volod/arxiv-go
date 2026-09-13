# Recover an incomplete run before replacing it

## Task and scope

- Id / capability / checkpoint: `recover-incomplete-run-before-replacing-it` / `video-restore` /
  `review-stage-1-integrity`
- State: accepted.
- Source: blocking repair task added to the plan by the
  [stage-1 integrity review](0021-restore-review-stage-1-integrity.md) (finding 1); revision
  `7bf111f`, worktree clean at the start of the review.
- Plan counts at start: 20 open tasks (17 agent, 3 human) after the review added its two repair
  tasks; next eligible `recover-incomplete-run-before-replacing-it`.
- Accepted task:

```markdown
#### recover-incomplete-run-before-replacing-it

An interrupted split or restore whose rerun uses other defining options, `--new-run` or another
operation starts a new run without recovering the interrupted one, so a placed video can stay
without its stub and registry row, and part files stay behind.

- Serves: `video-restore` -- [State layout](../openspec/stage-1-core/integrity.md#state-layout)
- Agent status: CLEAR
- Dependencies: [Write-ahead log and recovery](records/0007-safety-implement-write-ahead-log-and-recovery.md);
  [Video restore](records/0020-restore-implement-video-restore.md).
- User-visible outcome: `current` never moves away from unfinished transactions: a new run first
  finishes the interrupted run with the options that run recorded, or exits 5 naming the roots
  that can recover it.
- Scope boundary: Recovery of the incomplete `current` run before a new run replaces it (other
  defining options, other operation, `--new-run`) with a resolver rebuilt from its `options.json`;
  exit 5 without changing `current` when this process does not lock that run's roots (`scan`, or
  another video archive); dry runs unchanged; spec amendment for state layout, recovery and
  `--new-run`. No change to the recovery table.
- Data and artifact paths: `internal/archive/resume.go`, `internal/archive/session.go`,
  `internal/cli/session.go`, `docs/openspec/stage-1-core/integrity.md`,
  `docs/openspec/stage-1-core/cli.md`.
- Execution path: Crash injection through the split body, then a second session with
  `--new-run`, other defining options, `restore`, `scan` and another video archive; CLI test that
  rebuilds the resolver from `options.json`.
- Acceptance gates: A split crashed after `placed` and rerun with `--new-run` or other options
  leaves the stub, no source and a `moved` registry row; restore after a split crashed with a part
  file removes the part and completes; `scan` or another video archive exits 5, releases the lock
  and keeps `current`; `make ci` passes.
- Documentation target: `docs/impl/current/crash-safety.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

Defect: `Session.tryResume` returned "start a new run" for `--new-run` or a definition mismatch
without looking at the WAL of the run named in `current`, then `openRun` pointed `current` at the
new run. Recovery only ever runs for `current`, so the interrupted transactions were orphaned.
Reproduced before the fix: a split crashed at `wal:placed` (rename path) and rerun with `--new-run`
or other defining options completed with exit 0, the video in the video archive, no stub and no
`arxgo-videos.csv` row; a copy crashed at `wal:copied` left `<dst>.arxgo-part` for good.

- `internal/archive/resume_replaced.go`: `recoverReplaced` opens the previous run's WAL (torn tail
  truncated as usual) and, when it has open transactions, rebuilds that run's resolver through
  `Config.RecovererFor(op, options.json)` and runs `state.Recover`. Log lines go to the console and
  are appended to the previous run's `run.log.jsonl`. When the previous run's archive or video
  archive differs from this process's roots (always for `scan`, which has no video archive), or no
  factory is configured, it returns `ErrUnrecoveredRun` with the run id and the roots to rerun.
- `resume.go`: `tryResume` calls it for `--new-run` and for a definition mismatch before creating
  the new run directory; the mismatch warning no longer suggests "rerun with the same options to
  resume it", which could never succeed once `current` moved.
- `session.go`: `Config.RecovererFor` and `archive.Resolver` (alias of `state.Resolver`, so `cli`
  still does not import `state`); `runLogLevel` shared by both run logs.
- `finish.go`: `StartFailed` maps `ErrUnrecoveredRun` to `StatusNeedsOperator` (exit 5). Start
  releases its locks, as for other refused starts.
- `internal/cli/session.go`: `splitResolver`, `restoreResolver` and `verifyMode` are shared by the
  handlers and by `recovererFor`, which decodes `SplitOptions` / `RestoreOptions` from
  `options.json`. `root.go` handlers use them.
- Spec: [integrity](../../openspec/stage-1-core/integrity.md#state-layout) state layout, recovery
  and run-lock sections amended; the `--new-run` row of the CLI contract already said "after
  recovering it" and is unchanged.

Decisions: recovering with the previous run's own options (not the new command's) keeps stub
content (`--base-url`, `--verify`) and restore policies (`--stubs`, `--transfer copy`) that the
transactions started with. Refusing instead of taking extra locks for `scan` or another video
archive keeps lock ownership simple; the error tells the operator which command recovers the run.
Root paths are compared as recorded, so another spelling of the same directory is refused
conservatively. Current state: [crash safety](../current/crash-safety.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Split crashed after `placed`, rerun with `--new-run` or other options: stub, no source, `moved` row | `TestReplacedSplitRunIsRecoveredBeforeNewRun/{new-run,other-options}` (`go test ./internal/archive`) | pass, Linux; also asserts the recovery lines in the previous run's log |
| Restore after a split crashed with a part file removes the part and completes | `TestRestoreRecoversInterruptedSplitFirst` | pass, Linux |
| `scan`, another video archive, or no factory: exit 5, lock released, `current` kept, archive unchanged | `TestReplacedRunOutsideLockedRootsNeedsOperator/{scan,other-video-archive,no-recoverer}` | pass, Linux |
| Resolver rebuilt from a real `options.json` | `TestNewRunRecoversInterruptedSplitFromItsOptions` (`go test ./internal/cli`): crash injected through `sessionHooks`, rerun `--verify hash --new-run` writes the stub with the first run's `--base-url` | pass, Linux |
| Real processes | Review declared run (`e2e.sh`, record 0021): `kill -9` during split and restore, reruns with other options, `--new-run` and `--force-unlock` logged `recovering the interrupted run before a new run replaces it` and the round trip matched | pass, Linux, generated archive |
| `make ci` | `PATH=/usr/local/go/bin:$PATH make ci` | pass, Linux (Go 1.27.1); Windows cross-compiled and vetted only |
| Race | `CGO_ENABLED=1 go test -race -count=1 ./internal/archive ./internal/cli ./internal/state ./internal/report ./internal/scanner` | pass, Linux |

## Audit handoff

none identified beyond the checkpoint's notes. Reviewed: dry runs never reach recovery; a resumed
run still recovers through `recoverWAL`; the previous run stays incomplete (no report) and its WAL
still feeds the video registry replay; lock release on a refused start. Windows root comparison is
cross-compiled only and is covered by the checkpoint's deferred Windows note.

## Close or resume

All gates pass. The task is removed from the plan; the crash-safety current page and the index
describe the behavior. Capability `video-restore` stays `planned` until the stage-1 proof.
