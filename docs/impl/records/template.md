# Task Record

Copy to `NNNN-<group>-<task-id>.md` using the
[record naming rules](../../guide/planning-workflow.md#record-file-naming), then index it.
Keep evidence concise; keep accepted task text complete.

## Task and scope

- Id / capability / checkpoint: `task-id` / `capability-id` / `checkpoint-id`
- State: active, blocked, or accepted only after required gates pass.
- Source: plan task id or ad hoc request; code revision and relevant dirty scope at start.
- Plan counts at start: open tasks (agent, human) and the next eligible task.
- Accepted task: paste the full original block verbatim in a fenced Markdown block.
- Amendments: none, or each full revised block with reason and authorization; keep the original.

## Implementation

Changed packages and behavior, reused code, decisions and rejected alternatives, compatibility
effects, limitations, and the current-state page link.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Every acceptance gate | Reproducible evidence | pass, fail, not-run, blocked, valid-negative |

Distinguish generated fixtures from real runs, and Linux from Windows evidence. Missing, mocked or
stale evidence does not prove acceptance.

## Audit handoff

`none identified` with the reviewed scope, or one entry per `AUD-<task-id>-N`:

- Observation; blocking or nonblocking; location, invariant, evidence and impact.
- Next check; exactly one owner task or checkpoint; disposition.

For checkpoints: invariant-to-evidence table, every incoming note's disposition, refactor verdict
and proceed / proceed-with-nonblocking-notes / blocked decision.

## Close or resume

Passed and remaining gates, next action, current-page and index updates, plan counts after, and
capability status changes.
