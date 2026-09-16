# Refactor Repository Layout

## Task and scope

- Id / capability / checkpoint: `refactor-repository-layout` / `project-foundation` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending)
- Source: ad hoc operator request on 2026-09-13, branch `ag-01-stage-1` at `4a05e16`
  (after [0007](0007-safety-implement-write-ahead-log-and-recovery.md)): "Refactor to improve
  code quality: (1) Follow the Go standard for project layout
  https://github.com/golang-standards/project-layout, but create only the necessary directories;
  (2) Do not mix Go code and shell scripts, as in tools. Collect all shell code in the scripts
  directory; (3) Keep Makefile small as entry point. See
  https://github.com/volod/arxiv-int/blob/main/Makefile for an example; (4) Keep the code agent
  md files and development documentation circle without changes."
- Plan counts at start: 28 open (25 agent, 3 human); next agent task
  `implement-disk-space-preflight`.
- Accepted task (ad hoc, no plan block):

```markdown
#### refactor-repository-layout

Separate Go tools from shell, keep the root Makefile as a small entry point, and add only the
layout directories this repository needs.

- Serves: `project-foundation` -- [Repository layout](../../openspec/architecture.md#repository-layout)
- Agent status: CLEAR
- Task kind: refactor
- Dependencies: [Repository and agent harness](0001-foundation-bootstrap-repository-and-agent-harness.md);
  [Environment file and setup](0004-foundation-add-env-file-and-setup.md).
- User-visible outcome: Contributors run the same Make targets; `tools/` holds only Go, shell
  lives in `scripts/`, and the root Makefile includes `make/*.mk`.
- Scope boundary: Move `tools/fetch-ffmpeg.sh` to `scripts/`; split Makefile recipes into
  `make/`; document the tree on the architecture and current-state pages; update the remaining
  plan path for the ffmpeg fetcher. Do not add unused layout directories (`pkg/`, `configs/`,
  `build/`, empty `test/`). Do not change `AGENTS.md`, `CLAUDE.md`, `GEMINI.md`, `.cursor/rules/`,
  or `docs/guide/`. No `arxgo` behavior change.
- Data and artifact paths: `Makefile`, `make/`, `scripts/fetch-ffmpeg.sh`, `tools/plancheck/`,
  `packaging/ffmpeg.lock`, `docs/openspec/architecture.md`,
  `docs/impl/current/project-foundation.md`, `docs/impl/plan.md`.
- Execution path: `make help`, `make -n ffmpeg`, `make ci`.
- Acceptance gates: Root Makefile is includes plus help; `tools/` contains only Go;
  `make -n ffmpeg` invokes `scripts/fetch-ffmpeg.sh`; agent instruction files and `docs/guide/`
  are unchanged; `make ci` passes.
- Documentation target: `docs/impl/current/project-foundation.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

No Go packages changed. Make targets and `arxgo` behavior are the same.

- Root `Makefile` matches the arxiv-int entry-point pattern: `PROJECT_ROOT`, includes, default
  `help` via `make/help.awk`.
- Recipes: `make/config.mk` (variables, including lazy `HOST_EXE`), `make/build.mk` (`setup`,
  `env`, `build`, `build-all`, `ffmpeg`, `clean`), `make/quality.mk` (tests, format, vet,
  coverage, `ci`, planning checks). `go run ./tools/plancheck` is unchanged.
- `tools/fetch-ffmpeg.sh` moved to `scripts/fetch-ffmpeg.sh` (repo-root lookup still
  `dirname/..`). `packaging/ffmpeg.lock` comment updated. `tools/plancheck` stays the only
  supporting Go tool.
- [Architecture](../../openspec/architecture.md#repository-layout) lists `scripts/` and `make/`.
  Remaining plan task `implement-release-bundle-with-ffmpeg` now names `scripts/fetch-ffmpeg.sh`.

Decisions and rejected alternatives:

- Only `scripts/` and `make/` were added. `pkg/` is for public libraries; `configs/` would move
  `.env.example`; `build/` would move `packaging/ffmpeg.lock`; empty `test/` is unused. The
  development guide names `.env.example` and `packaging/ffmpeg.lock`.
- Make recipes stayed in `make/*.mk`; only the standalone ffmpeg program moved to `scripts/`, as
  in arxiv-int.
- `plancheck` stays under `tools/` (supporting Go tool), not `cmd/` (shipped applications).
- Agent files and `docs/guide/` were left unchanged per the request. `make ffmpeg` is the
  supported command; the development guide "Runs" cell still shows `bash tools/fetch-ffmpeg.sh`.

Current state: [project foundation](../current/project-foundation.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Root Makefile is includes plus help | `Makefile` (15 lines); `make help` lists Setup, Build, Quality, Planning | pass, Linux |
| `tools/` contains only Go | `ls tools` is `plancheck/` | pass |
| `make -n ffmpeg` invokes the script | `make -n ffmpeg` prints `/bin/bash .../scripts/fetch-ffmpeg.sh bin linux/amd64 windows/amd64`; `bash -n scripts/fetch-ffmpeg.sh` | pass |
| Agent and guide docs unchanged | `git diff HEAD -- AGENTS.md CLAUDE.md GEMINI.md docs/guide .cursor` empty | pass |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |
| Windows CI | `.github/workflows/ci.yml` `windows` job | not-run: not pushed |

## Audit handoff

- `AUD-refactor-repository-layout-1`: nonblocking. `docs/guide/development.md` still documents
  `bash tools/fetch-ffmpeg.sh` as what `make ffmpeg` runs. Left unchanged by operator request.
  Copying that command fails; `make ffmpeg` works. Next check: operator whether to update the
  "Runs" cell. Owner: `review-stage-1-integrity`.

## Close or resume

All Linux gates pass. No plan task existed to remove. The project-foundation current page and
the records index were updated. `project-foundation` stays `shipped`. Plan counts after: 28 open
(25 agent, 3 human); next agent task `implement-disk-space-preflight`.
