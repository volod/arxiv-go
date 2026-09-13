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
| [Project foundation](current/project-foundation.md) | Module, CLI contract (flags, `.env` file, validation, exit codes, logger, signals), `make setup`, Make targets, CI, planning tooling | Shipped; operations validate options, then exit 70 |
| [Crash safety](current/crash-safety.md) | Filesystem primitives (`internal/fsops`): device and free-space queries, durable verified copy, no-replace rename, atomic write | In progress; primitives available to later tasks, not yet used by an operation |

`arxgo help [op]`, `arxgo version` and full flag validation work. No operation (`scan`, `split`,
`restore`) is implemented yet. The next work is reported by
`make plan-status`.
