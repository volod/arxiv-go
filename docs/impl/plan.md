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

### CATIA archive -- `catia-archive`

#### repair-replaced-restore-sidecar-cleanup

A restore interrupted between a commit and its sidecar deletion and then replaced by another run
leaves owned video previews behind, and CATIA text cleanup learns the earlier run's intent by
decoding CLI options through the recovery resolver.

- Serves: `catia-archive` -- [Replaced restores](../openspec/stage-4-catia/split-restore.md#replaced-restores)
- Agent status: CLEAR
- Dependencies: [CATIA restore](records/0048-catia-implement-catia-restore.md).
- User-visible outcome: After any interrupted restore, the next restore with sidecar cleanup leaves
  no owned previews or text sidecars of files that earlier run restored, whichever command
  recovered it, and never deletes sidecars a `keep` restore or a later split left.
- Scope boundary: `sidecar_cleanup` in `state.RunOptions` written by `archive.Start` for restore
  runs from the restore configuration; one payload-generic earlier-restore scan in the shared
  sidecar cleanup used by video (with description link refresh) and CATIA restore; replace
  `catiaRestore.deletedDescriptions` and its `RecovererFor` use; current run's resumed commits keep
  the existing path. Routes `AUD-implement-catia-restore-1` and `AUD-implement-catia-restore-2`. No
  change to WAL events, registries or split.
- Data and artifact paths: `internal/state/`, `internal/archive/`, `internal/cli/`,
  `docs/impl/current/catia-archive.md`, `docs/impl/current/media-previews.md`,
  `docs/impl/current/crash-safety.md`.
- Execution path: Generated trees in `t.TempDir()` with ffmpeg-generated previews (skip without
  ffmpeg) and synthetic CATIA sidecars; crash at `wal:commit` and `wal:placed` of a restore, then
  recovery by the other payload and by `--new-run`; archive tests with `RecovererFor` nil; declared
  extra: the scratch kill driver of record 0048 extended with `--previews delete`.
- Acceptance gates: Video previews and CATIA text sidecars of a replaced restore are deleted by the
  next restore with cleanup and kept by one without; a `keep` restore in between and a later split
  keep them; an earlier run without `sidecar_cleanup` deletes nothing; changed sidecars are logged,
  not reported; results identical with `RecovererFor` nil; rerun changes nothing; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.

#### prove-stage-4-on-generated-archive

Prove CATIA split and restore through the built binary the way stage 1 proved video.

- Serves: `catia-archive` -- [Exit criteria](../openspec/stage-4-catia/README.md#exit-criteria)
- Agent status: CLEAR
- Dependencies: `repair-replaced-restore-sidecar-cleanup`.
- User-visible outcome: A generated mixed archive survives killed CATIA split (with `--catia-text`)
  and restore, resume, and a byte-identical round trip, alongside a video split of the same archive
  into a separate video archive.
- Scope boundary: `test/integration` through the built `arxgo`; synthetic CATIA-like files only.
  No experimental-tree content. Linux only as a gate.
- Data and artifact paths: `test/integration/`.
- Execution path: `make test-integration` driving `split --catia --catia-text`, `split` (video),
  `restore --catia` and `restore` with seeded kills.
- Acceptance gates: Round trip manifests match; resume after kill completes; each mirror root and
  payload registry contains only its own payload; `make ci` and `make test-integration` pass.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.

#### review-stage-4-catia

Review CATIA payload reuse, mirror-root separation, registry layout and extraction boundaries before cloud work starts.

- Serves: `catia-archive` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: `prove-stage-4-on-generated-archive`.
- User-visible outcome: Stage 4 is coherent, video default is intact, and stage 3 can start without
  CATIA cloud scope.
- Scope boundary: Payload kind, mirror-root separation and history filter, registry column order,
  WAL text events,
  description marker reuse, extractor memory bounds, experimental-data ban (grep of committed files
  for strings from the gitignored tree). No speculative refactor.
- Data and artifact paths: Stage-4 records, `internal/catia/`, `internal/archive/`.
- Execution path: Invariant-to-evidence table, targeted tests, routed notes.
- Acceptance gates: Notes dispositioned; verdicts recorded; blockers repaired first; `make ci` passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.

### Cloud publishing -- `cloud-publishing`

#### research-cloud-target-apis

Confirm or amend the cloud target design against current Google Drive and Microsoft Graph APIs.

- Serves: `cloud-publishing` -- [Cloud targets](../openspec/stage-3-cloud/cloud-targets.md)
- Agent status: CLEAR
- Research: yes
- Dependencies: `review-stage-4-catia`.
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
- Dependencies: `prove-stage-4-on-generated-archive`.
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
