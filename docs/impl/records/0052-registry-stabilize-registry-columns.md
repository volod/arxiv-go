# Registry full schema

## Task and scope

- Id / capability / checkpoint: `stabilize-registry-columns` / `archive-registry` /
  `review-registry-and-metadata`
- State: accepted
- Source: plan task `stabilize-registry-columns`; code revision `db1907d`, clean tree at start.
- Plan counts at start: 15 open tasks (12 agent, 3 human); next eligible
  `stabilize-registry-columns`; also eligible `record-source-location-in-metadata`,
  `research-cloud-target-apis`.
- Accepted task:

```markdown
#### stabilize-registry-columns

A registry's header depends on what the archive currently holds, so the same tree yields different
schemas from one command to the next and a downstream loader breaks.

- Serves: `archive-registry` -- [Full schema](../openspec/stage-1-core/registry.md#full-schema)
- Agent status: CLEAR
- Dependencies: [Stage-4 checkpoint](records/0051-catia-review-stage-4-catia.md).
- User-visible outcome: Every CSV arxgo writes (file, video and CATIA registry) carries its full
  contract header on every run of every command, so a spreadsheet, database or search index sees
  one stable schema.
- Scope boundary: Remove empty-column compaction from the three writers; readers keep accepting
  files from earlier builds that omitted optional columns; update golden files and the manuals. No
  option, no new column, no change to column order, cell values or rows.
- Data and artifact paths: `internal/report/csv_compact.go`, `internal/report/csv.go`,
  `internal/report/csv_read.go`, `internal/report/videos.go`, `internal/report/catia_registry.go`,
  `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md`.
- Execution path: Table tests over the three writers with rows that leave every optional column
  empty; reader tests over compacted files; a scan-split-scan fixture comparing headers.
- Acceptance gates: An empty archive, a video tree, the same tree after `split` and a CATIA tree
  write byte-identical file-registry headers; a payload registry has the same header with and
  without `--verify hash`, previews and `--catia-text`; a compacted registry from an earlier build
  is read with missing columns empty; `make ci` passes.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-registry-and-metadata`.
```

- Amendments: none.

## Implementation

- `internal/report`: `csv_compact.go` (`DropEmptyCSVColumns`, `dropEmptyColumns`, the temp-file
  rewrite) and its test are deleted. `WriteVideoCSV` writes `VideoHeader` and `WriteCatiaCSV` writes
  `CatiaHeader` with every cell; `RegistryWriter` already streamed the full `RegistryHeader`, so the
  file registry only needed the scan's post-walk compaction removed. The reader constants, which
  only mean "leading columns a reader requires" now, are renamed `FileRegistryKeep` ->
  `FileRegistryRequired` (in `csv.go`) and `VideoRegistryKeep` -> `VideoRegistryRequired` (in
  `videos.go`). Reader code is unchanged: `checkRequiredHeader`/`expandMetadata` and
  `catiaColumns` already mapped any canonical-order subset, and their comments now say why.
- `internal/archive/scan.go`: the completed part file is renamed into place as written; the
  "compact registry" rewrite (a second full read and write of the registry per scan) is gone.
- Compatibility: registries from earlier builds with omitted columns still load, with missing
  columns empty; the next write of the file has the full header. No column, order, cell value or
  row changed: the regenerated golden `test/testdata/archive/scan-registry.golden.csv` projects the
  previous golden's 25 rows onto the 40-column header cell for cell (checked with a throwaway
  script comparing both files by column name).
- Not in scope and still open: the `location` column, which the contract table already lists as
  column 12, arrives with `preserve-archive-registry`, as does its reader default. The full header
  here is the current writer header (40 file-registry, 40 video-registry, 16 CATIA-registry
  columns).
- Current-state page: [archive registry](../current/archive-registry.md#full-schema); also updated
  [media metadata](../current/media-metadata.md), [video split](../current/video-split.md),
  [CATIA archive](../current/catia-archive.md), both manuals and `README.md`.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Empty archive, video tree, same tree after `split` and a CATIA tree write byte-identical file-registry headers | `go test ./internal/archive -run TestRegistryHeadersAreStableAcrossCommands` (empty scan; video tree scan, split, scan with `--verify size` and `hash`; CATIA tree scan, split, scan with and without `--catia-text`); `TestScanEmptyArchiveWritesFullHeaderOnly`; `TestScanClassifiesCatiaAndNotVideo` header check; real binary: `bin/arxgo` scan, `split --verify size`, scan, `split --catia --catia-text`, and scan of an empty dir on an ffmpeg-generated clip plus a synthetic CATPart | pass; generated fixtures; all five real-binary headers had the same MD5 (585 bytes) |
| Payload registry has the same header with and without `--verify hash`, previews and `--catia-text` | Same test asserts `arxgo-videos.csv` in both roots equals `VideoHeader` for size and hash, `arxgo-catia.csv` in both roots equals `CatiaHeader` with and without text; `TestVideoRegistryHeaderWithPreviewsLive` (ffmpeg, sample and image previews); `TestRegistryWritersWriteFullHeader` table over the three writers with every optional column empty and with no rows | pass on Linux with system ffmpeg/ffprobe; the live test skips without ffmpeg |
| A compacted registry from an earlier build is read with missing columns empty | `TestRegistryReadersAcceptCompactedFiles` (file registry with a metadata subset and required-only; video registry with `mtime` only, rewritten with full header and reloaded equal; CATIA registry without `text_rel_path`/`catia_release`, rewritten and reloaded equal); existing `TestCatiaRegistryColumnsAndRoundTrip` rejection cases | pass |
| `make ci` passes | `make ci` | pass on Linux (vet incl. `GOOS=windows`, tests, `build-all`, `lint-spec-plan`, `lint-doc-links`); `make test-integration` also passes. Windows cross-compiled and vetted only |

## Audit handoff

`none identified`. Reviewed scope: the three writers and their readers, every caller of
`WriteVideoCSV`/`WriteCatiaCSV` (split and restore reports for both payloads) and the scan finish
path. Registry files grow by the empty cells (about 29 commas per file-registry row without media
metadata); this is the specified behavior, not a concern.

## Close or resume

All gates pass. Task removed from the plan; its references in `preserve-archive-registry` and
`review-registry-and-metadata` now link this record. `archive-registry` stays `planned`
(`preserve-archive-registry` and `reuse-registry-detection` remain). Plan counts after: 14 open
tasks (11 agent, 3 human); next eligible `preserve-archive-registry`.
