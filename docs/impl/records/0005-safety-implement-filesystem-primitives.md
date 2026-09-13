# Implement Filesystem Primitives

## Task and scope

- Id / capability / checkpoint: `implement-filesystem-primitives` / `crash-safety` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; the Windows CI run is still pending, see audit handoff)
- Source: plan task `implement-filesystem-primitives`, operator request on 2026-09-13 ("implement,
  run, fix, improve implementation and then update documentation and plan.md"). Branch
  `ag-01-stage-1` at `2042ac7`, clean working tree.
- Plan counts at start: 31 open (28 agent, 3 human); next agent task
  `implement-filesystem-primitives`; also eligible `implement-directory-walker`,
  `implement-file-type-detection`, `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-filesystem-primitives

Provide the platform-aware file operations every mutating task relies on.

- Serves: `crash-safety` -- [Integrity](../../openspec/stage-1-core/integrity.md)
- Agent status: CLEAR
- Dependencies: [CLI contract](0003-foundation-implement-cli-contract.md).
- User-visible outcome: Moves and copies are durable and verified on Linux and Windows, and the
  utility can tell whether two paths share a device and how much space is free.
- Scope boundary: `SameDevice`, `FreeSpace`, `AtomicWriteFile`, `DurableCopy` (part file, fsync,
  size or SHA-256 verification, mtime/mode preservation, rename, directory fsync), `Rename` with
  cross-device error classification, `IsCrossDevice(err)`. Injectable interfaces for tests. No WAL.
- Data and artifact paths: `internal/fsops/` with `device_unix.go`, `device_windows.go`,
  `space_unix.go`, `space_windows.go`, `transfer.go`, `atomic.go`; `golang.org/x/sys`.
- Execution path: `unix.Statfs`/`Stat_t.Dev` and `windows.GetDiskFreeSpaceEx`/
  `GetVolumePathName`+`GetVolumeInformation`; streaming copy with `io.CopyBuffer` and optional
  `sha256` tee; cross-device detection from `*os.LinkError` (`EXDEV`, `ERROR_NOT_SAME_DEVICE`).
- Acceptance gates: Copy preserves bytes, mtime and (Linux) mode; verification mismatch leaves no
  destination; part files are removed on error; atomic write never exposes partial content; same-
  device true for siblings and false across `/dev/shm` and the workspace when both exist (skip with
  reason otherwise); `FreeSpace` returns non-zero for the temp dir; Windows build compiles and its
  tests pass in CI.
- Documentation target: `docs/impl/current/crash-safety.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Spec clarifications made while implementing it:
  [integrity](../../openspec/stage-1-core/integrity.md#checkpoints) now names the checkpoint temp
  file `checkpoint.json.arxgo-part` (the reserved suffix used by every atomic write) instead of
  `checkpoint.json.tmp`; [split](../../openspec/stage-1-core/split-restore.md#split) preserves
  permission bits "when the destination filesystem supports them".

## Implementation

`internal/fsops` (dependency `golang.org/x/sys` v0.48.0, listed in the
[dependency table](../../openspec/spec.md#dependencies); `go.mod` and `go.sum` updated together):

- `ops.go`: `Ops` interface (`SameDevice`, `FreeSpace`, `Rename`, `Replace`, `DurableCopy`,
  `AtomicWriteFile`) and `System`, the real implementation; `PartSuffix`/`PartPath`; `Device`,
  `DeviceOf`, `SameDevice`; `Space` (`Total`, `Free`, `Available`) and `FreeSpace`. Paths that do
  not exist yet (a split video archive root that will be created) resolve to the nearest existing
  ancestor; `FreeSpace` on a file uses its directory.
- `device_unix.go`: `st_dev` from `unix.Stat`. `device_windows.go`: `GetVolumePathName`, then the
  volume serial from `GetVolumeInformation`; `Device.Volume` holds the mount point for logs.
- `space_unix.go`: `unix.Statfs` with block unit `f_frsize` on Linux (`sys_linux.go`) and
  `f_bsize` elsewhere (`sys_other_unix.go`). `space_windows.go`: `GetDiskFreeSpaceEx` with a
  trailing backslash (required for UNC roots); `Available` honors quotas.
- `rename.go`, `rename_unix.go`, `rename_windows.go`: `Rename` never replaces an existing target:
  `renameat2(RENAME_NOREPLACE)` on Linux, falling back to `Lstat` then `rename` when the
  filesystem returns `EINVAL`/`ENOSYS`; `MoveFileEx` without `MOVEFILE_REPLACE_EXISTING` (with
  `MOVEFILE_WRITE_THROUGH`) on Windows. `Replace` overwrites atomically. Both flush the new and old
  parent directories (Unix; `EINVAL`/`ENOTSUP` from a directory fsync are tolerated); Windows has no
  directory flush and relies on `MOVEFILE_WRITE_THROUGH`. Failures are `*os.LinkError`, so
  `errors.Is(err, fs.ErrExist)` and `IsCrossDevice(err)` (`EXDEV` / `ERROR_NOT_SAME_DEVICE`) work.
  A non-`LinkError` from `Rename`/`Replace` means the move happened but the flush failed.
  `NewCrossDeviceError` lets fakes simulate a cross-device layout. On Windows, `MoveFileEx` is
  retried up to 8 times (5 ms doubling, about 1.3 s) on `ERROR_ACCESS_DENIED`/
  `ERROR_SHARING_VIOLATION` (a reader, indexer or antivirus holding the file), and paths of 248+
  characters get the `\\?\` prefix.
- `transfer.go`, `copyloop.go`: `DurableCopy(ctx, src, dst, CopyOptions)` refuses an existing
  `dst` unless `Overwrite`; checks the source against `ExpectSize`/`ExpectModTime` (the WAL
  `begin` values) and returns `ErrSourceChanged`; replaces a stale part file; streams to
  `dst.arxgo-part` in 32 MiB chunks, checking `ctx` and calling `Progress` between chunks; fsyncs;
  re-stats the source (size or mtime change -> `ErrSourceChanged`); closes, sets mtime by path
  (Windows can rewrite the last-write time when a writing handle closes), reopens and verifies
  size, or re-reads and compares SHA-256 for `VerifyHash`; applies permission bits on non-Windows
  (EPERM/unsupported ignored, for CIFS/exFAT mounts); fsyncs again so mtime and mode are durable;
  then `Rename`/`Replace` into place. Any failure removes the part file and leaves `dst` as it was.
  `CopyResult` carries size, mtime and the hex digest for the WAL `verified` record.
- `atomic.go`: `AtomicWrite(path, perm, func(io.Writer) error)` (buffered streaming, for reports
  and registries) and `AtomicWriteFile`: `path.arxgo-part`, fsync, `Replace`, directory flush;
  failures remove the part file and keep the old content.

Run, fix and improve:

- `strace` of the test binary confirmed `renameat2(..., RENAME_NOREPLACE)` returning `EEXIST` and
  `EXDEV`, `fsync` of files and directories, and `utimensat`/`fchmod` on the part file.
- The trace also showed `copy_file_range` returning `EXDEV` between ext4 and tmpfs, with Go falling
  back to its own copy. A scratch benchmark (4 GiB, i9-14900K, NVMe ext4 and `/dev/shm`, removed
  after the run) showed no gain from a larger explicit buffer: size mode was about 4.4 GiB/s across
  devices and 1.5 GiB/s ext4->ext4 with fsync. The size path therefore keeps `io.CopyN`, which lets
  `*os.File` use `copy_file_range`/`sendfile` (and reflinks on filesystems that support them).
- Hash mode was CPU-bound: SHA-256 runs at 2.8 GB/s with SHA-NI here, and the first version
  (an `io.MultiWriter` tee) reached 936 MiB/s across devices. The source digest now runs on its own
  goroutine, fed from a ring of four 1 MiB buffers, while the data is written. That gives
  1173 MiB/s (+25%). The destination re-read uses a 1 MiB buffer by hiding `File.WriteTo`. The
  ext4->ext4 figure (about 770 MiB/s) is limited by writeback and fsync. A GPU does not help: the
  binary must stay pure Go with `CGO_ENABLED=0`, and SHA-256 is a serial stream hash.
- Review fixes: duplicate progress callbacks when the size is an exact chunk multiple (regression
  test `TestDurableCopyProgressAtExactChunkMultiple`); a Windows flake risk in the concurrent
  atomic-write test (Go opens files without `FILE_SHARE_DELETE`, so the reader now leaves gaps for
  the retry); chmod on filesystems without Unix modes no longer fails the copy; `transfer.go` split
  to keep files under about 300 lines.

Decisions and rejected alternatives:

- File names differ from the task's path list at real seams: `rename*.go` and `sys_*.go` carry the
  platform rename/flush code and the Linux-only `renameat2`/`f_frsize` details; `copyloop.go` holds
  the copy loops.
- `Rename` is no-replace because split never overwrites; restore `--overwrite` uses `Replace`.
- Test hooks (`chunk`, `beforeVerify`) are unexported `CopyOptions` fields, so they are per call
  and safe in parallel tests. Downstream tasks inject behavior through the `Ops` interface.
- `DurableCopy` does not create parent directories (split owns directory creation and its
  permission bits) and does not remove the source (the WAL step order owns that).
- Rejected: `fallocate` before copying, which would stop same-filesystem reflinks for
  `--transfer copy`; a fixed hashing buffer size tuned to this host; hashing the source in a
  second pass, which would double reads from a slow NAS source.

Current state: [crash safety](../current/crash-safety.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Copy preserves bytes, mtime, (Linux) mode | `TestDurableCopyPreservesBytesMtimeAndMode` (size, hash, read-only 0444 source; 4 chunks), `TestDurableCopyEmptyFile`, `TestDurableCopyAcrossDevices` (tmp ext4 -> `/dev/shm` tmpfs and back, both verify modes) | pass, Linux, real cross-device copy |
| Verification mismatch leaves no destination | `TestDurableCopyVerificationMismatchLeavesNoDestination` (flipped byte with hash, truncation with size, injected after the data is durable) | pass, Linux |
| Part files removed on error | `TestDurableCopyRemovesPartFileOnError` (cancel mid-copy in both modes, injected error, missing parent, directory source), `TestDurableCopyDetectsSourceChanges` (expected attrs, append during copy in both modes), `TestDurableCopyReplacesStalePartFile`, `TestDurableCopyNeverOverwritesUnlessAsked` | pass, Linux |
| Atomic write never exposes partial content | `TestAtomicWriteNeverExposesPartialContent` (concurrent reader, 60 replacements of 1 MiB files), `TestAtomicWriteFailureKeepsPreviousContent` (old content visible mid-write and after failure), `TestAtomicWriteReplacesStalePartFile`, `TestAtomicWriteFileCreatesAndReplaces` | pass, Linux |
| Same device true for siblings | `TestSameDeviceSiblings` (dirs, file, missing path, relative path), `TestDeviceOfMissingPathMatchesAncestor` | pass, Linux |
| Same device false across `/dev/shm` and workspace | `TestSameDeviceFalseAcrossShmAndWorkspace` (skips with reason when unavailable or on one device) | pass, Linux (ran, not skipped) |
| Rename classification | `TestRenameMovesWithoutReplacing`, `TestReplaceOverwrites`, `TestRenameMissingSource`, `TestRenameDeepPath`, `TestIsCrossDevice`, `TestRenameAcrossDevicesIsClassified` (real `EXDEV`) | pass, Linux; strace shows `RENAME_NOREPLACE` |
| `FreeSpace` non-zero for temp dir | `TestFreeSpaceTempDir`, `TestFreeSpaceFileAndMissingPathUseTheirVolume` | pass, Linux |
| Tests detect regressions | Scratch mutations: `os.WriteFile` instead of atomic write, replacing rename, ignored verification, no `Chtimes`, kept part file, no source-change check | each mutation failed at least one test; sources restored |
| Race and repeat | `CGO_ENABLED=1 go test -race -count=5 ./internal/fsops/` | pass, Linux |
| Windows build compiles | `GOOS=windows go vet ./...`; `GOOS=windows go test -c ./internal/fsops`; `make build-all` | pass (cross-compiled only) |
| Windows tests pass in CI | `.github/workflows/ci.yml` `windows` job (`go test ./...`) | not-run: changes are not pushed |
| `make ci` on Linux | `make ci` (Go 1.27.1 at `/usr/local/go/bin`) | pass |

## Audit handoff

- `AUD-implement-filesystem-primitives-1`: nonblocking. The Windows code paths (volume serial,
  `GetDiskFreeSpaceEx`, `MoveFileEx` no-replace and retry, `\\?\` prefix, mtime set after close)
  are only cross-compiled; the Windows CI job has not run. Real cross-volume
  `ERROR_NOT_SAME_DEVICE` is untested because runners have one volume. Next check: the Windows CI
  result after push, and a second drive or `subst`/UNC root in the stage-1 proof. Owner:
  `review-stage-1-integrity`.
- `AUD-implement-filesystem-primitives-2`: nonblocking. Tests do not exercise the Linux fallback
  for filesystems without `RENAME_NOREPLACE` (some NFS, CIFS and FUSE mounts) or the tolerated
  directory-fsync and chmod errors; there it is check-then-rename, with a race window only against
  writers outside arxgo. Next check: run the stage-1 proof once against a CIFS or NFS mount if one
  is available. Owner: `review-stage-1-integrity`.
- `AUD-implement-filesystem-primitives-3`: nonblocking. `ExpectModTime` compares mtime exactly,
  but the [WAL record example](../../openspec/stage-1-core/contracts.md#wal-record) shows
  second-precision `mtime`. If `begin` stores whole seconds, every copy of a file with a
  sub-second mtime returns `ErrSourceChanged`. Next check: the WAL/split format stores RFC 3339
  with nanoseconds (or the caller truncates both sides consistently); cover it with a test. Owner:
  `implement-video-split-transactions`.
- `AUD-implement-filesystem-primitives-4`: nonblocking. `DurableCopy` needs an existing parent
  directory and does not flush newly created parents, and a non-`LinkError` from
  `Rename`/`DurableCopy` means "placed, flush failed". Next check: split creates parents with a
  durable mkdir (fsync of each new directory's parent) and treats a flush error after placement as
  the `placed` state rather than a failed transfer. Owner: `implement-video-split-transactions`.
- `AUD-implement-cli-contract-1` (Windows pending): not changed by this task; still owned by
  `review-stage-1-integrity`.

## Close or resume

All Linux gates pass. The Windows CI gate stays pending and is owned by `review-stage-1-integrity`
(`AUD-implement-filesystem-primitives-1`). The task was removed from the plan, and its dependents
(`implement-run-lock-and-checkpoint`, `implement-disk-space-preflight`) now link this record. The
crash-safety current page, the current index and the records index were updated. Capability
`crash-safety` stays `planned` (three tasks remain). Plan counts after: 30 open (27 agent, 3 human);
next agent task `implement-run-lock-and-checkpoint`.
