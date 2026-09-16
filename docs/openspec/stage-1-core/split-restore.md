# Split and restore

Owners: `video-split` and `video-restore`. CATIA payload: [stage 4](../stage-4-catia/split-restore.md).
Transaction mechanics: [integrity](integrity.md).
Formats: [contracts](contracts.md).

## Split

### Operator problem

Move every video out of the main archive into a video archive with the same directory tree, so the
main archive becomes small and shareable, while each former video location still says what was
there and where it went.

### Behavior

1. Validate options and discover required tools ([CLI](cli.md#validation)).
2. Take the lock on both roots and recover any incomplete run ([integrity](integrity.md#recovery)).
3. Scan the archive ([registry](registry.md)); present `is_video=true` rows become candidates. The
   registry is written as in `scan`, reusing an earlier registry of the tree
   ([incremental update](registry.md#incremental-update)), and its rows supply the metadata of the
   descriptions and the video registry.
4. Preflight free space and print the plan ([integrity](integrity.md#preflight)). A shortfall exits
   4, also with `--dry-run`; otherwise `--dry-run` stops here with exit 0.
5. For each candidate in walk order, run one transaction:
   - destination `<video-archive>/<rel_path>`; parent directories are created with the source
     directory permission bits (Linux) and never removed by split;
   - `--transfer auto` on the same device: `os.Rename`; if it fails with a cross-device error,
     fall back to the copy path and log it once. With `--verify hash` split reads the file once
     before the rename and records the SHA-256 in a `verified` record, so the payload registry,
     which is rebuilt from the WAL, carries the same hash the description does. A rename moves no
     bytes, so there is nothing to compare; restore keeps no hash column and skips the read;
   - copy path: stream to `<dst>.arxgo-part`, fsync, verify (`size` or `hash`), rename to `<dst>`,
     fsync the parent directory, preserve modification time (and permission bits on Linux when
     the destination filesystem supports them);
   - write the video description `<archive>/<rel_path>.md` ([format](contracts.md#video-description));
   - copy path only: remove the source after the description is durable;
   - commit.
6. Write `arxgo-videos.csv` into the archive root, and an identical copy into
   the video archive root so each root is self-describing. Write the file registry again with the
   `location` of the moved videos ([archive view](registry.md#archive-view)).
7. Release the lock and log the final statistics.

### Rules

- **Destination exists.** Same size (and hash with `--verify hash`) as the source: treat as already
  placed, continue from `placed`, log `adopted`. Different content: skip the video, log a conflict,
  exit code 6, and remove a `<dst>.arxgo-part` left by an earlier aborted transfer. Split never
  overwrites. A destination that appears during a copy aborts the transaction; the `aborted` record
  is durable before the part file is removed, so a crash in between is never rolled forward.
- **Description exists.** A description whose first line is `arxgo: <rel_path>` for this video is rewritten; anything else
  at `<rel_path>.md` (a foreign file, a directory, a symlink or a file arxgo cannot read) is a
  conflict: the video is still moved, the description is written as `<rel_path>.arxgo.md` (then
  `<name-prefix>-<idx>.<ext>.md`), and the conflict is logged. Only the first 64 KiB of a file are
  read to find its marker line.
- **Source changed during the run.** Size or mtime differs from the WAL `begin` record at copy
  completion: discard the part file, abort the transaction, retry once, then skip with a warning.
- **Previews are not candidates.** Stage 2 preview clips are videos inside the archive; the scan
  excludes every preview path recorded by the WAL preview events of any run, together with owned
  descriptions ([archive view](registry.md#archive-view)), and preview part files are reserved
  paths.
- **Idempotency.** A rerun after success scans, finds no video candidates (moved videos keep
  preserved rows, which are never candidates), leaves every registry untouched, and exits 0.
- **Links.** The description always contains the relative filesystem path from the description to the video when
  both roots are on the same filesystem namespace, the absolute video path, and, when `--base-url`
  is given, `base-url + "/" + url-escaped rel_path` segments.
- **Metadata.** Default `--metadata file` records filesystem fields and ISO BMFF media fields for
  MP4, MOV, M4A, M4V and 3GP. `--metadata media` adds ffprobe fields for other audio/video and ISO
  failures. The description and video registry carry whatever media fields were collected.

## Restore

### Operator problem

Return videos to their original locations, for example before re-sharing the complete archive or
after the video archive is retired, and decide what happens to descriptions, previews, registries and
directories that no longer exist.

### Behavior

1. Validate, take locks, recover.
2. Scan the **video archive** with the registry scanner (registry output goes to the run directory,
   not to a root). Candidates are `is_video=true` rows, plus every file whose path is the
   `rel_path` of a `moved` or `restored` row in `arxgo-videos.csv`: a video that split
   selected through `--video-extensions` (not a restore flag) is restored too. Reserved paths are
   excluded. Previews live in the main archive (stage 2), so the video archive holds only
   originals.
3. When `arxgo-videos.csv` exists, each candidate is matched to its row by `rel_path` to
   recover the original path, size and hash. Candidates without a row are restored to the same
   relative path and logged as `unregistered`. The registry is read from a root that may be shared,
   so a row whose `rel_path` or `description_rel_path` is not a local relative path
   (empty, `.` or `..` segments, an absolute or volume path) or names a
   [reserved path](../spec.md#reserved-paths) is ignored with a warning.
4. Preflight free space for the archive device; a shortfall exits 4 before any mutation.
5. For each candidate, one transaction:
   - destination `<archive>/<rel_path>`;
   - missing parent directory: without `--create-dirs` skip the video and log
     `missing-directory` (exit 6); with `--create-dirs` create it;
   - destination exists: identical content -> treat as placed; different content -> skip with a
     conflict unless `--overwrite`, which replaces it through a `.arxgo-part` rename;
   - `--transfer auto`: rename on the same device, otherwise copy + verify into the archive and
     delete from the video archive; `--transfer copy` keeps the video archive copy;
   - `--descriptions delete` removes `<rel_path>.md` only when its marker line names this video;
   - commit.
6. With `--registry-update`, rows in both `arxgo-videos.csv` copies get `status=restored` (the
   registry replays the transactions of every run, see [contracts](contracts.md#video-registry-csv),
   so restores of an interrupted earlier run count too), and the file registry rows of the restored
   videos get `location` `archive` ([archive view](registry.md#archive-view)). When no
   `moved` rows remain and `--descriptions delete` was used, the registries are renamed to
   `arxgo-videos.restored-<run-id>.csv` rather than deleted. Restore never creates a registry.
7. With `--transfer auto`, the video-archive directories that held restored videos (restored by
   this or an earlier run) are removed, deepest first with their ancestors, when they are empty;
   the video archive root and unrelated directories are kept. The cleanup is best effort: a
   directory that cannot be read or removed is logged and kept.

### Rules

- Restore never deletes a file it did not create or move, and never deletes a description whose front
  matter does not match.
- A video that exists in the video archive but whose registry row says `restored` is restored again
  (the registry is advisory; the filesystem is the source of truth) and logged.

## Acceptance

- Generated archive with nested directories, videos at root and deep levels, non-video media,
  name collisions (`clip.mp4` and `clip.mp4.md`), and Unicode names.
- Same-device and cross-device paths. Cross-device is simulated by an injectable `fsops` device
  function in unit tests and exercised for real in CI where two filesystems are available
  (`/dev/shm` vs the workspace on Linux runners).
- Round trip: `split` then `restore` yields identical relative paths, sizes, mtimes and SHA-256.
- Restore with a deleted archive directory: skipped by default, recreated with `--create-dirs`.
- Conflicts: split never overwrites; restore overwrites only with `--overwrite`.
- Rerunning either operation after success changes nothing and exits 0.
