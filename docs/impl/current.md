# Current Implementation

This index describes behavior available now. For product intent read the
[specification](../openspec/spec.md); for work that remains read the [forward plan](plan.md).

## Documentation shape

Each capability gets one page under `current/` when its first task is accepted. When a page grows,
it becomes a short index and focused topics move under `current/<capability>/`. New pages are
linked here in the same change.

## Areas

| Area | Owns | State |
| --- | --- | --- |
| [Project foundation](current/project-foundation.md) | Module, CLI contract (flags, `.env` file, validation, exit codes, logger, signals), `make setup`, Make targets, CI, planning tooling | Shipped; operations validate options, run the lock/checkpoint session, then exit 70 |
| [Crash safety](current/crash-safety.md) | Filesystem primitives; run lock, `.arxgo/` layout, checkpoints, run log, progress, report | In progress; session used by every operation; WAL and preflight remain |

`arxgo help [op]`, `arxgo version` and full flag validation work. `scan`, `split` and `restore`
take the run lock, write `options.json`, `checkpoint.json`, `run.log.jsonl` and `report.json`, then
exit 70 (operation bodies are not in this build). The next work is reported by
`make plan-status`.
