# Archive registry

Owner: `archive-registry`. Output format: [contracts](contracts.md#file-registry-csv).

## Operator problem

Before separating anything, the operator needs to know what the archive contains: which files are
binary, which are media, which are video, and which are unusually large. The registry is also the
input list for `split`.

## Traversal

- Uses `filepath.WalkDir` from the archive root. `WalkDir` visits entries in lexical order within
  each directory, which makes traversal order deterministic for an unchanged tree.
- The **walk order key** of a path is its list of slash-separated segments; key comparison is
  segment-by-segment byte comparison. This matches `WalkDir` order and is what a checkpoint cursor
  uses (plain string comparison does not, because `/` sorts after `-` and `.`).
- Resume: every entry whose key is less than or equal to the cursor is skipped. A directory whose
  key is less than the cursor and not a prefix of it is not listed at all, because its whole
  subtree precedes the cursor. A directory on the cursor's path is listed but not reported again.
- Excluded: [reserved paths](../spec.md#reserved-paths), paths matching `--exclude`, and the video
  archive root if it lies inside the archive (validation already forbids nesting; this is a guard).
  An explicit `--registry` inside the archive is excluded the same way. The reserved file names and
  `.arxgo/` are reserved only directly under the walked root; the `.arxgo-part` suffix at any depth.
  Excluded entries are not counted.
- `--exclude` globs match the whole relative slash path and are anchored at the walked root:
  `*.tmp` matches only top-level names, `**/*.tmp` matches at any depth, and `**` matches zero or
  more segments (`cache/**` also matches `cache`). A matching directory is excluded with its whole
  subtree without being listed.
- Symlinks and other non-regular entries (devices, sockets, junctions) are not followed or read.
  Symlinks get a row with `file_type=symlink`, size 0 and all flags `false`; other special entries
  are logged and counted as skipped. A root given as a symlink to a directory is walked through its
  target.
- Unreadable directories or files are logged with the error, counted, and do not abort the run;
  the final exit code is 6 when any entry was skipped. A directory that cannot be listed counts as
  a directory and as one skipped entry; its readable children, if any, are still walked. An archive
  root that cannot be listed fails the run (exit 1).
- Hidden files are included.

## Type detection

Detection reads at most the first 4096 bytes (the `mimetype` default read limit) of each regular
file; a file that yields no bytes is `inode/x-empty`. A file that cannot be opened or read, or is
no longer a regular file when opened, is logged and counted as skipped (`unreadable`) like other
unreadable entries. Opening never blocks on a FIFO that replaced the file after the walk.

| Field | Rule |
| --- | --- |
| `file_mime` | `mimetype.DetectReader` result without parameters, e.g. `video/mp4`, `text/plain` |
| `file_type` | Canonical extension from detection without the dot (`mp4`, `pdf`, `txt`); when detection returns `application/octet-stream`, the lower-cased extension of the file name (the part after its last dot; a name whose only dot is leading, such as `.profile`, has none); empty when neither exists |
| `is_binary` | `false` when the detected MIME or any ancestor in the `mimetype` hierarchy is `text/plain`, or the MIME is in the text allow-list (`application/json`, `application/xml`, `image/svg+xml`, `text/*`); `true` otherwise. Empty files are `false` |
| `is_video` | MIME has prefix `video/`, or the MIME is ambiguous (`application/octet-stream`) and the extension is in the built-in video list or `--video-extensions`. ISO BMFF files are then refined: when `--metadata media` finds no video track, `is_video=false` (for example an audio-only `.mp4`) |
| `is_picture` | MIME has prefix `image/` |
| `is_media` | `is_video` or `is_picture` or MIME has prefix `audio/` |
| `is_large` | `file_size >= --large-threshold` |

Built-in video extension list (used only for ambiguous signatures): `mp4 m4v mov qt 3gp 3g2 mkv
webm avi wmv asf flv f4v mpg mpeg m2v ts m2ts mts vob ogv mxf dv rm rmvb`.

M4A files (brand `M4A `) are detected by `mimetype` as `audio/x-m4a`, and other audio-brand ISO
BMFF files (`M4B `, `M4P `, `F4A `, ...) as `audio/mp4`; both are media but not video. An audio-only
file with a generic brand (`isom`, `mp42`) is `video/mp4` until the ISO BMFF refinement. Matroska
and WebM files are `video/matroska` and `video/webm` whether or not they contain a video track.
MPEG transport streams (`ts`, `m2ts`, `mts`), MXF and DV have no signature `mimetype` recognizes in
the first bytes, so they are video through the extension list. Pictures and audio are registered
but never moved.

## Registry writing

- Rows are written in walk order. The CSV header is written once; the writer flushes and fsyncs at
  each checkpoint and records the byte offset in the checkpoint.
- The registry is first written to `<registry>.arxgo-part` and renamed over the final path when the
  scan completes, so an existing registry is replaced only by a complete one.
- On resume the part file is truncated to the checkpointed offset and traversal skips every path
  whose walk order key is less than or equal to the checkpoint cursor.
- `metadata` holds compact JSON (see [contracts](contracts.md#metadata-json)). In `file` mode it
  contains modification time and permission bits; in `media` mode media rows also include the
  fields from [metadata](metadata.md).

## Statistics

The scan summary logs and stores in the run report: files, directories, bytes, counts and bytes per
flag (`binary`, `media`, `picture`, `video`, `large`), top 10 MIME types by bytes, skipped entries
by reason, and elapsed time.

## Edge cases

Fixtures must cover: empty archive; empty files; file names with spaces, commas, quotes, newlines
and non-ASCII characters (CSV quoting); an extension that lies (`.txt` containing MP4 bytes, `.mp4`
containing text); an audio-only MP4; an M4A; a symlink; an unreadable file (Linux only); a path
whose walk order differs from string order (`a-b/x` vs `a/b`); files exactly at the large
threshold; reserved paths present in the tree; resume from a cursor in the middle of a deep
directory.

## Acceptance

- Golden CSV for the generated fixture tree matches exactly on Linux. Paths use `/` separators on
  every platform, so the same golden file serves the deferred Windows scenario.
- Interrupting after N rows and resuming produces a byte-identical registry to an uninterrupted
  run.
- `scan` never writes inside the archive except the registry output, its part file and `.arxgo/`.
