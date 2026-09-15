# CATIA Archive

Accepted work: [0043 CATIA classification](../records/0043-catia-implement-catia-classification.md);
[0044 Payload split and restore](../records/0044-catia-generalize-payload-split-restore.md);
[0045 CATIA extraction](../records/0045-catia-implement-catia-extraction.md);
[0046 CATIA split](../records/0046-catia-implement-catia-split.md).
Specification: [CATIA files](../../openspec/stage-4-catia/catia.md);
[split and restore](../../openspec/stage-4-catia/split-restore.md). CATIA split ships; text
sidecars (`--catia-text`) and CATIA restore are not implemented, so the capability remains planned.

## Classification (`internal/catia`, `internal/scanner`)

`scan` always classifies CATIA files. There is no CATIA flag on scan.

`internal/catia` is a leaf package that owns the built-in kind table. The scanner sets `is_catia`
from the last dotted suffix, compared case-insensitively:

| Extension | Kind token |
| --- | --- |
| `.CATPart` | `CATPart` |
| `.CATProduct` | `CATProduct` |
| `.CATDrawing` | `CATDrawing` |
| `.cgr` | `cgr` |
| `.3dxml` | `3dxml` |

Content is not read for this flag, so a truncated or empty CATIA file is still marked. A macOS
AppleDouble sidecar (`._<name>`, magic `00 05 16 07`) is never CATIA, whatever its extension.
`is_catia` files are never `is_video`. MIME, `file_type`, `is_binary`, `is_picture` and `is_media`
follow the stage-1 rules after that override (`is_media` is video, picture or `audio/`).

A built-in CATIA extension in `--video-extensions` is a usage error (exit 2), on both `scan` and
`split`.

The file registry always has `is_catia` as required column 11, after the other type flags, in the
[column order](../../openspec/stage-1-core/contracts.md#file-registry-csv). Readers require this
header; registries written in the previous order do not load. Scan statistics include a `catia`
count and byte total, omitted from report JSON when zero. Root-level `arxgo-catia.csv` is reserved
the same way as `arxgo-videos.csv`.

## Extraction (`internal/catia`, `internal/report`)

`catia.Extract` / `ExtractPath` reads one CATIA file in pure Go. Kind comes from the file name;
format from the leading bytes (`V5_CFV2`, `zip`, `xml`, or `unknown`). Memory is bounded by the
component window (4 MiB), ZIP member caps (1024 members, 64 MiB each, 256 MiB total) and the 1 MiB
sidecar collection cap, not by file size. An extraction error fills empty/unknown fields and sets
`ErrorKind`; it never panics. A broken 3dxml root is `TextFailed` (no sidecar body).

- **V5.** Length-prefixed string properties: `LastSaveVersion` (first wins; `<Release>` / `<ServicePack>`
  written as `<Release>30/<Release>`), then `MinimalVersionToRead`, plus `CATBuildLevel` for the
  sidecar. Components: stream to the first `CATOctetArray` ... `0x08FINJPL` window, strip `;` U+0001,
  split on U+0001 U+0004 `File`, skip malformed and `feat` chunks, drop the file's own base name,
  unique-sort. A missing window is `0 components`.
- **3dxml.** Raw XML or `archive/zip`. Unsafe ZIP names (`..`, absolute, volume) are skipped. Header
  `SchemaVersion` becomes release `3DXML <version>`. `ReferenceRep` `associatedFile` and `urn:3DXML:`
  file parts become components (external URLs ignored); `Reference3D` / `Instance3D` `name` values
  go to strings.
- **Strings.** ASCII 0x20-0x7E and UTF-16LE runs, at least 6 characters with 3 ASCII letters,
  excluding component names, unique-sorted. ZIP 3dxml harvests XML text and attributes, not
  compressed bytes.
- **Report.** `RenderDescription` with `Catia` set writes `catia:` instead of `created:` / `video:`.
  `CatiaLine` is `kind | format | release | N component(s)`. `RenderCatiaText` writes the
  `arxgo-text:` sidecar (properties, components, strings) with the 1 MiB cap and `truncated`.

CATIA split calls `ExtractPath` on the placed destination for the description; text sidecars are
not written yet.

## Payload executor (`internal/archive`, `internal/state`, `internal/report`)

Split and restore run one executor for every payload kind. `archive.Config.Payload` holds the kind
and its mirror root; `cli` sets `video` and the video archive, and there are no CATIA flags yet. A
split or restore of kind `catia` is refused by `archive.Start` ("not available in this build").

- **Payload spec** (`payload.go`, `payload_video.go`). The kind selects the candidate predicate
  (`ScanConfig.Candidate`; video: `is_video`), the preflight role and need names (`video_archive`,
  `video_registry`, `videos`, `largest_video`), the payload registry name and loader, the counters
  (video: `videos_*`, `video_bytes`, `video_archive_bytes_*`) used by execution, progress, the
  partial status and the report roots, and two hook sets. Split hooks prepare (registry, history,
  preview index, skip paths), plan (preview bytes for preflight), start the post-commit sidecar
  (preview queue with catch-up) and write the registry. Restore hooks provide the registry rows and
  extra candidates, finish interrupted sidecar work, run per-commit cleanup (`--previews delete`)
  and update the registry. Transfer, WAL `begin` through `commit`, recovery, preflight, progress,
  locks, description occupancy and conflict naming are shared code that names the mirror root
  generically.
- **Run history by payload** (`history.go`). `readHistory(archive, kind)` returns only runs whose
  `options.json` names that payload, with that run's archive and mirror roots. The video registry,
  the preview index, restore description hints (through the payload registry) and mirror directory
  cleanup use video runs only. Scans and run directories without readable options are not history.
- **Run options.** Split and restore write `payload` and the matching root (`video_archive`, or
  `catia_archive` for a CATIA run). The payload is a defining option. Replacing an interrupted run
  rebuilds its resolver from the recorded payload (`RecovererFor(op, payload, options)`); another
  payload or mirror root exits 5 naming that payload's mirror flag, and a run without a payload is
  corrupt state.
- **Payload registry columns** (`report.PayloadHeader`, `report.PayloadRow`). Columns 1-10 are
  `rel_path`, `file_name`, `status`, `url`, `description_rel_path`, `file_size`, `sha256`,
  `transfer`, `run_id`, `file_mime`, shared by `arxgo-videos.csv` (then `previews` and metadata)
  and `arxgo-catia.csv`. Registries in the earlier video order do not load.
- **Sidecar event families** (`state.EventFamily`, `wal_event.go`). Begin, done or failed, delete
  and deleted steps of a family use the version 2 envelope through `WAL.BeginEvent` and
  `WAL.FinishEvent`; a family's outcome for another family's begin is rejected on write and is
  corrupt on replay. `archive.eventIndex` replays one family (owned, generating, deleting) and
  removes part files of unfinished generations with the family's part-path rule. Previews are the
  only registered family.

No text event family exists yet.

## CATIA split (`internal/cli`, `internal/archive`, `internal/report`, `internal/state`)

`arxgo split --catia --archive PATH --catia-archive PATH` moves every `is_catia` file into the
CATIA archive through the shared executor. Default `split` (or `--video`) still moves only videos.

- **Flags and validation** (`cli.selectPayload`, `cli.checkRoots`). `--video` and `--catia` are
  split flags; `--catia-archive` is a common flag of split and scan (scan ignores it). A payload
  flag on the command line ignores both `ARXGO_VIDEO` and `ARXGO_CATIA`. Exit 2 before the lock:
  both payloads; `--video-archive` on the command line with `--catia`; `--catia-archive` on the
  command line without it; a missing selected root; `--sample` or `--image` other than `none` with
  `--catia`; `--publish` (still reserved). The archive, the selected root and the other payload's
  root when it is set from any source are pairwise neither equal nor nested (the other root is
  compared by path and need not exist). Only the selected root is stored in the options
  (`Common.VideoArchive` or `Common.CatiaArchive`), so an `ARXGO_VIDEO_ARCHIVE` value never becomes
  a defining option of a CATIA run. `SplitOptions.CreateMirror` replaces `CreateVideoArchive`.
  Restore has no CATIA flags yet.
- **Payload spec** (`payload_catia.go`). Candidate `is_catia`; preflight role `catia_archive` with
  needs `catia_registry`, `catia`, `largest_catia`; counters `catia_done`, `catia_skipped`,
  `catia_failed`, `catia_bytes`, `catia_archive_bytes_written`/`_freed` (checkpoint, report,
  progress, finish log keys); no post-commit sidecar; no restore (`archive.Start` refuses a CATIA
  restore before any lock). A resumed run must also match the recorded payload and mirror root.
- **Description.** `DescriptionConfig.Payload` `catia` makes `MarkdownDescription.Write` run
  `catia.ExtractPath` on `tx.Begin.Dst` after `placed` (execute and recovery), render the `catia:`
  line without `video:`/`created:`, and keep a `state.CatiaSummary` (kind, format, release empty
  when unknown, component count) for the `described` record. Recovery gets it through the optional
  `state.DescribedAnnotator` (`SplitResolver.DescribedCatia`). An extraction error is logged at warn
  with its kind (`CATIA metadata extraction failed; description has empty values`) and never fails
  the move. Occupancy, fallback names (`.arxgo.md`), conflicts, adoption and source-changed retry
  are the video rules.
- **Registry** (`report.CatiaHeader`, `CatiaRow`, `WriteCatiaCSV`, `LoadCatiaCSV`,
  `MergeCatiaRows`). `arxgo-catia.csv` in both roots: payload columns 1-10, then `text_rel_path`
  (always empty in this build), `catia_kind`, `catia_format`, `catia_release`,
  `catia_components`, `mtime`; columns 11-16 empty in every row are omitted and readers accept any
  canonical-order subset. Rows replay CATIA runs only (`splitEvents`, shared with video) with the
  video status rules; the summary comes from the `described` record, so regeneration never re-reads
  CATIA files; `mtime`, name, size and MIME are completed from the file registry; URLs follow the
  video rules with the CATIA archive as mirror. A corrupt registry stops before the first move
  (exit 5). CATIA split never writes `arxgo-videos.csv`, and video split never writes
  `arxgo-catia.csv`.
- **Recovery by the other payload** (`recoverReplaced`, `lockOtherMirror`). When a split or restore
  replaces an interrupted run of the other payload in the same archive, it takes the lock of the
  mirror root that run recorded (with the current `--force-unlock` setting), rebuilds its resolver
  (`cli.recovererFor` accepts CATIA split runs) and releases the lock after recovery. A missing root
  exits 5 naming it. A run of the same payload on another mirror root, or a scan, still exits 5.
