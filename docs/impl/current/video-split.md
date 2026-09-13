# Video Split

Accepted work: [0018 Video split transactions](../records/0018-split-implement-video-split-transactions.md);
[0019 Stubs and video registry](../records/0019-split-implement-stubs-and-video-registry.md);
[0023 Split/restore round-trip repairs](../records/0023-restore-repair-split-restore-round-trip-defects.md).
Specification: [split](../../openspec/stage-1-core/split-restore.md#split),
[contracts](../../openspec/stage-1-core/contracts.md#markdown-stub),
[recovery](../../openspec/stage-1-core/integrity.md#recovery).
The capability is shipped.

`arxgo split` scans the archive into `arxgo-registry.csv` and a durable candidate list, checks
free space, then handles videos in walk order. Transfer mode is classified from the two archive
roots: the same device uses a no-replace rename; `--transfer copy` or different devices use a
verified part file, no-replace placement, and source removal after the stub is durable. A rename
that returns a cross-device error rechecks space for all remaining videos as copies, then uses
the copy path. The destination keeps the archive-relative path. Newly created parent directories
keep the source permission bits on Linux and each new directory's parent is fsynced. A
directory-flush error after a successful rename is treated as placed. Non-video files stay in
place.

Each candidate has a WAL transaction. An incomplete transfer before `placed` is aborted and
retried; at or after `placed`, recovery writes the stub, verifies the destination, removes a
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

## Stubs

After the video is placed, split writes a Markdown stub at the former location. The file starts
with YAML front matter whose first key is `arxgo_stub: 1` (the overwrite marker) and includes
`rel_path`, the absolute video-archive path, optional `--base-url` link, size, MIME, optional
SHA-256 (`--verify hash`), `run_id` and `moved_at`. The body has a relative filesystem link
(URL-escaped per segment), the absolute path, the cloud link when `--base-url` is set, a human
size, and a duration line when `--metadata media` supplied media fields. Front matter is parsed
with a line-based reader (flat keys, no YAML library).

Name choice, in order: `<name>.<ext>.md`; if that path is foreign,
`<name>.<ext>.arxgo.md`; if that is also foreign, `<name-prefix>-<idx>.<ext>.md`. A regular file
whose front matter (read from its first 64 KiB) names this `rel_path` is treated as owned and
overwritten; any other file, a directory, a symlink or an unreadable file is foreign and never
touched. Filenames and full paths
that would exceed OS limits are truncated in the prefix so the index, video extension, `.md` and
separating dots still fit. `--base-url` is composed as `base + "/" + escaped rel_path segments`.

## Video registry and summary

Phase `report` writes identical `arxgo-videos.csv` and `arxgo-videos.md` into both roots (atomic
part file and rename). The CSV starts from any existing registry and replays every run's WAL in
start order (`created_at` in `options.json`, then run id), so a later run wins: committed splits
are `moved`; splits aborted at their destination are `conflict` and other aborted splits `skipped`,
unless the row is `moved`; committed restores set `restored` with the restoring run id. This run's
pre-transaction skips add `conflict` / `skipped` rows. A split after a restore therefore keeps
restored videos `restored`, and after a retired registry the new registry keeps them as history.
Rows are sorted by the walk-order key. The summary
lists generation time, version, run ids, roots, optional base URL, totals, container/codec
counts, resolution bands (`<SD`, `SD`, `HD`, `4K+`), skip/conflict lists and the 100 largest
videos. `--dry-run` still scans and reports a plan without moving videos or writing stubs or
registries; the normal run directory under `.arxgo` is still written.

Linux tests cover both transfer paths, every WAL and filesystem crash point, injected
cross-device detection and rename fallback, a real tmpfs video archive when `/dev/shm` is
available, stub collisions including the indexed name and a directory at the stub path, two-run
registry merge and replay after restores, golden stubs (file and media mode, MP4 and QuickTime
MOV, with and without base URL, Unicode and spaces), summary bands, a crash after a conflict abort,
a name that is not valid UTF-8, and a second run. The Windows binary cross-compiles and passes `make vet-windows`; runtime checks
belong to the [Windows verification scenario](../../guide/windows-verification.md).
