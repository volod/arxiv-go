# Add Environment File and Setup

## Task and scope

- Id / capability / checkpoint: `add-env-file-and-setup` / `project-foundation` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending)
- Source: ad hoc operator request on 2026-09-13, branch `ag-01-stage-1` on top of the
  uncommitted `implement-cli-contract` work ([0003](0003-foundation-implement-cli-contract.md)):
  "Add a .env file using, e.g., https://github.com/joho/godotenv to provide a simple, standard
  mechanism for defining command-line parameters and credentials via environment variables. In
  this way, we also simplify management for 'run needed' proof tasks. Provide .env.example,
  update the make setup target to copy .env.example into .env, print instructions on how to set
  up, and run make diff so that the result is a single command that sets up the proper env to
  run. In the future, we will distribute binary artifacts, so the logical path for .env is
  bin/.env". Clarified by the operator: "make diff" means `make ffmpeg`. The `.env` file is
  optional, and a regular run needs only flags or environment variables. `make dist` is
  future work.
- Plan counts at start: 31 open (28 agent, 3 human); next agent task
  `implement-filesystem-primitives`.
- Accepted task (ad hoc, no plan block):

```markdown
#### add-env-file-and-setup

Let operators and declared runs keep arxgo settings and credentials in one optional file next to
the binary, and create a ready-to-run bin/ with one command.

- Serves: `project-foundation` -- [Environment file](../../openspec/stage-1-core/cli.md#environment-file)
- Agent status: CLEAR
- Dependencies: [CLI contract](0003-foundation-implement-cli-contract.md).
- User-visible outcome: `make setup` builds arxgo, downloads ffmpeg/ffprobe, creates `bin/.env`
  from `.env.example` and prints how to configure and run; arxgo reads the optional `.env` next
  to its executable with lower precedence than flags and the process environment.
- Scope boundary: `godotenv` dependency and spec amendment, file lookup and precedence, error and
  logging rules, `.env.example`, `setup`/`env` Make targets, `clean` preserving `bin/.env`, docs
  and plan notes for proof and release tasks. No `make dist` (owned by
  `implement-release-bundle-with-ffmpeg`), no credential variables (owned by the cloud target
  tasks).
- Data and artifact paths: `internal/cli/envfile.go`, `internal/cli/root.go`, `.env.example`,
  `Makefile`, `go.mod`, `go.sum`, docs.
- Execution path: Table tests with temporary `.env` files; `make setup` run on the host.
- Acceptance gates: Precedence command line > process env > file > default; missing file is a
  no-op; malformed file exits 2 naming the file; process environment unchanged; secrets not
  logged; `.env.example` lists every flag and sets nothing; `make setup` idempotent and never
  overwrites `bin/.env`; `make ci` passes.
- Documentation target: `docs/impl/current/project-foundation.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: spec first. The [dependency table](../../openspec/spec.md#dependencies) now lists
  `github.com/joho/godotenv`, and the [CLI contract](../../openspec/stage-1-core/cli.md#environment-file)
  gained the environment file section and the precedence line. Architecture, the stage-3 security
  note, the development guide, the README and the plan were adjusted to match.

## Implementation

- Dependency: `github.com/joho/godotenv v1.5.1` (MIT, pure Go, no transitive modules). `go.mod` and
  `go.sum` change together. `bin/arxgo` is still `statically linked`.
- `internal/cli/envfile.go`:
  - `readEnvFile` uses `godotenv.Parse` (never `Load`), so the process environment and child
    processes stay untouched. A missing file yields no values.
  - godotenv silently accepts a line without `=` (empty key) and keys with spaces, so every key is
    checked against `[A-Za-z_][A-Za-z0-9_]*` and a bad name is an error.
  - `layeredLookup` returns process env (non-empty) then file values, with a source label
    `ARXGO_X (from <path>)` that flows into value and reserved-flag errors.
  - `unknownArxgoKeys` finds misspelled `ARXGO_*` names.
  - `defaultEnvFile` returns `.env` in the directory of `os.Executable()`, symlinks resolved.
- `internal/cli/root.go`: operations read the file before parsing flags. A file error exits 2,
  except when the arguments ask for `--help`; `help` and `version` never read the file. A debug
  line reports the path and variable count. Unknown `ARXGO_*` keys log a warning. No values are
  logged.
- `internal/cli/flags.go`: `parseFlags` takes a `lookupFunc` that reports where each value came
  from.
- `.env.example`: every flag of the table as a commented line with its default, grouped like the
  help output, including stage-2/3 names marked as not available. It also explains precedence,
  quoting (double quotes eat Windows backslashes) and relative paths, and has a placeholder
  comment for stage-3 credentials (names deliberately not invented).
- `Makefile`:
  - `setup` = `check-go`, then `build`, `ffmpeg` and `env`, then printed configure/run
    instructions.
  - `env` copies `.env.example` to `bin/.env` with mode 0600 only when missing.
  - `check-go` fails with install and PATH guidance.
  - `clean` removes `bin/` contents except `.env`, so local credentials survive.
- Plan: the release bundle ships `.env.example` and never `.env`. The stage-1 proof builds its
  binary in a temporary directory and scrubs `ARXGO_*`. The cloud proof and the human credential
  tasks name `bin/.env` as a delivery location.
- Decisions and rejected alternatives:
  - `godotenv.Load`: it mutates the process environment, leaks values to ffmpeg, and makes tests
    order-dependent.
  - A `.env` in the current working directory: the operator chose the executable directory
    because it matches the future distribution layout. A cwd lookup would also let a random
    directory change behavior.
  - Failing on unknown `ARXGO_*` keys: future credential names would break older binaries, so a
    warning is used instead.
  - A key diff target: the operator clarified that "diff" meant `make ffmpeg`. `make env` prints a
    hint to compare with `.env.example`, and a unit test catches missing template lines.

Current state: [project foundation](../current/project-foundation.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Precedence | `TestLayeredLookupPrecedence`; `TestRunWithEnvFile/process_environment_beats_file,_command_line_beats_both`, `.../file_supplies_mandatory_roots` | pass, Linux |
| Syntax (comments, `export`, quotes, Windows and UNC paths, `${VAR}`, CRLF) | `TestReadEnvFile/dotenv_syntax` | pass, Linux |
| Missing file no-op; malformed exits 2 naming the file | `TestReadEnvFile` (4 malformed variants, directory); `TestRunWithEnvFile/malformed_file_exits_2` | pass, Linux |
| Errors name the file; reserved flags rejected from the file | `.../invalid_value_names_the_file`, `.../reserved_flag_in_file` | pass, Linux |
| Help works with a broken file | `.../help_and_version_work_with_a_broken_file` (`help`, `version`, `help split`, `split --help`, `-h`) | pass, Linux |
| Secrets not logged; typo warning | `.../unknown_ARXGO_variable_warns,_secrets_are_not_logged` | pass, Linux |
| Process environment unchanged | `.../process_environment_is_not_modified` | pass, Linux |
| Template complete and inert | `TestEnvExampleListsEveryFlag` | pass |
| Real binary | `bin/arxgo split` run from `/` with roots in `bin/.env`; via a symlink from another directory; `ARXGO_LOG_LEVEL=error` process override; broken file | pass: exits 70/70/70/2; debug line `environment file loaded ... variables=4`; typo warning |
| `make setup` | fresh run (created `bin/.env`, mode 600); second run (`keep existing bin/.env`); without Go on `PATH` | pass: about 0.7 s with ffmpeg cached; idempotent; `check-go` guidance and exit 2 |
| `make clean` keeps `bin/.env` | `make clean; ls -A bin` | pass: only `.env` left; `make setup` restored binaries and ffmpeg |
| Static binary | `file bin/arxgo` | pass: `statically linked` |
| `make ci` on Linux | `make ci` | pass |
| Windows | CI `windows` job; `make setup` under Git Bash | not-run: not pushed; no Windows host |

## Audit handoff

- `AUD-add-env-file-and-setup-1`: nonblocking. Mode 0600 has no effect on Windows, where
  `bin/.env` inherits the directory ACL, and `make setup` on Git Bash has not been run. Next check:
  Windows CI plus a Git Bash `make setup`. Decide with the stage-3 token-cache ACL work whether
  `arxgo` should warn about a world-readable `.env` that holds credentials. Owner:
  `review-stage-3-cloud`.
- `AUD-add-env-file-and-setup-2`: nonblocking. Once proof and cloud tasks add credential keys, the
  `slog` redaction handler must also cover values that came from the file. Owner:
  `implement-cloud-target-interface`.

## Close or resume

All Linux gates pass; Windows evidence is pending with the open notes above. No plan task existed
to remove. Plan notes were added to `prove-stage-1-on-generated-archive`,
`implement-release-bundle-with-ffmpeg`, `prove-cloud-targets-on-test-accounts` and the two
credential human tasks. `project-foundation` stays `shipped`. Plan counts after: 31 open (28 agent,
3 human); next agent task `implement-filesystem-primitives`.
