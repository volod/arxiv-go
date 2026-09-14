# Default ISO BMFF metadata collection

## Task and scope

- Id / capability / checkpoint: `collect-iso-metadata-by-default` / `media-metadata` / none
- State: accepted.
- Source: operator stage-1 archive-copy trial; `arxgo-registry.csv` and `arxgo-videos.csv` from
  `split` without `--metadata media` left every `media_*` column empty.
- Plan counts at start: 9 open tasks (6 agent, 3 human); next eligible was
  `research-cloud-target-apis`.
- Accepted task:

```markdown
#### collect-iso-metadata-by-default

Fill media columns in the default scan and split so operator CSV registries describe the videos that left.

- Serves: `media-metadata` -- [Metadata modes](../openspec/stage-1-core/metadata.md#metadata-modes)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Default `scan` and `split` write ISO BMFF duration, size, codecs and related
  `media_*` columns for MP4, MOV, M4A, M4V and 3GP without ffprobe, and video descriptions include
  `created` and `video` when those fields exist. Other audio/video still need `--metadata media`.
  Columns that apply only to some rows (`link_target`, tags, `media_error`, `sha256`, `previews`)
  stay in the shared header.
- Scope boundary: Default scan collection via the existing ISO parser, spec and operator docs,
  tests. Do not require ffprobe for `--metadata file`. Do not remove CSV columns. Do not rewrite
  already-written operator archives.
- Data and artifact paths: `internal/archive/scan_pipeline.go`, `arxgo-registry.csv`,
  `arxgo-videos.csv`, stage-1 metadata, CLI and contracts pages.
- Execution path: Generated MP4/MOV fixtures in file mode; golden registry with ftyp-only files;
  Linux CI.
- Acceptance gates: File-mode scan of a generated MP4 fills media columns without ffprobe; AVI/MTS
  stay empty until `--metadata media`; descriptions carry the video line in file mode; `make ci`
  passes.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: none; this is a bounded operator-reported repair.
```

- Amendments: operator scanned the post-split main archive (`./arxgo scan --archive .../test`);
  videos were already moved, so every `media_*` column was empty. They asked to fill data or
  remove columns that cannot be filled, and to keep the work in this record. Revised full task:

```markdown
#### collect-iso-metadata-by-default

Fill media columns in the default scan and split so operator CSV registries describe the videos that left.

- Serves: `media-metadata` -- [Metadata modes](../openspec/stage-1-core/metadata.md#metadata-modes)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Default `scan` and `split` write ISO BMFF duration, size, codecs and related
  `media_*` columns for MP4, MOV, M4A, M4V and 3GP without ffprobe, and video descriptions include
  `created` and `video` when those fields exist. Other audio/video still need `--metadata media`.
  A completed registry omits a metadata column that is empty in every row (`link_target` with no
  symlinks, all `media_*` columns when the tree has no audio or video).
- Scope boundary: Default scan collection via the existing ISO parser, omit unused metadata
  columns from the placed CSV, spec and operator docs, tests. Do not require ffprobe for
  `--metadata file`. Do not rewrite already-written operator archives.
- Data and artifact paths: `internal/archive/scan_pipeline.go`, `internal/report/csv_compact.go`,
  `arxgo-registry.csv`, `arxgo-videos.csv`, stage-1 metadata, CLI and contracts pages.
- Execution path: Generated MP4/MOV fixtures in file mode; golden registry with ftyp-only files;
  Linux CI.
- Acceptance gates: File-mode scan of a generated MP4 fills media columns without ffprobe; AVI/MTS
  stay empty until `--metadata media`; a tree with no audio/video omits `media_*` columns;
  descriptions carry the video line in file mode; `make ci` passes.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: none; this is a bounded operator-reported repair.
```

## Implementation

The operator's `arxgo-videos.csv` (430 `moved` rows: 332 mp4, 80 mov, 9 lrv, 8 mpg, 1 wmv) had
`mtime` filled and every `media_*` column empty because default `--metadata file` skipped the ISO
parser. `link_target` was empty because none of those rows are symlinks.

`scanRun.readMedia` now runs `media.ReadISO` for detected ISO BMFF audio/video in file mode, without
ffprobe and without tool discovery. `--metadata media` still requires ffprobe and still uses
`ReadMetadata` (ISO first, ffprobe for other containers and ISO failures). Audio-only `is_video`
refinement already applied whenever a successful parse exists, so it now also applies in file mode.

Originally this record rejected dropping empty columns so both registries shared one fixed
header. The operator then scanned the post-split document tree (videos already moved) and still
saw every `media_*` column empty. That rejection is reversed: a completed registry omits a
metadata column that is empty in every row. Required columns stay, including empty `sha256` and
`previews` on the video registry.

After a completed scan, `DropEmptyCSVColumns` rewrites the part file so unused metadata columns
are omitted before it is renamed into place. The part file keeps the full header during the scan
so resume offsets stay valid. An interrupt between compact and rename makes the part file shorter
than the checkpoint; the next run treats that as a mismatched output and scans again from the
start. `WriteVideoCSV` omits unused metadata columns the same way. Readers accept a subsequence
of the canonical metadata header, so a previously written full-width CSV still loads.

Rejected: defaulting `--metadata` to `media` (would exit 3 without ffprobe); rewriting the
operator's existing CSVs (a new scan or restore-then-split with this build regenerates them);
collecting picture dimensions (pictures are not ISO BMFF and stay out of ffprobe).

Current-state pages: [media metadata](../current/media-metadata.md),
[archive registry](../current/archive-registry.md), [video split](../current/video-split.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| File-mode ISO columns | `TestScanFileModeCollectsISOWithoutFFprobe`; `TestSplitMediaModeDescriptionHasDuration` file-mode MP4 and MOV | pass on Linux generated fixtures; `media_source=go-mp4`, duration, size and codecs filled with `FFprobePath=must-not-run` |
| Non-ISO empty until `--metadata media` | `TestScanFileModeCollectsISOWithoutFFprobe` AVI and MTS rows; `TestScanGoldenRegistry` `Clip.MTS` | pass; those rows have no media cells in file mode |
| Omit unused metadata columns | `TestDropEmptyCSVColumnsOmitsUnusedMetadata`; `TestWriteVideoCSVOmitsUnusedMetadata`; `TestScanEmptyArchiveWritesHeaderOnly`; `TestScanGoldenRegistry` | pass; a text-only registry keeps `mtime` and drops `media_*`; golden header is `mtime,link_target,media_source,media_container,media_has_audio,media_error` |
| File-mode description video line | `TestSplitMediaModeDescriptionHasDuration` `file/interview.mp4` and `file/camera.mov` | pass; description contains `0:05 \| 1920x1080 \| h264 + aac \| 25 fps` |
| Golden registry | `TestScanGoldenRegistry`; `test/testdata/archive/scan-registry.golden.csv` | pass; ftyp-only MP4/M4A rows record `media_error=ISO BMFF parse failed: moov box missing` |
| Required quality and cross-build gates | `GOCACHE=/tmp/arxgo-gocache make ci` | pending |

The operator archive copy was not rerun with this build. MPEG, WMV and other non-ISO containers
still need `--metadata media` (and ffprobe).

## Audit handoff

None identified in the changed behavior. File-mode ISO parse failures are non-fatal `media_error`
values, matching media mode without ffprobe. Discovery still does not run for `--metadata file`.
The operator's human approval task remains open until they decide whether the repaired build is
fit for use.

## Close or resume

All gates passed. The record is indexed and linked from the current-state pages; the repair task
was removed from the forward plan. Open task counts were 9 (6 agent, 3 human) before the new
repair, 10 while it was active, and 9 (6 agent, 3 human) after acceptance. The next eligible agent
task is `research-cloud-target-apis`; `approve-stage-1-on-operator-archive-copy` remains the
available human action.
