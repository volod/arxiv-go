# Project Foundation

Accepted work: [0001 Repository and agent harness](../records/0001-foundation-bootstrap-repository-and-agent-harness.md).

## Identity

- Module `github.com/volod/arxiv-go`, Go 1.27, no third-party dependencies yet.
- Binary `arxgo` built from `cmd/arxgo/main.go`, which only calls `internal/cli.Run`.
- `internal/cli/root.go` fixes the operation names (`scan` default, `split`, `restore`), the
  `version` and `help` commands, and the exit-code constants from the
  [CLI contract](../../openspec/stage-1-core/cli.md#exit-codes). Operations currently exit 70
  (not implemented). The version string is stamped with `-ldflags -X`.
- Package directories `internal/{scanner,archive,state,media,report,fsops}` contain only `doc.go`
  files naming the files their plan tasks will add.

## Build and quality

- `Makefile` provides host and cross builds (`CGO_ENABLED=0`, linux/windows amd64), tests,
  vet, gofmt check, coverage report and `make ci`; see the
  [development guide](../../guide/development.md#make-targets).
- `.github/workflows/ci.yml` runs `make ci` on Ubuntu and tests plus cross builds on Windows.

## Planning tooling

`tools/plancheck` (backed by `internal/devtools/planning`, never linked into `arxgo`) implements:

- `make lint-spec-plan`: registry rows parse with valid statuses and record groups; plan groups
  follow registry order per lane; tasks carry every required field and a valid status; `Serves`
  matches the group; dependencies resolve to open tasks or existing records; no cycles; planned
  capabilities have open tasks and shipped ones do not; record names use known groups, name their
  task id, are indexed, and are not still planned.
- `make lint-doc-links`: relative links and GitHub-style heading anchors resolve in every Markdown
  file outside hidden and build directories; fenced code is ignored.
- `make plan-status`: open agent/human task counts, the next eligible agent task in plan order,
  other tasks eligible in parallel, and human tasks that can be acted on.
