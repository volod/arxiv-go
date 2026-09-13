# Split and restore

Owners: `video-split` and `video-restore`. Transaction mechanics: [integrity](integrity.md).
Formats: [contracts](contracts.md).

## Split

### Operator problem

Move every video out of the main archive into a video archive with the same directory tree, so the
main archive becomes small and shareable, while each former video location still says what was
there and where it went.

### Behavior

1. Validate options and discover required tools ([CLI](cli.md#validation)).
2. Take the lock on both roots and recover any incomplete run ([integrity](integrity.md#recovery)).
3. Scan the archive ([registry](registry.md)); `is_video=true` rows become candidates. The registry
   is written as in `scan`.
4. Preflight free space and print the plan ([integrity](integrity.md#preflight)). With
   `--dry-run`, stop here with exit 0.
5. For each candidate in walk order, run one transaction:
   - destination `<video-archive>/<rel_path>`; parent directories are created with the source
     directory permission bits (Linux) and never removed by split;
   - `--transfer auto` on the same device: `os.Rename`; if it fails with a cross-device error,
     fall back to the copy path and log it once;
   - copy path: stream to `<dst>.arxgo-part`, fsync, verify (`size` or `hash`), rename to `<dst>`,
     fsync the parent directory, preserve modification time (and permission bits on Linux);
   - write the Markdown stub `<archive>/<rel_path>.md` ([format](contracts.md#markdown-stub));
   - copy path only: remove the source after the stub is durable;
   - commit.
6. Write `arxgo-videos.csv` and `arxgo-videos.md` into the archive root, and identical copies into
   the video archive root so each root is self-describing.
7. Release the lock and log the final statistics.

### Rules

- **Destination exists.** Same size (and hash with `--verify hash`) as the source: treat as already
  placed, continue from `placed`, log `adopted`. Different content: skip the video, log a conflict,
  exit code 6. Split never overwrites.
- **Stub exists.** A stub whose front matter names the same `rel_path` is rewritten; any other file
  at `<rel_path>.md` is a conflict: the video is still moved, the stub is written as
  `<rel_path>.arxgo.md`, and the conflict is logged.
- **Source changed during the run.** Size or mtime differs from the WAL `begin` record at copy
  completion: discard the part file, abort the transaction, retry once, then skip with a warning.
- **Previews are not candidates.** Stage 2 preview clips are videos inside the archive; the scan
  excludes every path listed in the `previews` column of the existing `arxgo-videos.csv`.
- **Idempotency.** A rerun after success scans, finds no video candidates (only stubs), and exits 0
  after regenerating the summaries.
- **Links.** The stub always contains the relative filesystem path from the stub to the video when
  both roots are on the same filesystem namespace, the absolute video path, and, when `--base-url`
  is given, `base-url + "/" + url-escaped rel_path` segments.
- **Metadata.** With `--metadata media` the stub and video registry carry the media fields; with
  `file` they carry file fields only.

## Restore

### Operator problem

Return videos to their original locations, for example before re-sharing the complete archive or
after the video archive is retired, and decide what happens to stubs, previews, registries and
directories that no longer exist.

### Behavior

1. Validate, take locks, recover.
2. Scan the **video archive** with the registry scanner (registry output goes to the run directory,
   not to a root). Candidates are `is_video=true` rows; reserved paths are excluded. Previews live
   in the main archive (stage 2), so the video archive holds only originals.
3. When `arxgo-videos.csv` exists, each candidate is matched to its row by `video_rel_path` to
   recover the original path, size and hash. Candidates without a row are restored to the same
   relative path and logged as `unregistered`.
4. Preflight free space for the archive device.
5. For each candidate, one transaction:
   - destination `<archive>/<rel_path>`;
   - missing parent directory: without `--create-dirs` skip the video and log
     `missing-directory` (exit 6); with `--create-dirs` create it;
   - destination exists: identical content -> treat as placed; different content -> skip with a
     conflict unless `--overwrite`, which replaces it through a `.arxgo-part` rename;
   - `--transfer auto`: rename on the same device, otherwise copy + verify into the archive and
     delete from the video archive; `--transfer copy` keeps the video archive copy;
   - `--stubs delete` removes `<rel_path>.md` only when its front matter names this video;
   - commit.
6. With `--registry-update`, rows in both `arxgo-videos.csv` copies get `status=restored` and the
   summaries are regenerated. When no `moved` rows remain and `--stubs delete` was used, the
   registries are renamed to `arxgo-videos.restored-<run-id>.csv/.md` rather than deleted.
7. Empty directories left in the video archive are removed only with `--transfer auto` and only if
   they contain no other files.

### Rules

- Restore never deletes a file it did not create or move, and never deletes a stub whose front
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
