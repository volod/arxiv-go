# Video Split

Accepted work: [0018 Video split transactions](../records/0018-split-implement-video-split-transactions.md);
[0019 Descriptions and video registry](../records/0019-split-implement-stubs-and-video-registry.md);
[0023 Split/restore round-trip repairs](../records/0023-restore-repair-split-restore-round-trip-defects.md);
relocated archive roots in [0034](../records/0034-preview-repair-stage-2-preview-defects.md);
flat operator CSVs in [0040](../records/0040-split-flatten-operator-csv-outputs.md);
default ISO BMFF media columns in
[0041](../records/0041-metadata-collect-iso-metadata-by-default.md);
payload executor and registry column order in
[0044](../records/0044-catia-generalize-payload-split-restore.md).
Specification: [split](../../openspec/stage-1-core/split-restore.md#split),
[contracts](../../openspec/stage-1-core/contracts.md#video-description),
[recovery](../../openspec/stage-1-core/integrity.md#recovery).
The capability is shipped.

`arxgo split` scans the archive into `arxgo-registry.csv` and a durable candidate list, checks
free space, then handles videos in walk order. Transfer mode is classified from the two archive
roots: the same device uses a no-replace rename; `--transfer copy` or different devices use a
verified part file, no-replace placement, and source removal after the description is durable. A rename
that returns a cross-device error rechecks space for all remaining videos as copies, then uses
the copy path. The destination keeps the archive-relative path. Newly created parent directories
keep the source permission bits on Linux and each new directory's parent is fsynced. A
directory-flush error after a successful rename is treated as placed. Non-video files stay in
place.

Each candidate has a WAL transaction. An incomplete transfer before `placed` is aborted and
retried; at or after `placed`, recovery writes the description, verifies the destination, removes a
leftover source if it has not changed, and commits. A split that replaces an interrupted run (other
options, `--new-run`, or after a restore) recovers it first with that run's options. A matching
existing destination is adopted; a different one is left untouched and reported as a skipped video
(exit 6), and a stale `<dst>.arxgo-part` of an earlier aborted transfer is removed. A destination
that appears during a copy aborts the transaction: the `aborted` record is durable before the part
file is removed, so a crash in between cannot roll the foreign file forward. Copy checks the source
size and mtime at completion and retries one change. A second change is skipped with exit 6.
The committed set makes a resumed run and a later successful rerun idempotent. A Linux file name
that is not valid UTF-8 cannot be split (JSON state replaces its invalid bytes); it is skipped with
a reason that says so.

## Descriptions

After the video is placed, split writes a Markdown description of the original video at its former
location
([0039](../records/0039-split-compact-markdown-stub.md)): one block of `key: value` lines with no
blank lines. The first line `arxgo: <rel_path>` marks the description and names its video; then
`file_size` in bytes with a binary-unit size, `file_mime`, `sha256` with `--verify hash`,
`created` (container creation time) and `video` (duration, size, codecs, frame rate) when media
metadata was collected, the file's `modified` time, `moved_at`, `moved_to` (a link to the moved video's absolute
`file://` URL in the video archive), and `url` with `--base-url`. Preview
links follow as `- ` lines. A value with edge spaces, quotes, backslashes or line breaks is
double-quoted.

Name choice, in order: `<name>.<ext>.md`; if that path is foreign,
`<name>.<ext>.arxgo.md`; if that is also foreign, `<name-prefix>-<idx>.<ext>.md`. A regular file
whose first line (read within 64 KiB) is `arxgo: <rel_path>` for this video is treated as owned and
overwritten; any other file, a directory, a symlink or an unreadable file is foreign and never
touched. Filenames and full paths
that would exceed OS limits are truncated in the prefix so the index, video extension, `.md` and
separating dots still fit. `--base-url` is composed as `base + "/" + escaped rel_path segments`.

## Video registry

Phase `report` writes identical `arxgo-videos.csv` into both roots (atomic
part file and rename). Columns 1-10 are the payload registry columns shared with the future CATIA
registry: `rel_path`, `file_name`, `status`, `url`, `description_rel_path`, `file_size`, `sha256`,
`transfer`, `run_id`, `file_mime`; then `previews` and the flat metadata columns. A registry in the
earlier order does not load and stops split and restore with exit 5. The CSV starts from any
existing registry and replays the WAL of every video run (`payload` `video` in `options.json`) in
start order (`created_at` in `options.json`, then run id), so a later run wins: committed splits
are `moved`; splits aborted at their destination are `conflict` and other aborted splits `skipped`,
unless the row is `moved`; committed restores set `restored` with the restoring run id. This run's
pre-transaction skips add `conflict` / `skipped` rows. A split after a restore therefore keeps
restored videos `restored`, and after a retired registry the new registry keeps them as history.
Absolute WAL paths (descriptions, previews) are read against the roots recorded in each run's
`options.json`, so `description_rel_path` and `previews` stay archive-relative after the archive root is
mounted or renamed elsewhere ([0034](../records/0034-preview-repair-stage-2-preview-defects.md)).
Placement and its failure handling (destination conflict, cross-device fallback, changed source)
are shared with restore in `archive/transfer.go`.
Rows are sorted by the walk-order key. The CSV uses flat metadata columns, omits the duplicate
video path, supplies a local `file://` URL when no base URL is set, and omits metadata columns that
are empty in every row. `--dry-run` still scans and
reports a plan without moving videos or writing descriptions or
registries; the normal run directory under `.arxgo` is still written.

Linux tests cover both transfer paths, every WAL and filesystem crash point, injected
cross-device detection and rename fallback, a real tmpfs video archive when `/dev/shm` is
available, description collisions including the indexed name and a directory at the description path, two-run
registry merge and replay after restores, golden descriptions (file and media mode, MP4 and QuickTime
MOV, with and without base URL, Unicode and spaces), a crash after a conflict abort,
a name that is not valid UTF-8, and a second run. The Windows binary cross-compiles and passes `make vet-windows`; runtime checks
belong to the [Windows verification scenario](../../guide/windows-verification.md).
