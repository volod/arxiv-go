# Bootstrap Repository and Agent Harness

## Task and scope

- Id / capability / checkpoint: `bootstrap-repository-and-agent-harness` / `project-foundation` /
  `review-stage-1-integrity`
- State: accepted
- Source: ad hoc operator request (2026-09-13) on the initial commit `c155c76` (LICENSE, README,
  .gitignore only). Harness layout modeled on `volod/arxiv-int` (AGENTS.md, tool adapters,
  specification registry, forward plan, task records).
- Plan counts at start: no plan existed.
- Accepted task:

```markdown
#### bootstrap-repository-and-agent-harness

Create the Go repository setup, agent instruction files, a specification organized as a tree of
delivery stages, and a forward plan so implementation can start immediately.

- Serves: `project-foundation` -- [Specification](../../openspec/spec.md)
- Agent status: CLEAR
- Dependencies: none.
- User-visible outcome: Contributors and agents find one canonical rule file, a staged
  specification (stage 1: CSV registry, video split and restore without previews; stage 2: stage 1
  plus simple media previews; stage 3: Google Drive and SharePoint integration), and an ordered
  task plan whose integrity is machine-checked.
- Scope boundary: Go module and compiling `arxgo` scaffold following the requested layout
  (`cmd/arxgo`, `internal/{cli,scanner,archive,state,media,report}`, `docs/openspec`), Makefile,
  CI, AGENTS.md with CLAUDE.md/GEMINI.md/Cursor adapters, specification tree, architecture, plan,
  planning workflow, record template and planning lint tooling. No archive operation behavior.
- Data and artifact paths: repository root files, `cmd/`, `internal/`, `tools/plancheck/`,
  `docs/`, `.github/workflows/ci.yml`.
- Execution path: `make ci`, `make plan-status`.
- Acceptance gates: `make ci` passes (gofmt, vet, tests, static cross builds, spec/plan lint, doc
  links); every planned capability has open tasks; the next eligible task is reported.
- Documentation target: `docs/impl/current/project-foundation.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

- Module `github.com/volod/arxiv-go` on Go 1.27; binary `arxgo`. `internal/cli/root.go` fixes the
  operation names, `version`/`help`, and the exit-code table; operations return 70 until
  implemented. Other packages hold `doc.go` only, naming the files their tasks add. An
  `internal/fsops` package was added beyond the requested layout so platform-specific syscalls have
  one owner.
- Specification lives under `docs/openspec/` (the requested location) rather than `docs/design/`
  as in `arxiv-int`; it plays the same role: `spec.md` holds purpose, principles, the capability
  registry and cross-cutting rules, and links stage pages.
- Requirement interpretations recorded in the spec (confirm or amend through the planning
  workflow):
  - The registry adds an `is_large` column before `metadata` to express "highlight files larger
    than a specified size"; the other columns are exactly as requested.
  - "Type of video examples by location" is interpreted as the position inside the video (start,
    middle, end, series).
  - Stubs are `<video file name>.md` at the original location; previews are placed next to the
    stub in the main archive for local viewing; registries are written to both roots.
  - `--metadata media` requires ffprobe at startup (exit 3 with a platform download link) even if
    only MP4 files would be found, so a run never fails halfway.
  - Split never overwrites; restore overwrites only with `--overwrite`; restore skips missing
    directories unless `--create-dirs`.
  - Crash safety uses a JSONL write-ahead log per transaction plus atomic checkpoints; the WAL is
    authoritative for file placement.
- `crash-safety` precedes `archive-registry` in the registry because scan resume uses the
  checkpoint primitives.
- `tools/plancheck` implements `lint-spec-plan`, `lint-doc-links` and `plan-status` with tests in
  `internal/devtools/planning`.
- Current page: [Project foundation](../current/project-foundation.md).

### Operator change: amd64-only CI, local-only ffmpeg tests (2026-09-13)

Made by the operator after acceptance and reviewed here; the accepted task text is unchanged
because its scope names neither ARM targets nor ffmpeg in CI.

- ARM build targets were removed. `PLATFORMS` in the `Makefile` is `linux/amd64 windows/amd64`;
  the [spec](../../openspec/spec.md#platforms-and-build), development guide, README and current
  page name only those two targets. No `arm64`, `aarch64` or `darwin` references remain.
- ffmpeg/ffprobe installation was removed from `.github/workflows/ci.yml`. Neither the
  `ubuntu-latest` job (`make ci`) nor the `windows-latest` job (vet, test, native build) installs
  them. The [development guide](../../guide/development.md#ci), the
  [development integrity](../../openspec/spec.md#development-integrity) rules, the
  [metadata acceptance](../../openspec/stage-1-core/metadata.md) section and the
  [stage-2 exit criteria](../../openspec/stage-2-previews/README.md#exit-criteria) and
  [acceptance](../../openspec/stage-2-previews/previews.md#acceptance) now state that live
  ffmpeg/ffprobe tests skip in GitHub CI and run only on a local machine with the tools on `PATH`.
- Consequence: live media evidence for `implement-ffprobe-metadata`, `implement-ffmpeg-runner`,
  `implement-video-samples`, `implement-frame-images` and
  `integrate-previews-into-split-and-restore` is local Linux evidence only. Pure-Go parts (captured
  ffprobe JSON, fake-ffmpeg scripts, preview planning, missing-tool exit 3) still run in CI on both
  operating systems.
- Rejected alternative: keeping a CI ffmpeg install step. It adds a network download and
  third-party action or package-manager dependency to every run for tests that stage 1 does not
  need.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Formatting, vet, unit tests | `make ci` (fmt-check, vet, test) | pass on Linux with Go 1.27.1 |
| Static cross builds | `make build-all` within `make ci` | pass: linux/amd64, windows/amd64 |
| Planning integrity | `make lint-spec-plan`, `make lint-doc-links` within `make ci` | pass |
| Next eligible task reported | `make plan-status` | 33 open (29 agent, 4 human); next `implement-cli-contract` |
| Windows CI job | `.github/workflows/ci.yml` | not-run: workflow added, not yet executed on GitHub |
| Operator change: amd64-only builds | `make ci` after the change (Go 1.27.1, Linux) | pass: `build-all` emits only `arxgo-linux-amd64` and `arxgo-windows-amd64.exe`; lint and links ok |
| Operator change: no ffmpeg in CI | review of `ci.yml` and docs; `grep -rniE 'arm64\|aarch\|ffmpeg'` | pass: no install step; docs consistently say live ffmpeg tests are local-only. No ffmpeg-backed tests exist yet, so skip behavior is not-run |
| Plan counts after the change | `make plan-status` | unchanged: 33 open (29 agent, 4 human); next `implement-cli-contract` |

## Audit handoff

- `AUD-bootstrap-repository-and-agent-harness-1`: nonblocking. The requirement interpretations
  listed above are specification decisions made without operator confirmation. Owner:
  `implement-cli-contract` for flag names and defaults; `review-stage-1-integrity` for the rest.
- `AUD-bootstrap-repository-and-agent-harness-2`: nonblocking. CI action versions are
  unverified until the first push. Owner:
  `implement-cli-contract` (first task that relies on Windows CI evidence).
- `AUD-bootstrap-repository-and-agent-harness-3`: nonblocking. With ffmpeg gone from CI, nothing
  ever fails when a live media test is skipped, so a broken tool lookup or skip helper could make
  local runs skip silently too and still look green. Next check: the shared skip helper accepts an
  opt-in (for example an environment variable) that turns a missing tool into a failure, and media
  task records cite a local run showing the live tests ran rather than skipped. Owner:
  `implement-ffprobe-metadata` (first task with a live tool test).
- `AUD-bootstrap-repository-and-agent-harness-4`: nonblocking. `implement-release-bundle-with-ffmpeg`
  still scopes a "CI release job on tags" (`.github/workflows/release.yml`) that downloads pinned
  ffmpeg builds on GitHub. That is packaging, not testing, so it does not contradict this change,
  but it does bring ffmpeg downloads back into GitHub Actions. Confirm it is intended or rescope it
  to a local `make dist`. Owner: `implement-release-bundle-with-ffmpeg`, decided together with
  `approve-ffmpeg-distribution`.

## Close or resume

All local gates passed, including after the 2026-09-13 operator change (amd64-only builds,
local-only ffmpeg tests). Next action: `implement-cli-contract`. Capability `project-foundation`
remains `planned` until that task is accepted.
