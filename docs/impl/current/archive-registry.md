# Archive Registry

Accepted work: [0010 Directory walker](../records/0010-registry-implement-directory-walker.md),
[0011 File type detection](../records/0011-registry-implement-file-type-detection.md),
[0012 Scan operation and CSV registry](../records/0012-registry-implement-scan-operation-and-csv-registry.md).
The flat CSV metadata layout was delivered in
[0040](../records/0040-split-flatten-operator-csv-outputs.md); default ISO BMFF collection in
[0041](../records/0041-metadata-collect-iso-metadata-by-default.md). `is_catia` and the current
file-registry column order were added in
[0043](../records/0043-catia-implement-catia-classification.md). Every registry keeps its full header
since [0052](../records/0052-registry-stabilize-registry-columns.md); the preserved archive view,
`location`, the registry stamp and unchanged-file rule since
[0053](../records/0053-registry-preserve-archive-registry.md); reuse of unchanged rows and
`--redetect` since [0054](../records/0054-registry-reuse-registry-detection.md).
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
- Rows: regular files with the detected type, including `is_catia` for the built-in CATIA
  extensions (never `is_video`; AppleDouble sidecars are never CATIA). Symlinks are `symlink`
  rows with `link_target`. Directories, special entries (walker `special`), `Lstat` failures and
  files that cannot be opened or read get no row; each is logged once, counted in `skipped` by
  reason and listed in the report's `issues`. The required columns are `rel_path`, `file_name`,
  `file_type`, `file_size`, `is_large`, `file_mime`, `is_binary`, `is_media`, `is_picture`,
  `is_video`, `is_catia`, then `location` and the flat metadata columns. Default `--metadata file` writes `mtime` and, for detected MP4, MOV, M4A, M4V
  and 3GP, flat `media_*` columns from the ISO BMFF parser (a parse failure sets `media_error`).
  `--metadata media` requires `ffprobe` ([tool discovery](media-metadata.md#tool-discovery-internalmedia))
  and fills those columns for other audio/video files and ISO failures
  ([0041](../records/0041-metadata-collect-iso-metadata-by-default.md)). The registry always has the
  full header, also on an empty archive ([full schema](#full-schema)).
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
  bytes, flag totals for binary, media, picture, video, catia and large, top 10 MIME types by bytes,
  skipped by reason, elapsed). The `catia` object is omitted from report JSON when its count is
  zero. The run
  counters `files`/`bytes` count rows. Skipped entries from an earlier process of the run still
  make it partial (`Session.MarkPartial`).
- `--video-extensions` is a scan flag used by `scan` and `split`, so both classify the same way. A
  built-in CATIA extension in that list exits 2.

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
- Exclusion: root-level `.arxgo`, `arxgo-registry.csv`, `arxgo-videos.csv`, `arxgo-catia.csv`; the
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
  binary and never video or CATIA: before [0034](../records/0034-preview-repair-stage-2-preview-defects.md)
  42 such 4 KiB files on an operator archive were moved as videos with previews that failed
  on every rerun.
  `IsPicture` is an `image/` type; `IsMedia` is video, picture or `audio/`.
  `IsCatia` is a last-dotted suffix in the `internal/catia` kind table, independent of content;
  those files have `IsVideo` cleared so they cannot also be split candidates.
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

`file_type` falls back to the file name's extension whenever detection yields no canonical
extension, not only for `application/octet-stream`. On a real archive this filled 194 of 4031 rows
that were empty before, all `application/x-ole-storage` (CAD parts and assemblies of another vendor,
and legacy Office documents). An AppleDouble sidecar keeps an empty `file_type`, because the
extension in its name belongs to the file it describes
([0051](../records/0051-catia-review-stage-4-catia.md)).

## Full schema

([0052](../records/0052-registry-stabilize-registry-columns.md);
[spec](../../openspec/stage-1-core/registry.md#full-schema).) `arxgo-registry.csv`,
`arxgo-videos.csv` and `arxgo-catia.csv` are written with every column their writer knows
(`report.RegistryHeader`, `report.VideoHeader`, `report.CatiaHeader`), in contract order, on every
run of `scan`, `split` and `restore`; a column empty in every row has empty cells. The scan no
longer rewrites its part file after the walk, and no writer drops columns. The same tree therefore
yields a byte-identical 40-column file-registry header from an empty archive, a video tree, the
same tree after `split` and a CATIA tree, and a payload registry has the same header with or
without `--verify hash`, previews and `--catia-text`.

Readers still accept registries from earlier builds that omitted optional columns: the file and
video registries require their leading columns (`report.FileRegistryRequired`, 11;
`report.VideoRegistryRequired`, 11) and read any canonical-order subset of the metadata columns;
the CATIA registry reads any canonical-order subset of columns 11-16. A missing column is empty,
and the next write of that file has the full header.

## Archive view

([0053](../records/0053-registry-preserve-archive-registry.md);
[spec](../../openspec/stage-1-core/registry.md#archive-view).) The file registry of the archive
describes the archive the operator curated. `scan` and the scan phase of `split` share
`archive.Scan`. Restore's scan of a mirror root (`ScanConfig.mirror`) does none of the following.

- **History** (`archive_view.go`): `loadArchiveView` reads the run history of both payloads
  (`readHistory`) and never reads `arxgo-videos.csv` or `arxgo-catia.csv`. `replayMoves` applies
  the payload registry rules run by run: a committed split makes a file moved (a later abort never
  undoes it), a committed restore takes it back. The mirror root, size and mtime come from the run
  that last moved the file.
- **Owned artifacts** join the walker's `SkipPaths`: previews and text sidecars recorded by their
  event index (complete, generating or deleting), plus descriptions recorded by `described` and not
  removed since. A restore records the owned description both when it deletes it and when
  `--descriptions keep` leaves it. A description a restore recorded therefore stays owned while
  `report.InspectDescription` still finds its marker. A file the operator writes there later is an
  ordinary file with a row. Split no longer adds preview skip paths itself.
- **Preserved rows** (`scan_preserved.go`): before the walk, every moved rel_path after the resume
  cursor that `--exclude` (checked on each ancestor, like pruning) and reserved paths do not cover
  gets a row from the first source that has one. Sources are resolved on the scan worker pool: the
  stamped base row; detection of `<mirror>/<rel_path>` through the same `inspect`/`fileRow` path as
  a present file (audio-only refinement included); any readable base row (`registry-row-kept`
  warning); a row rebuilt from the replayed payload registry and WAL (`registry-row-reconstructed`
  warning). Base rows get `is_large` and `is_catia` recomputed. Warnings are logged only and leave
  the exit code alone.
- **Merge**: the writer emits queued preserved rows whose key precedes each walked entry, so rows
  stay in walk order. A walked entry other than a directory at a preserved key drops that row
  (presence wins). Rows after the last entry are written only after a completed walk. A preserved
  row advances the cursor, so a scan synced after them and stopped before placement resumes
  without duplicates. Preserved rows never reach `candidates.jsonl`, and statistics other than
  `preserved` and the generic `files`/`bytes` counters count present rows only.
- **Placement** (`registry_base.go`): `placeRegistry` hashes the part file while comparing it with
  the existing registry. Equal bytes remove the part and keep the file with its modification time
  (`registry` `unchanged`). Otherwise `fsops.Replace` places it (`written`). The payload registries
  use `writeFileIfChanged` for both copies, also when restore updates them.
- **Stamp**: `.arxgo/registry.json` (`state.RegistryStamp`) is written after every non-dry-run
  archive scan, with the placed size and SHA-256 and `scan_started_at` from `ScanStats.StartedAt`.
  `StartedAt` is kept in the checkpoint across processes and is the scan start of the first
  process. The stamp also records the detection settings: version, metadata mode, and the
  effective sorted video extension list without dots. A base row counts as stamped only when path,
  size, SHA-256 and settings all match.
- **Location updates** (`registry_location.go`): after execute and the payload registry, split
  sets the payload location on the rows of the files its WAL committed. It parses its scan's
  registry, rewrites it through the part file and placement, and stamps it. It never detects.
  `restore --registry-update` reads the stamped registry only while it matches its stamp, sets
  `archive` on rows whose replayed status is restored, updates the stamp's size and SHA-256, and
  never adds or removes a row.
- The retired payload registries `arxgo-videos.restored-<run-id>.csv` and
  `arxgo-catia.restored-<run-id>.csv` are reserved at a root (`scanner.Reserved`), so a restore that
  retires them leaves the file registry unchanged.
- Readers accept a registry without `location` (every row `archive`) and reject an unknown
  `location` value. The report's `scan` section and the `scan summary` line carry `preserved` and
  `registry`.

Evidence on a generated archive through the built binary: scan, video split with previews,
CATIA split with `--catia-text`, and rescans left the registry byte-identical with the same
modification time. Deleting the registry and stamp rebuilt it byte-identically. An added video
added one row. Both restores set `location` back to `archive`, and the scans after them left the
registry untouched.

## Incremental update

([0054](../records/0054-registry-reuse-registry-detection.md);
[spec](../../openspec/stage-1-core/registry.md#incremental-update).) Every scan still walks the
whole tree, but it opens a file for detection only when its row in the previous registry cannot be
reused.

- **Base** (`registry_base.go`): each archive scan opens the registry at the target path, hashes
  it and compares it with `.arxgo/registry.json`. The file is the base when the stamp names it with
  that size and SHA-256. Its rows are reusable when the stamp's version, `--metadata` mode and
  effective video extension list also equal the run's and `--redetect` is not given. The file stays
  open during the scan and is closed before the new registry is placed.
- **Rule** (`scan_reuse.go`): the base is read row by row in walk order next to the walker, so memory
  does not grow with the registry. A present regular file takes its base row when the row has
  `location` `archive`, is not a symlink row, and has the same `file_size` and `mtime` (to the
  second), and the file's mtime truncated to the second is earlier than the stamp's
  `scan_started_at`. Such a file is never opened: no signature read, no ISO BMFF parse, no
  ffprobe. `is_large` and `is_catia` are recomputed. A symlink always has its link text read and
  counts as reused when the text and mtime match. A future mtime, a change in the base scan start
  second, a changed size, a changed setting and a file at the path of a moved file are detected. A
  base row out of walk order or unparseable ends reuse for the rest of the scan with a warning.
- **Limit**: a same-size change that keeps the old modification time is not seen; `arxgo scan
  --redetect` (or `split --redetect`) detects every present file and every mirror copy and rewrites
  the rows. Permissions are not checked either: a file that became unreadable keeps its row until
  a detection (routed as `AUD-reuse-registry-detection-1`).
- **Resume**: a fresh scan stores `registry_base` (`{size, sha256}` of a stamped base) together
  with a reset cursor, offsets and statistics. A resumed scan whose base now has another identity
  (deleted, edited, replaced) logs `base registry changed since the checkpoint; scanning again from
  the start` and restarts, keeping the first process's `started_at`.
- **Statistics**: `reused` counts present rows taken from the base, in the `scan summary` line, the
  checkpoint and the report.
- **Split** (`split.go`): after its scan, split reads the scan's registry once. Preview planning,
  `arxgo-videos.csv`, `arxgo-catia.csv` and the description writer take `file_mime` and media values
  from those rows. A description writer that read the registry during recovery before the scan gets
  the scan's rows. Split still probes a video whose row has no usable media values when it plans
  previews.
- `report.RegistryReader` streams a registry; `ReadRegistry` uses it.

Evidence through the built binary on 20,000 random files and 3 generated clips (openat traced):
the second scan opened no archive file and reported `reused` 20003. A split after adding a clip
opened only that clip. The scan after the split opened nothing and left the registry
byte-identical, and `--redetect` wrote the same bytes after opening every file.
