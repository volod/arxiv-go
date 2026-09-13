# Centralize Test Layout

## Task and scope

- Id / capability / checkpoint: `centralize-test-layout` / `project-foundation` / none
- State: accepted.
- Source: ad hoc user request; working tree clean at start. The next eligible planned task is `implement-video-split-transactions`.
- Plan counts at start: 21 open (18 agent, 3 human).
- Accepted task:

```text
Analyze tests and fixtures, and move integration tests, end-to-end tests, external test applications, and mock data into the root-level /test directory, following https://github.com/golang-standards/project-layout/tree/master/test. Document these requirements.
```

- Execution scope: Move the existing cross-package crash/recovery proof, reusable test-only mock transaction code, shared MP4 builder, captured ffprobe JSON, and CSV golden data to `test/`; update references and test-layout documentation. Keep package-local tests that require private hooks beside their packages. There are no current binary end-to-end test applications to move; the planned stage-1 proof already targets `test/integration/`.

## Implementation

Moved the crash/recovery integration proof from `internal/state/crashtest/crash_test.go` to
`test/integration/crash_test.go`. Its reusable fake split/restore transaction and crash hook now
live in `test/fixtures/crashtest/`; the MP4 builder lives in `test/fixtures/testmp4/`. Seven
captured ffprobe JSON files and the scan CSV golden file now live in `test/testdata/`, with their
package-test paths updated. All eight static data files are byte-identical to their previous
tracked versions.

Unit and white-box component tests remain package-local, including package-level scan, CLI and
ffprobe tests that need private hooks. The repository currently has no standalone external test
application or binary end-to-end proof to move; `test/apps/` defines the future location, and the
existing stage-1 proof task already specifies `test/integration/`. Future cloud mock-data paths in
the plan now point to `test/testdata/cloud/`. The [test layout](../../../test/README.md),
[specification](../../openspec/spec.md#development-integrity),
[architecture](../../openspec/architecture.md#repository-layout) and
[development guide](../../guide/development.md#conventions) document the placement rule. The
[project foundation](../current/project-foundation.md) and
[crash-safety](../current/crash-safety.md) pages describe the resulting state.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Moved tests and fixtures remain functional | `GOCACHE=/tmp/arxiv-go-gocache make test-integration` | pass, Linux; crash/recovery proof under `test/integration/` |
| Static fixture preservation | SHA-256 comparison of each new `test/testdata/` file with `git show HEAD:<old path>` | pass, eight files byte-identical |
| Required project gates | `GOCACHE=/tmp/arxiv-go-gocache make ci` | pass, Linux; includes `go test ./...`, Windows cross-build and Windows vet |
| Moved integration package Windows type check | `GOCACHE=/tmp/arxiv-go-gocache GOOS=windows GOARCH=amd64 go vet -tags integration ./test/integration/...` | pass, cross-compiled only; Windows runtime not run |
| Documentation and plan links | `GOCACHE=/tmp/arxiv-go-gocache make lint-spec-plan lint-doc-links` | pass |

## Audit handoff

None identified. Reviewed imports to ensure production packages do not depend on `test/`,
remaining package-local tests for private-hook use, the complete moved-file inventory, static
fixture bytes, plan references, and `git diff --check`.

## Close or resume

All gates passed. The record is indexed and linked from the narrow current-state pages. The
planned work count remains 21 open tasks (18 agent, 3 human); the next eligible task remains
`implement-video-split-transactions`. `project-foundation` remains shipped. No commit or push was
made.
