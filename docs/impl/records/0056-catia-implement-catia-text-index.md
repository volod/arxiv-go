# CATIA text index

## Task and scope

- Id / capability / checkpoint: `implement-catia-text-index` / `catia-archive` /
  `review-registry-and-metadata`
- State: accepted
- Source: plan task `implement-catia-text-index`; code revision `1559fa7`, clean tree at start.
- Plan counts at start: 11 open tasks (8 agent, 3 human); next eligible
  `implement-catia-text-index`; also eligible `research-cloud-target-apis`.
- Accepted task:

```markdown
#### implement-catia-text-index

Extracted CATIA text lands in one sidecar per file; reading or feeding a whole archive at once
means assembling them in the shell by guessing sidecar names.

- Serves: `catia-archive` -- [Text index](../openspec/stage-4-catia/catia.md#text-index)
- Agent status: CLEAR
- Dependencies: [Self-locating metadata](records/0055-catia-record-source-location-in-metadata.md).
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
```

- Amendments (specification, within the task's scope of the index document and its default path;
  no task-text change):
  1. [Text index](../../openspec/stage-4-catia/catia.md#text-index): the header time is
     `history_at`, the time of the newest record of the CATIA run history, instead of
     `generated_at`. Reason: a wall-clock `generated_at` contradicts the specified evaluation "a
     rerun writes a byte-identical document" and the gate "`--strings` is the only difference
     between the two outputs".
  2. Same section: "takes no lock beyond the archive read lock" became "takes no lock; a lock held by
     a live process or another host exits 5, a stale or unreadable lock is logged". Reason: arxgo
     has no read lock; taking the run lock would write under `.arxgo/`, which the scope excludes.
  3. Same section: sections are the files whose replayed CATIA history ends `moved`; every moved
     file keeps its section and a file without a usable owned sidecar is also listed under
     `Missing text` with a reason (`not_recorded`, `missing`, `foreign`, `unreadable`); the
     identity comes from the sidecar when the description is gone; exit codes (0 with missing
     text, 5 on corrupt state, 130 on interrupt) and `--out` validation. Reason: the spec named the
     section and the list but not what a section of a file without text holds, where the identity
     comes from, or what `--out` may replace.
  4. [Contracts](../../openspec/stage-1-core/contracts.md#catia-text-index): new document contract
     with example. [CLI](../../openspec/stage-1-core/cli.md): synopsis, `Index flags`, validation,
     exit 5 wording, example. [Spec](../../openspec/spec.md#operations-and-defaults): operation row.

## Implementation

- `internal/archive`:
  - `catia_index.go` (new): `CatiaIndex(ctx, CatiaIndexConfig, log) Status`. `state.InspectLock`
    first (held or remote: `StatusLocked`; stale or unreadable: warning). `readCatiaIndex` reads
    `readHistory(archive, catia)`, the text event index (`newTextIndex`) and
    `replayCatiaRows(nil, history)`, keeps local `moved` rows, sorts them with `scanner.Compare`
    (walk order), and reads each file's owned description (`ownedDescriptionPath`: WAL hint, then
    naming order) and owned sidecar (`eventIndex.ownedPath`, first line checked) without strings.
    The document is streamed through `fsops.AtomicWrite`; with `--strings` each sidecar is read a
    second time for its `strings:` block only, so memory holds identities and component lists, not
    strings. `history_at` is `newestRecord(history)`. Corrupt state maps through `StartFailed` to
    `StatusNeedsOperator`; other errors log `CATIA text index not written`.
  - `text_identity.go`: `ownedDescriptionPath(archive, hint, rel)` extracted from
    `splitTexts.descriptionPath` and shared by split and the index.
- `internal/report/catia_index.go` (new): `ReadCatiaText` (marker check, header fields, blocks with
  items kept byte-identical, optional strings, text after the blocks ignored, 1 MiB line limit),
  `TextIdentityOfSidecar`, `WriteCatiaIndexHeader`, `WriteCatiaIndexSection` (reuses the sidecar
  `identityLines`), `WriteMissingText`, the reason constants, `HasArxgoMarker`.
- `internal/state/lock.go`: `InspectLock(root, opts)` classifies an existing lock without taking it.
- `internal/scanner`: `CatiaIndexName` (`arxgo-catia-text.md`) is a reserved name directly under a
  walked root, as the spec's reserved paths already said; before this change the name was reserved
  only on paper and a scan would have registered the file.
- `internal/cli`: operation `catia-index` (`OpCatiaIndex`, `Handlers.CatiaIndex`, `lockHooks` test
  seam); flag table rows `--out` and `--strings` in the new `Index flags` group, and `--archive`,
  `--log-level`, `--log-format` shared through `allOps`, while the other common flags use `runOps`;
  `catia_index.go` builds `CatiaIndexOptions` (archive directory, absolute `--out`, not a directory,
  existing parent, not reserved under the archive with case folding on Windows, not a part file,
  not an existing arxgo description or sidecar); help and general usage. `.env.example` gains
  `ARXGO_OUT` and `ARXGO_STRINGS`.
- Decisions:
  - Ownership comes from the WAL (`described` hint, last `text_done`), never from `.text.md` names,
    so fallback names such as `<rel_path>.arxgo.text.md` are indexed under the right file.
  - Sections come from the history replay rather than from `arxgo-catia.csv`: the spec names the run
    history as the source, and the replay is the same function that writes the registry, so the
    section count equals its `moved` rows.
  - A file without a usable sidecar keeps its section (identity only) and is listed once under
    `Missing text`; the section count stays equal to the moved rows.
  - Missing text does not change the exit code: the document is complete and says what is missing,
    and a CATIA split without `--catia-text` would otherwise always exit 6.
  - The sidecar is accepted by its first line, not by its recorded size (restore's deletion rule):
    the index copies text, it deletes nothing.
  - An explicit `--out` may replace an operator file, as `--registry` may; it may not replace arxgo
    state, registries, descriptions or sidecars.
  - Rejected: `generated_at` from the clock with "rewrite only when content changed" (two outputs
    written at different times would still differ); taking the run lock (writes `.arxgo/lock`);
    keeping harvested strings in memory (tens of MB on the operator archive).
- Compatibility: new operation and reserved name only; split, restore and scan outputs are
  unchanged apart from never registering `arxgo-catia-text.md`.
- Documentation: [CATIA archive](../current/catia-archive.md#catia-text-index-internalarchive-internalreport-internalcli),
  [current state](../current.md), both manuals (the aggregated shell recipe replaced by
  `catia-index`; the per-file copy recipe kept), `README.md`, architecture file list,
  [Windows verification](../../guide/windows-verification.md) W10.

## Acceptance evidence

Linux amd64, Go toolchain from `go.mod`. Fixtures are generated in `t.TempDir()`; CATIA files are
synthetic with invented names.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Section count equals the `moved` rows of `arxgo-catia.csv` | `TestCatiaIndexCoversMovedFilesAndListsMissingText` (split `--catia-text --verify hash`, four sections in walk order equal to the moved rows, identity lines equal to the description, fallback `.arxgo.md` description read); `TestCatiaIndexWithoutTextOrAfterRestore` (no history, no `--catia-text`, zero sections after restore); integration `checkCatiaIndex` in `TestCatiaSplitRestoreRoundTrip` (binary, 22 generated CATIA files after killed and resumed splits) | pass |
| A missing sidecar is listed once and not skipped silently | `TestCatiaIndexCoversMovedFilesAndListsMissingText` (deleted sidecar: `- missing:` once under `Missing text`, section kept without `text:`, warning logged, list gone after a catch-up split); `TestCatiaIndexReadsRecordedOwnershipNotNames` (`foreign`, fallback sidecar name, identity from the sidecar when the description is gone); `TestCatiaIndexWithoutTextOrAfterRestore` (`not_recorded`) | pass |
| `--strings` is the only difference between the two outputs | `TestCatiaIndexCoversMovedFilesAndListsMissingText` (removing `strings:` blocks from the `--strings` output equals the default output byte for byte; the block equals the sidecar's); integration `checkCatiaIndex` (same comparison through the binary); `TestCatiaIndexCommandAfterTextSplit` (`ARXGO_STRINGS`, `ARXGO_OUT`) | pass |
| A rerun is byte-identical | `TestCatiaIndexCoversMovedFilesAndListsMissingText`; integration `checkCatiaIndex` | pass |
| Read-only | `TestCatiaIndexCoversMovedFilesAndListsMissingText` (archive tree identical apart from the output, no new run, no lock, index not a registry row after a later split); `TestCatiaIndexStopsOnLiveLock` (live and remote lock exit without output, stale lock indexed, lock untouched); `TestCatiaIndexCanceledWritesNothing`; `TestCatiaIndexFailures` (unwritable output, corrupt WAL path: needs operator, nothing written) | pass |
| CLI | `TestCatiaIndexCommandAfterTextSplit` (exit 0, exit 5 under a live lock), `TestCatiaIndexUsageErrors` (12 exit-2 cases, operator file replaced), `TestCatiaIndexOutReservedCaseFolded`, `TestCatiaIndexHelp`, `TestEnvExampleListsEveryFlag` | pass |
| Report | `TestReadCatiaTextRoundTripsRenderedSidecar`, `TestReadCatiaTextRejectsForeignFiles`, `TestWriteCatiaIndexDocument`, `TestHasArxgoMarker`; scanner `TestWalkExcludesReservedPathsSkipPathsAndGlobs`, `TestLocalRelPath` | pass |
| `make ci` passes | `make ci` | see close |
| Integration | `make test-integration` | pass (10.9 s) |
| Real binary | `go build` of `cmd/arxgo` in a scratch directory; 2000 generated V5 files (37 x 5 directories, 0-12 invented components, 50-400 invented strings each, 29 MB); `split --catia --catia-text`; `catia-index` default and `--strings --out`; rerun; one sidecar deleted; `scan` | pass: 2000 sections = 2000 `moved` rows, 11687 components; 0.10 s and 26 MB max RSS (1.4 MB document); with strings 0.19 s and 26 MB (20.6 MB document); rerun `sha256sum -c` OK; deleted sidecar listed as `- missing:` with a warning; `arxgo-registry.csv` has no index row. The split itself took 56 s (outside this task) |

Windows: cross-compiled and vetted only.

## Audit handoff

- `AUD-implement-catia-text-index-1`: nonblocking, Windows-only. Case-folded refusal of reserved
  and owned `--out` paths is tested on Linux through `rootFS.foldCase` only; atomic replace of an
  output open in another program, drive-letter and UNC roots, and slash headings need a runtime
  check. Owner: [Windows verification](../../guide/windows-verification.md) W10. Disposition:
  routed.
- `AUD-implement-catia-text-index-2`: nonblocking. The index accepts an owned sidecar by its first
  line; an operator-edited sidecar (size differs from the WAL `text_done`) is indexed as edited,
  while restore keeps such a file as not ours. Both follow their spec; the checkpoint should confirm
  that the two ownership tests may differ. Owner: `review-registry-and-metadata` (index correctness
  against the run history). Disposition: routed.
- `AUD-implement-catia-text-index-3`: nonblocking. `history_at` names the history, not the disk:
  deleting a sidecar changes the document but not `history_at`. Specified that way; noted so the
  checkpoint reviews the header against a reader's expectation. Owner:
  `review-registry-and-metadata`. Disposition: routed.

## Close or resume

All gates pass: `make ci` on the final tree (fmt, vet including `GOOS=windows`, tests, `build-all`,
`lint-spec-plan`, `lint-doc-links`) and `make test-integration`. Task removed from the plan; its
reference in `review-registry-and-metadata` now links this record. Plan counts after: 10 open
tasks (7 agent, 3 human); next eligible `review-registry-and-metadata`, also
`research-cloud-target-apis`. `catia-archive` stays planned until that checkpoint is accepted.
