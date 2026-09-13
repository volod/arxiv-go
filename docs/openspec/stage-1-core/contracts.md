# Data contracts

Owners: `archive-registry` (file registry), `video-split` (video registry, stub, summary),
`crash-safety` (run lock, run options, WAL, checkpoint, run report). A change to any format increments its version field or `format`
column value and is a spec amendment.

All text outputs are UTF-8 without BOM, `\n` line endings on every platform, paths relative to the
owning root with `/` separators. CSV follows RFC 4180 as written by Go `encoding/csv` (quotes when
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
| 11 | `metadata` | `{"v":1,"mtime":"..."}` | Compact JSON, see below |

Columns 1-9 and 11 are the requested structure; `is_large` implements "highlighting files larger
than a specified size" as an explicit column, inserted before `metadata` so the JSON column stays
last.

## Metadata JSON

```json
{
  "v": 1,
  "mtime": "2024-05-01T10:22:03Z",
  "mode": "0644",
  "media": {
    "source": "go-mp4",
    "container": "mp4",
    "duration_s": 1834.12,
    "width": 1920,
    "height": 1080,
    "frame_rate": "25/1",
    "video_codec": "h264",
    "audio_codec": "aac",
    "has_audio": true,
    "video_streams": 1,
    "audio_streams": 1,
    "subtitle_streams": 0,
    "bit_rate": 3200000,
    "rotation": 0,
    "creation_time": "2024-05-01T09:51:00Z",
    "tags": {"title": "Interview"}
  }
}
```

`mtime` is the modification time in RFC 3339 UTC to the second; `mode` is the permission bits as
four octal digits. `media` is present only in `media` mode for media files. Keys with zero/empty
values are omitted. Symlink rows add `"link_target"`, the link text as read, without following it.
The JSON is compact and does not escape `<`, `>` or `&`.

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
| 2 | `video_rel_path` | Path in the video archive (equal to `rel_path` in stage 1) |
| 3 | `stub_rel_path` | Path of the Markdown stub in the archive |
| 4 | `file_name` | |
| 5 | `file_size` | |
| 6 | `file_mime` | |
| 7 | `sha256` | Empty unless `--verify hash` |
| 8 | `transfer` | `rename` or `copy` |
| 9 | `status` | `moved`, `restored`, `conflict`, `skipped` |
| 10 | `run_id` | Run that last changed the row |
| 11 | `url` | `--base-url` link; empty when not given |
| 12 | `previews` | Stage 2: `;`-separated preview paths relative to the archive; empty in stage 1 |
| 13 | `metadata` | Same JSON as the file registry |

Rows are sorted by `rel_path` walk order key. The file is regenerated from the WAL of all runs plus
the existing file, written via `.arxgo-part` and rename.

## Markdown stub

`<archive>/<rel_path>.md`, for example `projects/2024/interview.mp4.md`. YAML front matter is the
machine-readable part and is what restore matches; the body is for people.

```markdown
---
arxgo_stub: 1
rel_path: projects/2024/interview.mp4
video_archive_path: /mnt/nas/video/projects/2024/interview.mp4
url: https://storage.example.com/video/projects/2024/interview.mp4
file_size: 734003200
file_mime: video/mp4
sha256: ""
run_id: 20260913T101500Z-1a2b3c4d
moved_at: 2026-09-13T10:17:42Z
---

# interview.mp4

This video was moved to the video archive by arxgo.

- Video archive: [interview.mp4](../../../../mnt/nas/video/projects/2024/interview.mp4)
- Cloud link: <https://storage.example.com/video/projects/2024/interview.mp4>
- Size: 700.0 MiB
- Duration: 30:34 | 1920x1080 | h264 + aac | 25 fps

## Previews

(stage 2: embedded PNG frames and sample clip links)
```

Rules: relative links are URL-escaped per segment; lines absent for missing data are omitted; the
file is written atomically; front matter keys are stable and additive.

## Archive summary

`arxgo-videos.md` in both roots: generation time, arxgo version, run ids, roots, base URL, totals
(videos, bytes, total duration when known), counts by container/codec/resolution band
(`<SD`, `SD`, `HD`, `4K+`), skipped/conflict lists, and a table of the 100 largest videos with
links.

## WAL record

```json
{"v":1,"txid":"20260913T101500Z-1a2b3c4d-000042","seq":187,"step":"begin","ts":"2026-09-13T10:17:40Z",
 "op":"split","rel_path":"projects/2024/interview.mp4","src":"/data/archive/projects/2024/interview.mp4",
 "dst":"/mnt/nas/video/projects/2024/interview.mp4","size":734003200,"mtime":"2024-05-01T10:22:03Z",
 "transfer":"copy"}
```

Later steps carry only `v`, `txid`, `seq`, `step`, `ts` and step data (`sha256` on `verified`,
`stub` on `stubbed`/`stub_removed`, `reason` on `aborted`). `txid` is `{run-id}-{6-digit}`; `seq`
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
as `{"count","bytes"}`, `largest_video`, `mime` per type and `skipped` per reason). `video_bytes` (bytes of handled videos) and the per-root `*_bytes_written`/`*_bytes_freed`
counters are omitted when zero. `elapsed_s` accumulates across resumed processes.

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
