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
| [Crash safety](current/crash-safety.md) | Filesystem primitives; run lock, `.arxgo/` layout, checkpoints, run log, progress, report, WAL and recovery, disk-space preflight | Shipped; session used by every operation; restore preflight awaits candidates |
| [Media metadata](current/media-metadata.md) | Pure-Go MP4/MOV/M4A metadata, bounded ffprobe parsing for other audio/video formats and ISO fallback, audio-only classification, tool discovery | Shipped; Linux tests and generated-archive scan pass; Windows cross-compiled |
| [Archive registry](current/archive-registry.md) | Directory walker, file type detection, resumable `scan` operation: `arxgo-registry.csv`, candidate list, statistics, exit 6 for skipped entries | Shipped for `--metadata file`; media fields come with media metadata |
| [Video split](current/video-split.md) | Resumable split transactions, same-device rename, cross-device copy, recovery, Markdown stubs, `arxgo-videos.csv` and `arxgo-videos.md` | Shipped |

`arxgo help [op]`, `arxgo version` and full flag validation work. `scan` writes the resumable file
registry; `--metadata media` requires `ffprobe` (exit 3 with download links when it is missing).
`split` moves videos transactionally, writes Markdown stubs and regenerates `arxgo-videos.csv`
and `arxgo-videos.md` in both roots. `restore` still exits 70. The
next work is reported by `make plan-status`.
