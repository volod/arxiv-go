# CATIA classification in the file registry

## Task and scope

- Id / capability / checkpoint: `implement-catia-classification` / `catia-archive` / `review-stage-4-catia`
- State: accepted.
- Source: plan task `implement-catia-classification`; code revision `4e831f1`, working tree clean.
- Plan counts at start: 17 open tasks (14 agent, 3 human); next eligible
  `implement-catia-classification`; also eligible `generalize-payload-split-restore`.
- Accepted task:

```markdown
#### implement-catia-classification

Mark CATIA files in the file registry so split can select them without guessing from ad hoc extensions.

- Serves: `catia-archive` -- [Classification](../openspec/stage-4-catia/catia.md#classification)
- Agent status: CLEAR
- Dependencies: [Stage-2 review](records/0033-preview-review-stage-2-previews.md).
- User-visible outcome: `scan` sets `is_catia` for the built-in CATIA extensions, never classifies
  those files as video, and writes the file registry in the new logical column order.
- Scope boundary: `internal/catia` kind table (leaf package); scanner classification by extension;
  AppleDouble exclusion; the [file registry column order](../openspec/stage-1-core/contracts.md#file-registry-csv)
  with `is_catia` (writer, reader, `FileRegistryKeep`, column compaction, positional parsing and
  every test or integration check that names columns); scan statistics `catia` omitted when zero;
  reserved `arxgo-catia.csv`; a CATIA extension in `--video-extensions` exits 2. No compatibility
  with registries in the old order. No split, restore, payload flags, format detection or extraction.
- Data and artifact paths: `internal/catia/`, `internal/scanner/`, `internal/report/`,
  `internal/state/scanstats.go`, `internal/archive/scan_pipeline.go`, `internal/cli/`.
- Execution path: Synthetic files in `t.TempDir()` with invented names; no content from gitignored
  experimental trees.
- Acceptance gates: CATIA extensions set `is_catia` and not `is_video` whatever the content;
  `._fixture.CATPart` AppleDouble is not CATIA; the registry header matches the contract and a
  written registry reads back with all flags and metadata; `--video-extensions CATPart` exits 2;
  `make ci` and `make test-integration` pass.
- Documentation target: `docs/impl/current/catia-archive.md`; column references in
  `docs/impl/current/archive-registry.md` and the operator manuals.
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none.

## Implementation

`internal/catia` is a leaf package with the built-in kind table (`CATPart`, `CATProduct`,
`CATDrawing`, `cgr`, `3dxml`). `scanner.Classify` sets `IsCatia` from the last dotted suffix and
clears `IsVideo`. AppleDouble magic is classified before extension rules, so
`._fixture.CATPart` is never CATIA. Empty CATIA files are still CATIA: the flag does not read
content. After the override, `IsMedia` is recomputed from the stage-1 formula (`is_video` or
`is_picture` or `audio/`), so a CATIA file whose bytes happen to look like MP4 is not media and
does not trigger ISO BMFF or ffprobe collection.

The file registry required columns are now `rel_path`, `file_name`, `file_type`, `file_size`,
`is_large`, `file_mime`, `is_binary`, `is_media`, `is_picture`, `is_video`, `is_catia`
(`FileRegistryKeep` 11). Readers require this header. Scan statistics add `catia`; report JSON
omits it when zero (`omitzero`, because `omitempty` does not omit a zero struct). Root-level
`arxgo-catia.csv` is reserved with the other registry names. `--video-extensions` that names a
CATIA extension exits 2 on scan and split. CLI validates through `scanner.IsCatiaExtension` so
it does not import `internal/catia`.

No format detection, extraction, split or restore. Current-state page:
[CATIA archive](../current/catia-archive.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| CATIA extensions set `is_catia` and not `is_video` | `TestClassifyCatiaExtensions`; `TestScanClassifiesCatiaAndNotVideo` (synthetic `fixture-*.CATPart/CATProduct/CATDrawing/cgr/3dxml` in `t.TempDir()`, including video-shaped and ZIP content); candidates contain only the video | pass, Linux |
| `._fixture.CATPart` AppleDouble is not CATIA | `TestClassifyCatiaExtensions`; `TestScanClassifiesCatiaAndNotVideo` (`cad/._fixture.CATPart` with magic `00 05 16 07`) | pass, Linux |
| Registry header and round-trip | `TestFileRegistryHeaderOrder`; `TestRegistryRoundTripFlagsAndMetadata`; `TestLoadRegistryRejectsPreviousColumnOrder`; golden `test/testdata/archive/scan-registry.golden.csv`; `TestScanGoldenRegistry` | pass, Linux |
| `--video-extensions CATPart` exits 2 | `TestScanRejectsCatiaVideoExtensions`; `TestRunExitCodes` case `scan catia video extension`; `TestValueValidation` | pass, Linux |
| Zero `catia` omitted from report JSON | `TestScanSummaryOmitsZeroCatiaJSON`; `TestScanGoldenRegistry` (raw `report.json`) | pass, Linux |
| `make ci` | `make ci` (fmt-check, vet, vet-windows, `go test ./...`, `make build-all`, lint-spec-plan, lint-doc-links) | pass, Linux; Windows cross-compiled only |
| `make test-integration` | `make test-integration` | pass, Linux |

## Audit handoff

`none identified`. Reviewed: kind table vs architecture leaf rule, AppleDouble vs extension
order, column-order writer/reader/compaction, scan statistics JSON, reserved `arxgo-catia.csv`,
CLI rejection, and that CATIA rows are not video candidates.

## Close or resume

All gates passed. Record indexed; current pages and operator manuals updated; plan task removed
and remaining CATIA tasks now depend on this record. Capability `catia-archive` stays planned.
Plan counts after: 16 open (13 agent, 3 human); next eligible
`generalize-payload-split-restore`; also eligible `implement-catia-extraction`.
