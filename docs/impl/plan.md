# arxiv-go Implementation Plan

Forward-only: this file contains only work that remains. Product behavior, boundaries and
evaluation belong in [the specification](../openspec/spec.md). Task shape, statuses, ordering and
lifecycle rules belong in the [planning workflow](../guide/planning-workflow.md). Available behavior
and accepted results belong in [current-state documentation](current.md).

Stages: tasks for `crash-safety` through `video-restore` complete
[stage 1](../openspec/stage-1-core/README.md); `media-previews` delivers
[stage 2](../openspec/stage-2-previews/README.md); `cloud-publishing` delivers
[stage 3](../openspec/stage-3-cloud/README.md). A stage starts only after the previous stage's
checkpoint and proof are accepted.

All tasks: tests are deterministic, network-free and build fixtures in `t.TempDir()`; media
fixtures are generated, never committed; each new dependency is the one approved in the
[dependency table](../openspec/spec.md#dependencies) and enters `go.mod`/`go.sum` in the same
change; `make ci` must pass.

## Agent Implementation Tasks

### Crash safety -- `crash-safety`

#### implement-disk-space-preflight

Refuse to start a mutating run that cannot finish for lack of space.

- Serves: `crash-safety` -- [Preflight](../openspec/stage-1-core/integrity.md#preflight)
- Agent status: CLEAR
- Dependencies: [Filesystem primitives](records/0005-safety-implement-filesystem-primitives.md).
- User-visible outcome: `split`, `restore` and `--dry-run` print required, available and shortfall
  per device, and exit 4 before any mutation when space is insufficient.
- Scope boundary: Requirement model per operation/transfer/device placement, `--min-free`, shared-
  device aggregation, unknown network free space warning, stage-2 preview estimate hook (zero until
  stage 2). Input is a candidate summary, not the scanner itself.
- Data and artifact paths: `internal/archive/preflight.go`.
- Execution path: Pure function `Plan(candidates, options, deviceInfo) Requirement` plus a thin
  adapter over `fsops.SameDevice`/`FreeSpace`.
- Acceptance gates: Table tests for each row of the preflight table, both roots on one device,
  exactly-at-threshold pass, one byte short fails, zero-total network filesystem warns; formatted
  report is stable.
- Documentation target: `docs/impl/current/crash-safety.md`
- Review checkpoint: `review-stage-1-integrity`.

### Archive registry -- `archive-registry`

#### implement-directory-walker

Traverse an archive deterministically with exclusions, special-entry handling and a resume cursor.

- Serves: `archive-registry` -- [Traversal](../openspec/stage-1-core/registry.md#traversal)
- Agent status: CLEAR
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md).
- User-visible outcome: Scans visit every regular file once in a stable order, skip arxgo's own
  files and excluded globs, report unreadable entries without aborting, and can restart after a
  cursor.
- Scope boundary: `Walk(ctx, root, opts, fn)` over `filepath.WalkDir`, walk order key and
  comparison, reserved-path and `--exclude` matching (including `**`), symlink/special handling,
  error accounting. No type detection or output.
- Data and artifact paths: `internal/scanner/walker.go`, `internal/scanner/order.go`.
- Execution path: Relative slash paths; cursor skip by key comparison with directory pruning when a
  whole subtree precedes the cursor.
- Acceptance gates: Order equals `WalkDir` order for trees where string order differs; resume after
  every possible cursor in a fixture yields the uninterrupted sequence suffix; reserved paths and
  globs excluded; symlink reported not followed; unreadable directory counted (Linux).
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-stage-1-integrity`.

#### implement-file-type-detection

Classify each file's MIME type and binary, media, picture, video and large flags.

- Serves: `archive-registry` -- [Type detection](../openspec/stage-1-core/registry.md#type-detection)
- Agent status: CLEAR
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md).
- User-visible outcome: Registry flags reflect file content rather than extensions, with the
  specified extension fallback for ambiguous signatures.
- Scope boundary: `Detect(path, size, opts) FileType` using `github.com/gabriel-vasile/mimetype`,
  text hierarchy for `is_binary`, built-in and extra video extension lists, large threshold. The
  ISO BMFF no-video-track refinement is owned by `implement-iso-bmff-metadata`.
- Data and artifact paths: `internal/scanner/mimetype.go`; `go.mod`, `go.sum`.
- Execution path: `mimetype.DetectReader` over a bounded read; parent traversal for text.
- Acceptance gates: Generated fixtures: plain text, UTF-8 with BOM, JSON, SVG, PDF header, PNG,
  JPEG, WAV, minimal MP4/M4A/MOV `ftyp` headers, Matroska/WebM EBML header, random bytes with a
  video extension, text with `.mp4` extension, empty file, exact large threshold.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-stage-1-integrity`.

#### implement-scan-operation-and-csv-registry

Wire the walker, detection and checkpoints into the resumable `scan` operation and CSV registry.

- Serves: `archive-registry` -- [Registry writing](../openspec/stage-1-core/registry.md#registry-writing)
- Agent status: CLEAR
- Dependencies: `implement-directory-walker`; `implement-file-type-detection`;
  [Run lock and checkpoint](records/0006-safety-implement-run-lock-and-checkpoint.md).
- User-visible outcome: `arxgo scan --archive PATH` writes `arxgo-registry.csv` with the specified
  columns and statistics, resumes after interruption, and exits 6 when entries were skipped.
- Scope boundary: CSV writer with part file, offset checkpoints and final rename; `file`-mode
  metadata JSON; scan statistics; candidate list output for split/restore consumers. `media`-mode
  fields are added by `media-metadata` tasks.
- Data and artifact paths: `internal/report/csv.go`, `internal/archive/scan.go`, `internal/cli/`.
- Execution path: Ordered pipeline walker -> bounded detection pool -> re-order buffer -> writer;
  checkpoint cursor and offset written together.
- Acceptance gates: Golden CSV for the full [edge-case list](../openspec/stage-1-core/registry.md#edge-cases);
  interrupted-then-resumed registry is byte-identical to an uninterrupted one; an existing
  registry is replaced only on completion; scan writes nothing else inside the archive.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-stage-1-integrity`.

### Media metadata -- `media-metadata`

#### implement-tool-discovery

Find `ffprobe`/`ffmpeg` next to the executable or on `PATH`, and fail fast with download guidance.

- Serves: `media-metadata` -- [Tool discovery](../openspec/stage-1-core/metadata.md#tool-discovery)
- Agent status: CLEAR
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md).
- User-visible outcome: Requesting `--metadata media` without ffprobe logs the unavailable options
  and the platform download link and exits 3 before any lock or write.
- Scope boundary: Lookup order, `-version` validation with timeout, requirement computation from
  options, platform link table, injectable search path. Stage-2 requirements are declared but only
  enforced when those flags become available.
- Data and artifact paths: `internal/media/tools.go`, `internal/cli/`.
- Execution path: `os.Executable` + `filepath.EvalSymlinks`, then `exec.LookPath`; fake tool scripts
  (`.sh` / `.bat`) generated in tests.
- Acceptance gates: Next-to-executable beats PATH; PATH-only found; missing -> exit 3 with the
  `GOOS/GOARCH` link; non-zero `-version` treated as missing; `--metadata file` needs no tool.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.

#### implement-iso-bmff-metadata

Read MP4/MOV/M4A container and stream metadata in pure Go.

- Serves: `media-metadata` -- [ISO BMFF parser](../openspec/stage-1-core/metadata.md#iso-bmff-parser)
- Agent status: CLEAR
- Dependencies: `implement-scan-operation-and-csv-registry`.
- User-visible outcome: `arxgo scan --metadata media` fills duration, resolution, codecs, audio
  presence and tags for ISO BMFF files without any external tool, and audio-only MP4 files are not
  flagged as video.
- Scope boundary: Normalized `MediaInfo` type and JSON, `github.com/abema/go-mp4` probe, codec
  fourcc mapping, rotation, fragmented files, tags, non-fatal errors, registry integration and the
  `is_video` refinement.
- Data and artifact paths: `internal/media/metadata.go`, `internal/media/isobmff.go`,
  `internal/media/testmp4/` (box builder test helper); `go.mod`, `go.sum`.
- Execution path: `mp4.Probe` plus targeted `ReadBoxStructure` for `udta/meta/ilst` and `tkhd`
  matrices.
- Acceptance gates: Generated fixtures: video+audio, audio-only `.mp4`, M4A, MOV with rotation
  matrix, `moov` at end, fragmented with `mehd`, truncated/corrupt box -> `error` field and scan
  continues; parse reads a bounded number of bytes regardless of `mdat` size.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.

#### implement-ffprobe-metadata

Read metadata for other containers through ffprobe JSON output.

- Serves: `media-metadata` -- [ffprobe parser](../openspec/stage-1-core/metadata.md#ffprobe-parser)
- Agent status: CLEAR
- Dependencies: `implement-tool-discovery`; `implement-iso-bmff-metadata`.
- User-visible outcome: Matroska, WebM, AVI, MPEG-TS and other media get the same metadata fields
  as MP4 when ffprobe is available; ISO BMFF parse failures fall back to ffprobe.
- Scope boundary: `exec.CommandContext` invocation, timeout, output limit, typed JSON decode,
  normalization to `MediaInfo`, fallback routing. No ffmpeg.
- Data and artifact paths: `internal/media/ffprobe.go`, `internal/media/testdata/ffprobe/*.json`.
- Execution path: Captured ffprobe JSON text fixtures for normalization; live test gated on tool
  presence with a generated `lavfi` clip.
- Acceptance gates: Normalization of captured JSON for mkv, webm, avi, ts, audio-only, rotated
  side data, missing duration; timeout kills the process; oversized output rejected; live test runs
  only locally when ffmpeg is on PATH.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.

### Video split -- `video-split`

#### implement-video-split-transactions

Move every video into the mirrored video archive as a write-ahead-logged transaction.

- Serves: `video-split` -- [Split](../openspec/stage-1-core/split-restore.md#split)
- Agent status: CLEAR
- Dependencies: [Write-ahead log and recovery](records/0007-safety-implement-write-ahead-log-and-recovery.md);
  `implement-disk-space-preflight`;
  `implement-scan-operation-and-csv-registry`.
- User-visible outcome: `arxgo split` moves videos by rename on one device or copy+verify+delete
  across devices, honors `--dry-run`, never overwrites, and resumes after any crash.
- Scope boundary: Split phases 1-5, split recovery resolver, destination-exists and source-changed
  rules, cross-device fallback, directory creation, exit 6 accounting. Stubs and registries are
  written through a minimal interface completed by `implement-stubs-and-video-registry`; this task
  writes a placeholder stub that satisfies the WAL `stubbed` step.
- Data and artifact paths: `internal/archive/split.go`, `internal/archive/split_recovery.go`.
- Execution path: Candidate iterator from the scan run, per-candidate transaction using `fsops` and
  `state`, crash-injection tests through `state/crashtest`, cross-device simulated by an injected
  device function.
- Acceptance gates: Byte-identical videos at mirrored paths; non-video media untouched; crash
  injection after every step on both transfer paths converges; adopted identical destination;
  conflicting destination skipped with exit 6; source modified mid-copy aborted and retried;
  `--dry-run` mutates nothing; second run is a no-op.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: `review-stage-1-integrity`.

#### implement-stubs-and-video-registry

Leave a Markdown stub at each former video location and write the video registry and summary.

- Serves: `video-split` -- [Data contracts](../openspec/stage-1-core/contracts.md#markdown-stub)
- Agent status: CLEAR
- Dependencies: `implement-video-split-transactions`; `implement-ffprobe-metadata`.
- User-visible outcome: Every moved video has `<name>.md` with front matter, relative and absolute
  links, optional base-URL link and metadata; both roots contain `arxgo-videos.csv` and
  `arxgo-videos.md`.
- Scope boundary: Stub renderer and parser (front matter), stub collision rule, URL composition and
  escaping, video registry regeneration from WAL + existing file, summary aggregation. No previews.
- Data and artifact paths: `internal/report/markdown.go`, `internal/report/videos.go`,
  `internal/report/summary.go`.
- Execution path: `text/template` rendering with golden files; front matter parsed with a minimal
  line-based reader (flat keys only, no YAML dependency).
- Acceptance gates: Golden stub for file and media modes, with and without base URL, Unicode and
  space-containing paths; existing foreign `<name>.md` triggers `<name>.arxgo.md`; registry
  regeneration after two runs merges rows deterministically; summary bands and top-100 table.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: `review-stage-1-integrity`.

### Video restore -- `video-restore`

#### implement-video-restore

Return videos from the video archive to the main archive with the specified directory, conflict,
stub and registry policies.

- Serves: `video-restore` -- [Restore](../openspec/stage-1-core/split-restore.md#restore)
- Agent status: CLEAR
- Dependencies: `implement-stubs-and-video-registry`.
- User-visible outcome: `arxgo restore` moves or copies videos back, skips or recreates missing
  directories, deletes or keeps matching stubs, updates registries, and resumes after a crash.
- Scope boundary: Restore phases, recovery resolver, registry matching, `--create-dirs`,
  `--overwrite`, `--stubs`, `--registry-update`, empty video-archive directory cleanup. `--previews`
  stays reserved until stage 2.
- Data and artifact paths: `internal/archive/restore.go`, `internal/archive/restore_recovery.go`.
- Execution path: Reuses the split transaction engine with swapped roots and restore steps.
- Acceptance gates: Split-then-restore round trip reproduces paths, sizes, mtimes and SHA-256;
  missing directory skipped by default and recreated with the flag; conflict skipped vs
  overwritten; foreign stub never deleted; `--transfer copy` keeps the video archive copy; crash
  injection converges; rerun is a no-op.
- Documentation target: `docs/impl/current/video-restore.md`
- Review checkpoint: `review-stage-1-integrity`.

#### review-stage-1-integrity

Review stage-1 cross-module invariants before the stage proof and before stage 2 builds on them.

- Serves: `video-restore` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md); `implement-disk-space-preflight`;
  `implement-video-restore`; `implement-ffprobe-metadata`.
- User-visible outcome: Stage 1 is known to be coherent: WAL steps, recovery table, registry
  contracts, lock and preflight agree across scan, split and restore.
- Scope boundary: Read all stage-1 records, code and tests; trace a video through scan, split,
  crash, recover, restore; check contract/spec drift, Windows-specific paths and error accounting.
  Add missing behavior tests at stable seams. No speculative refactor.
- Data and artifact paths: Stage-1 records, `internal/`, `docs/openspec/stage-1-core/`.
- Execution path: Invariant-to-evidence table in the record; targeted tests; audit notes routed to
  one owner each.
- Acceptance gates: Every incoming audit note dispositioned; refactor/no-refactor verdict and
  `proceed`, `proceed-with-nonblocking-notes` or `blocked` recorded; blockers get separate repair
  tasks before this checkpoint closes; `make ci` passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.

#### prove-stage-1-on-generated-archive

Run the complete stage-1 workflow end to end on a generated archive on Linux and Windows.

- Serves: `video-restore` -- [Evaluation and acceptance](../openspec/spec.md#evaluation-and-acceptance)
- Agent status: CLEAR
- Dependencies: `review-stage-1-integrity`.
- User-visible outcome: A single integration test proves scan, split, kill, resume, restore and the
  round-trip gate through the built `arxgo` binary.
- Scope boundary: Build the binary in a test temporary directory (so a developer's `bin/.env` is
  never read) and run it with a scrubbed `ARXGO_*` environment, generate a multi-level archive (hundreds of files,
  generated MP4 headers and optional ffmpeg clips), run operations as subprocesses, kill the split
  process mid-run, resume, restore, compare tree manifests. Not run against operator data.
- Data and artifact paths: `test/integration/stage1_test.go` (build tag `integration`),
  `make test-integration`, CI job on `ubuntu-latest` and `windows-latest`.
- Execution path: `go test -tags integration ./test/integration/...`; manifest of path/size/
  mtime/SHA-256 before split and after restore.
- Acceptance gates: Manifests equal; registries and stubs validate against contracts; process kill
  at three random points (seeded, seed logged) converges; exit codes match the contract; CI passes
  on both operating systems.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: `review-stage-1-integrity` record addendum.

### Media previews -- `media-previews`

#### implement-ffmpeg-runner

Run ffmpeg safely with progress, timeouts and error capture.

- Serves: `media-previews` -- [ffmpeg invocation](../openspec/stage-2-previews/previews.md#ffmpeg-invocation)
- Agent status: CLEAR
- Dependencies: `prove-stage-1-on-generated-archive`.
- User-visible outcome: Preview generation reports progress and fails cleanly with a readable reason
  instead of hanging or leaving partial files.
- Scope boundary: `media.Runner` for ffmpeg/ffprobe, `-progress pipe:1` parsing, stderr ring buffer,
  timeout, cancellation, part-file naming and rename, encoder list probe. No preview planning.
- Data and artifact paths: `internal/media/ffmpeg.go`.
- Execution path: Fake ffmpeg scripts for exit-code/timeout tests; live test on a `lavfi` input.
- Acceptance gates: Progress parsed; timeout kills the process tree; non-zero exit removes the part
  file and returns the stderr tail; encoder list parsed from captured output.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.

#### implement-preview-planning

Compute preview positions, series, resolution clamp and names without running ffmpeg.

- Serves: `media-previews` -- [Position modes](../openspec/stage-2-previews/previews.md#position-modes)
- Agent status: CLEAR
- Dependencies: `prove-stage-1-on-generated-archive`.
- User-visible outcome: Operators get predictable preview files for every mode and resolution.
- Scope boundary: Pure planner from `MediaInfo` and options to a list of preview jobs (time ranges,
  output size, encoder choice, file names, collision suffix), series cap, space estimate. Enables
  stage-2 flags in the CLI.
- Data and artifact paths: `internal/media/preview.go`, `internal/cli/`.
- Execution path: Table tests only.
- Acceptance gates: Positions for start/middle/end/series including `D <= L`, unknown duration,
  cap; clamp for landscape/portrait below and above each box with even rounding; names with index
  padding and collisions; estimate formula.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.

#### implement-video-samples

Generate sample clips in the source container format.

- Serves: `media-previews` -- [Encoding by container](../openspec/stage-2-previews/previews.md#encoding-by-container)
- Agent status: CLEAR
- Dependencies: `implement-ffmpeg-runner`; `implement-preview-planning`.
- User-visible outcome: `--sample start|middle|end|series` produces playable `-smplNN` clips at the
  requested duration and clamped resolution.
- Scope boundary: ffmpeg argument construction for single and chunked series clips, encoder
  fallback, quality mapping, output validation with ffprobe.
- Data and artifact paths: `internal/media/samples.go`.
- Execution path: Tests generate `testsrc2`+`sine` sources in mp4/mov/mkv/webm; skip with reason
  without ffmpeg.
- Acceptance gates: Duration within tolerance, dimensions per clamp, container matches source,
  series with more than 50 fragments concatenates correctly, missing encoder falls back.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.

#### implement-frame-images

Generate PNG frames at the planned positions.

- Serves: `media-previews` -- [Position modes](../openspec/stage-2-previews/previews.md#position-modes)
- Agent status: CLEAR
- Dependencies: `implement-ffmpeg-runner`; `implement-preview-planning`.
- User-visible outcome: `--image start|middle|end|series` produces `-imgNN.png` frames at the
  clamped resolution.
- Scope boundary: Frame extraction arguments, compression mapping, PNG validation with
  `image/png` decode.
- Data and artifact paths: `internal/media/frames.go`.
- Execution path: Generated sources as for samples.
- Acceptance gates: Frame count and names, decoded dimensions, rotated source orientation, frame
  near end of a short video.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.

#### integrate-previews-into-split-and-restore

Generate previews after each committed move and clean them up on restore.

- Serves: `media-previews` -- [Transactions and failures](../openspec/stage-2-previews/previews.md#transactions-and-failures)
- Agent status: CLEAR
- Dependencies: `implement-video-samples`; `implement-frame-images`.
- User-visible outcome: `arxgo split --sample ... --image ...` leaves previews next to stubs, stubs
  embed them, registries list them, failed previews do not affect moves, and `restore --previews
  delete` removes them.
- Scope boundary: WAL preview records and recovery, preflight estimate, worker pool, stub/registry
  preview sections, scan exclusion of recorded previews, restore policy, missing-preview catch-up on
  rerun, exit 6 on preview failure.
- Data and artifact paths: `internal/archive/split.go`, `internal/archive/restore.go`,
  `internal/report/markdown.go`, `internal/state/`.
- Execution path: Extend stage-1 integration test with preview flags when ffmpeg is present.
- Acceptance gates: Crash during preview resumes only missing previews; previews never become split
  candidates; restore deletes only recorded previews with matching size; failure injection yields
  moved video plus exit 6.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.

#### implement-release-bundle-with-ffmpeg

Package `arxgo` with pinned ffmpeg/ffprobe builds per platform.

- Serves: `media-previews` -- [Release bundle](../openspec/stage-2-previews/previews.md#release-bundle)
- Agent status: RUN NEEDED
- Dependencies: `integrate-previews-into-split-and-restore`; [Approve ffmpeg distribution](records/0002-preview-approve-ffmpeg-distribution.md).
- User-visible outcome: Operators download one archive per platform that runs previews with no
  installation.
- Scope boundary: `make dist` packaging on top of the approved pins in `packaging/ffmpeg.lock` and
  the verified download in `scripts/fetch-ffmpeg.sh` (`make ffmpeg`), `SHA256SUMS`, GPLv3 licence
  text and source offer per bundle, CI release job on tags. Each bundle ships `.env.example` next
  to `arxgo` (operators copy it to `.env`) and never a `.env`. Binaries never committed.
- Data and artifact paths: `packaging/`, `scripts/fetch-ffmpeg.sh`, `make/`, `Makefile`,
  `.github/workflows/release.yml`, `dist/` (ignored).
- Execution path: Declared run: `make dist` for linux/amd64 and windows/amd64 with network access,
  then smoke-test each bundle (`arxgo split --image start` on a generated video) on its OS.
- Acceptance gates: Checksums verified before packaging; bundle smoke tests pass on both OSes;
  licence files match the approved variant; checksum mismatch fails the build; no bundle contains
  a `.env` file, even when `bin/.env` exists on the build host.
- Documentation target: `docs/guide/development.md`
- Review checkpoint: `review-stage-2-previews`.

#### review-stage-2-previews

Review preview integration before cloud publishing builds on the stage-2 WAL and registry.

- Serves: `media-previews` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: `integrate-previews-into-split-and-restore`; `implement-release-bundle-with-ffmpeg`.
- User-visible outcome: Stage 2 is coherent and stage 3 can rely on its contracts.
- Scope boundary: Preview WAL/recovery, naming/exclusion invariants, restore cleanup, bundle
  licensing evidence, cross-platform behavior. Add missing behavior tests; no speculative refactor.
- Data and artifact paths: Stage-2 records, `internal/media/`, `internal/archive/`.
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
- Dependencies: `review-stage-2-previews`.
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
  stub and registry links.
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
- Data and artifact paths: `internal/cloud/gdrive/`, `internal/cloud/gdrive/testdata/`.
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
- Data and artifact paths: `internal/cloud/sharepoint/`, `internal/cloud/sharepoint/testdata/`.
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
- Execution path: `go test -tags cloudlive ./test/integration/...` with credentials present.
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

### Video restore -- `video-restore`

#### approve-stage-1-on-operator-archive-copy

Decide whether stage 1 is fit for use on real archives after a trial on a copy of operator data.

- Serves: `video-restore` -- [Success criteria](../openspec/spec.md#success-criteria)
- Human status: HUMAN-GATED
- Dependencies: `prove-stage-1-on-generated-archive`.
- Requested input or decision: Run scan, split, interrupt, resume and restore on a disposable copy
  of a representative archive; review registry, stubs, logs and timings; accept, or file defects.
- Unblocks: Production use of stage 1. Stage-2 development does not wait for this decision.

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
