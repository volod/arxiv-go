# Crash Safety

Accepted work: [0005 Filesystem primitives](../records/0005-safety-implement-filesystem-primitives.md);
[0006 Run lock and checkpoint](../records/0006-safety-implement-run-lock-and-checkpoint.md).
Specification: [integrity](../../openspec/stage-1-core/integrity.md) and
[contracts](../../openspec/stage-1-core/contracts.md#run-lock). Remaining work (write-ahead log and
recovery, disk-space preflight) is in the [plan](../plan.md#crash-safety----crash-safety).

## Filesystem primitives (`internal/fsops`)

Building blocks for lock, WAL, preflight, split and restore. Process liveness for the run lock also
lives here (the only package with build-tagged files).

- `Ops` interface with the real `System` implementation. Tests of later packages substitute fakes,
  for example a device check that reports two roots on different devices, or a `Rename` returning
  `NewCrossDeviceError`.
- `SameDevice(a, b)` / `DeviceOf(path)`: `st_dev` on Unix; volume serial via `GetVolumePathName` +
  `GetVolumeInformation` on Windows. Missing paths resolve to the nearest existing ancestor. A
  Windows volume serial of 0 (some network shares) also requires equal mount-point strings so two
  serial-0 shares are not treated as one device.
- `FreeSpace(path)` returns `Space{Total, Free, Available}` from `statfs` (`f_frsize` units on
  Linux) or `GetDiskFreeSpaceEx`. `Available` is the unprivileged/quota-aware figure preflight
  should use; `Total == 0` signals an unknown network value.
- `Rename(old, new)` moves without ever replacing `new` (`renameat2(RENAME_NOREPLACE)` with a
  check-then-rename fallback; `MoveFileEx` without replace on Windows) and flushes both parent
  directories on Unix. `Replace` overwrites atomically. Errors are `*os.LinkError`:
  `errors.Is(err, fs.ErrExist)` for an existing target and `IsCrossDevice(err)` for `EXDEV` /
  `ERROR_NOT_SAME_DEVICE`. Any other error means the move happened but the directory flush failed.
  Windows retries transient sharing violations for about 1.3 s and supports paths longer than
  `MAX_PATH`.
- `DurableCopy(ctx, src, dst, CopyOptions)` writes `dst.arxgo-part`, fsyncs, rejects a source whose
  size or mtime changed (`ErrSourceChanged`, also against `ExpectSize`/`ExpectModTime`), restores
  mtime and (on Unix, where the filesystem supports modes) permission bits, verifies size or
  SHA-256 by re-reading the part file (`ErrVerifyMismatch` / `*VerifyError`), then renames into
  place and flushes the directory. It never overwrites unless `Overwrite` is set, removes the part
  file on every error (and leaves an existing destination untouched even with `Overwrite`), honors
  cancellation before creating a part file and between chunks, and reports progress every 32 MiB.
  `CopyResult` returns size, mtime and the hex digest. The size mode keeps the kernel copy fast
  paths; the hash mode hashes on a separate goroutine.
- `AtomicWrite(path, perm, fn)` / `AtomicWriteFile(path, data, perm)`: buffered write to
  `path.arxgo-part`, fsync, atomic replace, directory flush. Readers see the old or the new content
  only.
- `ProcessAlive(pid)`: `kill(pid, 0)` on Unix (`EPERM` still means alive; `/proc/<pid>` if kill is
  denied) and `OpenProcess` + `GetExitCodeProcess` on Windows.

Measured on the development host (4 GiB file, NVMe ext4 and tmpfs): size-verified copy about
4.4 GiB/s across devices; hash-verified about 1.15 GiB/s across devices, bounded by SHA-256.

## Run layout (`internal/state`)

`<archive>/.arxgo/lock`, `<archive>/.arxgo/current` (plain file holding the run id) and
`<archive>/.arxgo/runs/<run-id>/` with `options.json`, `checkpoint.json`, `run.log.jsonl` and, when
the run reaches a final state, `report.json`. `<run-id>` is `YYYYMMDDTHHMMSSZ-<8 hex>` in UTC.
Formats are in [contracts](../../openspec/stage-1-core/contracts.md#run-lock).

A later process resumes `current` when that run has no report and the operation plus defining
options match (roots and operation flags; not logging, progress, checkpoint cadence, `--min-free`,
`--dry-run`, `--new-run` or `--force-unlock`). `--dry-run` always creates its own directory and
never changes `current`. `--new-run` leaves an incomplete run in place.

## Run lock

- Created with `O_CREATE|O_EXCL` in the archive root, then in the video archive root. `scan` takes
  only the archive lock. If the mirror lock is refused, the archive lock is released and no run
  directory is created. `split` creates a missing video archive root after the archive lock, not
  in `--dry-run`.
- A second process exits 5 and prints the owner (`pid`, `host`, `run_id`, `op`).
- Stale: same host (case-insensitive) and a dead pid, or a lock that names this process's own pid.
  Takeover requires `--force-unlock`. Another host is never taken over, even with `--force-unlock`.
- Takeover moves the judged bytes to `lock.<hex>.arxgo-part`, checks them, removes, then creates
  with `O_EXCL`.
- Every checkpoint `Verify`s that each held lock still names this run. A lost lock stops the run
  with exit 5 and writes nothing more into the run directory. The lock is released on the exits the
  process controls except that operator-needed `Finish` keeps it.

## Checkpoints, log, progress and report (`internal/archive`)

`archive.Start` / `Session` / `Finish` own the run lifecycle. `cli` maps flags onto `archive.Config`
and `archive.Status` onto exit codes; it does not import `state` in production.

- Checkpoints at start, every `--checkpoint-every` files or `--checkpoint-interval`, at phase
  boundaries, and at end (including interrupt and failure), through `fsops.AtomicWriteFile`. A
  leftover `checkpoint.json.arxgo-part` is ignored. Contents include phase, scan cursor, offsets,
  counters and elapsed time accumulated across resumed processes.
- `run.log.jsonl` is JSON Lines at info (debug when `--log-level debug`), UTC times, independent of
  the console level and format. A torn last line is terminated on resume. The console and the file
  share one `slog` logger via `state.Fanout`.
- Progress lines at most every `--progress-interval`, plus one at each phase boundary. Scan-style
  phases (unknown total) report entries, bytes and entries/s with no ETA. Execute-style phases
  report done/total, bytes, window rate, phase-average ETA, skipped and failed. Counters are
  atomic; tests inject a clock.
- `report.json` is written on completed, partial and not-implemented runs, and on every dry run
  (including interrupted or failed dry runs). An interrupted or failed real run has none and is
  resumed. Context cancel writes a final checkpoint and exits 130.

`scan`, `split` and `restore` currently run this lifecycle and then exit 70 (operation body not in
this build). WAL records are not written yet.

## Limits

- Windows behavior is cross-compiled only; the CI Windows test job has not run it yet.
- `DurableCopy` needs the destination directory to exist and does not remove the source.
- Remote-host lock refusal is tested with injected host names, not a live network share.
- A `Start` that sees corrupt run state releases the lock (exit 5) so `--new-run` does not need
  `--force-unlock`; see the record's audit note.
