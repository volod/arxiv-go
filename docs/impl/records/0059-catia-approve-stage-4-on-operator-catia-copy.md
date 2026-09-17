# Approve stage 4 on an operator CATIA copy

## Task and scope

- Id / capability / checkpoint: `approve-stage-4-on-operator-catia-copy` / `catia-archive` / none
- State: accepted (operator decision, 2026-09-17).
- Source: plan task `approve-stage-4-on-operator-catia-copy` (Human-Assisted, HUMAN-GATED).
  Operator request in this session: "I have approve @docs/impl/plan.md ####
  approve-stage-4-on-operator-catia-copy. Please update records and finalise implementation of
  phase 4". Code revision `1ba5b0a` with the uncommitted work of
  [0058](0058-catia-extract-catia-notes-and-properties.md) in the tree.
- Plan counts at start: 9 open tasks (6 agent, 3 human); next eligible agent task
  `research-cloud-target-apis`.
- Accepted task:

```markdown
#### approve-stage-4-on-operator-catia-copy

- Serves: `catia-archive` -- [Success criteria](../openspec/spec.md#success-criteria)
- Human status: HUMAN-GATED
- Dependencies: [Stage-4 proof](records/0050-catia-prove-stage-4-on-generated-archive.md);
  [Registry and metadata checkpoint](records/0057-catia-review-registry-and-metadata.md);
  [CATIA notes and properties](records/0058-catia-extract-catia-notes-and-properties.md).
- Requested input or decision: Run `split --catia --catia-text`, interrupt, resume and
  `restore --catia` on a disposable copy of the experimental CATIA tree; review descriptions,
  sidecars (usefulness of the properties and `notes:`, whether the names they carry are acceptable,
  and whether repeated title-block captions need removing), logs and timings; accept, or file
  defects. The operator judged the harvested `strings:` not useful; the checkpoint's question on
  workstation paths in `strings:` is answered by
  [CATIA notes and properties](records/0058-catia-extract-catia-notes-and-properties.md), which
  removes them and routes two notes here (repeated title-block captions, format-pattern limits). The checkpoint prepared the file registry and the CATIA text index of such a run
  outside the repository and routed the question on directory modification times. Records keep
  aggregate counts only.
- Unblocks: Production use of stage 4. Stage-3 development does not wait for this decision.
```

- Amendments: none.

## Decision

The operator decided; the agent recorded the decision, closed the routed notes and did not mark the
task done before this approval.

- Stage 4 (`split --catia`, `--catia-text`, `catia-index`, `restore --catia`) is fit for
  production use.
- Sidecar content is accepted as specified by
  [0058](0058-catia-extract-catia-notes-and-properties.md): product properties, material and
  `notes:`, including the names they carry. The operator did not ask for repeated title-block
  captions to be removed, so no index-level deduplication is planned.
- Directory modification times are not restored by a round trip, and no spec promises them. The
  operator did not ask for a change; the specification stays as it is.
- No defects were filed with the approval, and no spec amendment was requested.

## Implementation

No production code in this record. Capability `catia-archive` is `shipped` in the
[capability registry](../../openspec/spec.md#capability-registry); current-state page:
[CATIA archive](../current/catia-archive.md).

## Acceptance evidence

The operator's own trial outputs stayed outside the repository and were not given to the agent;
only the decision is recorded. Agent-side evidence on disposable copies of the operator archive is
in the linked records, which keep aggregates only.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Operator decision | this session: approve `approve-stage-4-on-operator-catia-copy` | pass: recorded |
| Split, interrupt, resume and restore on an archive copy | agent runs of the built binary in [0051](0051-catia-review-stage-4-catia.md) and [0057](0057-catia-review-registry-and-metadata.md) (killed and resumed CATIA split with `--catia-text`, killed and resumed CATIA restore, byte-identical file manifest); the operator's run | pass on agent evidence; the operator's run details are not recorded |
| Descriptions and sidecars review | [0057](0057-catia-review-registry-and-metadata.md) review input; [0058](0058-catia-extract-catia-notes-and-properties.md) real-data extraction and binary run | pass: accepted by the operator |
| Generated-archive proof | `make test-integration` ([0050](0050-catia-prove-stage-4-on-generated-archive.md), rerun in 0058) | pass on Linux |
| Final tree | `make ci` | see close |
| Plan and doc lint | `make lint-spec-plan`; `make lint-doc-links` | see close |

Windows: cross-compiled and vetted only; runtime checks remain in the deferred
[Windows verification scenario](../../guide/windows-verification.md).

## Audit handoff

Incoming notes, closed here:

- `AUD-prove-stage-4-on-generated-archive-2` (V5 documents with `0 components`): answered by
  [0051](0051-catia-review-stage-4-catia.md) (a property of parts, not an extraction failure);
  accepted. Closed.
- `AUD-review-stage-4-catia-4` (`strings:` carries format boilerplate): superseded by
  [0058](0058-catia-extract-catia-notes-and-properties.md), which removes the harvest. Closed.
- `AUD-review-stage-4-catia-5` (`--metadata file` leaves non-ISO video media columns empty):
  specified behavior; the manuals recommend `--metadata media`. Accepted. Closed.
- `AUD-review-registry-and-metadata-2` (directory modification times after a round trip): the
  operator accepted without asking for them; no spec change. Closed.
- `AUD-review-registry-and-metadata-3` (workstation paths in harvested strings): no sidecar written
  since 0058 carries harvested strings; earlier sidecars are rewritten by the manual recipe.
  Closed.
- `AUD-extract-catia-notes-and-properties-1` (repeated title-block captions and names in notes):
  accepted as specified; no removal requested. Closed.
- `AUD-extract-catia-notes-and-properties-2` (properties and material rely on byte patterns): the
  measured limits (about 10 of 2447 materials are format keywords, 5 files without a product run)
  are accepted. Closed.

Notes that stay with other owners: `AUD-review-registry-and-metadata-1` with
`review-stage-3-cloud`; Windows-only notes with
[Windows verification](../../guide/windows-verification.md) W10.

None identified in this decision beyond those closures.

## Close or resume

Operator approval recorded. The task is removed from the forward plan, the CATIA group leaves the
Human-Assisted lane, and `catia-archive` is `shipped`; current-state pages and the records index
are updated. `make ci` passes on the final tree (fmt, vet including `GOOS=windows`, tests,
`build-all`, `lint-spec-plan`, `lint-doc-links`). Plan counts after: 8 open tasks (6 agent,
2 human); next eligible agent task `research-cloud-target-apis`. Remaining human actions are
`provide-google-drive-test-account` and `provide-sharepoint-test-tenant`.
