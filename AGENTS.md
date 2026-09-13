# Project rules

Canonical rules for all agents and contributors. Tool-specific instruction files link here; do not
duplicate rules. Read only the selected task, the relevant code, and the linked specification and
current-state sections. Load other guidance when the condition under "Read when needed" applies.

## Project

`arxgo` is a single static Go executable that builds a CSV registry of a file archive, moves video
files into a mirrored video archive with Markdown metadata stubs, and restores them (stage 1); adds
ffmpeg-based previews (stage 2); and publishes to Google Drive or SharePoint (stage 3). Start with
the [specification](docs/openspec/spec.md) and the [architecture](docs/openspec/architecture.md).

## Guardrails

- Preserve unrelated work. Diagnostic or review requests do not authorize code changes.
- Do not commit, push, rewrite history or revert user changes unless explicitly asked.
- Go 1.27+, module `github.com/volod/arxiv-go`, `CGO_ENABLED=0`. The shipped binary must stay a
  single static executable for Linux and Windows; keep `make build-all` green.
- Add only dependencies listed in the [dependency table](docs/openspec/spec.md#dependencies); they
  must be pure Go. Otherwise amend the spec first. Commit `go.mod` and `go.sum` together.
- `cmd/arxgo/main.go` only calls `cli.Run`. Flags and exit codes live in `internal/cli`; behavior
  lives in the package the [architecture](docs/openspec/architecture.md#dependency-direction)
  assigns. Respect its dependency direction; platform code only in build-tagged files.
- Never mutate an archive outside a write-ahead-logged transaction. Never overwrite or delete a
  user file that the spec does not explicitly allow. Honor [reserved paths](docs/openspec/spec.md#reserved-paths).
- External tools run only through `internal/media` with `exec.CommandContext` and argument vectors,
  never a shell.
- Use `log/slog` in domain code, not `fmt.Print*`. Use ASCII in code, logs and docs. Never
  hardcode machine-specific paths or put secrets in code, logs, fixtures or docs.
- Aim for files of at most about 300 lines; split at real seams.

## Task cycle

1. Run `make plan-status`; select one eligible task and note the task counts. Check its
   dependencies and the records they link. Do not start blocked work; do not start a stage before
   the previous stage's proof is accepted.
2. Create `docs/impl/records/NNNN-<group>-<task-id>.md` from the
   [record template](docs/impl/records/template.md) using the
   [naming rules](docs/guide/planning-workflow.md#record-file-naming); paste the full task text; add
   the record to its index. Identify affected packages and reusable code; do not silently broaden
   scope.
3. Implement and self-review. Tests cover integrity, correctness and business logic: the happy
   path, the main corner cases, and a failing regression for every bug fixed. Tests are
   deterministic, network-free, build fixtures in `t.TempDir()`, and never snapshot incidental
   implementation details. Tests that need ffmpeg skip with a reason when it is missing.
4. Verify: relevant tests and `make ci` are required. Record failures and unrun checks honestly
   (for example Windows-only behavior not run locally). Fix causes; never weaken a gate silently.
   Coverage is diagnostic, never a gate. Failed acceptance keeps the task open.
5. Before stopping, update the record with evidence, decisions, audit notes and the next action.
   On acceptance: update or create the narrow `docs/impl/current/` page and link the record;
   replace references to the task id in the plan with the record link; remove the task from
   `docs/impl/plan.md`; mark the capability `shipped` in the registry when its last task is done.
   Run `make lint-spec-plan` and `make lint-doc-links`; report task counts before and after, and the
   next eligible task. Inspect `git status` and remove temporary files.

## Read when needed

- **Adding or amending task scope or capabilities:** the
  [planning workflow](docs/guide/planning-workflow.md#task-shape). Specify behavior and evaluation
  in the stage page before planning or coding. `docs/impl/plan.md` holds only remaining work.
- **Concerns outside the task or checkpoint tasks:** the
  [audit and checkpoint rules](docs/guide/planning-workflow.md#audit-notes-and-checkpoints). Route
  each concern to one owner; plan blocking repairs before dependent work continues.
- **Human-assisted tasks:** agents prepare inputs and report what is needed; they never mark a human
  task done. See [task lanes](docs/guide/planning-workflow.md#task-lanes).
- **Toolchain, Make targets or CI changes:** the [development guide](docs/guide/development.md).
- **File formats:** the [data contracts](docs/openspec/stage-1-core/contracts.md); a format change
  is a spec amendment.
