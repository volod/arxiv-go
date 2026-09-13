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
| [Project foundation](current/project-foundation.md) | Module, `arxgo` scaffold, Make targets, CI, planning tooling | Scaffold only; operations exit 70 |

No operation (`scan`, `split`, `restore`) is implemented yet. The next work is reported by
`make plan-status`.
