# Video Split

Accepted work: [0018 Video split transactions](../records/0018-split-implement-video-split-transactions.md).
Specification: [split](../../openspec/stage-1-core/split-restore.md#split),
[recovery](../../openspec/stage-1-core/integrity.md#recovery).

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
leftover source if it has not changed, and commits. A matching existing destination is adopted; a
different one is left untouched and reported as a skipped video (exit 6). Copy checks the source
size and mtime at completion and retries one change. A second change is skipped with exit 6.
The committed set makes a resumed run and a later successful rerun idempotent.

This task writes a minimal Markdown placeholder with `rel_path` front matter at `<source>.md`.
If that path contains a foreign file, the placeholder is `<source>.arxgo.md` and the foreign
file stays untouched. The [remaining stub and video-registry task](../plan.md#implement-stubs-and-video-registry)
will add links, metadata, `arxgo-videos.csv` and `arxgo-videos.md`. `--base-url` is validated
but has no output in the placeholder yet. `--dry-run` scans and reports a plan without moving
videos or writing stubs or a registry; the normal run directory under `.arxgo` is still written.

Linux tests cover both transfer paths, every WAL and filesystem crash point, injected
cross-device detection and rename fallback, a real tmpfs video archive when `/dev/shm` is
available, conflicts, source changes and a second run. The Windows binary cross-compiles and
passes `make vet-windows`; runtime checks belong to the
[Windows verification scenario](../../guide/windows-verification.md).
