# Implement CLI Contract

## Task and scope

- Id / capability / checkpoint: `implement-cli-contract` / `project-foundation` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending, see audit handoff)
- Source: plan task `implement-cli-contract`, operator request on 2026-09-13 ("implement, run, fix,
  improve implementation and then update documentation and plan.md"). Branch `ag-01-stage-1`
  at `441ae1b`. The only dirty file at start was `README.md` (an operator title fix, left
  untouched).
- Plan counts at start: 32 open (29 agent, 3 human); next agent task `implement-cli-contract`.
- Accepted task:

```markdown
#### implement-cli-contract

Replace the scaffold dispatcher with the full, validated operation and flag contract so every later
task receives typed options instead of parsing flags.

- Serves: `project-foundation` -- [CLI contract](../../openspec/stage-1-core/cli.md)
- Agent status: CLEAR
- Dependencies: [Repository and agent harness](0001-foundation-bootstrap-repository-and-agent-harness.md).
- User-visible outcome: `arxgo help [op]`, `arxgo version` and every stage-1 flag parse and validate
  with the specified defaults, environment overrides, size/duration parsing and exit codes; stage-2
  and stage-3 flags are reserved and rejected with exit 2.
- Scope boundary: Parsing, validation, `Options` types, exit-code mapping, `slog` console logger
  construction and signal-to-context wiring. Operations still return exit 70. No filesystem writes;
  root existence/nesting checks are read-only.
- Data and artifact paths: `internal/cli/`, `cmd/arxgo/main.go`.
- Execution path: One `flag.FlagSet` per operation built from a shared table; `Options` struct per
  operation; `ParseSize`, enum and URL validators; root nesting check with symlink resolution and
  Windows case folding; `signal.NotifyContext` in `Run`.
- Acceptance gates: Table tests for every flag default, env override precedence, invalid enum/size/
  duration/URL, missing required roots, equal and nested roots (including case-variant on Windows
  CI), reserved stage-2/3 flags, and exit codes 0/2/70. `make ci` passes on Linux; CI passes on
  Windows.
- Documentation target: `docs/impl/current/project-foundation.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. The [CLI contract](../../openspec/stage-1-core/cli.md) gained
  clarifications for behavior the task had to decide: parsing details, size and duration rules,
  and value validation for `--base-url`, `--video-extensions`, `--exclude` and `--registry`.

## Implementation

`internal/cli` (`cmd/arxgo/main.go` is unchanged and only calls `cli.Run`):

- `flagtable.go`: the shared flag table, one row per flag per operation, with group, stage,
  argument placeholder, usage text and binding. It is the only place that lists flags. Help
  output, flag sets and environment overrides all come from it.
- `flags.go`: builds one `flag.FlagSet` per operation from the table and parses it. It then
  applies `ARXGO_<NAME>` values for flags not given on the command line. Empty values are ignored;
  `--exclude` values are split on the path list separator. Parse errors use `--name` spelling.
  Stage-2/3 flags bind to `reservedValue`, so giving them on the command line or in the
  environment exits 2 with `option not available in this build`, not an unknown-flag error.
- `values.go`: `ParseSize` and `Size` (a `flag.Value`, printed with the largest exact binary
  unit), the enum value and the repeatable list value. Parsing uses `math/big` to avoid overflow
  and float rounding.
- `options.go`: `Common`, `ScanSettings`, `ScanOptions`, `SplitOptions`, `RestoreOptions` and
  the validators for durations, counts, globs, base URL, extensions and registry path. Errors are
  collected with `errors.Join`, so the operator sees all of them at once.
- `roots.go`: read-only root checks. Roots must be existing directories; a missing split video
  archive is accepted when its parent exists and is reported as `SplitOptions.CreateVideoArchive`.
  Roots must be neither equal nor nested, compared after `filepath.Abs` and `EvalSymlinks` (the
  parent is resolved for a missing root) and case-folded on Windows. The filesystem view is
  injectable so tests can check case folding on Linux.
- `logging.go`: `NewLogger` builds a `slog` text or JSON console handler at the chosen level.
- `root.go`: dispatch. `help [op]`, `<op> --help`, `version`, reserved `publish`, unknown
  operations, and a `Handlers` table. Each handler of this build logs `operation not implemented
  in this build` and returns 70. `Run` wraps `signal.NotifyContext(os.Interrupt, SIGTERM)` and
  restores default handling after the first signal, so a second signal ends the process. A
  handler that returns non-zero after the context is canceled maps to exit 130.
- `help.go`: general usage and per-operation help, with defaults, environment names and the
  reserved later-stage flags.

Decisions:

- Options types live in `internal/cli`, as the task specifies. Domain packages cannot import
  `cli`, because `cli` imports them. Later tasks should define narrow config structs in their own
  packages, and `cli` should map `Options` onto them. See `AUD-implement-cli-contract-2`.
- `--follow-symlinks=true` is rejected as not available, because stage 1 never follows symlinks.
  `false` is accepted.
- Interrupt mapping keeps `ExitOK` when a handler finished its work even though the context was
  canceled. Any other code after cancellation becomes 130.
- `Makefile`: `HOST_EXE` is now recursively expanded and silences `go env`. `make ffmpeg` no longer
  runs or prints a `go` probe when Go is missing. This resolves `AUD-approve-ffmpeg-distribution-3`,
  which was routed to this task.
- Rejected: a third-party flag library (not in the dependency table); failing `scan` when
  `--video-archive` is set (breaks a shared environment used for split); dropping reserved flags
  entirely (the operator would get "flag provided but not defined" instead of the specified
  message).

Current state: [project foundation](../current/project-foundation.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Every flag default | `TestDefaults` (full `ScanOptions`/`SplitOptions`/`RestoreOptions` equality) | pass, Linux |
| Every stage-1 flag parses | `TestEveryFlagParses` (all split flags plus all restore flags set) | pass, Linux |
| Env override precedence | `TestEnvironmentOverrides`: env replaces defaults, command line wins, empty ignored, other-operation env ignored, invalid env names the variable | pass, Linux |
| Invalid enum/size/duration/URL | `TestParseErrors`, `TestValueValidation`, `TestParseSize` (30 cases), `TestValidateBaseURL`, `TestParseExtensions`, `TestValidateGlob` | pass, Linux |
| Missing required roots | `TestRootValidation` (scan/split/restore, missing, not a directory, missing parent) | pass, Linux; also checks that validation creates nothing |
| Equal and nested roots | `TestRootValidation` (both directions, prefix sibling, `/./` cleaning, missing root inside the archive), `TestRootNestingThroughSymlinks`, `TestWithin` | pass, Linux |
| Case-variant roots on Windows | `TestRootCaseFolding` (injected case-insensitive view) | pass on Linux. `TestRootCaseVariantOnWindows` (real filesystem) skips on Linux and is not-run until Windows CI runs |
| Reserved stage-2/3 flags | `TestReservedFlagsRejected` (every stage-2/3 table row, flag and env; `--follow-symlinks`) and `publish` in `TestRunExitCodes` | pass, Linux |
| Exit codes 0/2/70 (+130) | `TestRunExitCodes` (21 cases), `TestRunCanceledContextExitsInterrupted`, `TestRunSignalCancelsContext` (real SIGINT to the test process) | pass, Linux; the signal test skips on Windows |
| Logger | `TestRunPassesOptionsAndLogger` (JSON console, debug options line), `TestRunLogLevelFiltersConsole` | pass, Linux |
| Race and repeat | `CGO_ENABLED=1 go test -race -count=5 ./internal/cli/` | pass, Linux |
| Windows compile | `GOOS=windows go vet ./internal/cli/`; `GOOS=windows go test -c ./internal/cli`; `make build-all` | pass (cross-compiled only) |
| Built binary smoke | `make build`; `bin/arxgo` with no args, scan, nested split, split with a missing video root, `--previews`, `publish`, `ARXGO_SAMPLE`, bad `--min-free`, JSON env logging | pass: exits 2/70/0 as specified; the missing video root was not created |
| `make ci` on Linux | `make ci` (Go 1.27.1 at `/usr/local/go/bin`) | pass |
| CI on Windows | `.github/workflows/ci.yml` `windows` job | not-run: changes are not pushed |

## Audit handoff

- `AUD-implement-cli-contract-1`: nonblocking. The Windows gates are still pending:
  `TestRootCaseVariantOnWindows` and the `windows-latest` job have not run. `EvalSymlinks` on UNC
  paths and mapped drives is also untested. Next check: the Windows CI result after push, plus a
  UNC root in the stage-1 proof. Owner: `review-stage-1-integrity`.
- `AUD-implement-cli-contract-2`: nonblocking. `cli.Options` types cannot be imported by domain
  packages (import cycle). Next check: the scan, split and restore tasks define their own config
  types, and `cli` maps onto them, without moving flag parsing out of `cli`. Owner:
  `review-stage-1-integrity`.
- `AUD-implement-cli-contract-3`: nonblocking. `--exclude` treats `\` as a `path.Match` escape on
  every platform, so a Windows operator who types `cache\*` gets a pattern that matches nothing.
  Next check: when the walker lands, decide whether to reject or translate backslashes on Windows.
  Owner: `implement-directory-walker`.
- `AUD-bootstrap-repository-and-agent-harness-1` (incoming, flag names and defaults): resolved.
  Every flag name and default in the contract is implemented unchanged and checked by
  `TestDefaults`. `arxgo help <op>` lists them for operator review. The added parsing details are
  clarifications, not changes. The remaining interpretations stay with `review-stage-1-integrity`.
- `AUD-bootstrap-repository-and-agent-harness-2` (incoming, CI action versions): resolved. Run
  `34746828028` (`main`, head `2219a7b`) passed the `linux` and `windows` jobs with
  `actions/checkout@v7` and `actions/setup-go@v7`. Its log has no Node 20 deprecation or cache
  restore warning.
- `AUD-approve-ffmpeg-distribution-3` (incoming): resolved here by the `HOST_EXE` change.
  `make -n ffmpeg` with Go absent from `PATH` prints no `go` error.

## Close or resume

All Linux gates pass. Windows CI stays pending and is owned by `review-stage-1-integrity`. The task
was removed from the plan, and dependents now link this record. The project foundation page and
the records index were updated. Capability `project-foundation` is marked `shipped`, since this was
its last open task. Plan counts after: 31 open (28 agent, 3 human).
