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

`<run-id>` is `YYYYMMDDTHHMMSSZ-<8 hex>` (UTC). Run directories are kept; a later cleanup flag may
prune them. Formats of `lock`, `options.json`, `checkpoint.json` and `report.json` are in
[contracts](contracts.md#run-lock).

A run is complete when its `report.json` exists. Starting an operation (after taking the lock)
resumes the run named in `current` when that run is incomplete, has the same operation and the
same defining options (the roots and operation flags; not logging, progress, checkpoint cadence,
`--min-free`, `--dry-run`, `--new-run` or `--force-unlock`). Otherwise a new run directory is
created and `current` points at it; an incomplete run with different options is left in place with
a warning. `--dry-run` always creates its own run directory, never resumes and never changes
`current`, so it cannot hide an interrupted real run from recovery.

## Run lock

- Created with `O_CREATE|O_EXCL` in both roots before any mutation; `scan` takes only the archive
  lock.
- A second process finding the lock exits 5 and prints the owner.
- The archive lock is taken first, then the mirror lock; when the mirror lock is refused, the archive
  lock is released again and nothing else is written. `split` creates a missing video archive root
  after taking the archive lock (not in `--dry-run`, which then skips the mirror lock).
- A lock whose `host` equals this host (case-insensitive) and whose `pid` is not alive is stale; a
  lock naming the current process's own pid is also stale (pid reuse, for example pid 1 in a
  container). The process exits 5 with instructions unless `--force-unlock` is given. Liveness is
  `kill(pid, 0)` on Unix and `OpenProcess` + `GetExitCodeProcess` on Windows. Locks from another
  host are never taken over, even with `--force-unlock` (network shares): the operator deletes the
  file once that run is known to be stopped.
- An empty or undecodable lock (a crash between create and write) is re-read briefly, then exits 5
  as unreadable; `--force-unlock` replaces it.
- A takeover moves the judged lock aside under a unique `lock.<hex>.arxgo-part` name, checks that it
  moved exactly the bytes it judged (otherwise it puts the lock back and retries), removes it and
  creates its own lock with `O_EXCL`.
- Every checkpoint verifies that both lock files still name this run; a removed or replaced lock
  stops the run with exit 5 and nothing more is written into the run directory.
- The lock is removed on every exit the process controls (0, 1, 3, 4, 6, 70, and 130 after the
  checkpoint), except exit 5 for lost locks or state that needs operator action.

## Write-ahead log

- JSON Lines, one record per line, appended with `O_APPEND`. `begin`, `placed`, `stubbed`,
  `stub_removed`, `source_removed`, `commit` and `aborted` are fsynced after the write. `copied` and
  `verified` are not: losing them is equivalent to still being at `begin`, and recovery aborts.
- Record fields: `v` (format version), `txid` (`{run-id}-{6-digit}`), `seq` (monotonic in the file),
  `step`, `ts`, and step payload. Formats are in [contracts](contracts.md#wal-record).
- Opening `wal.jsonl` truncates a torn last line (JSON decode failure on the final line only). A
  decode failure on any other line, or a last line that decodes but is not `v` 1, exits 5 as
  corruption.
- Steps for split: `begin -> [copied -> verified] -> placed -> stubbed -> [source_removed] ->
  commit`. Restore uses the same steps with `stub_removed` replacing `stubbed`. Stage 2 adds
  `preview` records after `commit`; stage 3 adds `published`.

## Recovery

Recovery runs after taking the lock and before scanning, for the run named in `current` when its
report is absent. For each transaction without `commit` or `aborted`, in begin `seq` order, the last
durable step decides:

| Last step | Filesystem check | Action |
| --- | --- | --- |
| `begin` | source present (`dst.arxgo-part` may exist; `dst` absent or ignored) | Delete part file; `aborted` |
| `begin` (same-device rename) | `src` absent and `dst` present with begin size | Delete leftover part; write `placed`, roll forward |
| `begin` | `src` absent and `dst` missing or wrong size | Corruption: log, exit 5, leave all files |
| `copied`, `verified` | part file present | Delete part file; `aborted` (cheaper and safer than re-verifying) |
| `copied`, `verified` | part absent and `dst` present with begin size | Place finished before the WAL record: write `placed`, roll forward |
| `copied`, `verified` | part and `dst` absent, source present | `aborted` |
| `copied`, `verified` | part and `dst` absent, source absent | Corruption: log, exit 5 |
| `placed` | `dst` present and size matches | Roll forward: write stub, remove source (copy path), commit |
| `placed` | `dst` missing or wrong size | Corruption: log, exit 5, leave all files |
| `stubbed`, `stub_removed` | | Remove source if present and `dst` verifies; commit |
| `source_removed` | | Commit |

Aborted transactions are retried by the resumed run because their sources are still candidates.
After recovery, a run whose scan had completed continues from `candidates.jsonl`, skipping
committed `rel_path`s (a hash set built from the WAL). With `--new-run` a fresh run id starts
instead. Recovery is idempotent: a second pass writes nothing when the first succeeded.

## Checkpoints

- Written when a run starts, every `--checkpoint-every` processed files or `--checkpoint-interval`,
  whichever comes first, at phase boundaries, and when the run ends (including interrupt and
  failure).
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
| `split`, other devices | video archive device: sum of candidate sizes + registry copy; archive device: stubs + registry |
| `split`, same device, `--transfer copy` | the shared device: stubs + both registry copies + the largest candidate. Sources are removed one by one after each copy commits, so only the largest file is ever held twice |
| `restore`, same device, `auto` | archive device: negligible (renames) |
| `restore`, other devices, or `--transfer copy` | archive device: sum of candidate sizes (`copy` keeps the video archive copy) |
| `split` and `restore` run state | archive device additionally: 2 KiB of WAL records per candidate |
| Stage 2 previews | archive device additionally: estimated preview bytes from [previews](../stage-2-previews/previews.md#space-estimate) |

A device is a write device when a role placed on it is written by the operation: the archive and
the video archive for `split`, the archive for `restore`, and the registry file's device for
`scan`. Every write device must keep `--min-free` after the operation, even when its estimate is
zero. Free space is the caller-available figure of `fsops.FreeSpace(path)` (Statfs `Bavail` /
GetDiskFreeSpaceEx); a missing root (a video archive split will create) uses its nearest existing
ancestor. A device passes when required + `--min-free` <= available, so exactly at the threshold
passes and one byte less fails. Devices are identified with `fsops.SameDevice`; two roots on one
device are checked once with summed requirements and free space is read once per device.

Preflight prints one `preflight device` line per write device with its roles, path, `required`,
`min_free`, `available`, `shortfall` and the named estimates (`needs`), plus the same figures in
exact bytes (`*_bytes`), then `preflight passed` or `preflight failed: insufficient free space`
with the summed shortfall. The lines go to the console and the run log. On a shortfall `arxgo`
exits 4 before any archive mutation, also with `--dry-run` (whose report records status
`insufficient_space`). A real run refused by preflight writes no report and releases its locks, so
the same command resumes it once space is freed (`--min-free` may change on resume). Network
filesystems that report no free space (`0` total) show `available=unknown`, produce a warning and
continue, since the value is unknowable. Failing to read device or free-space information is an
ordinary failure (exit 1).

Resume recomputes preflight from the remaining candidates only.

## Progress and statistics

- A progress line at most every `--progress-interval`, plus one at each phase boundary:
  `phase=execute done=1204/5530 videos bytes=812.4GiB/3.1TiB rate=182MiB/s eta=3h41m skipped=2
  failed=0`.
- Scan progress reports entries, bytes seen and entries/s; ETA is not shown during scan because
  the total is unknown.
- `debug` level logs each transaction step; `info` logs one line per completed video only when the
  video is larger than `--large-threshold`.
- Scan-style phases (unknown total) log `phase=scan entries=5000 bytes=3.0GiB rate="2500 entries/s"`.
  The rate covers the window since the previous line; the ETA extrapolates the phase average over
  the remaining bytes (items when the byte total is unknown).
- `report.json` and the final log line hold totals per phase, skipped/failed items with reasons,
  bytes written and freed per root, and wall time. `report.json` is written when a run completes
  (exit 0 or 6, or 70 while an operation is not implemented) and for every dry run (including one
  refused by preflight); an interrupted
  or failed run has none and is resumed. Per-phase figures cover the process that wrote the report;
  the counters are cumulative across resumed processes.
- `run.log.jsonl` receives records at `info` and above (or `debug` with `--log-level debug`) as JSON
  Lines with UTC times, whatever the console level and format. It is appended on resume (a torn
  last line is terminated first) and flushed at phase boundaries and at the end of the run.
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
  exactly at requirement plus `--min-free` passes; one byte less fails; a zero-total device warns.
- Interrupt: canceling the context mid-run leaves a checkpoint and resumes to completion.
