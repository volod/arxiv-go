# Preserved archive registry

## Task and scope

- Id / capability / checkpoint: `preserve-archive-registry` / `archive-registry` /
  `review-registry-and-metadata`
- State: accepted
- Source: plan task `preserve-archive-registry`; code revision `b4402fe`, clean tree at start.
- Plan counts at start: 14 open tasks (11 agent, 3 human); next eligible
  `preserve-archive-registry`; also eligible `record-source-location-in-metadata`,
  `research-cloud-target-apis`.
- Accepted task:

```markdown
#### preserve-archive-registry

After `split` moves payload files out and writes descriptions and sidecars, the next scan writes a
registry that has lost the moved files and lists arxgo's own artifacts, so the inventory of the
original archive is gone and every run rewrites the file.

- Serves: `archive-registry` -- [Archive view](../openspec/stage-1-core/registry.md#archive-view)
- Agent status: CLEAR
- Dependencies: [Registry full schema](records/0052-registry-stabilize-registry-columns.md).
- User-visible outcome: The file registry keeps a row for every moved file with its `location`,
  never lists descriptions, sidecars or previews, gains exactly the rows of newly added files, and
  is left untouched when nothing changed; `scan` and `split` write the same registry.
- Scope boundary: The `location` column and its reader default; replay of both payload histories
  during scan; owned-artifact exclusion; preserved-row sources (base row, mirror detection, any
  base row, payload reconstruction) with their warnings; candidates from present rows only;
  split's post-execute and restore's `--registry-update` `location` rewrite; the registry stamp;
  the unchanged-file rule for all three CSVs; `preserved` and `registry` statistics. No detection
  reuse for present files, no `--redetect`, no write to a mirror root, no registry of a mirror.
- Data and artifact paths: `internal/report/csv.go`, `internal/report/csv_read.go`,
  `internal/archive/scan.go`, `internal/archive/scan_pipeline.go`, `internal/archive/history.go`,
  `internal/archive/finish.go`, `internal/archive/restore_report.go`, `internal/state/`.
- Execution path: Generated fixture archive run through scan, video split with previews, CATIA
  split with `--catia-text`, scan, file additions, restore with `--registry-update`, and scan;
  variants with the registry and stamp deleted, a mirror unreadable, a payload registry deleted,
  a file put back by hand at a moved path, and an `--exclude` covering a moved path.
- Acceptance gates: A scan after a completed split leaves every registry byte-identical with an
  unchanged modification time; split's registry keeps every pre-split row with the same values and
  only moved rows' `location` changed; no description, sidecar or preview has a row; adding a video
  or CATIA file adds exactly its row; deleting registry and stamp with mirrors present rebuilds a
  byte-identical registry; an unreadable mirror yields reconstructed rows and a warning; a deleted
  payload registry changes nothing; restore then scan leaves the registry untouched; preserved rows
  never enter `candidates.jsonl`; `make ci` passes.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-registry-and-metadata`.
```

- Amendments (specification, made in this task, within its scope):
  1. [Reserved paths](../../openspec/spec.md#reserved-paths) now include the retired payload
     registries `arxgo-videos.restored-<run-id>.csv` and `arxgo-catia.restored-<run-id>.csv`
     directly under a root. Reason: restore itself creates them when it retires a registry. As
     ordinary files they gave the file registry new rows, so the "restore then scan leaves the
     registry untouched" gate could not hold for `--descriptions delete`.
  2. [Checkpoint](../../openspec/stage-1-core/contracts.md#checkpoint): the `scan` object also
     holds `started_at` (the value the stamp records as `scan_started_at`, kept across processes)
     and `registry` (`written`/`unchanged`), so a resumed run reports and stamps the same values.
     Neither changes an existing field.

## Implementation

- `internal/report`: `location` is column 12 of `RegistryHeader` (`LocationColumn`,
  `LocationArchive`/`LocationVideoArchive`/`LocationCatiaArchive`); `RegistryRow.Location` (empty
  writes `archive`). `ReadRegistry` accepts headers with or without `location` (missing means
  `archive`) and rejects unknown values. `FileRegistryRequired` stays 11.
- `internal/state`: `ScanStats.StartedAt`, `Preserved`, `Registry`; `Rows()` includes preserved
  rows; `ScanSummary` gains `preserved` and `registry`. New `registry_stamp.go`
  (`RegistryStamp`, `DetectSettings.Equal`, read/write of `.arxgo/registry.json`).
- `internal/scanner`: retired registry names are reserved at a root; `IsCatiaName`.
- `internal/archive`:
  - `archive_view.go`: replays both payload histories into moved files (with the mirror root of
    the moving run) and restored files. It collects owned artifacts: event-index previews and text
    sidecars (owned, generating, deleting), and descriptions from `described` records not removed
    since.
  - `scan_preserved.go`: resolves preserved rows in the four specified source orders on a worker
    pool and merges them into the writer in walk order (presence wins, trailing rows only after a
    completed walk, cursor advanced per preserved row). `--exclude` applies to every ancestor.
  - `registry_base.go`: base registry and stamp check, `placeRegistry` (single-pass hash and
    compare, unchanged keeps the file), stamp writing, `writeFileIfChanged`.
  - `registry_location.go`: split's post-execute `location` update from its WAL commits and
    restore's `--registry-update` update of the stamped registry, both through part file,
    placement and stamp.
  - `scan.go` / `scan_pipeline.go`: view loading, owned-artifact skip paths, `fileRow` shared by
    present and mirror detection, placement and stamp, summary attributes. `restore.go` marks its
    scan as a mirror scan. `payload_video.go` drops its own preview skip paths (the view covers
    them). The payload registry writers keep an unchanged copy untouched.
- Decisions:
  - A restore records the owned description both when it deletes it and when it keeps it. Options
    were to decode the CLI options of the earlier run (forbidden by the run-options contract) or to
    check the marker. The view treats a description a restore recorded as owned while its marker
    names its file.
  - The base registry is loaded only when moved files exist, so a plain scan pays nothing new. Its
    stamped status only selects source 1 here; reuse for present files is
    `reuse-registry-detection`.
  - Split's update uses its own committed set, not the full history. Files earlier runs moved are
    already preserved rows of its scan.
  - The restore update changes rows whose replayed status is restored and whose location is the
    payload's. A registry that no longer matches its stamp is left alone with a warning.
  - The stamp keeps the scan's `run_id`, `op` and `scan_started_at` when restore updates it,
    because restore detects nothing.
- Reversal of an earlier repair: [0051](0051-catia-review-stage-4-catia.md) kept text sidecars in
  the file registry. The amended specification (commit `db1907d`) makes them owned artifacts
  without a row, and `TestCatiaSplitKeepsTextSidecarsInFileRegistry` became
  `TestCatiaSplitExcludesTextSidecarsFromFileRegistry`.
- Compatibility: the golden registry gained the `location` column. Registries from earlier builds
  load. The first scan after upgrading an already split archive rebuilds moved rows from the
  mirrors, because no stamp exists yet.
- Current-state page: [archive registry](../current/archive-registry.md#archive-view); also
  [video split](../current/video-split.md), [video restore](../current/video-restore.md),
  [current state](../current.md), both manuals and `README.md`.

## Acceptance evidence

All tests run on Linux amd64 with system ffmpeg/ffprobe (the fixture uses real clips and previews
when they are present and synthetic MP4 bytes without previews otherwise).

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Scan after a completed split leaves every registry byte-identical with unchanged mtime | `go test ./internal/archive -run TestFileRegistryKeepsArchiveViewAcrossSplitAndRestore` (scan after video split with previews and CATIA split with `--catia-text`; report `registry` `unchanged`; video split rerun leaves `arxgo-videos.csv` and the file registry untouched); real binary on ffmpeg clips (below) | pass |
| Split keeps every pre-split row, only moved rows' `location` changed | same test, `checkLocations` after each split and after splitting additions | pass |
| No description, sidecar or preview has a row | same test over every `description_rel_path`, `previews` and `text_rel_path`; `TestCatiaSplitExcludesTextSidecarsFromFileRegistry` (foreign file at a description path keeps its row) | pass |
| Adding a video or CATIA file adds exactly its row | same test, `checkAddedLines` (+2 lines, all others identical) | pass |
| Deleting registry and stamp with mirrors present rebuilds byte-identical registry | `TestFileRegistryRebuildsFromMirrors`; `TestFileRegistryIgnoresDeletedPayloadRegistries` (also without payload registries); real binary `cmp` | pass |
| Unreadable mirror yields reconstructed rows and a warning | `TestFileRegistryWithUnreadableMirrors` (with registry: `registry-row-kept`, byte-identical; without: `registry-row-reconstructed` per moved file, completed status) | pass; skipped as root |
| Deleted payload registry changes nothing | `TestFileRegistryIgnoresDeletedPayloadRegistries` | pass |
| Restore then scan leaves the registry untouched | lifecycle test (video and CATIA restore with `--registry-update`, stamp matches, next scan unchanged, round trip all `archive`); `TestFileRegistryKeptDescriptionStaysOwned`; `TestRestoreIgnoresRegistryPathsOutsideArchive` (only `location` changes) | pass |
| Preserved rows never enter `candidates.jsonl` | lifecycle test, scan with video/CATIA candidate selection after both splits: 0 candidates | pass |
| Variants: file put back by hand, `--exclude` | `TestFileRegistryPresenceWinsAndExcludeApplies` | pass |
| Resume with preserved rows (scope: merge and cursor) | `TestFileRegistryResumeWithPreservedRows` (cancel after every entry); `TestFileRegistryResumeAfterTrailingPreservedRows` (fails if a preserved row does not advance the cursor, checked by mutation) | pass |
| Reader default and schema | `TestRegistryLocationRoundTripAndValidation`, `TestRegistryReadersAcceptCompactedFiles`, `TestFileRegistryHeaderOrder`, regenerated golden; `TestLocalRelPath` for retired names | pass |
| `make ci` passes | `make ci`; also `make test-integration` | pass on Linux (vet incl. `GOOS=windows`, tests, `build-all`, `lint-spec-plan`, `lint-doc-links`); Windows cross-compiled and vetted only |

Real binary (`bin/arxgo`, generated archive with two libx264 clips, a synthetic CATPart and a text
file): scan; `split --sample start --image start`: only the two video rows changed `location`.
Next scan: same MD5 and mtime. `split --catia --catia-text`, then scan: unchanged. Registry and
stamp deleted, then scan: `cmp` identical. Added clip: one new line. `restore`: locations back to
`archive`, next scan unchanged. `restore --catia` (retiring both payload registries): next scan
unchanged, report `preserved` 0, `registry` `unchanged`.

## Audit handoff

- `AUD-preserve-archive-registry-1`: nonblocking. `.arxgo/registry.json` is written with
  `state.WriteJSON` (indented, one trailing newline), like the other state files. The contract
  example shows it compact. Readers are unaffected. Owner: `review-registry-and-metadata`
  (formatting consistency of state JSON). Disposition: routed.
- `AUD-preserve-archive-registry-2`: nonblocking. Every archive scan now reads the WAL of every
  split and restore run to build the view. The cost grows with history (one JSON decode per WAL
  record), not with the tree. It was not measured on a long operator history. Owner:
  `review-registry-and-metadata`. Disposition: routed.

## Close or resume

All gates pass. Task removed from the plan; its references in `reuse-registry-detection` and
`review-registry-and-metadata` now link this record. `archive-registry` stays `planned`
(`reuse-registry-detection` remains).
