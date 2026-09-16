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
| `file_mime` | `mimetype.DetectReader` result without parameters, e.g. `video/mp4`, `text/plain`; `multipart/appledouble` for a macOS AppleDouble sidecar (`._<name>`, magic `00 05 16 07`), which is binary and never media, video or CATIA whatever its extension |
| `file_type` | Canonical extension from detection without the dot (`mp4`, `pdf`, `txt`); the lower-cased extension of the file name (the part after its last dot; a name whose only dot is leading, such as `.profile`, has none) when detection returns `application/octet-stream` or knows the type but has no canonical extension for it (`application/x-ole-storage`, which carries SolidWorks parts as well as legacy Office documents); empty when neither exists, and on an AppleDouble sidecar, whose extension belongs to the file it describes |
| `is_binary` | `false` when the detected MIME or any ancestor in the `mimetype` hierarchy is `text/plain`, or the MIME is in the text allow-list (`application/json`, `application/xml`, `image/svg+xml`, `text/*`); `true` otherwise. Empty files are `false` |
| `is_video` | MIME has prefix `video/`, or the MIME is ambiguous (`application/octet-stream`) and the extension is in the built-in video list or `--video-extensions`. Then refined when a successful ISO BMFF or ffprobe parse finds audio but no video stream: `is_video=false` (for example an audio-only `.mp4`). A result with neither audio nor video streams (a damaged file that ffprobe misreads, for example as LRC lyrics) keeps the flag. A CATIA extension is never `is_video` |
| `is_picture` | MIME has prefix `image/` |
| `is_media` | `is_video` or `is_picture` or MIME has prefix `audio/` |
| `is_large` | `file_size >= --large-threshold` |
| `is_catia` | The file's last dotted extension, compared case-insensitively, is in the built-in CATIA list (`.CATPart`, `.CATProduct`, `.CATDrawing`, `.cgr`, `.3dxml`). Content is not read; AppleDouble sidecars are never CATIA. Details: [CATIA classification](../stage-4-catia/catia.md#classification) |

Built-in video extension list (used only for ambiguous signatures): `mp4 m4v mov qt 3gp 3g2 mkv
webm avi wmv asf flv f4v mpg mpeg m2v ts m2ts mts vob ogv mxf dv rm rmvb`.

M4A files (brand `M4A `) are detected by `mimetype` as `audio/x-m4a`, and other audio-brand ISO
BMFF files (`M4B `, `M4P `, `F4A `, ...) as `audio/mp4`; both are media but not video. An audio-only
file with a generic brand (`isom`, `mp42`) is `video/mp4` until the ISO BMFF refinement. Matroska
and WebM files are `video/matroska` and `video/webm` whether or not they contain a video track.
MPEG transport streams (`ts`, `m2ts`, `mts`), MXF and DV have no signature `mimetype` recognizes in
the first bytes, so they are video through the extension list. Pictures and audio are registered
but never moved. CATIA files are registered with `is_catia` on every scan and moved only by
`split --catia` ([stage 4](../stage-4-catia/catia.md)).

## Registry writing

- Rows are written in walk order while type detection runs concurrently on a bounded worker pool;
  a re-order stage keeps the output independent of detection timing. The CSV header is written
  once.
- Directories, special entries and entries that cannot be read (a failed `Lstat`, or a file that
  cannot be opened or read for detection) get no row; they are logged and counted as skipped.
- The registry is first written to `<registry>.arxgo-part` and renamed over the final path when the
  scan completes, so an existing registry is replaced only by a complete one. `--dry-run` walks,
  detects and reports statistics but writes neither file.
- Every payload-candidate row is also appended to `candidates.jsonl` in the run directory
  ([format](contracts.md#candidate-list)), the input of split and restore. Video mode writes
  `is_video=true` rows; CATIA mode writes `is_catia=true` rows.
- Before each checkpoint the writer flushes and fsyncs the part file and the candidate list; the
  checkpoint then stores the scan cursor, both byte offsets and the scan statistics together, so
  it never names output that is not durable.
- On resume both files are truncated to their checkpointed offsets and traversal skips every path
  whose walk order key is less than or equal to the checkpoint cursor. When either file is missing
  or shorter than its offset, the scan starts again from the beginning with a warning; scanning is
  read-only, so repeating it is safe. A run whose registry was already renamed into place does not
  scan again when it is resumed.
- The cursor never moves backwards: a directory on the cursor's path that became unreadable is
  counted when it is reported again, but the cursor stays.
- Free space: preflight runs before traversal and estimates the registry from the size of the
  registry file it replaces (zero for a first scan), less what the part file already holds. While
  writing, each checkpoint reads the free space of the registry device again; below `--min-free`
  the scan stops with exit 4 and keeps its part file and checkpoint, so the same command resumes
  once space is freed. A device that reports no total size never stops the scan.
- [Flat metadata columns](contracts.md#flat-metadata-columns) hold modification time and, in
  `media` mode, the normalized fields from [metadata](metadata.md).

## Column stability

An operator who loads the registries into a spreadsheet, a database or a search index needs the
same columns on every run. Dropping an optional column that is empty in every row makes the header
depend on the archive's current content, so one archive yields different schemas from one command
to the next: a scan of a tree with videos writes the `media_*` columns, the same scan after
`split` moved them out writes none of them, and a payload registry gains or loses `sha256` with
`--verify`.

`--registry-columns MODE` selects the layout of every CSV arxgo writes (file registry, video
registry, CATIA registry):

| Mode | Behavior |
| --- | --- |
| `full` (default) | Every canonical column of that registry is written, whether or not any row fills it |
| `compact` | Optional columns empty in every row are omitted, as described in [contracts](contracts.md#flat-metadata-columns) |

The mode never changes column order, cell values or which rows are written, and readers keep
accepting both layouts. The payload registries follow one rule in both modes: `previews` in the
video registry and `text_rel_path` through `mtime` in the CATIA registry are optional columns, so
`compact` may drop either and `full` keeps both. Today `previews` is always written while
`text_rel_path` is droppable, which is the asymmetry this removes.

Evaluation: a scan of a tree with videos and a scan of the same tree after `split` moved them out
write byte-identical headers in `full` mode and different headers in `compact` mode; a payload
registry written with and without `--verify hash` has the same header in `full` mode; an invalid
mode exits 2.

Excluded: per-column selection, renaming, reordering, and any change to the values themselves.

## Statistics

The scan summary logs (one `scan summary` line) and stores in the `scan` section of the run report:
files, directories, symlinks, bytes, counts and bytes per flag (`binary`, `media`, `picture`,
`video`, `catia`, `large`), top 10 MIME types by bytes (ties by rows, then name), skipped entries by reason
(`unreadable`, `special`), and elapsed time. `files` and `bytes` count regular files with a row;
the generic run counters `files` and `bytes` count registry rows (files and symlinks) and their
bytes. Any skipped entry, including one skipped by an earlier process of a resumed run, ends the
run with exit 6.

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
