# Archive Registry

Accepted work: [0010 Directory walker](../records/0010-registry-implement-directory-walker.md),
[0011 File type detection](../records/0011-registry-implement-file-type-detection.md).
Specification: [archive registry](../../openspec/stage-1-core/registry.md). The `scan` operation
that writes `arxgo-registry.csv` is still open in the
[plan](../plan.md#archive-registry----archive-registry); `arxgo scan` still exits 70.

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
- Exclusion: root-level `.arxgo`, `arxgo-registry.csv`, `arxgo-videos.csv`, `arxgo-videos.md`; the
  `.arxgo-part` suffix at any depth; `Options.SkipPaths` (OS paths inside the root, for an explicit
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
- `Stats.Count(Entry)` accumulates directories, files, symlinks, special entries and skipped counts
  by reason; the scan operation will persist it in the checkpoint so resumed runs keep counting.

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
  recognized signature always wins over the extension, so text named `.mp4` is not video.
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
