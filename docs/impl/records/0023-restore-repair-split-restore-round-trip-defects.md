# Split/restore round-trip repairs

## Task and scope

- Id / capability / checkpoint: `repair-split-restore-round-trip-defects` / `video-restore` /
  `review-stage-1-integrity`
- State: accepted.
- Source: blocking repair task added to the plan by the
  [stage-1 integrity review](0021-restore-review-stage-1-integrity.md) (findings 2-9); revision
  `7bf111f`, worktree clean at the start of the review.
- Plan counts at start: 20 open tasks (17 agent, 3 human); eligible in parallel with
  `recover-incomplete-run-before-replacing-it`.
- Accepted task (original block):

```markdown
#### repair-split-restore-round-trip-defects

The stage-1 review found round-trip defects: restore misses videos that split selected through
`--video-extensions`, trusts registry paths that leave the archive, the next split marks restored
videos `moved` again, an unreadable video-archive directory or a non-file at a stub path makes
every rerun fail, and a crash during a destination conflict can remove the source.

- Serves: `video-restore` -- [Restore](../openspec/stage-1-core/split-restore.md#restore)
- Agent status: CLEAR
- Dependencies: [Stubs and video registry](records/0019-split-implement-stubs-and-video-registry.md);
  [Video restore](records/0020-restore-implement-video-restore.md).
- User-visible outcome: A split/restore round trip returns every moved video, never writes outside
  the archive, keeps registry statuses true across repeated split and restore runs, and a rerun
  can always finish.
- Scope boundary: Restore candidates include registered `moved`/`restored` video paths; registry
  paths used by restore must be local and not reserved; the video registry replays split and
  restore transactions of every run in start order; empty-directory cleanup covers only
  directories of restored videos and is best effort; stub paths holding a non-regular or unreadable
  file are foreign and headers are read with a bound; a conflict abort is logged before its part
  file is removed; stub hints are cached; restore records the stub path it removed; clearer skip
  reason for file names that are not valid UTF-8. Spec amendments for restore candidates, cleanup
  and registry regeneration. No new flags.
- Data and artifact paths: `internal/archive/`, `internal/report/`, `internal/scanner/`,
  `docs/openspec/stage-1-core/split-restore.md`, `docs/openspec/stage-1-core/contracts.md`.
- Execution path: Package tests at the split, restore and report seams on generated fixtures; each
  regression fails before its fix.
- Acceptance gates: `--video-extensions` round trip restores the video; a registry `rel_path` with
  `..` restores inside the archive; split after restore keeps `restored`; restore with an
  unreadable video-archive directory completes and keeps unrelated empty directories; a directory
  at `<video>.md` gets the fallback stub; crash between conflict abort and part removal keeps the
  source; `make ci` passes.
- Documentation target: `docs/impl/current/video-restore.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments:
  1. Before implementation, the conflict wording was corrected: with `--verify size` a same-size
     foreign destination is adopted by the specified rule anyway, so the harm of the old order is
     a roll-forward (a wrong stub, and with `--verify hash` a recovery that fails on every rerun),
     not a silent source removal. Revised text: "... and a crash during a destination conflict can
     roll the foreign destination forward." and gate "a crash after the conflict abort keeps the
     source, writes no stub and the rerun finishes".
  2. The review's declared run (record 0021) found two more defects inside this scope, added with
     the operator's standing request to run, fix and improve: the pure-Go ISO BMFF parser let
     QuickTime's data-handler `hdlr` in `minf` overwrite the track kind, so a real `.mov` had no
     streams and `split --metadata media` left it in the archive as not video; and the first
     cleanup rewrite considered only this run's commits, leaving directories of videos restored by
     a replaced interrupted restore. Added scope: "track kind from the `hdlr` directly in `mdia`
     (spec amendment in metadata)"; "cleanup covers directories of videos restored by any run".
     Added gates: "a QuickTime movie with a data handler is a moved video in media mode";
     "directories of a replaced interrupted restore are cleaned".

## Implementation

Each defect was reproduced with a throwaway test before the fix; the committed regressions fail on
`7bf111f` (checked in a scratch worktree) and pass now.

1. **Restore candidates** (`restore.go`, `scan.go`, `scan_pipeline.go`): the video registry is loaded
   before the scan; `ScanConfig.Include` makes a file a candidate when its path is the
   `video_rel_path` of a `moved` or `restored` row. Before: a `.bik` split with
   `--video-extensions bik` was never restored and restore exited 0.
2. **Registry paths** (`scanner/relpath.go`, `restore_exec.go`, `restore_recovery.go`):
   `scanner.LocalRelPath` rejects empty, `.`/`..` segments, absolute/volume paths, OS separators
   other than `/` and reserved paths (`scanner.Reserved`, now shared with the walker). Rows failing
   it are ignored with a warning; their video restores as unregistered. Before: a `rel_path` of
   `../escaped.mp4` wrote the video outside the archive.
3. **Registry replay** (`split_report.go`, `restore_report.go`): `replayVideoRows` starts from the
   existing rows and applies each run's WAL in start order (`options.json` `created_at`, then id):
   split commits `moved`, destination aborts `conflict`, other aborts `skipped` unless moved,
   restore commits `restored` with the restoring run id. Split and restore reports both use it.
   Before: every split report replayed old split commits over newer `restored` rows and counted
   restore commits as `moved`. Test clocks now start one minute apart per configuration so runs
   have a real start order, as in production.
4. **Cleanup** (`restore_dirs.go`): `pruneRestoredDirs` collects the video-archive directories of
   restore transactions committed by any run, removes the empty ones deepest first, logs and keeps
   anything it cannot read or remove. Before: a walk over the whole video archive failed the run
   (exit 1, on every rerun) on one unreadable directory and removed unrelated empty directories.
5. **Stub path classification** (`report/frontmatter.go`): `InspectStub` uses `Lstat`; a
   non-regular entry, a file that cannot be opened or a header not closed within 64 KiB is foreign.
   Before: a directory named `<video>.md` failed split after the video was placed, on every rerun
   and in recovery.
6. **Conflict abort order** (`split.go`, `restore_exec.go`): `abortConflict` appends the durable
   `aborted` record before removing the part file (crash point `fs:delete_part`); a conflict skip
   also removes a stale `<dst>.arxgo-part` left by such a crash.
7. **Stub hints and path** (`restore_recovery.go`, `state/recovery.go`, `restore_exec.go`): stub
   hints load the registry once per resolver instead of twice per transaction; the owned stub is
   chosen before it is removed and recorded in `stub_removed` (empty when there is none). Before:
   the WAL recorded `<dst>.md` for a removed `.arxgo.md` stub.
8. **Invalid UTF-8 names** (`split.go`): a missing source whose relative path holds U+FFFD is
   skipped with a reason naming the limit; contracts document the exception.
9. **QuickTime handler** (`media/isobmff.go`, `test/fixtures/testmp4`): only `hdlr` whose parent is
   `mdia` sets the track kind. `testmp4.Options.QuickTime` builds the `mhlr`/`dhlr` pair.

Spec amendments: [split and restore](../../openspec/stage-1-core/split-restore.md) (stub and
destination rules, restore steps 2, 3, 6, 7), [contracts](../../openspec/stage-1-core/contracts.md)
(UTF-8 exception, WAL `mtime` precision and `stub` field, video registry replay),
[metadata](../../openspec/stage-1-core/metadata.md) (track kind, fixture list). No dependency, flag
or file-format column changed. Current state: [video restore](../current/video-restore.md),
[video split](../current/video-split.md), [media metadata](../current/media-metadata.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| `--video-extensions` round trip | `TestRestoreReturnsVideoSelectedByExtension` | pass, Linux; fails on `7bf111f` (not restored) |
| Registry `rel_path` with `..` or a reserved name restores inside the archive | `TestRestoreIgnoresRegistryPathsOutsideArchive/{parent,reserved}`; `TestLocalRelPath` | pass, Linux; fails on `7bf111f` (wrote outside) |
| Split after restore keeps `restored`; history after a retired registry | `TestSplitAfterRestoreKeepsRestoredStatus`, `TestSplitAfterRetiredRegistryKeepsHistory` | pass, Linux; both fail on `7bf111f` (`moved`) |
| Unreadable video-archive directory: restore completes (exit 6 for the skipped entry), unrelated dirs kept, rerun finishes | `TestRestoreCleanupIsLimitedToRestoredDirectories` | pass, Linux as non-root; fails on `7bf111f` (failed) |
| Directories of a replaced interrupted restore cleaned | `TestRestoreCleansDirectoriesOfReplacedInterruptedRestore`; declared run `dirs left: 0` | pass, Linux |
| Directory at `<video>.md` gets the fallback stub | `TestSplitDirectoryAtStubPathUsesFallback`, `TestInspectStubTreatsNonFilesAsForeign` | pass, Linux; fails on `7bf111f` (`is a directory`) |
| Crash after the conflict abort keeps the source, no stub, rerun finishes | `TestConflictAbortIsDurableBeforePartRemoval` | pass, Linux; fails on `7bf111f` (part removed first) |
| Owned fallback stub removed and recorded | `TestRestoreRemovesOwnedFallbackStubAndRecordsIt` | pass, Linux; fails on `7bf111f` (records `<dst>.md`) |
| Invalid UTF-8 skip reason | `TestSplitReportsFileNamesThatAreNotUTF8` | pass, Linux ext4; fails on `7bf111f` |
| QuickTime movie is a moved video in media mode | `TestISOQuickTimeDataHandlerKeepsTrackKind`, `TestSplitMediaModeStubHasDuration/camera.mov`, `TestISOLiveFFmpeg` (`live.mov`) with `ARXGO_TEST_REQUIRE_TOOLS=1` | pass, Linux, ffmpeg 6.1.1; unit and live `.mov` fail before the fix |
| Stub hints cached | `perf.sh`: split then restore of N copies of a 482 KiB `.mov` in 50 directories, same ext4 device | 1000 videos 14.5 s -> 10.2 s; 4000 videos 97.1 s -> 38.0 s (linear now, about 10 ms per video in specified fsyncs) |
| Declared run | `e2e.sh` (record 0021) with this build, seeds 4242 and 91 on tmpfs, 777 on ext4 | round trip equal; with `7bf111f` the same scenario exits 0 everywhere while `top.mov` is never moved and `custom.bik` stays in the video archive (manifests differ) |
| `make ci`; race | `make ci`; `CGO_ENABLED=1 go test -race ...` (see record 0021) | pass, Linux; Windows cross-compiled and vetted only |

## Audit handoff

none identified beyond the checkpoint's notes (Windows path validation and cleanup are covered by
its deferred Windows note; a video-registry file that no longer parses is its nonblocking note).

## Close or resume

All gates pass, including the amended ones. The task is removed from the plan; current pages for
video restore, video split, media metadata and archive registry describe the behavior.
Capability `video-restore` stays `planned` until the stage-1 proof.
