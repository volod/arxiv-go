# Integrity: lock, WAL, checkpoints, preflight and progress

Owner: `crash-safety`. Consumers: `archive-registry` (checkpoint cursor), `video-split`,
`video-restore`, `media-previews`, `cloud-publishing`.

## Operator problem

A run over millions of files and terabytes of video will be interrupted: power loss, a full disk,
a dropped network share, Ctrl+C. After any interruption the archives must be consistent (every
video is in exactly one complete place, or in both places with the original intact) and the
operator must be able to rerun the same command to continue.

## State layout

```text
<archive>/.arxgo/
|-- lock                         JSON: run id, pid, host, start time, operation, peer root
|-- current -> runs/<run-id>     text file holding the active run id (no symlink on Windows)
`-- runs/<run-id>/
    |-- options.json             validated options that define the run
    |-- checkpoint.json          last durable checkpoint
    |-- wal.jsonl                transaction log (split/restore)
    |-- candidates.jsonl         scan result for split/restore
    |-- report.json              final statistics
    `-- run.log.jsonl            structured log
<video-archive>/.arxgo/
`-- lock                         mirror lock naming the owning archive run
```

`<run-id>` is `YYYYMMDDTHHMMSSZ-<8 hex>`. Run directories are kept; a later cleanup flag may prune
them.

## Run lock

- Created with `O_CREATE|O_EXCL` in both roots before any mutation; `scan` takes only the archive
  lock.
- A second process finding the lock exits 5 and prints the owner.
- A lock whose `host` equals this host and whose `pid` is not alive is stale; the process exits 5
  with instructions unless `--force-unlock` is given. Locks from another host are never taken over
  automatically (network shares).
- The lock is removed on normal exit, on exit 3/4/6, and on interrupt after the checkpoint.

## Write-ahead log

- JSON Lines, one record per line, appended with `O_APPEND` and `fsync` after each record that
  precedes an irreversible step (`begin`, `placed`, `stubbed`, `source_removed`, `commit`).
- Record fields: `v` (format version), `txid`, `seq`, `step`, `ts`, and step payload. Formats are
  in [contracts](contracts.md#wal-record).
- A torn last line (crash mid-write) is detected by JSON decode failure on the final line only and
  truncated during recovery; a decode failure on any other line exits 5 as corruption.
- Steps for split: `begin -> [copied -> verified] -> placed -> stubbed -> [source_removed] ->
  commit`. Restore uses the same steps with `stub_removed` replacing `stubbed`. Stage 2 adds
  `preview` records after `commit`; stage 3 adds `published`.

## Recovery

Recovery runs after taking the lock and before scanning, for the run named in `current` when its
report is absent. For each transaction without `commit` or `aborted`, the last durable step decides:

| Last step | Filesystem check | Action |
| --- | --- | --- |
| `begin` | `dst.arxgo-part` may exist; `dst` absent | Delete part file; `aborted` |
| `begin` (same-device rename) | `src` absent and `dst` present with begin size | Rename happened: write `placed`, roll forward |
| `copied`, `verified` | part file present | Delete part file; `aborted` (cheaper and safer than re-verifying) |
| `placed` | `dst` present and size matches | Roll forward: write stub, remove source (copy path), commit |
| `placed` | `dst` missing or wrong size | Corruption: log, exit 5, leave all files |
| `stubbed` | | Remove source if present and `dst` verifies; commit |
| `source_removed` | | Commit |

Aborted transactions are retried by the resumed run because their sources are still candidates.
After recovery, a run whose scan had completed continues from `candidates.jsonl`, skipping
committed `rel_path`s (a hash set built from the WAL). With `--new-run` a fresh run id starts
instead.

## Checkpoints

- Written every `--checkpoint-every` processed files or `--checkpoint-interval`, whichever comes
  first, at phase boundaries, and on interrupt.
- Written atomically with `fsops.AtomicWriteFile`: `checkpoint.json.arxgo-part`, fsync, rename,
  fsync directory. A leftover part file is ignored and replaced.
- Contents: run id, phase, scan cursor (walk order key of the last fully processed entry), registry
  part-file byte offset, candidate index, counters (files, bytes, videos done/skipped/failed),
  WAL byte offset, and elapsed time.
- The checkpoint is an optimization for scan resume and progress counters; WAL records remain the
  authority for file placement.
- After a checkpoint, the WAL may be compacted by rewriting only uncommitted transactions to a new
  file and renaming it (optional optimization, not required in stage 1).

## Preflight

Preflight runs after the scan and before the first mutation, and prints its computation.

| Operation and placement | Required free space |
| --- | --- |
| `scan` | archive device (or `--registry` device): estimated registry size = rows x 256 B, plus metadata JSON estimate (512 B per media row in `media` mode) |
| `split`, same device, `--transfer auto` | archive device: stubs (4 KiB each) + video registries (1 KiB per video, two copies) |
| `split`, other device or `--transfer copy` | video archive device: sum of candidate sizes + registry copy; archive device: stubs + registry. Sources are removed one by one, so only the largest file is needed twice on a shared device when both roots share one |
| `restore`, same device, `auto` | archive device: negligible (renames) |
| `restore`, other device or `copy` | archive device: sum of candidate sizes |
| Stage 2 previews | archive device additionally: estimated preview bytes from [previews](../stage-2-previews/previews.md#space-estimate) |

Every write device must keep `--min-free` after the operation. Free space comes from
`fsops.FreeSpace(path)` (Statfs / GetDiskFreeSpaceEx). When the sum exceeds free space minus
`--min-free`, `arxgo` prints required, available and shortfall per device and exits 4. Devices are
identified with `fsops.SameDevice`; two roots on one device are checked once with summed
requirements. Network filesystems that report no free space (`0` total) produce a warning and
continue, since the value is unknowable.

Resume recomputes preflight from the remaining candidates only.

## Progress and statistics

- A progress line at most every `--progress-interval`, plus one at each phase boundary:
  `phase=execute done=1204/5530 videos bytes=812.4GiB/3.1TiB rate=182MiB/s eta=3h41m skipped=2
  failed=0`.
- Scan progress reports entries, bytes seen and entries/s; ETA is not shown during scan because
  the total is unknown.
- `debug` level logs each transaction step; `info` logs one line per completed video only when the
  video is larger than `--large-threshold`.
- `report.json` and the final log line hold totals per phase, skipped/failed items with reasons,
  per-device bytes written and freed, and wall time.
- SIGINT/SIGTERM (Ctrl+C / console close on Windows) cancels the context; the current transaction
  finishes its current step, a checkpoint is written, and the process exits 130. A second signal
  exits immediately; recovery handles the rest.

## Acceptance

- Crash injection: a test hook panics or returns an error after each WAL step and each filesystem
  effect in turn; recovery plus resume yields the same final state as an uninterrupted run, for
  both same-device and copy paths.
- Torn final WAL line is truncated; corrupt middle line exits 5.
- Lock: concurrent second run exits 5; stale lock requires `--force-unlock`.
- Preflight: an injected free-space function below requirement exits 4 with no file mutated;
  exactly at requirement plus `--min-free` passes.
- Interrupt: canceling the context mid-run leaves a checkpoint and resumes to completion.
