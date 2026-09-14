# Data contracts

Owners: `archive-registry` (file registry), `video-split` (video registry and description),
`crash-safety` (run lock, run options, WAL, checkpoint, run report). A change to a JSON format
increments its version field; a CSV schema change is identified by its required columns.
Both require a spec amendment. Metadata columns after the required columns may be omitted
when they are empty in every row.

All text outputs are UTF-8 without BOM, `\n` line endings on every platform, paths relative to the
owning root with `/` separators. Exception: a Linux file name that is not valid UTF-8 is written to
the CSV files as its raw bytes, while JSON (candidate list, WAL) replaces the invalid
bytes with U+FFFD; such a video is registered but cannot be split and is reported as skipped
(exit 6). CSV follows RFC 4180 as written by Go `encoding/csv` (quotes when
needed). Booleans are `true`/`false`. Sizes are bytes as base-10 integers. Times are RFC 3339 UTC.

## File registry CSV

`<archive>/arxgo-registry.csv` (or `--registry`). One row per traversed entry except directories and
skipped entries (special files and entries that cannot be read). Symlink rows have `file_type`
`symlink`, size 0, an empty `file_mime` and all flags `false`. Column order is fixed:

| # | Column | Example | Notes |
| --- | --- | --- | --- |
| 1 | `rel_path` | `projects/2024/interview.mp4` | Relative to the archive root, including the file name |
| 2 | `file_name` | `interview.mp4` | Base name |
| 3 | `file_size` | `734003200` | Bytes |
| 4 | `file_type` | `mp4` | See [type detection](registry.md#type-detection) |
| 5 | `file_mime` | `video/mp4` | |
| 6 | `is_binary` | `true` | |
| 7 | `is_media` | `true` | video, audio or picture |
| 8 | `is_picture` | `false` | |
| 9 | `is_video` | `true` | |
| 10 | `is_large` | `false` | `file_size >= --large-threshold` |
| 11 onward | [Flat metadata columns](#flat-metadata-columns) | | Modification time and optional media details |

`is_large` implements "highlighting files larger than a specified size" as an explicit column.

## Flat metadata columns

Both registries append these columns in this order: `mtime`, `link_target`, `media_source`,
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
information rendered in a video's `video:` description line as separate CSV fields. A written
registry omits a metadata column that is empty in every row (for example `link_target` when there
are no symlinks, or all `media_*` columns when the tree has no audio or video). Readers treat a
missing metadata column as empty. The remaining names stay in this order. The scan part file keeps
the full header until the scan completes, so resume offsets stay valid. Tag columns that appear
hold the selected container text tags; other container tags are not collected.

## Candidate list

`candidates.jsonl` in the run directory: one JSON object per line for every `is_video=true` registry
row, in walk order. It is run state for split and restore, not an operator output.

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
| 1 | `rel_path` | Original path in the archive |
| 2 | `description_rel_path` | Path of the video description in the archive |
| 3 | `file_name` | |
| 4 | `file_size` | |
| 5 | `file_mime` | |
| 6 | `sha256` | Empty unless `--verify hash` |
| 7 | `transfer` | `rename` or `copy` |
| 8 | `status` | `moved`, `restored`, `conflict`, `skipped` |
| 9 | `run_id` | Run that last changed the row |
| 10 | `url` | `--base-url` link, otherwise `file://` URL of the local video |
| 11 | `previews` | Stage 2: `;`-separated recorded preview paths relative to the archive; empty when none |
| 12 onward | [Flat metadata columns](#flat-metadata-columns) | Same layout as the file registry |

`rel_path` names the video in both roots. For a moved row without a base URL, `url` points into
the video archive; after restore it points into the main archive. Skipped or conflict rows use the
main archive path when they have no explicit URL. Readers accept the required columns plus any
subset of the metadata columns in the canonical order.

Rows are sorted by `rel_path` walk order key. The file is regenerated from the existing file and
the WAL of every run, written via `.arxgo-part` and rename. Runs are replayed one after another in
the order they started (`created_at` in `options.json`, then run id), so a later run wins: a
committed split makes the row `moved`; a split aborted at its destination makes it `conflict`, and
any other aborted split `skipped`, unless the row is `moved`; a committed restore sets `restored`
and that restore's `run_id`. A split then adds `conflict`/`skipped` rows for the videos it skipped
before a transaction began. Restore only updates rows; it never adds one.

## Video description

`<archive>/<rel_path>.md`, for example `projects/2024/interview.mp4.md`, describes the original
video that split moved: one block of `key: value` lines without blank lines. Every field is about
that video and its move. Preview link lines follow only when previews of the video were generated;
without previews the file holds the video's metadata alone.

```markdown
arxgo: projects/2024/interview.mp4
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
| `file_size` | bytes, then the binary-unit size in parentheses | always |
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
double-quoted, with `\"`, `\\`, `\n` and `\r` escapes, when it has leading or trailing space, a quote,
a backslash or a line break. Times are RFC 3339 UTC. The file is written atomically. Preview files
(PNG frames and sample clips) are written next to the description, so their `- ` lines directly after the
fields link relatively, frames as images and samples as links, URL-escaped per segment; arxgo rewrites only those lines and removes them when no
preview remains.

## WAL record

```json
{"v":1,"txid":"20260913T101500Z-1a2b3c4d-000042","seq":187,"step":"begin","ts":"2026-09-13T10:17:40Z",
 "op":"split","rel_path":"projects/2024/interview.mp4","src":"/data/archive/projects/2024/interview.mp4",
 "dst":"/mnt/nas/video/projects/2024/interview.mp4","size":734003200,"mtime":"2024-05-01T10:22:03Z",
 "transfer":"copy"}
```

`mtime` keeps full precision (RFC 3339 with nanoseconds when present) because a copy compares it
exactly. Stage-1 transaction records remain version 1. Stage-2 preview events use version 2 in
the same JSON Lines WAL and the same `txid` and `seq` scheme. They use `rel_path` for the owning
video, `dst` for the absolute path in the main archive, `size` on completion or deletion, and
`reason` on failure. `preview_begin`/`preview_done`/`preview_failed` surround generation;
`preview_delete`/`preview_deleted` surround size-checked restore cleanup. A preview event may
belong to a later run than the video move it serves. Readers of earlier runs resolve absolute `src`,
`dst` and `description` paths against the `archive` and `video_archive` roots in that run's
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

`role` is `archive` in the archive root and `mirror` in the video archive root, where `peer` names
the owning archive. `scan` omits `peer`. A lock without `v` 1, `run_id`, a positive `pid` and `host`
is unreadable.

## Run options

`options.json` in the run directory, written once when the run is created:

```json
{"v":1,"run_id":"20260913T101500Z-1a2b3c4d","op":"split","version":"v1.0.0",
 "created_at":"2026-09-13T10:15:00Z","archive":"/data/archive","video_archive":"/mnt/nas/video",
 "defining":{"...":"..."},"options":{"...":"..."}}
```

`options` holds every validated option and `defining` the subset compared for resume (see
[integrity](integrity.md#state-layout)); both use the Go field names of the validated option
types, durations in nanoseconds and sizes in bytes. `dry_run` is present only for dry runs.

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
`candidates.jsonl`, omitted when zero) and `scan`: the scan statistics matching the cursor and
offsets (`complete`, `files`, `dirs`, `symlinks`, `bytes`, `binary`/`media`/`picture`/`video`/`large`
as `{"count","bytes"}`, `largest_video`, `mime` per type and `skipped` per reason). `video_bytes` (bytes of handled videos), the per-root `*_bytes_written`/`*_bytes_freed`
counters and the split preview counters `previews_done`/`previews_failed` are omitted when zero. `elapsed_s` accumulates across resumed processes.

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

Runs that scanned add `scan`: `files`, `dirs`, `symlinks`, `bytes`, the five flag totals,
`top_mime` (up to 10 `{"mime","count","bytes"}` by bytes), `skipped` by reason and `elapsed_s`.

`status` is `completed`, `partial` (skipped or failed items) or `not_implemented`; a dry run that
was interrupted or failed also writes `interrupted` or `failed`, and one refused by preflight writes
`insufficient_space`. `dry_run` and `resumed` are present
only when true. `issues` keeps the first 10000 items and `issues_omitted` counts the rest; the run
log lists every issue. Phase times and counters cover the process that wrote the report
(`started_at` is that process's start); `counters` are cumulative for the run.
