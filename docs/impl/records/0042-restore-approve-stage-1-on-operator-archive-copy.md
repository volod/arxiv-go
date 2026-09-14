# Approve stage 1 on an operator archive copy

## Task and scope

- Id / capability / checkpoint: `approve-stage-1-on-operator-archive-copy` / `video-restore` / none
- State: accepted (operator decision, 2026-09-14).
- Source: plan task `approve-stage-1-on-operator-archive-copy` (Human-Assisted, HUMAN-GATED).
  Operator request in this session: "I have approved @docs/impl/plan.md ####
  approve-stage-1-on-operator-archive-copy Please create record, update documentation and plan.md"
- Plan counts at start: 9 open tasks (6 agent, 3 human); next eligible agent task
  `research-cloud-target-apis`.
- Accepted task:

```markdown
#### approve-stage-1-on-operator-archive-copy

Decide whether stage 1 is fit for use on real archives after a trial on a copy of operator data.

- Serves: `video-restore` -- [Success criteria](../openspec/spec.md#success-criteria)
- Human status: HUMAN-GATED
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- Requested input or decision: Run scan, split, interrupt, resume and restore on a disposable copy
  of a representative archive; review registry, descriptions, logs and timings; accept, or file defects.
  Where available, put one root on a CIFS/NFS share and start a second `arxgo` from another host
  against its lock; note scan throughput at the default `--checkpoint-every`; decide whether
  restore should skip video-archive files whose registry row is `conflict` or `skipped`. These are
  the routed notes `AUD-implement-filesystem-primitives-2`, `AUD-implement-run-lock-and-checkpoint-3`,
  `AUD-implement-scan-operation-and-csv-registry-3` and `AUD-review-stage-1-integrity-2` of
  [the stage-1 integrity review](records/0021-restore-review-stage-1-integrity.md#audit-handoff).
- Unblocks: Production use of stage 1. Stage-2 development does not wait for this decision.
```

- Amendments: none.

## Decision

The operator decided; the agent recorded the trial outputs and the defects they filed, and did
not mark the task done until this approval.

- Stage 1 is fit for use on real archives.
- Keep the specified `--checkpoint-every` default of 500. The operator did not ask to change it.
  A post-split scan of the document tree (20643 registry rows, default checkpoints) completed in
  about 1 s (`report.json` `wall_s` 1.03).
- Keep specified restore behavior for `conflict` and `skipped` rows: they remain restore
  candidates. The operator did not request a spec amendment.
- A CIFS/NFS share and a second-host lock check were not available on this trial. The operator
  accepted without that check.

Defects filed during the trial were repaired before this approval:
[0040](0040-split-flatten-operator-csv-outputs.md) (flat operator CSV) and
[0041](0041-metadata-collect-iso-metadata-by-default.md) (default ISO BMFF collection and omission
of unused metadata columns).

## Implementation

No production code in this record. Current-state page:
[video restore](../current/video-restore.md). Capability `video-restore` is `shipped`.

## Acceptance evidence

The trial used a disposable copy of operator data (paths omitted). Inspected artifacts stayed
outside the repository.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Operator decision | this session: approve `approve-stage-1-on-operator-archive-copy` | pass: recorded |
| Split on the copy | `arxgo split --archive <copy> --video-archive <video-copy>` (default `--metadata file`) | pass: completed; `arxgo-videos.csv` had 430 `moved` rows (332 mp4, 80 mov, 9 lrv, 8 mpg, 1 wmv); descriptions left in the document tree; no skipped or failed videos in the inspected registry |
| Scan after split | `arxgo scan --archive <copy>` | pass: completed, 20643 rows, 0 videos remaining in the document tree, 3019 picture rows |
| Registry and description review | `arxgo-registry.csv`, `arxgo-videos.csv`, sample `.mp4.md` descriptions | pass after 0040/0041: empty `media_*` columns on the first split were the filed defect; ISO BMFF files can fill them on a later split with the repaired build. MPEG/WMV still need `--metadata media` |
| Interrupt, resume and restore on the copy | not run on this copy | not-run; kill/resume/restore evidence remains the generated-archive proof in [0025](0025-restore-prove-stage-1-on-generated-archive.md) |
| CIFS/NFS share and second-host lock | not available | not-run; `AUD-implement-filesystem-primitives-2` and `AUD-implement-run-lock-and-checkpoint-3` |
| Checkpoint default | default `--checkpoint-every 500` on the post-split scan | pass: keep specified default; scan finished in about 1 s |
| Restore `conflict`/`skipped` policy | no spec amendment requested | pass: keep specified behavior (`AUD-review-stage-1-integrity-2`) |
| Plan and doc lint | `make lint-spec-plan`; `make lint-doc-links` | pass |

## Audit handoff

Incoming notes from [0021](0021-restore-review-stage-1-integrity.md#audit-handoff), closed here:

- `AUD-implement-filesystem-primitives-2`: nonblocking, not-run. The trial was a local copy, not
  a CIFS/NFS mount, so the Linux `RENAME_NOREPLACE` fallback was not exercised live. Disposition:
  accepted without that check; no new owner.
- `AUD-implement-run-lock-and-checkpoint-3`: nonblocking, not-run. No second host against a
  network-share lock. Disposition: accepted without that check; no new owner.
- `AUD-implement-scan-operation-and-csv-registry-3`: nonblocking. The specified default stays;
  the operator did not ask to raise it. The trial scan at that default finished in about 1 s on
  20643 rows. Disposition: closed.
- `AUD-review-stage-1-integrity-2`: nonblocking. Restore still treats `conflict` and `skipped`
  video-archive files as candidates. Disposition: keep the specification; closed.

None identified in this decision beyond those closures.

## Close or resume

Operator approval recorded. The task is removed from the forward plan; `video-restore` is
`shipped`. Plan counts after: 8 open (6 agent, 2 human). The next eligible agent task is
`research-cloud-target-apis`. Remaining human actions are `provide-google-drive-test-account` and
`provide-sharepoint-test-tenant`.
