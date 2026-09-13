# Planning Workflow

[AGENTS.md](../../AGENTS.md) gives the normal task cycle. Read only the section needed here.

## Task shape

Before adding or changing capability scope, follow [capability changes](#capability-changes).
Agent tasks use a stable slug under their capability group and all fields below:

```markdown
### Capability name -- `capability-id`

#### stable-task-id

State the unresolved operator problem in one or two sentences.

- Serves: `capability-id` -- [Owning spec section](../openspec/stage-1-core/page.md#section)
- Agent status: CLEAR
- Dependencies: Open task ids in backticks, or accepted record links; `none.` when there are none.
- User-visible outcome: What becomes possible or trustworthy for the operator.
- Scope boundary: Included and excluded behavior.
- Data and artifact paths: Repository-relative paths and outputs.
- Execution path: Packages, fixtures, commands and declared runs.
- Acceptance gates: Checks, acceptance signal and valid negative result.
- Documentation target: Narrow current-state page.
- Review checkpoint: Owning checkpoint task id, or `none; this is the bounded checkpoint.`
```

Optional fields, placed after `Agent status`: `Task kind: refactor | checkpoint` and
`Research: yes` (a supported negative result is valid; it does not replace acceptance gates).

Human tasks use:

```markdown
#### stable-task-id

- Serves: `capability-id` -- [Owning spec section](...)
- Human status: HUMAN-GATED
- Dependencies: Open task ids or record links, or `none.`
- Requested input or decision: What the human provides or decides, and how to inspect it.
- Unblocks: Backticked task ids that wait for it.
```

Multi-line fields continue on indented lines. `make lint-spec-plan` parses these fields, so keep
the `- Field name:` prefix exact.

## Task lanes

| Lane | Status | Acceptance needs |
| --- | --- | --- |
| Agent Implementation Tasks | `CLEAR` | Local code, tests and docs |
| Agent Implementation Tasks | `RUN NEEDED` | A declared heavier run (network download, live service, other OS) |
| Human-Assisted Tasks | `BLOCKED BY HUMAN` | Human-provided input or access (credentials, data) |
| Human-Assisted Tasks | `HUMAN-GATED` | Human judgment, authorization or licensing decision |

An agent task may depend on a human task; it stays ineligible until the human decision is recorded
as an accepted record. Agents never self-approve a human task.

## Ordering and stages

Capability groups appear in [registry](../openspec/spec.md#capability-registry) order in both
lanes. Within a group: prerequisites first, cheap deterministic work before expensive runs,
checkpoint and proof last. A stage's first tasks depend on the previous stage's proof, so stages
are strictly sequential while tasks inside a stage may run in parallel when their dependencies
allow.

Dependencies must resolve to open tasks or existing records, with no cycles. `make plan-status`
reports open task counts, the next eligible agent task and every other task that is eligible in
parallel.

## Capability changes

1. State the operator problem; amend the owning stage page with behavior, exclusions, evaluation
   and acceptance signal. Correct a misshaped capability before adding code.
2. For a new capability, add a registry row marked `planned` in the right stage position and add a
   group row to the [record group table](#record-file-naming), then add tasks in registry order.
3. Every task serves its group; every planned capability has open work. When all of a capability's
   tasks are accepted, mark it `shipped` and link its current-state page.

Never put completion notes, dates, measurements or history in `plan.md`.

## Durable task records

Copy the [template](../impl/records/template.md) at task start into a sequenced file and add it to
the [index](../impl/records/README.md). Keep the full original task text and any amendments after
the task leaves the plan. Records retain evidence and decisions; current pages describe available
behavior and link records. Future actions belong in the plan with one owner each.

### Record file naming

Filenames are `NNNN-<group>-<task-id>.md`.

1. **Sequence** -- four digits, `max(existing) + 1`; never reused or renumbered.
2. **Group** -- the abbreviation of the owning capability from the table below.
   `make lint-spec-plan` rejects unknown groups.
3. **Task id** -- the unchanged plan slug; the record's first scope line must name it.

| Capability id | Group abbrev |
| --- | --- |
| `project-foundation` | `foundation` |
| `crash-safety` | `safety` |
| `archive-registry` | `registry` |
| `media-metadata` | `metadata` |
| `video-split` | `split` |
| `video-restore` | `restore` |
| `media-previews` | `preview` |
| `cloud-publishing` | `cloud` |
| `governance` (instructions, audits, workflow) | `govern` |

## Audit notes and checkpoints

Self-review every task. Record `none identified` or evidence-bearing notes `AUD-<task-id>-N` in the
record. A concern outside task scope gets exactly one owner: a blocking repair task added to the
plan before dependent work, or the named checkpoint for nonblocking concerns. Do not broaden the
current task.

A checkpoint reads producer records, affected code and tests, and routed notes; records an
invariant-to-evidence table, every note's disposition, a refactor/no-refactor verdict, and
`proceed`, `proceed-with-nonblocking-notes` or `blocked`. A blocker keeps the checkpoint open until
its repair passes. Checkpoints add tests for important stabilized behavior; "existing tests already
cover it" is a valid conclusion. Coverage percentages are never a signal.

## Completion transition

Before removing a task from the plan: map every acceptance gate to evidence in the record, route
every note, link the record from the current-state page, replace references to the removed task id
in the plan with the record link, and update the registry status when the capability is complete.
Failed acceptance leaves the task open with partial results and the next action recorded.
