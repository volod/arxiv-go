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
- Excluded: [reserved paths](../spec.md#reserved-paths), paths matching `--exclude`, and the video
  archive root if it lies inside the archive (validation already forbids nesting; this is a guard).
- Symlinks and other non-regular entries (devices, sockets, junctions) are not followed or read.
  Symlinks get a row with `file_type=symlink`, size 0 and all flags `false`; other special entries
  are logged and counted as skipped.
- Unreadable directories or files are logged with the error, counted, and do not abort the run;
  the final exit code is 6 when any entry was skipped.
- Hidden files are included.

## Type detection

Detection reads at most the first 3072 bytes (the `mimetype` default read limit) of each regular
file; empty files are `inode/x-empty`.

| Field | Rule |
| --- | --- |
| `file_mime` | `mimetype.DetectReader` result without parameters, e.g. `video/mp4`, `text/plain` |
| `file_type` | Canonical extension from detection without the dot (`mp4`, `pdf`); when detection returns `application/octet-stream`, the lower-cased file extension; empty when neither exists |
| `is_binary` | `false` when the detected MIME or any ancestor in the `mimetype` hierarchy is `text/plain`, or the MIME is in the text allow-list (`application/json`, `application/xml`, `image/svg+xml`, `text/*`); `true` otherwise. Empty files are `false` |
| `is_video` | MIME has prefix `video/`, or the MIME is ambiguous (`application/octet-stream`) and the extension is in the built-in video list or `--video-extensions`. ISO BMFF files are then refined: when `--metadata media` finds no video track, `is_video=false` (for example an audio-only `.mp4`) |
| `is_picture` | MIME has prefix `image/` |
| `is_media` | `is_video` or `is_picture` or MIME has prefix `audio/` |
| `is_large` | `file_size >= --large-threshold` |

Built-in video extension list (used only for ambiguous signatures): `mp4 m4v mov qt 3gp 3g2 mkv
webm avi wmv asf flv f4v mpg mpeg m2v ts m2ts mts vob ogv mxf dv rm rmvb`.

`M4A` and other audio-brand ISO BMFF files are detected by `mimetype` as `audio/mp4` and are media
but not video. Pictures and audio are registered but never moved.

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

- Golden CSV for the generated fixture tree matches exactly (Linux and Windows, `/` separators).
- Interrupting after N rows and resuming produces a byte-identical registry to an uninterrupted
  run.
- `scan` never writes inside the archive except the registry output, its part file and `.arxgo/`.
