# Implement Scan Operation and CSV Registry

## Task and scope

- Id / capability / checkpoint: `implement-scan-operation-and-csv-registry` / `archive-registry` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; Windows is cross-compiled and vetted only, see audit handoff)
- Source: plan task `implement-scan-operation-and-csv-registry`, operator request on 2026-09-13
  ("implement, run, fix, improve implementation and then update documentation and plan.md").
  Branch `ag-01-stage-1` at `adf7aec`, clean tree.
- Plan counts at start: 25 open (22 agent, 3 human); next agent task
  `implement-scan-operation-and-csv-registry`; also eligible `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-scan-operation-and-csv-registry

Wire the walker, detection and checkpoints into the resumable `scan` operation and CSV registry.

- Serves: `archive-registry` -- [Registry writing](../openspec/stage-1-core/registry.md#registry-writing)
- Agent status: CLEAR
- Dependencies: [Directory walker](records/0010-registry-implement-directory-walker.md);
  [File type detection](records/0011-registry-implement-file-type-detection.md);
  [Run lock and checkpoint](records/0006-safety-implement-run-lock-and-checkpoint.md).
- User-visible outcome: `arxgo scan --archive PATH` writes `arxgo-registry.csv` with the specified
  columns and statistics, resumes after interruption, and exits 6 when entries were skipped.
- Scope boundary: CSV writer with part file, offset checkpoints and final rename; `file`-mode
  metadata JSON; scan statistics; candidate list output for split/restore consumers. `media`-mode
  fields are added by `media-metadata` tasks.
- Data and artifact paths: `internal/report/csv.go`, `internal/archive/scan.go`, `internal/cli/`.
- Execution path: Ordered pipeline walker -> bounded detection pool -> re-order buffer -> writer;
  checkpoint cursor and offset written together.
- Acceptance gates: Golden CSV for the full [edge-case list](../openspec/stage-1-core/registry.md#edge-cases);
  interrupted-then-resumed registry is byte-identical to an uninterrupted one; an existing
  registry is replaced only on completion; scan writes nothing else inside the archive.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Specification changes made while implementing it:
  - [registry writing](../../openspec/stage-1-core/registry.md#registry-writing): detection pool
    with ordered writer; no row for directories, special and unreadable entries; `--dry-run`
    writes no registry; video rows go to `candidates.jsonl`; outputs are fsynced before the
    checkpoint that names their offsets; resume truncates both files, restarts from scratch when a
    file is missing or short, and does not repeat a completed scan; the cursor never moves
    backwards; scan preflight runs before traversal with an estimate from the replaced registry,
    and each checkpoint re-checks `--min-free` (exit 4, resumable). Resolves
    `AUD-implement-disk-space-preflight-3`.
  - [statistics](../../openspec/stage-1-core/registry.md#statistics): report `scan` section, what
    `files`/`bytes` count, skipped entries from an earlier process still give exit 6.
  - [contracts](../../openspec/stage-1-core/contracts.md#file-registry-csv): symlink row fields,
    `mtime`/`mode` formats, no HTML escaping, new [candidate list](../../openspec/stage-1-core/contracts.md#candidate-list),
    checkpoint `candidates_offset` and `scan`, report `scan`. All additive; format versions stay 1.
  - [integrity](../../openspec/stage-1-core/integrity.md#preflight): scan preflight placement and
    estimate, checkpoint cadence counts walked entries, checkpoint contents.
  - [CLI](../../openspec/stage-1-core/cli.md#scan-flags): `--video-extensions` moved from split
    flags to scan flags (used by `scan` and `split`). Resolves
    `AUD-implement-file-type-detection-1`.
  - [architecture](../../openspec/architecture.md#repository-layout): file lists and scan pipeline
    note.

## Implementation

Packages `internal/archive`, `internal/report`, `internal/state`, `internal/cli`,
`internal/scanner`; no new dependencies.

- `report/csv.go`: `RegistryHeader`, `RegistryRow`, `Metadata`/`FileMetadata`, `RegistryWriter`
  (create with header, resume with truncate-to-offset, discard for dry runs, byte counting,
  `Sync` that skips fsync when nothing new was written).
- `state/scanstats.go`: `ScanStats` (checkpointed, cloned per checkpoint), `TopMIMEs`,
  `ScanSummary`; `Checkpoint.CandidatesOffset` and `Checkpoint.Scan`; `Report.Scan`.
- `archive/scan.go`: `ScanConfig`, `ScanBody`, `Scan` (completed-scan short cut, preflight,
  open or resume outputs, phase `scan`, pipeline, final sync, `fsops.Replace`, phase `summary`,
  summary log and report section), `scanEstimate`, periodic sync with free-space check.
- `archive/scan_pipeline.go`: walker -> `jobs` (16 workers: `scanner.Detect`, `os.Readlink`) and
  bounded `order` channel -> writer that waits for each entry's detection; cancellation by cause;
  per-entry accounting and candidate records; backwards-cursor guard.
- `archive/candidates.go`: `Candidate`, writer with durable offset, `ReadCandidates` for split.
- `archive/session_state.go`: `Checkpointed`, `AdvanceSync` (output sync before a due checkpoint),
  `RecordIssue` (no duplicate warning), `MarkPartial`, `SetScanSummary`, `ElapsedS`.
  `Session.Preflight` reports scan-style progress for the scan preflight phase.
- `cli`: `scan` runs `archive.ScanBody(scanConfig(...))`; `--video-extensions` moved into
  `ScanSettings`; CLI tests that expected exit 70 from `scan` now expect the real result.
- `scanner.readHead` reads exactly the file size when it is below the limit (one `read` instead
  of two per small file).

Run, fix and improve:

- Ran the binary on a generated archive with ffmpeg/NVENC clips (H.264 and AV1 MP4, HEVC MKV,
  MPEG-TS `.MTS`, M4A, WAV, PNG, JSON, a video named `.txt`, text named `.mp4`, a symlink, a
  space in a directory name): rows, flags and candidates as specified. With a mode-000 directory,
  a mode-000 file and a FIFO added: three warnings, no rows for them, report `partial` with
  `skipped={special:1, unreadable:2}`, exit 6. Dry run: exit 0, no registry or part file.
- Ran on a 1.2 GiB copy of `/usr/share` (170k entries): SIGINT at 2.3 s exited 130 and the rerun
  resumed at the checkpoint to a registry and candidate list byte-identical to an uninterrupted
  run; SIGKILL at 1.05, 0.73, 0.23 and 2.08 s: the rerun without `--force-unlock` exits 5 as
  specified, with it resumes to byte-identical outputs and leaves no part file.
- Fixed: the low-free-space error was logged twice, because the final sync after a failed
  checkpoint repeated the free-space check (`TestScanStopsWhenFreeSpaceFallsBelowMinFreeAndResumes`
  failed). The check now runs only before periodic checkpoints.
- Fixed (tests): the crash sweep asserted a checkpoint resume even when the crash came before the
  first checkpoint; a scan with no durable output correctly starts over. The golden fixture's
  extra video extension was wrong (`.mts` is built in); it now exercises `.DAT`.
- Improved: skipping fsync of outputs with no new bytes (candidate list rarely changes); fsyncs
  per scan of the copy fell from 1,393 to 1,051. Wall time is dominated by fsync latency at the
  default `--checkpoint-every 500` (3.0-3.6 s versus 1.0 s with checkpoints only at phase
  boundaries); the cadence is specified and left unchanged (audit note 3).
- Improved: default detection workers fixed at 16 after measuring 1, 2, 4, 8, 16, 32, 64 workers
  (3.3, 2.1, 1.3, 0.95, 1.0, 1.05, 1.06 s); the first draft used `2*GOMAXPROCS` capped at 32.
- Improved: one `read` per small file in detection (212,584 reads before, file-size cap after).
- Scratch mutations (each restored): registry resume without truncation (first survived; the
  crash test's torn tail was shorter than later rows, now 14 KiB); candidate resume without
  truncation; statistics not stored with the cursor; completed flag ignored; no `MarkPartial`; no
  free-space check; rename before the walk; candidates for every file; registry path not skipped;
  no backwards-cursor guard (first survived; `TestScanCursorNeverMovesBackwards` added); no
  restart on a short part file. Each now fails named tests.

Decisions and rejected alternatives:

- Unreadable files get no row (the contract excluded only special files): a row without a type
  would look classified, and split must never see an unreadable video as a candidate.
- The scan statistics live in the checkpoint rather than being recomputed, so a resumed run's
  summary and exit code equal an uninterrupted run's (`finishResume` compares them).
- A missing or short part file restarts the scan instead of exiting 5: scanning is read-only, so
  repeating it is always safe, and the operator needs no action.
- Scan preflight before traversal with an estimate plus a per-checkpoint `--min-free` guard,
  instead of a counting pre-walk (doubles traversal on slow shares) or a preflight after the scan
  (after the registry was already written).
- `--video-extensions` moved to scan flags rather than re-classifying in split, so the registry
  written by `scan` and by `split` agree.
- Rejected: writing directories as rows (contract), a reorder map keyed by sequence (the order
  channel bounds memory without one), per-row fsync.

Current state: [archive registry](../current/archive-registry.md#scan-operation-internalarchive).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Golden CSV for the full edge-case list | `TestScanGoldenRegistry` with `internal/archive/testdata/scan-registry.golden.csv`: empty files; spaces, comma, quote, newline, leading space, non-ASCII; `.txt` with MP4 bytes and `.mp4` with text; audio-only MP4; M4A; symlink (link target with `&<>`); unreadable file; `a/b` vs `a-b/x`; files at and below the threshold; reserved names at the root, `.arxgo-part` below it, a non-reserved `sub/arxgo-registry.csv`; hidden file; extra and built-in video extensions. Also candidates, report summary, issues | pass, Linux uid 1000 (skips as root) |
| Empty archive | `TestScanEmptyArchiveWritesHeaderOnly` | pass, Linux |
| Resume from a cursor in a deep directory | `TestScanInterruptedAfterEveryEntryResumesByteIdentical` (interrupt after each entry but the last of 39, including inside `deep/l1/l2/l3/l4`); `TestScanCursorNeverMovesBackwards` | pass, Linux |
| Interrupted-then-resumed registry is byte-identical | Interrupt sweep above; `TestScanCrashAfterEveryEntryResumesByteIdentical` (crash after each entry at cadences 1 and 3, 14 KiB torn tail appended to both files); `TestScanRestartsWhenPartFileIsShorterThanCheckpoint`; `TestScanCompletedBeforeReportIsNotRepeated`; `TestScanStopsWhenFreeSpaceFallsBelowMinFreeAndResumes`; registry, candidates and report summary compared | pass, Linux |
| Existing registry replaced only on completion | `finishResume` checks the original registry content after every interrupt and crash; `TestScanExplicitRegistryInsideArchiveIsNotRegistered` | pass, Linux |
| Scan writes nothing else inside the archive | `TestScanGoldenRegistry` manifest (mode, mtime, SHA-256) of the tree before and after, excluding `.arxgo/` and the registry; no part file left | pass, Linux |
| Exit 6 when entries were skipped | `TestScanGoldenRegistry` (partial); binary run with unreadable dir/file and FIFO (exit 6) | pass, Linux |
| Real runs | Binary on generated ffmpeg/NVENC archive and on a `/usr/share` copy: SIGINT and four SIGKILLs resumed byte-identical | pass (not committed gates) |
| Writer, statistics, CLI | `TestRegistryWriterQuotingAndMetadata`, `TestRegistryResumeTruncatesToOffset`, `TestDiscardRegistryCountsWithoutFile`, `TestFileMetadataOmitsZeroTime`, `TestScanStatsCountsFlagsMIMEAndSkipped`, `TestScanAcceptsVideoExtensions`, updated `TestRunExitCodes`, `TestPreflightOptionsReachTheSession`, `TestStaleLockExitCodesAndForceUnlock` | pass, Linux |
| Tests detect regressions | Eleven scratch mutations listed above | each fails named tests after two test fixes |
| Race | `CGO_ENABLED=1 go test -race -count=1 ./internal/archive ./internal/report ./internal/state ./internal/scanner ./internal/cli` | pass, Linux |
| Windows build compiles | `make build-all`, `make vet-windows` (in `make ci`) | pass (cross-compiled only) |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

The `internal/archive` tests now take about 21 s: the resume sweeps run about 200 sessions, and
their fsyncs serialize on the filesystem journal, so running them in parallel does not shorten
them.

## Audit handoff

- `AUD-implement-scan-operation-and-csv-registry-1`: nonblocking. Linux file names that are not
  valid UTF-8 are written to the CSV as raw bytes (the contract says UTF-8), and `link_target`
  JSON replaces invalid bytes with U+FFFD. Next check: decide an escaping rule or document the
  exception. Owner: `review-stage-1-integrity`.
- `AUD-implement-scan-operation-and-csv-registry-2`: nonblocking. `--metadata media` is accepted
  and writes file metadata only; the media fields and the ISO BMFF `is_video` refinement must be
  added in `scanRun.write`, and preflight `MediaRows` is still zero. Owner:
  `implement-iso-bmff-metadata`.
- `AUD-implement-scan-operation-and-csv-registry-3`: nonblocking. Each scan checkpoint costs about
  3 fsyncs (7 ms on NVMe, more on network shares); with the default `--checkpoint-every 500` the
  checkpoint overhead exceeds the scan itself on fast local trees. Next check: whether the
  specified default should be larger for scans, or data files use `fdatasync`. Owner:
  `review-stage-1-integrity`.
- `AUD-implement-scan-operation-and-csv-registry-4`: nonblocking. Split must call `Scan` with
  `Preflight=false` and the video archive in `SkipPaths`, skip the scan when `cp.Scan.Complete`,
  feed its preflight from `ScanStats` (`Video`, `LargestVideo`) and iterate `candidates.jsonl`
  with `ReadCandidates`; split's `Phase("preflight")` follows the scan phases. Owner:
  `implement-video-split-transactions`.
- `AUD-implement-scan-operation-and-csv-registry-5`: nonblocking, Windows only. Replacing the
  registry fails while another program holds it open without delete sharing (for example a
  spreadsheet); the run fails after the scan and a rerun only retries the rename. Also check
  `mode` values for Windows files. Next check: step W5 of the deferred
  [Windows verification scenario](../../guide/windows-verification.md#deferred-items). Owner:
  `review-stage-1-integrity` (disposition: deferred).
- Incoming `AUD-implement-directory-walker-2` (detection failures count as unreadable): resolved.
- Incoming `AUD-implement-directory-walker-3` (checkpoint counters, `--registry` in `SkipPaths`,
  cursor never backwards): resolved with `Checkpoint.Scan`, `SkipPaths` and the guard test.
- Incoming `AUD-implement-disk-space-preflight-3` (scan row estimate): resolved as above.
- Incoming `AUD-implement-file-type-detection-1` (`--video-extensions` split-only): resolved by
  making it a scan flag.

## Close or resume

All Linux gates pass. Windows host checks are not a gate. The task was removed from the plan;
`implement-iso-bmff-metadata` and `implement-video-split-transactions` link this record. The
`archive-registry` capability is marked `shipped`; its current page, the current index, the
project-foundation and crash-safety pages, README status and the records index were updated.
Plan counts after: 24 open (21 agent, 3 human); next agent task `implement-tool-discovery`, also
eligible `implement-iso-bmff-metadata` and `implement-video-split-transactions`.
