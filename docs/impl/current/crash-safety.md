# Crash Safety

Accepted work: [0005 Filesystem primitives](../records/0005-safety-implement-filesystem-primitives.md).
Specification: [integrity](../../openspec/stage-1-core/integrity.md). Remaining work (run lock and
checkpoints, write-ahead log and recovery, disk-space preflight) is in the
[plan](../plan.md#crash-safety----crash-safety).

## Filesystem primitives (`internal/fsops`)

No operation uses these yet; they are the building blocks for the lock, WAL, preflight, split and
restore tasks.

- `Ops` interface with the real `System` implementation. Tests of later packages substitute fakes,
  for example a device check that reports two roots on different devices, or a `Rename` returning
  `NewCrossDeviceError`.
- `SameDevice(a, b)` / `DeviceOf(path)`: `st_dev` on Unix; volume serial via `GetVolumePathName` +
  `GetVolumeInformation` on Windows. Missing paths resolve to the nearest existing ancestor.
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
  file on every error, honors cancellation and reports progress every 32 MiB. `CopyResult` returns
  size, mtime and the hex digest. The size mode keeps the kernel copy fast paths; the hash mode
  hashes on a separate goroutine.
- `AtomicWrite(path, perm, fn)` / `AtomicWriteFile(path, data, perm)`: buffered write to
  `path.arxgo-part`, fsync, atomic replace, directory flush. Readers see the old or the new content
  only.

Measured on the development host (4 GiB file, NVMe ext4 and tmpfs): size-verified copy about
4.4 GiB/s across devices; hash-verified about 1.15 GiB/s across devices, bounded by SHA-256.

## Limits

- Windows behavior is cross-compiled only; the CI Windows test job has not run it yet.
- `DurableCopy` needs the destination directory to exist and does not remove the source.
