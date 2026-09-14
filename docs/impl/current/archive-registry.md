# Archive Registry

Accepted work: [0010 Directory walker](../records/0010-registry-implement-directory-walker.md),
[0011 File type detection](../records/0011-registry-implement-file-type-detection.md),
[0012 Scan operation and CSV registry](../records/0012-registry-implement-scan-operation-and-csv-registry.md).
The flat CSV metadata layout was delivered in
[0040](../records/0040-split-flatten-operator-csv-outputs.md); default ISO BMFF collection in
[0041](../records/0041-metadata-collect-iso-metadata-by-default.md).
Specification: [archive registry](../../openspec/stage-1-core/registry.md); formats in
[contracts](../../openspec/stage-1-core/contracts.md#file-registry-csv). The capability is shipped
for both `--metadata file` and `--metadata media`; media fields are described in
[media metadata](media-metadata.md).

## Scan operation (`internal/archive`)

`arxgo scan --archive PATH` (the default operation) writes `arxgo-registry.csv`, or the
`--registry` path, and exits 0, or 6 when any entry was skipped.

```text
arxgo scan --archive /data/archive --large-threshold 500MiB --video-extensions braw,r3d
```

- Flow inside the run session: resume check, preflight, phase `scan`, rename, phase `summary`.
  Preflight estimates the registry from the file it replaces (256 B per row, less what a resumed
  part file holds). `Scan(ctx, session, ScanConfig)` is also the scan phase of
  [split](video-split.md) (`Preflight=false`, video archive in `SkipPaths`); [restore](video-restore.md)
  scans the video archive the same way (registry output in the run directory) and passes
  `ScanConfig.Include` so registered moved videos are candidates whatever detection says.
- Pipeline: `scanner.Walk` in the calling goroutine queues every entry on a bounded order channel
  (`Window`, default 256) and hands files and symlinks to 16 detection workers (`scanner.Detect`,
  `os.Readlink`). A writer goroutine takes entries in order, waits for each one's detection and
  writes the row, so output never depends on detection timing. A write error cancels the walk.
- Rows: regular files with the detected type, symlinks as `symlink` rows with `link_target`.
  Directories, special entries (walker `special`), `Lstat` failures and files that cannot be opened
  or read get no row; each is logged once, counted in `skipped` by reason and listed in the
  report's `issues`. Default `--metadata file` writes `mtime` and, for detected MP4, MOV, M4A, M4V
  and 3GP, flat `media_*` columns from the ISO BMFF parser (a parse failure sets `media_error`).
  `--metadata media` requires `ffprobe` ([tool discovery](media-metadata.md#tool-discovery-internalmedia))
  and fills those columns for other audio/video files and ISO failures. A completed registry omits
  metadata columns that are empty in every row ([0041](../records/0041-metadata-collect-iso-metadata-by-default.md)).
- Outputs: `report.RegistryWriter` (`encoding/csv`, `\n` line ends, compact JSON without HTML
  escaping) writes `<registry>.arxgo-part` and counts bytes; video rows also go to
  `candidates.jsonl` in the run directory (`archive.ReadCandidates` reads it back). On completion
  the part file is renamed over the registry with `fsops.Replace`. `--dry-run` discards rows and
  writes neither file.
- Checkpoints: `Session.AdvanceSync` counts every walked entry; when `--checkpoint-every` or
  `--checkpoint-interval` is due, the writer flushes and fsyncs both outputs (skipping an output
  with no new bytes), stores cursor, both offsets and `state.ScanStats` with `Session.Update`, then
  checks free space on the registry device: below `--min-free` the run stops with exit 4 and
  keeps its state. An interrupt (Ctrl+C) syncs once more before the final checkpoint and exits
  130.
- Resume: both files are truncated to the checkpointed offsets and the walk resumes after the
  cursor with the checkpointed statistics; the cursor never moves backwards. A missing or shorter
  part file restarts the scan from scratch with a warning. A run whose checkpoint says the scan is
  `complete` (renamed, but the report was not written) does not scan again. An entry added behind
  the cursor while a scan was interrupted is registered only by a later full scan (a later run);
  for `split` such a video is moved by the next split run.
- Statistics: one `scan summary` log line and the report's `scan` section (files, dirs, symlinks,
  bytes, the five flag totals, top 10 MIME types by bytes, skipped by reason, elapsed). The run
  counters `files`/`bytes` count rows. Skipped entries from an earlier process of the run still
  make it partial (`Session.MarkPartial`).
- `--video-extensions` is now a scan flag used by `scan` and `split`, so both classify the same way.

Measured on the development host (i9-14900K, NVMe ext4, Go 1.27.1), binary on a 1.2 GiB copy of
`/usr/share` (152,424 rows, 17,948 directories, 31,767 symlinks): 3.0-3.6 s with the default
`--checkpoint-every 500` (about 340 checkpoints, 3 fsyncs each), 1.0 s with checkpoints only at
phase boundaries; 14 MB RSS; registry 29 MB (191 B per row). Detection throughput is flat from 8
workers up (1.0 s at 8, 16 and 32 workers; 3.3 s at 1). For very large local archives a larger
`--checkpoint-every` trades resume granularity for speed. The stage-1 review measured the same
shape on 200,000 small files: 3.7 s at 500, 1.2 s at 5000, 0.8 s at 100000; the default stays as
specified.


## Directory walker (`internal/scanner`)

`Walk(ctx, root, Options, fn)` runs `filepath.WalkDir` over the root and calls `fn` once per entry
below it, in walk order. It performs no type detection and writes no output.

- `Entry` carries the OS path (the root as given joined with the relative path), the relative
  slash path `Rel`, its `Key`, a `Kind` (`dir`, `file`, `symlink`, `special`), the `Lstat`
  `Info`, and `Err` when the entry could not be read. `Entry.SkipReason()` is `unreadable` or
  `special` for entries that count toward exit 6.
- Order: `Key` is the list of path segments; `Compare` orders keys segment by segment, which equals
  `WalkDir` order (`a/b` before `a-b/x`, a directory before its contents). Delivered keys are
  strictly increasing, except the rare case below.
- Resume: `Options.Cursor` (the checkpoint `scan_cursor`) skips every key less than or equal to it.
  Directories wholly before the cursor are pruned with `SkipDir` and never listed; directories on
  the cursor's path are descended silently. Resuming near the end of a 20k-entry tree costs about
  0.1 ms instead of 33 ms for the full walk.
- Exclusion: root-level `.arxgo`, `arxgo-registry.csv`, `arxgo-videos.csv`; the
  `.arxgo-part` suffix and preview part files `<stem>.arxgo-part.<ext>` at any depth; `Options.SkipPaths` (OS paths inside the root, for an explicit
  `--registry` or a nested video archive); and `Options.Exclude` globs compiled by `CompileGlob`
  (anchored at the root, `path.Match` per segment, `**` for zero or more segments, linear-time
  matching). Excluded directories are pruned. `cli` validates `--exclude` with the same
  `CompileGlob`, and on Windows also rejects `\`.
- Symlinks are delivered as `symlink` with their own `Lstat` info and never followed (a symlink
  loop is one entry). A root that is itself a symlink is walked through its target. Sockets,
  devices, pipes and (on Windows) junctions are delivered as `special` without `Info`.
- Errors: a directory that cannot be listed is delivered once with `Err` set (the walker holds each
  directory back until `WalkDir` reports its listing result), and its readable children are still
  walked; a file whose `Lstat` fails is delivered with `Err`. Each skipped entry logs one
  `skipped entry` warning with `path`, `kind`, `reason` and `error`. When the directory on a
  resume cursor's path became unreadable, it is still reported although its key precedes the
  cursor. A missing, non-directory or unlistable root, a context cancellation, or a callback error
  ends the walk with an error; `ErrStop` from the callback ends it cleanly. `fs.SkipDir` and
  `fs.SkipAll` from the callback are rejected because the held-back directory makes them ambiguous.

Measured on the development host (i9-14900K, NVMe ext4, Go 1.27.1): 20,420-entry generated tree
in about 33 ms per full walk (about 20% over bare `WalkDir` plus `Info`); `/usr/share`
(171,567 entries, 31,767 symlinks, 2 unreadable directories) walked in the same order as
`WalkDir`, with resume checked at 200 cursors.

## File type detection (`internal/scanner`)

`Detect(path, size, DetectOptions) (FileType, error)` classifies one regular file for its registry
row; `Classify(head, name, opts)` is the pure part over already-read bytes. Neither writes anything.

- Reads at most `DetectLimit` (4096, the `mimetype` default) leading bytes into a pooled buffer and
  runs `github.com/gabriel-vasile/mimetype` (v1.4.15) on them. The open uses `O_NONBLOCK` and then
  requires a regular file, so a path swapped for a FIFO or directory after the walk returns an
  error instead of blocking. Open and read errors (for example `permission denied`) are returned;
  the scan counts them as `unreadable`.
- `FileType.MIME` is the detected type without parameters (`text/plain`, not
  `text/plain; charset=utf-8`); a file with no bytes is `inode/x-empty` with every flag `false`.
- `FileType.Type` is the detected canonical extension without the dot; for
  `application/octet-stream` it is the lower-cased extension of the file name (`.profile` and
  `Makefile` have none).
- `IsBinary` is false when the type or an ancestor in the `mimetype` hierarchy is `text/plain`,
  or the type is `application/json`, `application/xml`, `image/svg+xml` or `text/*`. HTML, XML,
  CSV, NDJSON, shell scripts and UTF-8/UTF-16 text with a BOM are text.
- `IsVideo` is a `video/` type, or `application/octet-stream` with an extension in
  `BuiltinVideoExtensions` or the extras given to `NewDetectOptions` (dot and case ignored). A
  recognized signature always wins over the extension, so text named `.mp4` is not video. A
  macOS AppleDouble sidecar (`._clip.MP4`, magic `00 05 16 07`) is `multipart/appledouble`,
  binary and never video: before [0034](../records/0034-preview-repair-stage-2-preview-defects.md)
  42 such 4 KiB files on an operator drone archive were moved as videos with previews that failed
  on every rerun.
  `IsPicture` is an `image/` type; `IsMedia` is video, picture or `audio/`.
- `IsLarge` is `size >= LargeThreshold` using the size recorded for the row; a threshold of zero
  marks nothing large.
- The ISO BMFF no-video-track refinement is not applied here: an audio-only `isom` MP4 and an
  audio-only WebM are `IsVideo=true`. M4A is `audio/x-m4a` and not video.

Observed on real files generated with ffmpeg on this host (libx264 and NVENC H.264/HEVC/AV1 in
MP4, fast-start and fragmented MP4, M4V, MOV, 3GP, MKV, WebM, AVI, FLV, WMV, MPEG-PS, VOB, OGV,
MPEG-TS, M2TS, MXF, DV; AAC M4A, WAV, MP3, FLAC, Ogg; PNG, JPEG, WebP, GIF, BMP, TIFF, AVIF):
every video is `IsVideo`, every audio file and picture is not; MPEG-TS, M2TS, MXF and DV are
`application/octet-stream` and video by extension; WMV is `video/x-ms-asf` (`asf`), VOB is
`video/mpeg` (`mpeg`).

Measured on the development host (i9-14900K, NVMe ext4, Go 1.27.1): about 2.6 us and 552 B per
cached file (4.2 us and 4.6 KiB before buffer pooling); classification alone is about 0.8 us for
an MP4 head and 22 us for a full 4 KiB text head. Walking and detecting all 121,820 files under
`/usr/share` took 1.4 s with a warm cache on one goroutine (12.6 s cold), with 2 `permission
denied` files reported as errors.
