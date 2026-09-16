# arxiv-go Implementation Plan

Forward-only: this file contains only work that remains. Product behavior, boundaries and
evaluation belong in [the specification](../openspec/spec.md). Task shape, statuses, ordering and
lifecycle rules belong in the [planning workflow](../guide/planning-workflow.md). Available behavior
and accepted results belong in [current-state documentation](current.md).

Stages: tasks for `crash-safety` through `video-restore` complete
[stage 1](../openspec/stage-1-core/README.md); `media-previews` delivers
[stage 2](../openspec/stage-2-previews/README.md); `catia-archive` delivers
[stage 4](../openspec/stage-4-catia/README.md) after stage 2; `cloud-publishing` delivers
[stage 3](../openspec/stage-3-cloud/README.md) after the stage-4 checkpoint.

All tasks: tests are deterministic, network-free and build fixtures in `t.TempDir()`; media
fixtures are generated, never committed; CATIA fixtures are synthetic files with invented names
and planted format markers, never copied from gitignored experimental trees; each new dependency is
the one approved in the [dependency table](../openspec/spec.md#dependencies) and enters
`go.mod`/`go.sum` in the same change; `make ci` must pass.

Test gates: every acceptance gate, declared run and CI job below runs on Linux (amd64) only.
Implementation still covers the Windows specifics in the specification; for Windows the gate is
`make build-all` and `make vet-windows` (both in `make ci`). Windows-only tests may be added and
skip on Linux, but they are not acceptance evidence. Runtime checks on a Windows host belong to the
deferred [Windows verification scenario](../guide/windows-verification.md), which is outside this
plan: no task waits for it, and Windows-only audit notes are routed there.

## Agent Implementation Tasks

### Archive registry -- `archive-registry`

#### stabilize-registry-columns

A registry's header depends on what the archive currently holds, so the same tree yields different
schemas from one command to the next and a downstream loader breaks.

- Serves: `archive-registry` -- [Column stability](../openspec/stage-1-core/registry.md#column-stability)
- Agent status: CLEAR
- Dependencies: [Stage-4 checkpoint](records/0051-catia-review-stage-4-catia.md).
- User-visible outcome: `--registry-columns full` writes every canonical column of every registry
  on every run, so a spreadsheet, database or search index sees one stable schema.
- Scope boundary: The flag, its validation and plumbing into the three CSV writers, and the
  `previews` / `text_rel_path` asymmetry between the payload registries. No column order, value or
  row change; no new columns.
- Data and artifact paths: `internal/report/csv_compact.go`, `internal/report/videos.go`,
  `internal/report/catia_registry.go`, `internal/cli/`.
- Execution path: Table tests over the three writers in both modes; a CLI test for the invalid
  value; a scan-then-split fixture comparing headers.
- Acceptance gates: In `full` mode a scan before and after a split of the same tree writes
  byte-identical headers, and a payload registry has the same header with and without
  `--verify hash`; `compact` reproduces the current output byte for byte; an invalid mode exits 2;
  `make ci` passes.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-registry-and-metadata`.

### CATIA archive -- `catia-archive`

#### record-source-location-in-metadata

A description or text sidecar harvested into a search index no longer sits next to its file, and
nothing inside it says which archive the path belongs to; a sidecar says nothing about the file
beyond its path.

- Serves: `catia-archive` -- [Self-locating metadata](../openspec/stage-4-catia/catia.md#self-locating-metadata)
- Agent status: CLEAR
- Dependencies: [Stage-4 checkpoint](records/0051-catia-review-stage-4-catia.md).
- User-visible outcome: Every description and text sidecar names its archive root, and each sidecar
  carries the identity block of its description, so a harvested document stands alone.
- Scope boundary: The shared description renderer and the sidecar renderer, the archive root passed
  into them, and the sidecar cap accounting. Both payloads share the renderer, so the video
  description gains `archive:` in the same change. No marker-line change, no new registry column,
  no rewrite of descriptions earlier runs wrote.
- Data and artifact paths: `internal/report/description.go`, `internal/report/catia.go`,
  `internal/archive/split_description.go`, `internal/archive/text.go`.
- Execution path: Renderer table tests; a CATIA split fixture comparing sidecar and description
  fields; a cap test whose identity block alone exceeds 1 MiB.
- Acceptance gates: `archive:` is the second line of both files; a hash-verified split writes the
  same `sha256`, `file_size` and `catia` values in sidecar and description; an oversized identity
  block yields `truncated: true` with no blocks; `restore --descriptions delete` still removes
  both; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-registry-and-metadata`.

#### implement-catia-text-index

Extracted CATIA text lands in one sidecar per file; reading or feeding a whole archive at once
means assembling them in the shell by guessing sidecar names.

- Serves: `catia-archive` -- [Text index](../openspec/stage-4-catia/catia.md#text-index)
- Agent status: CLEAR
- Dependencies: `record-source-location-in-metadata`.
- User-visible outcome: `arxgo catia-index` writes one Markdown document of every moved CATIA
  file's metadata, properties and components, with harvested strings only on request.
- Scope boundary: A read-only operation reading the CATIA run history, owned descriptions and owned
  sidecars; `--out` and `--strings`; the reserved default output path. No extraction, no write to a
  description, sidecar or registry, no other output format.
- Data and artifact paths: `internal/archive/catia_index.go`, `internal/cli/`,
  `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md`.
- Execution path: Fixture archive split with `--catia-text`, then the index built and compared;
  a deleted sidecar covering the `Missing text` section.
- Acceptance gates: Section count equals the `moved` rows of `arxgo-catia.csv`; a missing sidecar is
  listed once and not skipped silently; `--strings` is the only difference between the two outputs;
  a rerun is byte-identical; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-registry-and-metadata`.

#### review-registry-and-metadata

Review registry schema stability and the metadata a detached reader sees before cloud work starts.

- Serves: `catia-archive` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: `stabilize-registry-columns`; `record-source-location-in-metadata`;
  `implement-catia-text-index`.
- User-visible outcome: Registry schemas and metadata documents are coherent across commands and
  payloads, and stage 3 can start without reopening them.
- Scope boundary: Column mode plumbing, payload registry symmetry, description and sidecar field
  order and escaping, index correctness against the run history, cap accounting. No speculative
  refactor.
- Data and artifact paths: The records of the three tasks above, `internal/report/`,
  `internal/archive/`.
- Execution path: Invariant-to-evidence table, targeted tests, routed notes.
- Acceptance gates: Notes dispositioned; verdicts recorded; blockers repaired first; `make ci`
  passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.

### Cloud publishing -- `cloud-publishing`

#### research-cloud-target-apis

Confirm or amend the cloud target design against current Google Drive and Microsoft Graph APIs.

- Serves: `cloud-publishing` -- [Cloud targets](../openspec/stage-3-cloud/cloud-targets.md)
- Agent status: CLEAR
- Research: yes
- Dependencies: [Stage-4 checkpoint](records/0051-catia-review-stage-4-catia.md).
- User-visible outcome: The stage-3 specification reflects verified upload, resume, hash, auth and
  quota behavior, and dependency choices keep a static single binary.
- Scope boundary: Documentation research, small throwaway probes outside the repository, spec
  amendment and task refinement. No production code.
- Data and artifact paths: `docs/openspec/stage-3-cloud/cloud-targets.md`, `docs/impl/plan.md`.
- Execution path: Read current API references; compare REST-only versus SDK binary size with a
  scratch build; record findings in the task record.
- Acceptance gates: Every design assertion in the cloud spec is confirmed with a source link or
  amended; dependency decision recorded; a supported negative result (for example SDK required) is
  valid.
- Documentation target: `docs/openspec/stage-3-cloud/cloud-targets.md`
- Review checkpoint: `review-stage-3-cloud`.

#### implement-cloud-target-interface

Add the target interface, publish WAL steps, link rewrite and the `publish` operation.

- Serves: `cloud-publishing` -- [Target interface](../openspec/stage-3-cloud/cloud-targets.md#target-interface)
- Agent status: CLEAR
- Dependencies: `research-cloud-target-apis`.
- User-visible outcome: `arxgo publish` and `split --publish` drive any target resumably and rewrite
  description and registry links.
- Scope boundary: `internal/cloud` interface, in-memory fake target, session store in run state,
  publish recovery, link rewrite, secret redaction handler, CLI enablement. No real provider.
- Data and artifact paths: `internal/cloud/`, `internal/archive/publish.go`, `internal/cli/`.
- Execution path: Fake target with injectable failures between chunks.
- Acceptance gates: Resume after chunk failure, expired session restart, idempotent re-publish,
  links rewritten atomically, secrets absent from all outputs.
- Documentation target: `docs/impl/current/cloud-publishing.md`
- Review checkpoint: `review-stage-3-cloud`.

#### implement-google-drive-target

Publish the video archive to Google Drive.

- Serves: `cloud-publishing` -- [Google Drive](../openspec/stage-3-cloud/cloud-targets.md#google-drive)
- Agent status: CLEAR
- Dependencies: `implement-cloud-target-interface`.
- User-visible outcome: `--publish gdrive` mirrors folders, uploads resumably and records
  `webViewLink`s.
- Scope boundary: REST client, auth flows, folder cache, resumable upload, MD5 idempotency,
  backoff, sharing option. Tested against recorded fixtures only.
- Data and artifact paths: `internal/cloud/gdrive/`, `test/testdata/cloud/gdrive/`.
- Execution path: `httptest.Server` replaying sanitized exchanges.
- Acceptance gates: Fixture scenarios from the cloud spec pass; no network in tests.
- Documentation target: `docs/impl/current/cloud-publishing.md`
- Review checkpoint: `review-stage-3-cloud`.

#### implement-sharepoint-target

Publish the video archive to a SharePoint document library.

- Serves: `cloud-publishing` -- [Microsoft SharePoint](../openspec/stage-3-cloud/cloud-targets.md#microsoft-sharepoint)
- Agent status: CLEAR
- Dependencies: `implement-cloud-target-interface`.
- User-visible outcome: `--publish sharepoint` mirrors folders, uploads through upload sessions and
  records `webUrl`s.
- Scope boundary: Graph REST client, client-credential and device-code auth, folder creation,
  upload sessions, quickXorHash idempotency, throttling, sharing option. Recorded fixtures only.
- Data and artifact paths: `internal/cloud/sharepoint/`, `test/testdata/cloud/sharepoint/`.
- Execution path: `httptest.Server` replaying sanitized exchanges; quickXorHash test vectors.
- Acceptance gates: Fixture scenarios pass; quickXorHash matches published vectors; no network in
  tests.
- Documentation target: `docs/impl/current/cloud-publishing.md`
- Review checkpoint: `review-stage-3-cloud`.

#### prove-cloud-targets-on-test-accounts

Publish a generated video archive to real test accounts for both providers.

- Serves: `cloud-publishing` -- [Acceptance](../openspec/stage-3-cloud/cloud-targets.md#acceptance)
- Agent status: RUN NEEDED
- Dependencies: `implement-google-drive-target`; `implement-sharepoint-target`;
  `provide-google-drive-test-account`; `provide-sharepoint-test-tenant`.
- User-visible outcome: Publishing is proven against the real services, including an interrupted
  upload resumed to completion.
- Scope boundary: Declared live run with operator-provided credentials from environment variables
  or the [environment file](../openspec/stage-1-core/cli.md#environment-file) `bin/.env`, whose
  credential keys are added to `.env.example` by the target tasks; evidence (logs with secrets
  redacted, remote listing) kept outside the repository.
- Data and artifact paths: `test/integration/cloud_test.go` (build tag `cloudlive`), run evidence
  under the operator's chosen directory.
- Execution path: `go test -tags cloudlive ./test/integration/...` on Linux with credentials
  present.
- Acceptance gates: Uploaded sizes and hashes match; interrupted upload resumes; re-publish uploads
  nothing; links open for the test account.
- Documentation target: `docs/impl/current/cloud-publishing.md`
- Review checkpoint: `review-stage-3-cloud`.

#### review-stage-3-cloud

Review cloud publishing security, resume and link invariants.

- Serves: `cloud-publishing` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: `prove-cloud-targets-on-test-accounts`.
- User-visible outcome: Stage 3 is coherent, secrets are contained and the binary remains static.
- Scope boundary: Publish WAL/recovery, secret redaction, token cache permissions, binary size and
  `CGO_ENABLED=0` build, link rewrite consistency. No speculative refactor.
- Data and artifact paths: Stage-3 records, `internal/cloud/`.
- Execution path: Invariant-to-evidence table, targeted tests, routed notes.
- Acceptance gates: Notes dispositioned; verdicts recorded; blockers repaired first; `make ci`
  passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.

## Human-Assisted Tasks

### CATIA archive -- `catia-archive`

#### approve-stage-4-on-operator-catia-copy

- Serves: `catia-archive` -- [Success criteria](../openspec/spec.md#success-criteria)
- Human status: HUMAN-GATED
- Dependencies: [Stage-4 proof](records/0050-catia-prove-stage-4-on-generated-archive.md).
- Requested input or decision: Run `split --catia --catia-text`, interrupt, resume and
  `restore --catia` on a disposable copy of the experimental CATIA tree; review descriptions,
  sidecars (usefulness of `strings:`, and whether user ids or workstation paths are acceptable in
  them), logs and timings; accept, or file defects. Records keep aggregate counts only.
- Unblocks: Production use of stage 4. Stage-3 development does not wait for this decision.

### Cloud publishing -- `cloud-publishing`

#### provide-google-drive-test-account

Provide a Google Drive test location and credentials for the live proof.

- Serves: `cloud-publishing` -- [Google Drive](../openspec/stage-3-cloud/cloud-targets.md#google-drive)
- Human status: BLOCKED BY HUMAN
- Dependencies: none.
- Requested input or decision: A test Google account or Shared Drive, a service account or OAuth
  client, and the environment variable names under which the credentials will be supplied
  (placed in the operator's `bin/.env` or process environment, never in the repository).
- Unblocks: `prove-cloud-targets-on-test-accounts`.

#### provide-sharepoint-test-tenant

Provide a SharePoint site, document library and Entra ID app registration for the live proof.

- Serves: `cloud-publishing` -- [Microsoft SharePoint](../openspec/stage-3-cloud/cloud-targets.md#microsoft-sharepoint)
- Human status: BLOCKED BY HUMAN
- Dependencies: none.
- Requested input or decision: Tenant id, site and drive ids, app registration with
  `Sites.Selected` or `Files.ReadWrite.All` consent, and the credential delivery method (for
  example the operator's `bin/.env`).
- Unblocks: `prove-cloud-targets-on-test-accounts`.
