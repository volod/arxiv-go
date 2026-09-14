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
| [Archive registry](current/archive-registry.md) | Directory walker, file type detection, resumable `scan` operation: `arxgo-registry.csv`, candidate list, statistics, exit 6 for skipped entries | Shipped; default scan fills ISO BMFF media columns |
| [Video split](current/video-split.md) | Resumable split transactions, same-device rename, cross-device copy, recovery, video descriptions and `arxgo-videos.csv` | Shipped |
| [Video restore](current/video-restore.md) | Restore videos with directory, conflict, description and registry policies; crash recovery; the stage-1 end-to-end proof | Available; stage-1 checkpoint and proof accepted (proof runs in CI); operator trial on an archive copy pending |
| [Media previews](current/media-previews.md) | ffmpeg runner, planning, sample and PNG encoding, split/restore WAL integration, registry/description links, release bundles | Shipped; stage-2 checkpoint accepted after repairs, including a split/restore round trip on real drone footage |

`arxgo help [op]`, `arxgo version` and full flag validation work. `scan` writes the resumable file
registry; default `--metadata file` fills ISO BMFF `media_*` columns for MP4, MOV, M4A, M4V and 3GP
without ffprobe; `--metadata media` requires `ffprobe` (exit 3 with download links when it is missing).
`split` moves videos transactionally, writes video descriptions, optionally generates sample clips and
PNG frames with FFmpeg, and regenerates `arxgo-videos.csv` in both roots.
`restore` returns videos from the video archive and can delete their recorded previews. `make dist`
packages both platforms with pinned FFmpeg tools. Stage 1 is proven end to end by
`make test-integration` (kills, resume, restore, round trip through the built binary), which also
drives previews through the binary; stage 2 is accepted, so cloud publishing (stage 3) may start.
The next work is reported by `make plan-status`.
