# Video Restore

Accepted work: [0020 Video restore](../records/0020-restore-implement-video-restore.md).
Specification: [restore](../../openspec/stage-1-core/split-restore.md#restore),
[recovery](../../openspec/stage-1-core/integrity.md#recovery).
The capability remains planned until the stage-1 checkpoint and proof are accepted.

`arxgo restore` takes locks on both roots, recovers an incomplete restore run, then scans the
**video archive** (the file registry for that scan stays in the run directory). Candidates are
`is_video=true` rows. When `arxgo-videos.csv` exists, each candidate is matched by
`video_rel_path` to recover the original archive path; a video with no row is restored to the
same relative path and logged as unregistered. A row whose status is already `restored` is
restored again if the file is still in the video archive.

Preflight uses the restore table (negligible on a same-device rename; the sum of candidate sizes
on the archive device for copy or other devices). `--dry-run` stops after the plan. Each
candidate is one WAL transaction with `stub_removed` in place of split's `stubbed`:

- destination `<archive>/<rel_path>`;
- missing parent: skip with `missing-directory` (exit 6) unless `--create-dirs`;
- destination exists with identical content: adopt as placed; different content: skip as a
  conflict unless `--overwrite`, which replaces through a `.arxgo-part` rename;
- `--transfer auto` renames on the same device and otherwise copies, verifies, then deletes the
  video-archive file; `--transfer copy` keeps that copy;
- `--stubs delete` removes a Markdown stub only when its front matter names this `rel_path`;
  a foreign file at `<rel_path>.md` is left untouched. `--stubs keep` leaves stubs in place.

Empty directories left in the video archive are removed only with `--transfer auto` and only when
they contain no other files. The video-archive root and `.arxgo/` are kept.

With `--registry-update` (the default), both `arxgo-videos.csv` copies get `status=restored` for
committed paths and the summaries are regenerated. When no `moved` rows remain and `--stubs delete`
was used, the live files are renamed to `arxgo-videos.restored-<run-id>.csv/.md`.
`--registry-update=false` leaves the video registry unchanged.

Linux tests cover split-then-restore round trips (paths, sizes, mtimes, SHA-256) on rename and
copy, missing directories, conflicts vs `--overwrite`, foreign stubs, `--transfer copy`, crash
injection on both transfer paths, dry-run, registry retire, and a second run. Windows is
cross-compiled only; runtime checks belong to the
[Windows verification scenario](../../guide/windows-verification.md).
