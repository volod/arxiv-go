# CATIA Archive

Accepted work: [0043 CATIA classification](../records/0043-catia-implement-catia-classification.md);
[0044 Payload split and restore](../records/0044-catia-generalize-payload-split-restore.md);
[0045 CATIA extraction](../records/0045-catia-implement-catia-extraction.md).
Specification: [CATIA files](../../openspec/stage-4-catia/catia.md);
[split and restore](../../openspec/stage-4-catia/split-restore.md) is specified; the shared payload
executor exists, CATIA split and restore are not implemented. The capability remains planned until
those tasks ship.

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
the same way as `arxgo-videos.csv`. Split and restore of CATIA files are not available yet.

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

Split does not call the extractor yet.

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
  and the future `arxgo-catia.csv`. Registries in the earlier video order do not load.
- **Sidecar event families** (`state.EventFamily`, `wal_event.go`). Begin, done or failed, delete
  and deleted steps of a family use the version 2 envelope through `WAL.BeginEvent` and
  `WAL.FinishEvent`; a family's outcome for another family's begin is rejected on write and is
  corrupt on replay. `archive.eventIndex` replays one family (owned, generating, deleting) and
  removes part files of unfinished generations with the family's part-path rule. Previews are the
  only registered family.

No CLI flag, CATIA candidate, CATIA registry or text event exists yet.
