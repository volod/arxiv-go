# Current Implementation

This index describes behavior available now. For product intent read the
[specification](../openspec/spec.md); for work that remains read the [forward plan](plan.md).

## Documentation shape

Each capability gets one page under `current/` when its first task is accepted. When a page grows,
it becomes a short index and focused topics move under `current/<capability>/`. New pages are
linked here in the same change.

## Areas

| Area | Owns | State |
| --- | --- | --- |
| [Project foundation](current/project-foundation.md) | Module, CLI contract (flags, `.env` file, validation, exit codes, logger, signals), `make setup`, Make targets, CI, planning tooling | Shipped; operations validate options and run the lock/checkpoint session |
| [Crash safety](current/crash-safety.md) | Filesystem primitives; run lock, `.arxgo/` layout, checkpoints, run log, progress, report, WAL and recovery (including a run that a new run replaces), disk-space preflight | Shipped; session used by scan, split and restore |
| [Media metadata](current/media-metadata.md) | Pure-Go MP4/MOV/M4A metadata, bounded ffprobe parsing for other audio/video formats and ISO fallback, audio-only classification, tool discovery | Shipped; Linux tests and generated-archive scan pass; Windows cross-compiled |
| [Archive registry](current/archive-registry.md) | Directory walker, file type detection, resumable `scan` operation: `arxgo-registry.csv`, candidate list, statistics, exit 6 for skipped entries | Shipped; default scan fills ISO BMFF media columns; every registry keeps its full header ([0052](records/0052-registry-stabilize-registry-columns.md)); moved files keep rows with `location`, owned artifacts have none, unchanged registries stay untouched ([0053](records/0053-registry-preserve-archive-registry.md)); unchanged files are not opened again, `--redetect` forces detection ([0054](records/0054-registry-reuse-registry-detection.md)) |
| [Video split](current/video-split.md) | Resumable split transactions, same-device rename, cross-device copy, recovery, video descriptions and `arxgo-videos.csv` | Shipped |
| [Video restore](current/video-restore.md) | Restore videos with directory, conflict, description and registry policies; crash recovery; the stage-1 end-to-end proof | Shipped; stage-1 checkpoint, generated-archive proof and operator archive-copy trial accepted |
| [Media previews](current/media-previews.md) | ffmpeg runner, planning, sample and PNG encoding, split/restore WAL integration, registry/description links, release bundles | Shipped; stage-2 checkpoint accepted after repairs, including a split/restore round trip on real archive footage |
| [CATIA archive](current/catia-archive.md) | Built-in CATIA kind table, `is_catia` on every scan, file-registry column order, reserved `arxgo-catia.csv`; the payload split/restore executor; pure-Go CATIA extraction and `catia:` / text-sidecar rendering; `split --catia` with CATIA descriptions and `arxgo-catia.csv`; `--catia-text` post-commit sidecars; `archive:` in descriptions and sidecars, sidecar identity block; `restore --catia` with owned description and text sidecar cleanup; read-only `catia-index` document with reserved `arxgo-catia-text.md` | In progress; classification, payload executor, extraction, CATIA split, text sidecars, text index, CATIA restore, the stage-4 proof and checkpoint shipped; registry-and-metadata checkpoint remains |

`arxgo help [op]`, `arxgo version` and full flag validation work. `scan` writes the resumable file
registry; default `--metadata file` fills ISO BMFF `media_*` columns for MP4, MOV, M4A, M4V and 3GP
without ffprobe; `--metadata media` requires `ffprobe` (exit 3 with download links when it is missing).
A rescan or split opens only files that are new or changed since the last registry
([0054](records/0054-registry-reuse-registry-detection.md)).
`split` moves videos transactionally, writes video descriptions, optionally generates sample clips and
PNG frames with FFmpeg, and regenerates `arxgo-videos.csv` in both roots.
`restore` returns videos from the video archive and can delete their recorded previews. `make dist`
packages both platforms with pinned FFmpeg tools. Stage 1 is proven end to end by
`make test-integration` (kills, resume, restore, round trip through the built binary), which also
drives previews through the binary, and by the operator trial on an archive copy
([0042](records/0042-restore-approve-stage-1-on-operator-archive-copy.md)). Stage 2 is accepted.
`scan` marks CATIA files in the file registry
([0043](records/0043-catia-implement-catia-classification.md)); split and restore run one payload
executor that replays only runs of the selected payload
([0044](records/0044-catia-generalize-payload-split-restore.md)); CATIA metadata and optional text
are extracted in pure Go
([0045](records/0045-catia-implement-catia-extraction.md)); `split --catia` moves CATIA files into a
separate CATIA archive with `catia:` descriptions and `arxgo-catia.csv`, and a video split recovers
an interrupted CATIA split ([0046](records/0046-catia-implement-catia-split.md)); `--catia-text`
writes owned `arxgo-text:` sidecars after commit and for earlier moved files
([0047](records/0047-catia-implement-catia-text-sidecars.md)); `restore --catia` returns CATIA files
and deletes owned descriptions and text sidecars
([0048](records/0048-catia-implement-catia-restore.md)); a restore records `sidecar_cleanup`, so the
next restore deletes the previews or text sidecars an interrupted, replaced restore left
([0049](records/0049-catia-repair-replaced-restore-sidecar-cleanup.md)); `make test-integration` proves both
payloads with seeded kills and a byte-identical round trip on a generated archive
([0050](records/0050-catia-prove-stage-4-on-generated-archive.md)). Descriptions and sidecars name
their archive root, and each sidecar repeats its description's identity fields
([0055](records/0055-catia-record-source-location-in-metadata.md)); `catia-index` writes one
read-only Markdown document of every moved CATIA file from the recorded descriptions and sidecars,
listing files without text ([0056](records/0056-catia-implement-catia-text-index.md)). The stage-4 checkpoint ran the
built binary over a disposable copy of the operator archive, repaired five defects and routed the
registry-schema and metadata-completeness gaps to their own tasks
([0051](records/0051-catia-review-stage-4-catia.md)); `catia-archive` stays planned until the
registry-and-metadata checkpoint is accepted, and stage 3 cloud publishing may now start. The next work is reported by
`make plan-status`.
