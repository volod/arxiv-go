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

#### generalize-payload-split-restore

The split and restore executor is video-named end to end and replays every run's WAL, so CATIA would
otherwise copy the pipeline or leak into the video registry.

- Serves: `catia-archive` -- [Run history by payload](../openspec/stage-4-catia/split-restore.md#run-history-by-payload)
- Agent status: CLEAR
- Task kind: refactor
- Dependencies: [Stage-2 review](records/0033-preview-review-stage-2-previews.md).
- User-visible outcome: Video split and restore behave as today except for the new
  `arxgo-videos.csv` column order; the executor takes a payload (kind plus mirror root, only `video`
  reachable) that selects candidates, payload registry name and columns, counters, history filter,
  description renderer and post-commit sidecar hook without a second copy of transfer or recovery.
- Scope boundary: Payload type; executor, lock, preflight and history code name the mirror root
  generically instead of video archive; required `payload` in `options.json` and in the rebuilt
  recovery resolver; `readHistory` consumers filtered by payload (video registry, preview index,
  restore candidates, directory cleanup); `ScanConfig` candidate predicate replacing the `IsVideo`
  check; the shared payload registry columns 1-10 in the
  [video registry order](../openspec/stage-1-core/contracts.md#video-registry-csv); the preview WAL
  begin/finish/part-file mechanism made reusable for another event family. No CLI flags, no CATIA
  behavior, no compatibility with run directories or registries written before this task.
- Data and artifact paths: `internal/archive/`, `internal/state/`, `internal/report/`,
  `internal/cli/session.go`, `test/integration/`.
- Execution path: Existing video unit and integration tests; a new test that a run directory whose
  `options.json` names another payload is excluded from video registry replay.
- Acceptance gates: Video tests pass with only column-order expectations updated; foreign-payload
  history is ignored by video replay; `make ci` and `make test-integration` pass.
- Documentation target: `docs/impl/current/catia-archive.md`; column references in
  `docs/impl/current/video-split.md` and the operator manuals.
- Review checkpoint: `review-stage-4-catia`.

#### implement-catia-extraction

Operators need accessible metadata and optional text without a CATIA licence or copied archive names.

- Serves: `catia-archive` -- [Accessible metadata](../openspec/stage-4-catia/catia.md#accessible-metadata)
- Agent status: CLEAR
- Dependencies: [CATIA classification](records/0043-catia-implement-catia-classification.md).
- User-visible outcome: A pure-Go streaming extractor returns format, release, component names,
  properties and strings for every CATIA kind, with bounded memory.
- Scope boundary: `internal/catia` only: format detection, V5 property records, the V5 component
  walk (including the `;` U+0001 removal, self-name exclusion and malformed-chunk skip), 3dxml XML and
  ZIP with member limits, strings harvest, 1 MiB cap and `truncated`. `internal/report` rendering of
  the `catia:` line and the sidecar body. No archive mutation, CLI or WAL. No new module dependency.
- Data and artifact paths: `internal/catia/`, `internal/report/`.
- Execution path: Synthetic V5, cgr and 3dxml (XML and ZIP) fixtures in `t.TempDir()` with invented
  names and planted markers; a large synthetic stream for the memory bound. An optional local run
  against the gitignored experimental tree may record aggregate counts only in the task record.
- Acceptance gates: The [CATIA files acceptance](../openspec/stage-4-catia/catia.md#acceptance)
  extraction items; malformed input never panics (a short fuzz run of the extractor passes);
  `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.

#### implement-catia-split

Move CATIA files with the same safety as video and leave Markdown the operator can read.

- Serves: `catia-archive` -- [Split](../openspec/stage-4-catia/split-restore.md#split)
- Agent status: CLEAR
- Dependencies: [CATIA classification](records/0043-catia-implement-catia-classification.md);
  `generalize-payload-split-restore`;
  `implement-catia-extraction`.
- User-visible outcome: `arxgo split --catia --catia-archive PATH` moves CATIA files into the CATIA
  archive, writes CATIA descriptions with the `catia:` line and `arxgo-catia.csv`. Default split
  still moves only videos into `--video-archive`.
- Scope boundary: CLI `--video`, `--catia` and `--catia-archive` for split (and accepted-and-ignored
  on scan) with group environment resolution, payload-root matching, pairwise root nesting checks
  and the exit-2 combinations; CATIA payload in the executor with `catia_*` counters and preflight
  role; description written after `placed` and on recovery roll-forward; the `catia` object on
  `described`; `arxgo-catia.csv` columns 1-10 and 12-16; conflicts and idempotency. No
  `--catia-text`, restore or `--publish`. Update `.env.example` (`ARXGO_CATIA_ARCHIVE`) and the Linux
  and Windows manuals.
- Data and artifact paths: `internal/cli/`, `internal/archive/`, `internal/report/`, `.env.example`,
  `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md`.
- Execution path: Generated CATIA-like files in `t.TempDir()`; same-device and injected cross-device;
  crash after `placed`; an interrupted CATIA split recovered by a video split command.
- Acceptance gates: Byte-identical CATIA files in the CATIA archive; videos, the video archive and
  `arxgo-videos.csv` untouched; recovery by a video command writes a CATIA description;
  `--catia --video`, `--catia --video-archive X`, `--catia-archive X` without `--catia`, nested video
  and CATIA archives, `--catia --sample start` and `--catia --publish gdrive` exit 2 before the lock;
  `ARXGO_VIDEO_ARCHIVE` set in the environment does not affect a CATIA split; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.

#### implement-catia-text-sidecars

Operators want searchable text beside each moved CATIA file, also for archives split earlier.

- Serves: `catia-archive` -- [Text sidecars](../openspec/stage-4-catia/split-restore.md#text-sidecars)
- Agent status: CLEAR
- Dependencies: `implement-catia-split`.
- User-visible outcome: `split --catia --catia-text` writes owned `<rel_path>.text.md` sidecars after
  each commit and for previously moved files that lack one; failures never roll back a move.
- Scope boundary: `--catia-text` flag and validation; `text_begin`/`text_done`/`text_failed` events
  on the shared post-commit mechanism; part files and non-replacing rename; sidecar collision naming;
  rerun catch-up; `texts_done`/`texts_failed` counters and exit 6; `text_rel_path` in
  `arxgo-catia.csv`. No restore cleanup.
- Data and artifact paths: `internal/cli/`, `internal/archive/`, `internal/state/`.
- Execution path: Generated trees in `t.TempDir()`; injected extraction failure; crash between
  `text_begin` and `text_done`; split without then with `--catia-text`.
- Acceptance gates: Sidecars match the contract; a foreign `<rel_path>.text.md` is kept and the
  sidecar goes to `<rel_path>.arxgo.text.md`; failure leaves the file moved with exit 6; crash
  recovery leaves no part file and retries; second run moves nothing and writes only missing
  sidecars; `--catia-text` without `--catia` exits 2; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.

#### implement-catia-restore

Return CATIA files without touching the video payload or deleting foreign Markdown.

- Serves: `catia-archive` -- [Restore](../openspec/stage-4-catia/split-restore.md#restore)
- Agent status: CLEAR
- Dependencies: `implement-catia-split`; `implement-catia-text-sidecars`.
- User-visible outcome: `arxgo restore --catia` returns CATIA files, honors directory and conflict
  policies, and deletes or keeps owned descriptions and text sidecars.
- Scope boundary: Restore `--video`, `--catia` and `--catia-archive` with the split validation rules,
  and `--previews delete` refusal; scan of the CATIA archive; CATIA candidates from CATIA history and
  `arxgo-catia.csv`; registry update and rename when empty; `text_delete` /
  `text_deleted`; mirror directory cleanup. No video restore changes except payload selection.
- Data and artifact paths: `internal/archive/`, `internal/cli/`.
- Execution path: Split-then-restore on generated trees with both a video archive and a CATIA
  archive; `--create-dirs`; `--overwrite`; `--descriptions keep` and `delete`.
- Acceptance gates: Round trip of paths, sizes, mtimes and SHA-256; `--descriptions delete` removes
  owned sidecars only; video archive, video descriptions and `arxgo-videos.csv` unchanged; rerun
  changes nothing; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.

#### prove-stage-4-on-generated-archive

Prove CATIA split and restore through the built binary the way stage 1 proved video.

- Serves: `catia-archive` -- [Exit criteria](../openspec/stage-4-catia/README.md#exit-criteria)
- Agent status: CLEAR
- Dependencies: `implement-catia-restore`.
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
