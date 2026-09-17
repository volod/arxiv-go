# Data contracts

Owners: `archive-registry` (file registry), `video-split` (video registry and description),
`catia-archive` (CATIA registry, description and text sidecar),
`crash-safety` (run lock, run options, WAL, checkpoint, run report). A change to a JSON format
increments its version field; a CSV schema change is identified by its required columns.
Both require a spec amendment. Every CSV is written with all of its columns on every run
([full schema](registry.md#full-schema)); readers also accept files from earlier builds that omitted
optional columns empty in every row.

All text outputs are UTF-8 without BOM, `\n` line endings on every platform, paths relative to the
owning root with `/` separators. Exception: a Linux file name that is not valid UTF-8 is written to
the CSV files as its raw bytes, while JSON (candidate list, WAL) replaces the invalid
bytes with U+FFFD; such a video is registered but cannot be split and is reported as skipped
(exit 6). CSV follows RFC 4180 as written by Go `encoding/csv` (quotes when
needed). Booleans are `true`/`false`. Sizes are bytes as base-10 integers. Times are RFC 3339 UTC.

## File registry CSV

`<archive>/arxgo-registry.csv` (or `--registry`). One row per traversed entry except directories,
skipped entries (special files and entries that cannot be read) and owned arxgo artifacts, plus one
preserved row per payload file split moved out ([archive view](registry.md#archive-view)). Symlink
rows have `file_type` `symlink`, size 0, an empty `file_mime` and all flags `false`. Column order is
fixed:

| # | Column | Example | Notes |
| --- | --- | --- | --- |
| 1 | `rel_path` | `projects/2024/interview.mp4` | Relative to the archive root, including the file name |
| 2 | `file_name` | `interview.mp4` | Base name |
| 3 | `file_type` | `mp4` | See [type detection](registry.md#type-detection) |
| 4 | `file_size` | `734003200` | Bytes |
| 5 | `is_large` | `false` | `file_size >= --large-threshold` |
| 6 | `file_mime` | `video/mp4` | |
| 7 | `is_binary` | `true` | |
| 8 | `is_media` | `true` | video, audio or picture |
| 9 | `is_picture` | `false` | |
| 10 | `is_video` | `true` | |
| 11 | `is_catia` | `false` | [CATIA classification](../stage-4-catia/catia.md#classification) |
| 12 | `location` | `archive` | `archive`, `video-archive` or `catia-archive`: where the file is now |
| 13 onward | [Flat metadata columns](#flat-metadata-columns) | | Modification time and optional media details |

Columns read from general to specific: what the file is (path, name, type), how big it is, how
detection saw it, then the payload flags that decide what split moves, then where the file lives.
`is_large` implements "highlighting files larger than a specified size" as an explicit column.

## Flat metadata columns

The file registry and the video registry append these columns in this order: `mtime`, `link_target`, `media_source`,
`media_container`, `media_duration_s`, `media_bit_rate`, `media_width`, `media_height`,
`media_rotation`, `media_frame_rate`, `media_video_codec`, `media_audio_codec`, `media_has_audio`,
`media_video_streams`, `media_audio_streams`, `media_subtitle_streams`, `media_creation_time`,
`media_tag_title`, `media_tag_comment`, `media_tag_encoder`, `media_tag_artist`,
`media_tag_album`, `media_tag_date`, `media_tag_genre`, `media_tag_composer`,
`media_tag_grouping`, `media_tag_description`, `media_tag_copyright`, `media_error`.
The former `v`, `mode` and JSON `metadata` fields are absent.

`mtime` is RFC 3339 UTC to the second. `link_target` holds a symlink's link text without following
it; it is empty for regular files. `media_*` values are filled when ISO BMFF metadata was collected
(default `--metadata file` for MP4, MOV, M4A, M4V and 3GP) or when `--metadata media` ran ffprobe.
Numeric zero values and unknown values are empty; `media_has_audio` is `true` or `false` when media
metadata exists. `media_duration_s`, dimensions, codecs and rational `media_frame_rate` expose the
information rendered in a video's `video:` description line as separate CSV fields. Every column is
written, also when it is empty in every row. Readers treat a metadata column missing from an older
file as empty. Tag columns hold the selected container text tags; other container tags are not
collected.

## Registry stamp

`<archive>/.arxgo/registry.json` identifies the last file registry arxgo wrote or found unchanged
for this archive, so the next scan can reuse its rows
([incremental update](registry.md#incremental-update)). One JSON object and a newline, written
atomically and indented by two spaces like `options.json`, `checkpoint.json` and `report.json`
(shown compact here):

```json
{"v":1,"registry":"/data/archive/arxgo-registry.csv","size":29012345,
 "sha256":"9f86d081884c7d659a2feaa0c55ad015a3bf4f1b2b0b822cd15d6c15b0f00a08",
 "run_id":"20260913T101500Z-1a2b3c4d","op":"split","scan_started_at":"2026-09-13T10:15:02Z",
 "detect":{"version":"v1.0.0","metadata":"file","video_extensions":["3g2","3gp","asf"]}}
```

`registry` is the absolute registry path; `size` and `sha256` describe the file as renamed into
place, or as rewritten by split's `location` update or by restore; `scan_started_at` is the start of
the scan phase of the run's first process, to the second; `detect.video_extensions` is the effective
list (built-in plus `--video-extensions`), lower-cased, sorted and unique. A stamp that cannot be
decoded, has another `v` or names another path is ignored, and the next registry write replaces it.

## Candidate list

`candidates.jsonl` in the run directory: one JSON object per line for every payload candidate
registry row with `location` `archive` (`is_video=true` in video mode, `is_catia=true` in CATIA
mode), in walk order. It is
run state for split and restore, not an operator output.

```json
{"v":1,"rel_path":"projects/2024/interview.mp4","size":734003200,
 "mtime":"2024-05-01T10:22:03.123456789Z","mime":"video/mp4","file_type":"mp4"}
```

`mtime` keeps full precision so a later transaction can detect a changed source.

## Video registry CSV

`arxgo-videos.csv` in both roots. One row per video handled by any split run; restore updates
`status`.

| # | Column | Notes |
| --- | --- | --- |
| 1 | `rel_path` | Original path in the archive; the same relative path in the mirror |
| 2 | `file_name` | |
| 3 | `status` | `moved`, `restored`, `conflict`, `skipped` |
| 4 | `url` | `--base-url` link, otherwise `file://` URL of the local file |
| 5 | `description_rel_path` | Path of the description in the archive |
| 6 | `file_size` | |
| 7 | `sha256` | Empty unless `--verify hash` |
| 8 | `transfer` | `rename` or `copy` |
| 9 | `run_id` | Run that last changed the row |
| 10 | `file_mime` | |
| 11 | `previews` | Stage 2: `;`-separated recorded preview paths relative to the archive; empty when none, always written |
| 12 onward | [Flat metadata columns](#flat-metadata-columns) | Same layout as the file registry |

Columns 1-10 are the payload registry columns shared with the [CATIA registry](#catia-registry-csv):
what the file is and its state first, then where to find it and its description, then transfer
evidence.

`rel_path` names the video in both roots. For a moved row without a base URL, `url` points into
the video archive; after restore it points into the main archive. Skipped or conflict rows use the
main archive path when they have no explicit URL. Readers accept the required columns plus any
subset of the metadata columns in the canonical order, as written by earlier builds.

Rows are sorted by `rel_path` walk order key. The file is regenerated from the existing file and
the WAL of every run, written via `.arxgo-part` and rename. Runs are replayed one after another in
the order they started (`created_at` in `options.json`, then run id), so a later run wins: a
committed split makes the row `moved`; a split aborted at its destination makes it `conflict`, and
any other aborted split `skipped`, unless the row is `moved`; a committed restore sets `restored`
and that restore's `run_id`. A split then adds `conflict`/`skipped` rows for the videos it skipped
before a transaction began. Restore only updates rows; it never adds one.

## CATIA registry CSV

`arxgo-catia.csv` in the archive root and the CATIA archive root. One row per CATIA file handled by
any CATIA split run; restore updates `status`.

| # | Column | Notes |
| --- | --- | --- |
| 1-10 | payload registry columns | As in the [video registry](#video-registry-csv) |
| 11 | `text_rel_path` | Owned text sidecar path relative to the archive, from the last `text_done`; empty when none |
| 12 | `catia_kind` | Kind token |
| 13 | `catia_format` | Format token |
| 14 | `catia_release` | Release token, empty when `unknown` |
| 15 | `catia_components` | Component count |
| 16 | `mtime` | As in the flat metadata columns |

The `catia_*` values come from the `catia` object of the transaction's `described` WAL record
([WAL record](#wal-record)), so regenerating the registry never re-reads CATIA files. Columns 11-16
are always written; readers accept files from earlier builds that omitted them. Replay, sort, atomic write and
`moved`/`restored`/`conflict`/`skipped` rules are those of the video registry, applied to CATIA runs.
Video split does not rewrite this file; CATIA split does not rewrite `arxgo-videos.csv`.

## Video description

`<archive>/<rel_path>.md`, for example `projects/2024/interview.mp4.md`, describes the original
video that split moved: one block of `key: value` lines without blank lines. Every field is about
that video and its move. Preview link lines follow only when previews of the video were generated;
without previews the file holds the video's metadata alone.

```markdown
arxgo: projects/2024/interview.mp4
archive: /data/archive
file_size: 734003200 (700.0 MiB)
file_mime: video/mp4
sha256: 2c26b46b68ffc68ff99b453c1d30413413422d706483bfa0f98a5e886266e7ae
created: 2024-05-01T09:51:00Z
modified: 2024-05-01T10:22:03Z
video: 30:34 | 1920x1080 | h264 + aac | 25 fps
moved_at: 2026-09-13T10:17:42Z
moved_to: [interview.mp4](file:///mnt/nas/video/projects/2024/interview.mp4)
url: https://storage.example.com/video/projects/2024/interview.mp4
- ![interview-img01.png](interview-img01.png)
- [interview-smpl01.mp4](interview-smpl01.mp4)
```

| Field | Content | Present |
| --- | --- | --- |
| `arxgo` | `rel_path` of the video, relative to the archive root | always, first line |
| `archive` | absolute path of the archive root the description was written in (native form; on Windows the backslashes make it quoted, for example `"D:\\archive"`) | always, second line |
| `file_size` | bytes, then the binary-unit size in parentheses (`0 (0 B)` for an empty file) | always |
| `file_mime` | detected MIME type | when detected |
| `sha256` | SHA-256 of the video | `--verify hash` |
| `created` | container creation time | when media metadata exists and the file has one |
| `modified` | file modification time | always |
| `video` | duration, display size, codecs, frame rate | when media metadata exists |
| `moved_at` | time the video was moved | always |
| `moved_to` | Markdown link to the moved video: its file name and the `file://` URL of its absolute path in the video archive (`file:///D:/...` on Windows, `file://host/share/...` for UNC) | always |
| `url` | `--base-url` link | `--base-url` |

Rules: the first line `arxgo: <rel_path>` marks a file as an arxgo description for that video; restore,
recovery and description-name collision checks read only that line, from the first 64 KiB. A value is
double-quoted, with `\"`, `\\`, `\n` and `\r` escapes, when it has leading or trailing space (a
Unicode white-space character of the UTF-8 decoded value), a quote, a backslash or a line break.
Times are RFC 3339 UTC. The file is written atomically. Preview files
(PNG frames and sample clips) are written next to the description, so their `- ` lines directly after the
fields link relatively, frames as images and samples as links, URL-escaped per segment; arxgo rewrites only those lines and removes them when no
preview remains.

## CATIA description

`<archive>/<rel_path>.md`, for example `cad/fixture-part.CATPart.md`, describes the CATIA file that
`--catia` split moved. The first-line marker is the same `arxgo: <rel_path>` as a video
description, so occupancy, conflict naming, restore and recovery reuse that check. The `video:` and
`created:` fields are omitted; a `catia:` summary line is written instead
([accessible metadata](../stage-4-catia/catia.md#accessible-metadata)). Component names, properties
and notes are not written here.

```markdown
arxgo: cad/fixture-part.CATPart
archive: /data/archive
file_size: 4096 (4.0 KiB)
file_mime: application/octet-stream
modified: 2026-09-15T12:00:00Z
catia: CATPart | V5_CFV2 | V5R30 SP5 | 0 components
moved_at: 2026-09-15T12:05:00Z
moved_to: [fixture-part.CATPart](file:///mnt/nas/catia/cad/fixture-part.CATPart)
```

| Field | Content | Present |
| --- | --- | --- |
| `arxgo` | `rel_path` of the CATIA file | always, first line |
| `archive`, `file_size`, `file_mime`, `sha256`, `modified`, `moved_at`, `moved_to`, `url` | as for a video description | same rules |
| `catia` | kind, format, release, component count | always on a CATIA description |

## CATIA text sidecar

With `--catia-text`, split writes `<archive>/<rel_path>.text.md` after the move commits, or for a
file an earlier CATIA split moved. Example: `cad/fixture-product.CATProduct.text.md`. When that name
is taken by anything other than an owned sidecar for the same file: `<rel_path>.arxgo.text.md`, then
the indexed name of the description rule. The first line marks ownership. UTF-8 without BOM, `\n`
line endings, at most 1 MiB, written through a part file and a non-replacing rename.

```markdown
arxgo-text: cad/fixture-product.CATProduct
archive: /data/archive
file_name: fixture-product.CATProduct
file_size: 8192 (8.0 KiB)
file_mime: application/octet-stream
modified: 2026-09-15T12:00:00Z
catia: CATProduct | V5_CFV2 | V5R30 SP5 | 2 components
moved_to: [fixture-product.CATProduct](file:///mnt/nas/catia/cad/fixture-product.CATProduct)
description: cad/fixture-product.CATProduct.md
extracted_at: 2026-09-15T12:05:01Z
truncated: false
properties:
- release: V5R30 SP5
- build_level: 2026-01-01.00.00
- part_number: FIXTURE-100
- revision: B
- definition: fixture assembly
- source: made
- description: invented assembly for the contract example
components:
- fixture-part.CATPart
- fixture-sub.CATProduct
notes:
- "1. Invented first requirement.\n2. Invented second requirement."
```

| Field or block | Content |
| --- | --- |
| `arxgo-text` | `rel_path` of the CATIA file; always first |
| `archive` | absolute archive root, as in the description; always second |
| `file_name` | base name of the CATIA file; always |
| `file_size`, `file_mime`, `sha256`, `modified`, `catia`, `moved_to`, `url` | the values of the description's fields of the same name, when the description has them |
| `description` | archive-relative slash path of the owned description the values come from; omitted, with the fields above, when split finds no owned description (a warning is logged) |
| `extracted_at` | RFC 3339 UTC |
| `truncated` | `true` when the 1 MiB cap or a 3dxml ZIP limit dropped items |
| `properties:` | `- key: value` items in a fixed order: `release`, `build_level`, `part_number`, `revision`, `definition`, `nomenclature`, `source`, `description`, `material` (V5); `schema_version`, `title`, `author`, `generator`, `created` (3dxml); omitted when empty |
| `components:` | sorted unique base names; omitted when empty |
| `notes:` | plain text of the file's RTF texts that pass the note filter, unique, in file order, line breaks escaped; omitted when empty ([descriptive text](../stage-4-catia/catia.md#notes)) |

Restore `--descriptions delete` removes the sidecar only when the first line is
`arxgo-text: <rel_path>` (first 64 KiB). Extraction and sidecar rules:
[text extraction](../stage-4-catia/catia.md#text-extraction).

## CATIA text index

`arxgo catia-index` writes `<archive>/arxgo-catia-text.md` (or `--out`): UTF-8 without BOM, `\n`
line endings, values escaped as description values are. Behavior:
[text index](../stage-4-catia/catia.md#text-index).

```markdown
# arxgo CATIA text index

archive: /data/archive
catia_archive: /mnt/nas/catia
history_at: 2026-09-15T12:05:02Z
files: 2
components: 2
missing_text: 1

## cad/fixture-part.CATPart

file_name: fixture-part.CATPart
file_size: 4096 (4.0 KiB)
file_mime: application/octet-stream
modified: 2026-09-15T12:00:00Z
catia: CATPart | V5_CFV2 | V5R30 SP5 | 0 components
moved_to: [fixture-part.CATPart](file:///mnt/nas/catia/cad/fixture-part.CATPart)
description: cad/fixture-part.CATPart.md

## cad/fixture-product.CATProduct

file_name: fixture-product.CATProduct
file_size: 8192 (8.0 KiB)
file_mime: application/octet-stream
modified: 2026-09-15T12:00:00Z
catia: CATProduct | V5_CFV2 | V5R30 SP5 | 2 components
moved_to: [fixture-product.CATProduct](file:///mnt/nas/catia/cad/fixture-product.CATProduct)
description: cad/fixture-product.CATProduct.md
text: cad/fixture-product.CATProduct.text.md
truncated: false
properties:
- release: V5R30 SP5
- build_level: 2026-01-01.00.00
- part_number: FIXTURE-100
- description: invented assembly for the contract example
components:
- fixture-part.CATPart
- fixture-sub.CATProduct
notes:
- "1. Invented first requirement.\n2. Invented second requirement."

## Missing text

- missing: cad/fixture-part.CATPart
```

| Part | Content |
| --- | --- |
| title | `# arxgo CATIA text index`, then a blank line |
| `archive` | absolute archive root |
| `catia_archive` | mirror root of the latest CATIA run; omitted without CATIA history |
| `history_at` | RFC 3339 UTC time of the newest record of the CATIA run history; omitted without history |
| `files`, `components`, `missing_text` | section count, component items listed in all sections, entries under `Missing text` |
| `## <rel_path>` | one section per moved CATIA file in walk order, preceded by a blank line; `rel_path` quoted when it needs escaping |
| identity lines | `file_name`, then `file_size`, `file_mime`, `sha256`, `modified`, `catia`, `moved_to`, `url` and `description` as in the [text sidecar](#catia-text-sidecar), from the owned description; from the sidecar without `description` when the description is gone |
| `text`, `truncated` | archive-relative path of the owned sidecar and its `truncated` value; omitted when the file is listed under `Missing text` |
| `properties:`, `components:`, `notes:` | the sidecar's blocks, items byte-identical; omitted when empty. A `strings:` block of an earlier sidecar is not repeated |
| `## Missing text` | last section, only when some file has no usable owned sidecar: `- <reason>: <rel_path>` with reason `not_recorded`, `missing`, `foreign` or `unreadable` |

## WAL record

```json
{"v":1,"txid":"20260913T101500Z-1a2b3c4d-000042","seq":187,"step":"begin","ts":"2026-09-13T10:17:40Z",
 "op":"split","rel_path":"projects/2024/interview.mp4","src":"/data/archive/projects/2024/interview.mp4",
 "dst":"/mnt/nas/video/projects/2024/interview.mp4","size":734003200,"mtime":"2024-05-01T10:22:03Z",
 "transfer":"copy"}
```

`mtime` keeps full precision (RFC 3339 with nanoseconds when present) because a copy compares it
exactly, and is written only by a step that has one: a zero `mtime` is omitted rather than
serialized as `0001-01-01T00:00:00Z`. Stage-1 transaction records remain version 1. Stage-2 preview events use version 2 in
the same JSON Lines WAL and the same `txid` and `seq` scheme. They use `rel_path` for the owning
video, `dst` for the absolute path in the main archive, `size` on completion or deletion, and
`reason` on failure. `preview_begin`/`preview_done`/`preview_failed` surround generation;
`preview_delete`/`preview_deleted` surround size-checked restore cleanup. A preview event may
belong to a later run than the video move it serves. Stage-4 CATIA text sidecars use the same
version 2 envelope and `txid`/`seq` scheme with steps `text_begin`/`text_done`/`text_failed`
(generation) and `text_delete`/`text_deleted` (restore cleanup). `rel_path` is the owning CATIA
file; `dst` is the absolute sidecar path in the main archive; `size` on completion or deletion;
`reason` on failure. A text event may belong to a later run than the CATIA move it serves. A CATIA
`described` record also carries `catia`:
`{"kind":"CATProduct","format":"V5_CFV2","release":"V5R30 SP5","components":12}`. Readers of earlier
runs resolve absolute `src`, `dst` and `description` paths against the `archive` root and the
`video_archive` or `catia_archive` root in that run's
`options.json`, so the registries stay correct after a root is mounted or renamed elsewhere. Later stage-1 steps carry only `v`, `txid`, `seq`, `step`, `ts` and step data (`sha256` on
`verified`, `description` on `described`/`description_removed`: the description written, or the owned description restore removed
or kept, omitted when there is none; `reason` on `aborted`). `txid` is `{run-id}-{6-digit}`; `seq`
increases by one for each record in the file. Records are shown wrapped here; on disk each is one
line. Recovery writes `aborted` with `reason` `unplaced` when work had not reached `placed`.

## Run lock

`<root>/.arxgo/lock`, one JSON object and a newline, created with `O_CREATE|O_EXCL`:

```json
{"v":1,"run_id":"20260913T101500Z-1a2b3c4d","pid":48213,"host":"archive-host",
 "started_at":"2026-09-13T10:15:00Z","op":"split","role":"archive",
 "root":"/data/archive","peer":"/mnt/nas/video"}
```

`role` is `archive` in the archive root and `mirror` in the video or CATIA archive root, where `peer` names
the owning archive. `scan` omits `peer`. A lock without `v` 1, `run_id`, a positive `pid` and `host`
is unreadable.

## Run options

`options.json` in the run directory, written once when the run is created:

```json
{"v":1,"run_id":"20260913T101500Z-1a2b3c4d","op":"split","version":"v1.0.0",
 "created_at":"2026-09-13T10:15:00Z","archive":"/data/archive","payload":"video",
 "video_archive":"/mnt/nas/video","defining":{"...":"..."},"options":{"...":"..."}}
```

`options` holds every validated option and `defining` the subset compared for resume (see
[integrity](integrity.md#state-layout)); both use the Go field names of the validated option
types, durations in nanoseconds and sizes in bytes. `dry_run` is present only for dry runs.
Split and restore runs always carry `payload` (`video` or `catia`) and the matching mirror root,
`video_archive` or `catia_archive`; `scan` carries neither. Replays of earlier runs read only runs of
the selected payload ([run history by payload](../stage-4-catia/split-restore.md#run-history-by-payload)).
Restore runs also carry `sidecar_cleanup` (boolean): `true` when the run deletes the owned
post-commit sidecars of the files it restores (`--previews delete` for video, `--descriptions delete`
for CATIA). The archive layer writes it from the restore configuration, not from the CLI option
names, and readers use only this field to learn an earlier restore's cleanup intent; a restore run
without it (written by an earlier build) counts as `false`.

## Checkpoint

```json
{"v":1,"run_id":"20260913T101500Z-1a2b3c4d","op":"split","phase":"execute",
 "scan_cursor":["projects","2024","zeta.pdf"],"registry_offset":81234567,
 "candidate_index":42,"wal_offset":18233,"counters":{"files":1203344,"bytes":4012345678901,
 "videos_done":41,"videos_skipped":1,"videos_failed":0,"video_bytes":30000000000,
 "archive_bytes_written":164000,"video_archive_bytes_written":30000000000,
 "archive_bytes_freed":30000000000},"elapsed_s":5234.2,
 "written_at":"2026-09-13T11:44:54Z"}
```

`phase` is `start` until the operation enters its first phase. `scan_cursor` is omitted when
empty. During and after a scan the checkpoint also holds `candidates_offset` (durable length of
`candidates.jsonl`, omitted when zero), `registry_base` (`{"size","sha256"}` of the base registry
the scan reuses, omitted without a base) and `scan`: the scan statistics matching the cursor and
offsets (`complete`, `started_at` (the scan phase start of the run's first process, to the second,
which the registry stamp records as `scan_started_at`), `files`, `dirs`, `symlinks`, `bytes`,
`binary`/`media`/`picture`/`video`/`catia`/`large` as `{"count","bytes"}`, `largest_video`, `mime`
per type, `skipped` per reason, `reused`, `preserved` as `{"count","bytes"}`, and `registry`
(`written` or `unchanged`) once complete). `catia` is omitted
when zero. `video_bytes` (bytes of handled videos), the per-root `*_bytes_written`/`*_bytes_freed`
counters, the split preview counters `previews_done`/`previews_failed`, and the CATIA counters
`catia_done`/`catia_skipped`/`catia_failed`, `catia_bytes`, `catia_archive_bytes_written`/
`catia_archive_bytes_freed` and `texts_done`/`texts_failed` are omitted when zero. A CATIA run uses
the `catia_*` counters and never the `videos_*` ones. `elapsed_s` accumulates across resumed processes.

## Run report

`report.json` in the run directory; its presence marks the run complete:

```json
{"v":1,"run_id":"20260913T101500Z-1a2b3c4d","op":"split","version":"v1.0.0",
 "status":"partial","resumed":true,"started_at":"2026-09-13T12:00:00Z",
 "finished_at":"2026-09-13T13:10:00Z","wall_s":4200.0,"counters":{"files":1203344,"...":0},
 "phases":[{"name":"execute","started_at":"2026-09-13T12:00:02Z","wall_s":4190.1,
 "counters":{"videos_done":40,"...":0}}],
 "roots":[{"root":"/data/archive","bytes_written":164000,"bytes_freed":30000000000},
 {"root":"/mnt/nas/video","bytes_written":30000000000,"bytes_freed":0}],
 "issues":[{"kind":"skipped","rel_path":"projects/a.mp4","reason":"destination exists"}]}
```

Runs that scanned add `scan`: `files`, `dirs`, `symlinks`, `bytes`, the flag totals
(`binary`, `media`, `picture`, `video`, `catia`, `large`; `catia` omitted when zero),
`top_mime` (up to 10 `{"mime","count","bytes"}` by bytes), `skipped` by reason, `reused`,
`preserved` (`{"count","bytes"}`), `registry` (`written` or `unchanged`) and `elapsed_s`.

After a resumed run the item counters (`videos_done`, `catia_done`, `previews_done`, `texts_done`)
are cumulative for the whole run, because they are re-derived from the run's own WAL. The byte
counters (`*_bytes_written`, `*_bytes_freed`, `video_bytes`, `catia_bytes`) and the `*_skipped` and
`*_failed` counters are restored from the last durable checkpoint only, so a process killed between
checkpoints can leave them short of the run total. They are progress telemetry; no registry,
description or recovery decision reads them.

`status` is `completed`, `partial` (skipped or failed items) or `not_implemented`; a dry run that
was interrupted or failed also writes `interrupted` or `failed`, and one refused by preflight writes
`insufficient_space`. `dry_run` and `resumed` are present
only when true. `issues` keeps the first 10000 items and `issues_omitted` counts the rest; the run
log lists every issue. Phase times and counters cover the process that wrote the report
(`started_at` is that process's start); `counters` are cumulative for the run.
