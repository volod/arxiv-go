# Crash Safety

Accepted work: [0005 Filesystem primitives](../records/0005-safety-implement-filesystem-primitives.md);
[0006 Run lock and checkpoint](../records/0006-safety-implement-run-lock-and-checkpoint.md);
[0007 Write-ahead log and recovery](../records/0007-safety-implement-write-ahead-log-and-recovery.md);
[0009 Disk-space preflight](../records/0009-safety-implement-disk-space-preflight.md);
[0022 Recover an incomplete run before replacing it](../records/0022-restore-recover-incomplete-run-before-replacing-it.md).
Specification: [integrity](../../openspec/stage-1-core/integrity.md) and
[contracts](../../openspec/stage-1-core/contracts.md#wal-record). Reviewed by the
[stage-1 integrity checkpoint](../records/0021-restore-review-stage-1-integrity.md).

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
`<archive>/.arxgo/runs/<run-id>/` with `options.json`, `checkpoint.json`, `run.log.jsonl`,
`wal.jsonl` when a mutating run writes transactions, and, when the run reaches a final state,
`report.json`. `<run-id>` is `YYYYMMDDTHHMMSSZ-<8 hex>` in UTC.
Formats are in [contracts](../../openspec/stage-1-core/contracts.md#run-lock). Split and restore
record `payload` (`video` or `catia`) and that payload's mirror root (`video_archive` or
`catia_archive`) in
`options.json`; `scan` records neither. `archive.Config.Payload` carries the kind and mirror root,
and `archive.Start` refuses a split or restore without a known payload, or a scan with one, before
taking a lock ([0044](../records/0044-catia-generalize-payload-split-restore.md)); a CATIA restore is
also refused there until it ships ([0046](../records/0046-catia-implement-catia-split.md)). A run
resumes only when the recorded payload and mirror root match too.

A later process resumes `current` when that run has no report and the operation plus defining
options match (roots, payload, `--catia-text` and other operation flags; not logging, progress, checkpoint cadence, `--min-free`,
`--dry-run`, `--new-run` or `--force-unlock`). `--dry-run` always creates its own directory, recovers
nothing and never changes `current`.

Otherwise (other defining options, another operation, or `--new-run`) the incomplete run is left in
place, but first its unfinished WAL transactions are recovered, so `current` never moves away from
them. `archive.Config.RecovererFor` rebuilds that run's resolver from the payload and options in its
`options.json` (`cli.recovererFor` requires the payload, checks that the stored options name the same
one, and decodes `SplitOptions` or `RestoreOptions`), so a split crashed after `placed`
gets the description with its own `--base-url` and `--verify`, and a restore keeps its `--descriptions` and
`--transfer` policies. The recovery is logged to the console and appended to the earlier run's
`run.log.jsonl`. It needs the locks of that run's roots. A split or restore replacing a run of the
other payload takes the lock of that run's recorded mirror root for the recovery and releases it
afterwards, so a video split rolls an interrupted CATIA split forward with its CATIA description
([0046](../records/0046-catia-implement-catia-split.md)); a missing root, or a refused lock, exits 5.
`scan` (archive lock only) or a run of the same payload on another mirror root stops with exit 5
(`archive.ErrUnrecoveredRun`) before creating a run directory, names the run, the archive and the
mirror flag and root to rerun with, and releases its locks. An interrupted split or restore whose `options.json` names no payload is corrupt state
(exit 5).

## Run lock

- Created with `O_CREATE|O_EXCL` in the archive root, then in the payload's mirror root (the video
  or CATIA archive). `scan` takes only the archive lock. If the mirror lock is refused, the archive lock is
  released and no run directory is created. `split` creates a missing mirror root after the archive
  lock, not in `--dry-run`.
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
  leftover `checkpoint.json.arxgo-part` is ignored. Contents include phase, scan cursor, offsets
  (including `wal_offset` when a WAL is open), counters and elapsed time accumulated across resumed
  processes.
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

`scan` runs its [registry scan](archive-registry.md#scan-operation-internalarchive) inside this
lifecycle; `split` runs [video transactions](video-split.md); `restore` runs
[video restore](video-restore.md).
A resumed run opens `wal.jsonl`, truncates a torn tail, and runs recovery when a resolver is supplied.

## Write-ahead log and recovery (`internal/state`)

JSON Lines `wal.jsonl` in the run directory. Each video move is one transaction (`txid` =
`{run-id}-{6-digit}`). `copied` and `verified` are written without fsync; every other step is
fsynced. Opening the file truncates a torn final line; a corrupt middle line or an unsupported
version is `ErrStateCorrupt` (exit 5).

Recovery walks open transactions in begin `seq` order through an operation-supplied `Resolver`
(inspect, delete part, write or remove description, remove source). The engine applies the
[recovery table](../../openspec/stage-1-core/integrity.md#recovery): unplaced work is aborted and
retried; at or after `placed` it rolls forward; a missing or wrong-size destination after `placed`
stops for the operator. `description_removed` is the restore counterpart of `described`. A second recover
pass is a no-op. Resume skips `rel_path`s in the committed-set hash index.

`test/fixtures/crashtest` injects a crash after each WAL step and filesystem effect. Fake split
and restore operations (copy and rename) recover to the same tree as an uninterrupted run.
`archive.Start` recovers an existing WAL when `Config.Recoverer` is set.

## Disk-space preflight (`internal/archive`)

`preflight.go` holds the pure model: `Plan(Candidates, PreflightOptions, DeviceInfo) Requirement`.
`Candidates` is a summary (count, bytes, largest, media rows, `PreviewBytes`, the split
estimate of previews not yet published, and `TextBytes`, min(1 MiB, file size) per CATIA file that
still needs a `--catia-text` sidecar). `DeviceInfo` lists devices with their roles (`archive`,
`video_archive`, `catia_archive`, `registry`) and `fsops.Space`. The result has one `DeviceRequirement` per write
device with named estimates (`needs`), `Required`, `MinFree`, `Available` and `Shortfall`.

- Estimates follow the [preflight table](../../openspec/stage-1-core/integrity.md#preflight):
  registry 256 B per row plus 512 B per media row (scan); descriptions 4 KiB, video registry 1 KiB per
  copy, WAL 2 KiB per candidate (split); `--catia-text` adds need `texts` (min(1 MiB, file size) per
  file that still needs a sidecar) on the archive device; all candidate bytes on another device; only the largest
  candidate for `--transfer copy` on a shared device; nothing for same-device `restore` renames
  except WAL, and all candidate bytes for `restore` across devices or with `copy`.
- A device passes when required + `--min-free` <= available (caller-available bytes). A zero-total
  device (network share) is `Known == false`: `available=unknown`, a warning, no failure.
- Sums saturate at `MaxInt64`, negative inputs count as zero, and roles on one device merge their
  estimates (`video_registry` twice becomes one entry).
- `ProbeDevices(fsops.Ops, []RolePath)` groups roots with `SameDevice` and reads free space once per
  device; missing roots use their nearest existing ancestor.
- `Session.Preflight(ctx, Candidates)` starts phase `preflight`, probes the roots in
  `Config` (plus `Config.Registry` for scan), logs one `preflight device` line per device and a
  `preflight passed` / `preflight failed: insufficient free space` summary to the console and run
  log, and returns `*InsufficientSpaceError` (`errors.Is(err, ErrInsufficientSpace)`).
- `Finish` maps it to `StatusInsufficientSpace`; `cli` maps that to exit 4. A refused real run
  writes no report and releases its locks, so it resumes after space is freed; a refused dry run
  writes `report.json` with status `insufficient_space`.
- `cli` passes `--min-free`, `--transfer` (split/restore), `--metadata` and `--registry` (scan) in
  `Config.Preflight` / `Config.Registry`; `Config.FS` injects device and free-space queries in tests.

Example on the development host (ext4 archive, tmpfs video archive, 500 videos, 40 GiB):

```text
level=INFO msg="preflight device" roles=archive path=/home/.../archive required=3.4MiB min_free=1.0GiB available=992.4GiB shortfall=0B needs="descriptions=2.0MiB video_registry=500.0KiB wal=1000.0KiB" ...
level=INFO msg="preflight device" roles=video_archive path=/dev/shm/video required=40.0GiB min_free=1.0GiB available=62.0GiB shortfall=0B needs="video_registry=500.0KiB videos=40.0GiB" ...
level=INFO msg="preflight passed" op=split devices=2
```

## Limits

- Windows behavior is implemented, cross-compiled and vetted only; test gates are Linux only and
  host checks are in the deferred [Windows verification scenario](../../guide/windows-verification.md).
- Preflight is called by `scan` (before traversal, estimated from the registry it replaces, with a
  `--min-free` re-check at each scan checkpoint), `split` (after its scan) and `restore` (after its
  video-archive scan).
- `DurableCopy` needs the destination directory to exist and does not remove the source.
- Remote-host lock refusal is tested with injected host names, not a live network share.
- A `Start` refused with exit 5 (corrupt run state, an interrupted run it may not recover)
  releases the locks it took, so the rerun that fixes the cause needs no `--force-unlock`; exit 5
  after the run started (a lost lock) keeps them. The integrity specification states both.
- `FSResolver` writes a marker description (`rel_path: ...`); split writes the full video description
  through `archive.MarkdownDescription`; restore removes an owned description through `archive.RestoreResolver`.
  Crash injection in tests uses a hook (error or panic), not a killed process; the stage-1 review's
  declared run killed `split` and `restore` with SIGKILL at seeded random points and resumed them
  with changed options ([0021](../records/0021-restore-review-stage-1-integrity.md)).
- The 1e6 committed-set gate is an in-memory index, not a million-line WAL file.
