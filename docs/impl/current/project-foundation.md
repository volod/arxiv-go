# Project Foundation

Accepted work: [0001 Repository and agent harness](../records/0001-foundation-bootstrap-repository-and-agent-harness.md);
[0003 CLI contract](../records/0003-foundation-implement-cli-contract.md);
[0004 Environment file and setup](../records/0004-foundation-add-env-file-and-setup.md).

## Identity

- Module `github.com/volod/arxiv-go`, Go 1.27. Dependencies: `github.com/joho/godotenv` v1.5.1 and
  `golang.org/x/sys` v0.48.0 ([crash safety](crash-safety.md)).
- Binary `arxgo` built from `cmd/arxgo/main.go`, which only calls `internal/cli.Run`.
- `internal/cli` implements the full [CLI contract](../../openspec/stage-1-core/cli.md). Operations
  still exit 70 (not implemented) after validation. The version string is stamped with
  `-ldflags -X`.
- Package directories `internal/{scanner,archive,state,media,report}` contain only `doc.go` files
  naming the files their plan tasks will add; `internal/fsops` is described in
  [crash safety](crash-safety.md).

## CLI

- One shared flag table (`flagtable.go`) drives one `flag.FlagSet` per operation, the environment
  overrides (`ARXGO_<NAME>`, command line wins, empty ignored, `--exclude` split on the path list
  separator) and `arxgo help [op]` / `arxgo <op> --help`.
- Validation produces typed `ScanOptions`, `SplitOptions` and `RestoreOptions` (`Common` plus
  per-operation fields). It checks enums, sizes (`ParseSize`: B/KB/.../TB and KiB/.../TiB),
  positive durations and counts, `--base-url` (absolute http(s), no credentials, query or
  fragment), `--video-extensions`, `--exclude` globs and `--registry`. All errors are reported
  together and exit 2.
- Root checks are read-only. Roots must be existing directories. A missing split video archive
  with an existing parent sets `SplitOptions.CreateVideoArchive`. Roots must be neither equal nor
  nested after `Abs`, `EvalSymlinks` and, on Windows, case folding.
- Stage-2/3 flags (`--sample*`, `--image*`, `--preview-max-items`, `--publish*`, `--gdrive-*`,
  `--share`, restore `--previews`), `--follow-symlinks=true` and the `publish` operation exit 2
  with `option not available in this build`. This applies to the command line and the environment.
- Optional environment file: `.env` next to the executable (symlinks resolved; `bin/.env` after
  `make setup`), parsed with `godotenv` without touching the process environment. Precedence:
  command line, process environment (non-empty), file, default. Invalid variable names or syntax
  exit 2 naming the file, but `help`, `version` and `--help` still work. Errors about values name
  `ARXGO_X (from <path>)`. Unknown `ARXGO_*` keys log a warning. Values are never logged.
  `TestEnvExampleListsEveryFlag` keeps `.env.example` in step with the flag table.
- `NewLogger` builds a `slog` text or JSON console handler at `--log-level`. `Run` cancels the
  operation context on SIGINT/SIGTERM and restores default handling for a second signal. A
  canceled run that did not finish exits 130.
- Domain packages cannot import `cli`. Operation tasks pass narrow config types mapped from these
  options (see the record's audit notes).

## Build and quality

- `Makefile` provides host and cross builds (`CGO_ENABLED=0`, linux/windows amd64), tests,
  vet, gofmt check, coverage report and `make ci`; see the
  [development guide](../../guide/development.md#make-targets).
- `.github/workflows/ci.yml` runs `make ci` on Ubuntu and vet, tests and a static build on Windows
  (`actions/checkout@v7`, `actions/setup-go@v7`, cache keyed on `go.mod`).
- `HOST_EXE` in the `Makefile` probes `go env GOOS` lazily, so `make ffmpeg` works without Go.
- `make setup` runs `build`, `ffmpeg` and `env` (copy `.env.example` to `bin/.env` with mode 0600
  unless it exists) and prints configure/run instructions; it fails early with guidance when Go is
  not on `PATH`. `make clean` keeps `bin/.env`.

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
