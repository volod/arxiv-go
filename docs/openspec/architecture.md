# Architecture

This page describes how the [specification](spec.md) maps onto packages, data flow and the
transaction design. Behavior details live in the stage pages; this page owns module boundaries.

## Repository layout

```text
arxiv-go/
|-- cmd/arxgo/main.go            entry point: os.Exit(cli.Run(...)); nothing else
|-- internal/
|   |-- cli/                     flag parsing, validation, exit codes, logger setup, op dispatch
|   |-- scanner/                 walker.go, order.go, entry.go, relpath.go (traversal, reserved and local paths); mimetype.go (detection and flags)
|   |-- media/                   tools.go, guidance.go (discovery), metadata.go (stage 1); ffmpeg.go, preview.go (stage 2)
|   |-- fsops/                   device/space syscalls, durable copy/rename, atomic write
|   |-- state/                   rundir.go, lock.go, checkpoint.go, scanstats.go, report.go, runlog.go, wal.go, recovery.go
|   |-- archive/                 session.go, session_state.go, resume.go, resume_replaced.go, finish.go, progress.go, preflight.go, preflight_run.go, scan.go, scan_pipeline.go, candidates.go; split.go, split_transfer.go, split_recovery.go, split_stub.go, split_report.go, restore.go, restore_exec.go, restore_recovery.go, restore_dirs.go, restore_report.go
|   |-- report/                  csv.go, csv_read.go (file registry); markdown.go, frontmatter.go, names.go, videos.go, summary.go
|   |-- cloud/                   stage 3: target interface, gdrive/, sharepoint/
|   `-- devtools/planning/       repository tooling: plan/spec/doc-link lint and plan status
|-- tools/plancheck/             dev-only Go command wrapping internal/devtools/planning
|-- test/                        integration/end-to-end tests, test-only helpers and testdata
|-- scripts/                     shell helpers (pinned ffmpeg fetch)
|-- make/                        Makefile fragments included by the root Makefile
|-- docs/openspec/               specification tree (this directory)
|-- docs/impl/                   plan.md, current.md, current/, records/
|-- docs/guide/                  planning workflow, development guide
|-- AGENTS.md                    canonical agent rules; CLAUDE.md/GEMINI.md link to it
`-- Makefile                     entry point: help and includes of make/*.mk
```

## Dependency direction

```mermaid
flowchart TD
    main[cmd/arxgo] --> cli
    cli --> archive
    cli --> scanner
    cli --> media
    archive --> scanner
    archive --> media
    archive --> state
    archive --> report
    archive --> fsops
    state --> fsops
    scanner --> media
    report --> media
    archive -. stage 3 .-> cloud
```

Rules:

- `cli` is the only package that reads flags, the environment or the optional `.env` file; it
  passes a validated, typed
  `Options` value down. Domain packages never call `os.Exit` or print to stdout.
- `scanner`, `media`, `report` and `fsops` do not import `archive` or `state`.
- `media` is the only package that runs external processes. `cli` calls `media` only for tool
  discovery, which must finish before the run session starts.
- `fsops` is the only package with build-tagged platform files; it also holds the process liveness
  check used by the run lock.
- `cli` maps validated options onto `archive.Config` and exit codes onto `archive.Status`; it does
  not import `state` directly.
- All long-running functions accept a `context.Context`; cancellation (Ctrl+C) stops at the next
  transaction boundary after writing a checkpoint.

## Data flow

```mermaid
flowchart LR
    A[(archive root)] -->|WalkDir| S[scanner]
    S -->|FileRecord stream| R[registry CSV]
    S -->|video records| P[preflight]
    P -->|plan: videos, bytes, devices| X[executor]
    X -->|WAL begin/step/commit| W[(.arxgo/runs/ID/wal.jsonl)]
    X -->|move or copy+verify+delete| V[(video archive root)]
    X -->|stub .md| A
    X --> VR[video registry CSV + summary.md]
    X -.->|stage 2| FF[ffmpeg previews]
    X -.->|stage 3| C[cloud target]
```

### Split pipeline phases

| Phase | Reads | Writes | Resumable by |
| --- | --- | --- | --- |
| 1. Validate | flags, roots, required tools | nothing | rerun |
| 2. Lock and recover | `.arxgo/lock`, last run WAL | recovered WAL, checkpoint | lock ownership rules |
| 3. Scan | archive tree | `arxgo-registry.csv`, candidate list in run dir | checkpoint cursor |
| 4. Preflight | candidate list, statfs | preflight report in run log | rerun (read-only) |
| 5. Execute | candidate list | videos, stubs, WAL | committed transaction set |
| 6. Report | WAL, candidate list | `arxgo-videos.csv`, `arxgo-videos.md` | regenerate from WAL |

Restore uses the same phases with the roots swapped for scanning (it scans the video archive;
the file registry for that scan stays in the run directory). Phase 5 uses `stub_removed` instead
of `stubbed`. Phase 6 marks matching video-registry rows `restored`.
`scan` runs phases 1-3 only and takes only the archive lock (exclusive, since it writes the
registry); its free-space preflight runs before traversal. Within phase 3 the walker feeds a
bounded detection worker pool and a writer that takes entries back in walk order, so the registry,
the candidate list and the checkpoint cursor never depend on detection timing.

## Transaction state machine

Each video move is one transaction identified by `txid` (run id + sequence). Steps are appended to
the WAL and fsynced before the corresponding filesystem effect is considered durable.

```mermaid
stateDiagram-v2
    [*] --> begun: WAL begin(src, dst, size, mtime, mode)
    begun --> placed: same device: rename(src, dst)
    begun --> copied: other device: copy to dst.arxgo-part + fsync
    copied --> verified: size (+sha256 when --verify=hash) match
    verified --> placed: rename(dst.arxgo-part, dst) + fsync dir
    placed --> stubbed: write stub.md (temp + rename)
    stubbed --> source_removed: other device: remove(src)
    stubbed --> committed: same device
    source_removed --> committed
    committed --> [*]
```

Recovery rules for a transaction without `commit` (full table in
[integrity](stage-1-core/integrity.md#recovery)):

- Before `placed`: delete any `.arxgo-part`, keep the source, mark the transaction `aborted`; the
  file is retried in the resumed run.
- At or after `placed`: the destination is complete; roll forward (write stub, remove source if it
  still exists and the destination verifies), then `commit`.

## Concurrency

- Traversal is single-goroutine for deterministic order. Type detection may use a bounded worker
  pool whose results are re-ordered before writing.
- Transfers run sequentially in stage 1. A `--jobs` flag for parallel cross-device copies is a
  later refinement and must keep WAL ordering per transaction.
- Stage 2 previews run in a bounded ffmpeg worker pool (default 1) after the owning video commits.

## Cross-platform notes

Both columns are implemented. Linux behavior is tested as a gate; the Windows column is gated only
by cross-compilation and `make vet-windows`, and its runtime checks are listed in the deferred
[Windows verification scenario](../guide/windows-verification.md).

| Concern | Linux | Windows |
| --- | --- | --- |
| Same device | `stat.Dev` equality; missing paths use the nearest existing ancestor | Volume serial (`GetVolumePathName` + `GetVolumeInformation`); serial 0 also requires equal mount-point strings |
| Free space | `unix.Statfs` `Bavail` times `Frsize` on Linux (`Bsize` on other Unix) | `windows.GetDiskFreeSpaceEx` caller-available bytes |
| Rename across devices | `EXDEV` -> copy path | `ERROR_NOT_SAME_DEVICE` -> copy path |
| Directory fsync | `fsync` on the parent directory | not supported; skipped, rely on `MoveFileEx` write-through |
| Case sensitivity | sensitive | insensitive; collisions detected by case-folded key |
| Executable names | `ffprobe`, `ffmpeg` | `ffprobe.exe`, `ffmpeg.exe` |
