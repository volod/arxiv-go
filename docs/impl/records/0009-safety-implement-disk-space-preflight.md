# Implement Disk-Space Preflight

## Task and scope

- Id / capability / checkpoint: `implement-disk-space-preflight` / `crash-safety` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending, see audit handoff)
- Source: plan task `implement-disk-space-preflight`, operator request on 2026-09-13
  ("implement, run, fix, improve implementation and then update documentation and plan.md").
  Branch `ag-01-stage-1` at `2f8557d`, clean tree.
- Plan counts at start: 28 open (25 agent, 3 human); next agent task
  `implement-disk-space-preflight`; also eligible `implement-directory-walker`,
  `implement-file-type-detection`, `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-disk-space-preflight

Refuse to start a mutating run that cannot finish for lack of space.

- Serves: `crash-safety` -- [Preflight](../openspec/stage-1-core/integrity.md#preflight)
- Agent status: CLEAR
- Dependencies: [Filesystem primitives](records/0005-safety-implement-filesystem-primitives.md).
- User-visible outcome: `split`, `restore` and `--dry-run` print required, available and shortfall
  per device, and exit 4 before any mutation when space is insufficient.
- Scope boundary: Requirement model per operation/transfer/device placement, `--min-free`, shared-
  device aggregation, unknown network free space warning, stage-2 preview estimate hook (zero until
  stage 2). Input is a candidate summary, not the scanner itself.
- Data and artifact paths: `internal/archive/preflight.go`.
- Execution path: Pure function `Plan(candidates, options, deviceInfo) Requirement` plus a thin
  adapter over `fsops.SameDevice`/`FreeSpace`.
- Acceptance gates: Table tests for each row of the preflight table, both roots on one device,
  exactly-at-threshold pass, one byte short fails, zero-total network filesystem warns; formatted
  report is stable.
- Documentation target: `docs/impl/current/crash-safety.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Spec clarifications made while implementing it, in
  [integrity](../../openspec/stage-1-core/integrity.md#preflight):
  - split rows separated into other devices versus same device with `--transfer copy` (the old row
    mixed both and read as if a cross-device copy needed only the largest file);
  - run state row: 2 KiB of WAL records per candidate on the archive device for split and restore
    (a WAL `begin` record with two absolute paths is about 350 B and each of up to five later steps
    about 120 B; without it a million-video run could exhaust `--min-free` with WAL alone);
  - definition of a write device, `min-free` applies even when the estimate is zero, `<=` threshold,
    caller-available bytes, missing root uses its nearest ancestor, free space read once per device;
  - report line shape; a shortfall exits 4 also in `--dry-run` (report status
    `insufficient_space`, added to [contracts](../../openspec/stage-1-core/contracts.md#run-report));
    a refused real run writes no report and resumes; probe failure is exit 1.
  - [split and restore](../../openspec/stage-1-core/split-restore.md) step 4 names exit 4.

## Implementation

`internal/archive` and `internal/cli`; no new dependencies.

- `preflight.go`: pure `Plan(Candidates, PreflightOptions, DeviceInfo) Requirement`, named
  estimates per device (`needs`), `Sufficient`, `Shortfall`, `DeviceRequirement.Attrs` (stable
  slog attributes, human and exact bytes), `InsufficientSpaceError` matching
  `ErrInsufficientSpace`. Saturating arithmetic; negative input counts as zero; `Available` capped
  at `MaxInt64`; zero-total devices are unknown and never fail.
- `preflight_run.go`: `ProbeDevices(fsops.Ops, []RolePath)` (thin adapter over `SameDevice` and
  `FreeSpace`), `LogRequirement` (device lines, unknown-space warning, pass/fail summary) and
  `Session.Preflight(ctx, Candidates)` (phase `preflight`, probe, plan, log, error).
- `session.go` / `finish.go`: `Config.Preflight`, `Config.Registry`, `Config.FS` (defaults to
  `fsops.System`); `StatusInsufficientSpace` (`insufficient_space`), not reportable, so a real
  refused run stays resumable while dry runs still write their report.
- `cli`: `exitCode` maps `StatusInsufficientSpace` to `ExitInsufficientDisk` (4); scan, split and
  restore pass `--min-free`, `--transfer`, `--metadata` and `--registry`.

Run, fix and improve:

- Fixed: `sessionHooks` ran inside `sessionConfig`, before the handlers set op-specific fields, so
  a test hook saw an incomplete configuration. `TestPreflightOptionsReachTheSession` failed for all
  three operations; the hook now runs in `runSession` just before `Start`.
- Ran on this host (scratch test, deleted): real `statfs` on ext4 NVMe (992 GiB available) and
  tmpfs `/dev/shm` (62 GiB) grouped as two devices; a missing video root under the archive grouped
  with it; split auto/copy and restore produced the expected per-device lines (example in the
  current page). Kept as a permanent test: a dry run on the real filesystem with a 4 EiB candidate
  exits insufficient and does not create the missing video archive root.
- Improved: WAL estimate added (see amendments); shared-device estimates merge by name so the two
  registry copies print as one entry; exact byte figures added next to human units for scripts.
- Test mistakes found and corrected while writing the gates (model unchanged): golden line
  `required=12.0KiB` -> `14.0KiB`; shared-device arithmetic in the aggregation test.
- Scratch mutations (each restored, `cmp` checked): threshold off by one -> threshold, shared,
  golden and session tests failed; unknown device treated as known -> unknown-network test failed;
  shared copy using all bytes instead of the largest -> table test failed; restore same-device
  `copy` ignored -> table, threshold and cli report tests failed.

Decisions and rejected alternatives:

- Preflight is not yet called by `scan`, `split` or `restore`: their bodies have no candidate list
  in this build (scope boundary: input is a candidate summary). Calling it with an empty summary
  would turn today's exit 70 into exit 4 on nearly full disks based on invented data. Scan and
  split/restore tasks call `Session.Preflight`; `cli` already maps the result to exit 4.
- The "formatted report" is the slog record set, not a separate text renderer, because domain code
  must log through `slog` and the run log must hold the same figures. Stability is a golden test
  over the text handler.
- Dry run exits 4 on shortfall, as the task outcome states; the old split step "dry run stops with
  exit 0" is amended.
- Rejected: writing `report.json` for a refused real run (it would mark the run complete and force
  a new scan after freeing space).
- Rejected: using `Space.Free` (includes root-reserved blocks the process may not be able to use).

Current state: [crash safety](../current/crash-safety.md#disk-space-preflight-internalarchive).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Table tests for each preflight row | `TestPlanPreflightTable` (scan file/media, registry on other device, split same auto, other auto, other copy, same copy, previews hook, restore same auto, other auto, same copy, no candidates) | pass, Linux |
| Both roots on one device | `TestPlanSharedDeviceSumsRequirementsOnce`; `TestProbeDevicesGroupsRootsAndQueriesEachDeviceOnce` (2 free-space calls for 3 roles); `TestProbeDevicesRealSystemSharesTempDevice` | pass, Linux |
| Exactly-at-threshold pass, one byte short fails | `TestPlanThresholdIsRequiredPlusMinFree`; `TestSessionPreflightRefusesWithoutMutation` (short by 1 then resumes and passes at exactly the threshold) | pass, Linux |
| Zero-total network filesystem warns | `TestPlanUnknownNetworkFreeSpaceDoesNotFail` | pass, Linux; no live network share |
| Formatted report is stable | `TestPreflightReportFormatIsStable` (golden text-handler output and error text) | pass, Linux |
| Exit 4 before any mutation | `TestSessionPreflightRefusesWithoutMutation` (tree manifests of both roots equal, no report, locks released); `TestInsufficientSpaceExitsFour`; `TestInsufficientSpaceReportNamesDevices` (dry run, required/available/shortfall printed) | pass, Linux; injected free space |
| Real filesystem | `TestSessionPreflightRealFilesystemRefusesImpossibleCopy`; scratch probe on ext4 + tmpfs | pass, Linux host |
| Options reach the session | `TestPreflightOptionsReachTheSession` | pass after the hook fix |
| Tests detect regressions | Four scratch mutations listed above | each failed named tests; sources restored |
| Race and repeat | `CGO_ENABLED=1 go test -race -count=3 ./internal/archive ./internal/cli` | pass, Linux |
| Windows build compiles | `GOOS=windows go vet ./internal/archive ./internal/cli`; `GOOS=windows go test -c` for both | pass (cross-compiled only) |
| Windows tests pass in CI | `.github/workflows/ci.yml` `windows` job | not-run: changes are not pushed |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

## Audit handoff

- `AUD-implement-disk-space-preflight-1`: nonblocking. No operation calls `Session.Preflight` yet,
  so the user-visible exit 4 from `split`/`restore` is proven through `runSession` with a test body,
  not through the handlers. Next check: split calls preflight after the scan with the remaining
  candidates (and restore likewise). Owner: `implement-video-split-transactions` (restore follows
  in `implement-video-restore`, verified by `review-stage-1-integrity`).
- `AUD-implement-disk-space-preflight-2`: nonblocking. Windows `GetDiskFreeSpaceEx` and UNC
  zero-total behavior are cross-compiled only. Next check: Windows CI result after push. Owner:
  `review-stage-1-integrity`.
- `AUD-implement-disk-space-preflight-3`: nonblocking. Scan preflight needs a row estimate before
  rows exist; the model accepts it but the scan task must choose the source (for example the
  previous registry row count or a pre-count) or run it after traversal. Owner:
  `implement-scan-operation-and-csv-registry`.
- Earlier notes (`AUD-implement-write-ahead-log-and-recovery-*`, run-lock, filesystem, CLI): not
  changed by this task; owners unchanged.

## Close or resume

All Linux gates pass; the Windows CI gate stays pending with `review-stage-1-integrity`. The task
was removed from the plan (the `crash-safety` section is now empty and was removed), and its
dependents `implement-video-split-transactions` and `review-stage-1-integrity` link this record.
The crash-safety current page, current index, records index and capability registry
(`crash-safety` -> `shipped`) were updated. Plan counts after: 27 open (24 agent, 3 human); next
agent task `implement-directory-walker`.
