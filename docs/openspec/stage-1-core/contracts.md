# Data contracts

Owners: `archive-registry` (file registry), `video-split` (video registry, stub, summary),
`crash-safety` (WAL, checkpoint). A change to any format increments its version field or `format`
column value and is a spec amendment.

All text outputs are UTF-8 without BOM, `\n` line endings on every platform, paths relative to the
owning root with `/` separators. CSV follows RFC 4180 as written by Go `encoding/csv` (quotes when
needed). Booleans are `true`/`false`. Sizes are bytes as base-10 integers. Times are RFC 3339 UTC.

## File registry CSV

`<archive>/arxgo-registry.csv` (or `--registry`). One row per traversed entry except directories and
skipped special files. Column order is fixed:

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

`media` is present only in `media` mode for media files. Keys with zero/empty values are omitted.
Symlink rows add `"link_target"`.

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
`stub` on `stubbed`, `reason` on `aborted`). Records are shown wrapped here; on disk each is one line.

## Checkpoint

```json
{"v":1,"run_id":"20260913T101500Z-1a2b3c4d","op":"split","phase":"execute",
 "scan_cursor":["projects","2024","zeta.pdf"],"registry_offset":81234567,
 "candidate_index":42,"wal_offset":18233,"counters":{"files":1203344,"bytes":4012345678901,
 "videos_done":41,"videos_skipped":1,"videos_failed":0},"elapsed_s":5234.2,
 "written_at":"2026-09-13T11:44:54Z"}
```
