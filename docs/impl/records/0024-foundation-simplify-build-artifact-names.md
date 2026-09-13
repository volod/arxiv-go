# Simplify Build Artifact Names

## Task and scope

- Id / capability / checkpoint: `simplify-build-artifact-names` / `project-foundation` / none.
- State: accepted.
- Source: ad hoc operator request; working tree clean at start, HEAD `84f054d`.
- Plan counts at start: 17 open tasks (14 agent, 3 human); next eligible agent task
  `prove-stage-1-on-generated-archive`.
- Accepted task (original request):

```text
In the bin directory, we have build artifacts arxgo from "make build", and arxgo-linux-amd64, arxgo-windows-amd64.exe from "make build-all". The arxgo and arxgo-linux-amd64 are the same binary. Please refactor the build target; both "make build" and  "make build-all" commands should build a Linux executable named arxgo, and the Windows version naming should be arxgo.exe
We do not plan to build a utility for additional architectures, so this naming will be clear to users. Refactor and add the task to the records.
```

- Amendments: none. This is a bounded request for the shipped `project-foundation` capability;
  the forward plan remains reserved for unresolved work.

## Implementation

`make build` now sets `CGO_ENABLED=0 GOOS=linux GOARCH=amd64`, creates `bin/` when absent, and
writes `bin/arxgo`. `make build-all` depends on `build` rather than rebuilding Linux in a loop,
then cross-compiles Windows amd64 to `bin/arxgo.exe`. The only supported platform pair remains
Linux/Windows amd64; `PLATFORMS` still serves `make ffmpeg`. The existing version stamp and
stripping flags are shared.

`make setup` now calls `build-all` so it continues to create a runnable Windows binary; its
printed examples select the host executable through the existing lazy `HOST_EXE` variable.
Updated the specification, README, development guide and Windows verification scenario. Current
behavior is recorded on [project foundation](../current/project-foundation.md). The recipes no
longer produce the platform-suffixed names; they do not delete old files from a user's `bin/`.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Linux output from both targets | `make build`, then `make build-all`; `file bin/arxgo`; `sha256sum bin/arxgo` before and after | pass: static, stripped Linux amd64 ELF; SHA-256 unchanged (`cd0e36cc...06b30`) |
| Clean output directory and exact names | `make BIN_DIR=<empty temp dir>/bin build`, then `make BIN_DIR=<temp dir>/bin build-all`; compare SHA-256; `find` | pass: directory created; Linux hash unchanged; exactly `arxgo` and `arxgo.exe` |
| Windows output naming and cross-build | `make build-all`; `file bin/arxgo.exe`; `make vet-windows` in `make ci` | pass: Windows amd64 PE executable; cross-compiled and vetted on Linux only |
| Setup dependency | `make -n setup` | pass: expands `build-all` and host-specific example; full setup not run because it downloads ffmpeg |
| Required CI and docs gates | `make ci`, `make lint-spec-plan`, `make lint-doc-links` | pass on Linux |

## Audit handoff

`none identified` after reviewing the changed recipes, output names, setup dependency,
documentation and build artifacts. Windows host execution remains in the deferred
[Windows verification scenario](../../guide/windows-verification.md).

## Close or resume

All gates passed. The record is indexed and linked from the current-state page. This ad hoc
repair adds no forward-plan task, so counts remain 17 open (14 agent, 3 human); next eligible
agent task is `prove-stage-1-on-generated-archive`. `project-foundation` remains shipped.
