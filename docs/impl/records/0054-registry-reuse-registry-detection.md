# Registry detection reuse

## Task and scope

- Id / capability / checkpoint: `reuse-registry-detection` / `archive-registry` /
  `review-registry-and-metadata`
- State: accepted
- Source: plan task `reuse-registry-detection`; code revision `85dafb6`, clean tree at start.
- Plan counts at start: 13 open tasks (10 agent, 3 human); next eligible
  `reuse-registry-detection`; also eligible `record-source-location-in-metadata`,
  `research-cloud-target-apis`.
- Accepted task:

```markdown
#### reuse-registry-detection

Every scan and every split opens and parses each file again even when a registry of the same tree
was just written, so a rerun on a large archive spends its time re-reading unchanged content.

- Serves: `archive-registry` -- [Incremental update](../openspec/stage-1-core/registry.md#incremental-update)
- Agent status: CLEAR
- Dependencies: [Preserved archive registry](records/0053-registry-preserve-archive-registry.md).
- User-visible outcome: A scan or split after an earlier registry of the archive opens only new or
  changed files, and its registry is byte-identical to one written with `--redetect`.
- Scope boundary: The reuse rule (stamp match, detection settings, entry kind and link text, size,
  mtime second, modification before the base scan start), `is_large`/`is_catia` recomputation,
  `--redetect`, the `reused` statistic, `registry_base` in the checkpoint with restart on a changed
  base, and one metadata collection per split run. No content hashing for reuse, no change to
  detection rules.
- Data and artifact paths: `internal/archive/scan.go`, `internal/archive/scan_pipeline.go`,
  `internal/archive/resume.go`, `internal/archive/split_description.go`, `internal/state/`,
  `internal/cli/`, `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md`.
- Execution path: A counting detector injected into the scan pipeline over scan-scan, scan-add-split,
  a same-size change with its mtime set inside and before the base scan start second, a future
  mtime, a changed `--metadata` and version, `--large-threshold` change, and a resumed scan whose
  base was replaced between processes.
- Acceptance gates: A second scan of an unchanged tree opens no file for detection and writes only
  the stamp; after every fixture change the registry equals the `--redetect` registry byte for byte;
  a same-size change not earlier than the base scan start second, a future mtime and a changed
  detection setting are detected; a changed base restarts a resumed scan; split opens no file for
  metadata its scan registered; `make ci` passes.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-registry-and-metadata`.
```

- Amendments (specification, made in this task, within its reuse-rule scope):
  1. [Incremental update](../../openspec/stage-1-core/registry.md#incremental-update): a base row is
     reused for a present entry only when its `location` is `archive`. Reason: a file found at the
     path of a moved file would otherwise take the preserved row, which may have been reconstructed
     from the run history (source 4) and is then not what detection writes. The cost is one
     detection of a file that came back without `restore --registry-update`.

## Implementation

- `internal/report`: `RegistryReader` (`NewRegistryReader`, `Next`) streams a file registry row by
  row; `ReadRegistry` is built on it and keeps its errors and its non-nil result for a header-only
  file.
- `internal/state`: `ScanStats.Reused` and `ScanSummary.Reused` (`reused` in the checkpoint and the
  report); `Checkpoint.RegistryBase` (`registry_base`, `FileID{size, sha256}`), `SameFileID`.
- `internal/archive`:
  - `registry_base.go`: `openRegistryBase` opens the registry at the target path once per archive
    scan, hashes it in one pass and checks the stamp. `stamped` is the base (path, size, SHA-256);
    `reusable` adds equal detection settings and no `--redetect`. The file stays open and is read
    through independent section readers: `rowsFor` collects only the moved rel_paths for preserved
    rows, `reader` streams rows for present entries. It is closed before placement, because Windows
    cannot replace an open file.
  - `scan_reuse.go`: `baseStream` follows the reusable base in walk order next to the walker, so
    memory does not grow with the registry. It checks the rule for each file or symlink the walker
    reports: location `archive`, same kind (a symlink row is `file_type` `symlink` with no MIME),
    equal size for a file, the same RFC 3339 `mtime`, and mtime truncated to the second before
    the stamp's `scan_started_at`. A base row out of walk order or unparseable ends reuse for the
    rest of the scan with a warning. `baseRow` recomputes `is_large` and `is_catia`; an
    AppleDouble row stays non-CATIA. Preserved rows use `baseRow` too.
  - `scan_pipeline.go`: a reused regular file is never queued for detection (no open, no ISO or
    ffprobe read). A symlink is always queued because its link text must be read; it counts as
    reused when the text equals the row's. Candidates of reused rows use the row's
    classification. `ScanConfig.detectFile` is the test seam for the counting detector.
  - `scan.go`: `ScanConfig.Redetect`. A fresh scan records `registry_base` and resets the cursor,
    offsets and statistics in the checkpoint at once. A checkpoint written before the first sync
    can then never pair the new base with an earlier scan's offsets. A resumed scan whose base
    identity differs from the checkpoint (deleted, edited, or replaced by another stamped registry)
    warns and scans from the start; `started_at` of the first process is kept.
  - `scan_preserved.go`: source 1 needs a reusable base, so `--redetect` detects the mirror copies
    too; source 3 (any readable base) is unchanged.
  - `split.go`, `split_report.go`, `previews_plan.go`: split reads its scan's registry once after
    the scan (`SplitConfig.scanRows`). Preview planning, both payload registries and the
    description writer use those rows. Before, each loaded the file separately, and the
    description writer kept the rows it had read before the scan (for example during recovery), so
    a file added since had a description without `file_mime` and `video:`. The new
    `MarkdownDescription.useRows` replaces them.
- `internal/cli`: `--redetect` for `scan` and `split` (`ARXGO_REDETECT`, `.env.example`).
  `ScanSettings.Redetect` is `omitempty`, so options of incomplete runs of earlier builds keep their
  definition and still resume. It is a defining option: a run started with `--redetect` resumes only
  with it.
- Decisions:
  - Streaming the base in walk order instead of indexing it by path: the base of an operator
    archive has millions of rows, and the walk visits them in the same order.
  - `registry_base` is recorded only for a stamped base, the spec's definition. A resumed scan
    whose unstamped registry changed keeps going: none of its present rows came from that file.
  - `--redetect` also bypasses source 1 of preserved rows, because the spec makes source 1 depend on
    a reusable base.
  - Split still probes a video without usable media values for preview planning (`--metadata file`
    on a non-ISO container). The row carries no metadata there, so this is not a second collection.
- Compatibility: the first scan after upgrading detects every file (no stamp yet, or the stamp's
  version differs). Checkpoints and reports gain `reused` and `registry_base`; readers of earlier
  checkpoints treat them as zero and absent.
- Current-state page: [archive registry](../current/archive-registry.md#incremental-update); also
  [current state](../current.md), both manuals and `README.md`.

## Acceptance evidence

Linux amd64, Go 1.27.1, system ffmpeg/ffprobe present. Fixtures are generated in `t.TempDir()`.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Second scan of an unchanged tree opens no file and writes only the stamp | `go test ./internal/archive -run TestScanReusesUnchangedRows` (edge-case tree plus CATIA and AppleDouble names: 0 opened, `reused` = files + symlinks, registry byte-identical with the same mtime and `unchanged`, stamp renewed; `--redetect` byte-identical) | pass |
| Registry equals the `--redetect` registry after every fixture change | `TestScanReuseDetectsChanges`, `TestScanReuseFollowsDetectionSettings`, `TestSplitReusesScanRows`, `TestScanResumeRestartsOnChangedBase` (each change followed by a `--redetect` scan compared byte for byte) | pass; limit below |
| Same-size change not earlier than the base scan start second is detected | `TestScanReuseDetectsChanges`: text becomes binary with mtime base start + 500 ms, only that file opened, new values written; a same-size change with a new mtime before the start is detected by the mtime comparison | pass |
| Future mtime detected | same test: detected on two successive scans | pass |
| Changed detection setting detected | `TestScanReuseFollowsDetectionSettings`: `--metadata media` and back, `--video-extensions`, version: every file opened, rescan with the same setting opens none; `--large-threshold` opens none and recomputes `is_large` | pass |
| Changed base restarts a resumed scan | `TestScanResumeRestartsOnChangedBase`: interrupted after 8 entries with `registry_base` = stamp; unchanged base resumes; deleted, edited and another stamped registry restart (the stamped replacement opens only the row it lacks); final registry equals the uninterrupted and `--redetect` registries | pass; restart check verified by mutation (disabled comparison fails 3 subtests) |
| Split opens no file for metadata its scan registered | `TestSplitReusesScanRows`: scan, add a video, split opens only the addition; both descriptions have `file_mime` and `video:` from rows, `arxgo-videos.csv` has media columns; regression for the pre-scan description cache (fails when `useRows` is disabled) | pass |
| Real binary | `bin`-equivalent `go build` of `cmd/arxgo` traced with `strace -f -e openat` on 20000 random files and 3 generated clips (script kept outside the repository): scan 1 opens 20000 `.txt`; scan 2 opens 0, `reused` 20003, `unchanged`; add a clip, `split --sample start --image start` opens only the new clip (2 opens: detection and ISO metadata), descriptions carry `file_mime`/`video:`; the scan after split opens 0 and leaves the registry byte-identical; `--redetect` opens 20000 and writes the same bytes | pass; wall time is dominated by the warm page cache (about 1.2 s either way), so no speedup is claimed from this run |
| `make ci` passes | `make ci` | pass on Linux (fmt, vet incl. `GOOS=windows`, tests, `build-all`, `lint-spec-plan`, `lint-doc-links`); Windows cross-compiled and vetted only |

Limit (valid negative, specified): a same-size change that keeps the old modification time is
reused. `TestScanReuseDetectsChanges` shows the registry then differs from the `--redetect`
registry, and that `--redetect` corrects it.

## Audit handoff

- `AUD-reuse-registry-detection-1`: nonblocking. A file whose permissions change so that it can no
  longer be read, with its size and mtime unchanged (chmod changes only ctime), keeps its reused
  row. A `--redetect` scan skips it with exit 6. The rule in the specification does not check
  readability, and a portable check would need an open. Owner: `review-registry-and-metadata`
  (reuse rule). Disposition: routed.
- `AUD-reuse-registry-detection-2`: nonblocking. `restore --registry-update` sets `location`
  `archive` on a row that a scan reconstructed from the run history (source 4, mirror unreadable).
  The next scan can then reuse that row for the restored file although detection would write other
  values. This needs an unreadable mirror and no base, and the scan logged
  `registry-row-reconstructed`. Owner: `review-registry-and-metadata` (stamp lifecycle).
  Disposition: routed.
- `AUD-reuse-registry-detection-3`: nonblocking. The `media metadata unavailable` warning is
  logged only when a file is detected. A reused row with a `media_error` stays silent on later
  scans. The registry still carries the error. Owner: `review-registry-and-metadata`.
  Disposition: routed.

## Close or resume

All gates pass. Task removed from the plan; its reference in `review-registry-and-metadata` now
links this record. `archive-registry` is `shipped`: this was its last open task.
