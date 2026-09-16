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
  An explicit `--registry` inside the archive is excluded the same way, and so are the owned
  descriptions, text sidecars and previews recorded in the run history
  ([archive view](#archive-view)). The reserved file names and `.arxgo/` are reserved only directly
  under the walked root; the `.arxgo-part` suffix at any depth.
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
  scan completes, so an existing registry is replaced only by a complete one, and not at all when
  the result is unchanged ([registry stability](#registry-stability)). Preserved rows are merged
  into the stream in walk order. `--dry-run` walks,
  detects and reports statistics but writes neither file.
- Every present payload-candidate row is also appended to `candidates.jsonl` in the run directory
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

## Registry stability

An operator loads the file registry and the payload registries into a spreadsheet, a database or a
search index and reloads them after every run. That needs two guarantees:

1. **One schema.** Every CSV arxgo writes has the same columns on every run, whatever the archive
   currently holds, whichever command wrote it and whichever options (`--verify`, `--metadata`,
   previews, `--catia-text`) were used.
2. **One inventory.** The file registry describes the archive the operator curated, not only the
   bytes currently under its root. Moving payload files out with `split` and writing descriptions,
   previews and text sidecars adds, removes and changes no row except the `location` of the moved
   files; only a real change to the archive changes the registry.

`scan` and the scan phase of `split` are one implementation: for the same tree, run history and
scan options they write the same registry.

### Full schema

The file registry, the video registry and the CATIA registry are always written with every column
of their [contract](contracts.md), in contract order, whether or not any row fills it; a column
empty in every row is written with empty cells. No option selects a smaller layout. Readers still
accept a registry written by an earlier build that omitted empty optional columns (a missing
optional column is empty, a missing `location` is `archive`); the next write of that file has the
full header.

### Archive view

The file registry holds:

- **Present rows**: one row per entry found under the archive root, as detected, with `location`
  `archive`.
- **Preserved rows**: one row per payload file that split moved out. A `rel_path` whose replayed
  status is `moved` in the history of either payload
  ([replay rules](contracts.md#video-registry-csv)) and that is absent from the archive keeps a row
  with `location` `video-archive` or `catia-archive`, carrying the values of the file as detected
  before the move ([row sources](#preserved-row-sources)). The history is replayed from the WAL of
  every run, not read from `arxgo-videos.csv` or `arxgo-catia.csv`, so an edited or deleted payload
  registry changes nothing. The mirror's current content is not consulted for membership: a file
  the operator deleted from the mirror keeps its row until a restore records otherwise.

Rules:

- **Owned artifacts have no row.** A description, text sidecar or preview that the history of any
  run records as written (`described`, `text_done`, `preview_done`) and not removed afterwards is
  excluded from traversal like a reserved path. The payload registries list them
  (`description_rel_path`, `text_rel_path`, `previews`). Once a restore removed the artifact, a file
  at that path is an ordinary file again.
- **Presence wins.** A file found at the path of a moved file (a restore returned it, or the operator
  put a file there) is a present row, detected as usual; no preserved row is added for that path.
- **Removal is real.** A file absent from the archive and not moved by any split loses its row.
- `--exclude` applies to preserved rows as it does to present ones.
- **Candidates are present rows.** Only rows with `location` `archive` enter `candidates.jsonl`.
- **Split updates `location`.** After its execute phase, split writes the file registry again with
  `location` set for the files its committed transactions moved, reusing the rows of its own scan
  (no detection). A scan right after a completed split therefore finds nothing to change.
- **Restore updates `location`.** A restore with `--registry-update` sets `location` `archive` on the
  rows of the files it restored, in the registry named by the [registry stamp](contracts.md#registry-stamp)
  when that file still matches its stamp. Restore never creates a file registry and adds or removes
  no row.
- **Unchanged registries stay untouched.** When a completed registry (file, video or CATIA) is byte
  for byte equal to the file it would replace, its part file is removed and the existing file is
  kept with its modification time; the scan section of the run report records `registry`
  `unchanged`. An update always rewrites the whole file through the part file and a rename, because
  rows are in walk order and a new file can land anywhere.

#### Preserved row sources

A preserved row takes its values from the first source that has them:

1. the base registry row, when it is [reusable](#incremental-update);
2. detection of the mirror copy at `<mirror>/<rel_path>`, with the mirror root taken from the
   `options.json` of the run that moved it. The copy has the same content, name, size and
   modification time, so it yields the same values. Scan only reads the mirror and takes no lock
   there;
3. the base registry row from any readable base, logged as warning `registry-row-kept`;
4. a row built from the replayed payload registry row (`rel_path`, `file_name`, `file_size`,
   `file_mime`, `mtime`, and for video the flat metadata columns), with `file_type` the lower-cased
   extension, `is_binary` `true`, `is_video` and `is_media` `true` for a video or `is_catia` `true`
   for a CATIA file, `is_large` from `--large-threshold`, logged as warning
   `registry-row-reconstructed`.

Warnings do not change the exit code.

### Incremental update

A registry is updated, not rebuilt. Every scan walks the whole tree, because additions, removals and
changes are only visible that way, but it opens a file for detection only when its base row cannot
be reused.

The **base** is the registry at the target path when the [registry stamp](contracts.md#registry-stamp)
in `<archive>/.arxgo/registry.json` names that path and the file's size and SHA-256 match the stamp.
A missing, mismatched or unreadable stamp leaves no reusable base: every present file is detected
and preserved rows use the later sources. A base row of a present entry is reused when all hold:

- the stamp's detection settings equal the run's: arxgo version, `--metadata` mode and the effective
  video extension list;
- `--redetect` is not given;
- the base row's `location` is `archive` (a file present at the path of a moved file is detected,
  because a preserved row may have been reconstructed);
- the entry kind is the same (regular file or symlink), and a symlink's link text is unchanged;
- `file_size` is equal and `mtime` is equal at second precision;
- the file's modification time, truncated to the second, is earlier than the stamp's
  `scan_started_at` truncated to the second. A file changed during or after the base scan without
  changing its size and second is detected again; a modification time in the future is never
  reused.

`is_large` and `is_catia` are recomputed for every row from `--large-threshold` and the file name. A
reused row is byte-identical to the row detection would write for the unchanged file. A tool that
replaces content while preserving size and modification time defeats this check; `--redetect`
detects every present file and still preserves moved rows.

Within one run metadata is collected once: split's candidate list, descriptions and payload
registry use the values of its scan rows, reused or detected, and never open a file again for
metadata a row carries. Reading a file for `--verify hash` is not metadata collection.

The stamp is written atomically after the registry is renamed into place or found unchanged, and
records the run's `scan_started_at`: the start of the scan phase of the run's first process. A crash
between the rename and the stamp leaves a mismatched stamp, which costs one full detection and never
a wrong row. A resumed scan compares the base with the size and SHA-256 its checkpoint recorded
and starts the scan again from the beginning when they differ. `--dry-run` writes neither the
registry nor the stamp.

### Stability evaluation

- On an empty archive, a video tree, the same tree after `split` and a CATIA tree, every registry has
  its full contract header; a payload registry has the same header with and without `--verify hash`,
  previews and `--catia-text`; a registry from an earlier build that omitted columns is read.
- `scan`, then `split` (video, and CATIA with `--catia-text`, with previews), then `scan`: the last
  scan leaves every registry byte-identical with an unchanged modification time; the registry split
  wrote keeps every pre-split row with the same values and `location` of moved rows changed; no
  description, sidecar or preview has a row.
- Adding a video or a CATIA file after the split adds exactly its row and leaves every other row
  byte-identical; the next split moves it and changes only its `location`.
- Deleting the registry and the stamp and scanning with the mirrors present writes a byte-identical
  registry; with a mirror unreadable and no base, the moved rows are reconstructed with a warning.
- `restore --registry-update` sets `location` `archive`; a scan after it leaves the registry
  untouched. A file put back at a moved path by hand becomes a present row.
- A second scan of an unchanged tree opens no file for detection; after each change above, the
  registry is byte-identical to one written with `--redetect`; a same-size change whose modification
  second is not earlier than the base scan start, a changed detection setting and a base changed
  between processes of a resumed scan each lead to detection.

Excluded: per-column selection, renaming or reordering; a registry of the mirror roots (restore's
scan of a mirror stays run state); content hashing to validate reuse.

## Statistics

The scan summary logs (one `scan summary` line) and stores in the `scan` section of the run report:
files, directories, symlinks, bytes, counts and bytes per flag (`binary`, `media`, `picture`,
`video`, `catia`, `large`), top 10 MIME types by bytes (ties by rows, then name), skipped entries by reason
(`unreadable`, `special`), `reused` (present rows taken from the base), `preserved` (count and
bytes of preserved rows), `registry` (`written` or `unchanged`) and elapsed time. Every other
statistic counts present rows only. `files` and `bytes` count regular files with a row;
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
- `scan` never writes inside the archive except the registry output, its part file and `.arxgo/`,
  and never writes to a mirror root.
- [Stability evaluation](#stability-evaluation) passes.
