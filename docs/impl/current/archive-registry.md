# Archive Registry

Accepted work: [0010 Directory walker](../records/0010-registry-implement-directory-walker.md).
Specification: [archive registry](../../openspec/stage-1-core/registry.md). Type detection and the
`scan` operation that writes `arxgo-registry.csv` are still open in the
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
