# Stage-2 previews review

## Task and scope

- Id / capability / checkpoint: `review-stage-2-previews` / `media-previews` / none (this is the
  bounded checkpoint)
- State: accepted.
- Source: plan task; revision `2bf001b`, worktree clean at start. Host: Linux amd64, Go 1.27.1,
  ffmpeg/ffprobe 6.1.1 on `PATH` and the pinned bundle tools in `bin/`, NVIDIA RTX 4060 Ti (not
  used: previews use the specified software encoders).
- Plan counts at start: 10 open tasks (7 agent, 3 human); next eligible `review-stage-2-previews`.
- Accepted task (original block):

```markdown
#### review-stage-2-previews

Review preview integration before cloud publishing builds on the stage-2 WAL and registry.

- Serves: `media-previews` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: [Preview integration](records/0030-preview-integrate-previews-into-split-and-restore.md); [Release bundle](records/0032-preview-implement-release-bundle-with-ffmpeg.md).
- User-visible outcome: Stage 2 is coherent and stage 3 can rely on its contracts.
- Scope boundary: Preview WAL/recovery, naming/exclusion invariants, restore cleanup, bundle
  licensing evidence, Windows code paths by review and cross-compilation (runtime checks deferred to
  the Windows scenario). Add missing behavior tests; no speculative refactor.
- Data and artifact paths: Stage-2 records, `internal/media/`, `internal/archive/`.
- Execution path: Invariant-to-evidence table, targeted tests, routed notes.
- Acceptance gates: Notes dispositioned; verdicts recorded; blockers repaired first; `make ci`
  passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.
```

- Amendments:
  1. Dependencies gained the blocking repair task `repair-stage-2-preview-defects`
     ([0034](0034-preview-repair-stage-2-preview-defects.md)), as the acceptance gate requires
     ("blockers repaired first").
  2. Refactor scope, authorized by the operator's request for this task: "Analyze the codebase,
     refactor to improve readability, and balance module and submodule sizes. Keep only tests that
     ensure integrity and correctness, with adequate organic test coverage. Remove obsolete
     development-time artifacts, unnecessary backward-compatibility code, or adapters. Follow
     coding best practices and clean code rules. Implement, run, fix, improve implementation. And
     then update documentation, manuals, README and plan.md". This replaces "no speculative
     refactor"; the refactor stays behavior-preserving outside the repairs of 0034.
  3. Declared run on real media, requested by the operator during the review: scan a
     representative media archive (path omitted) and check that every video format it holds is
     supported, after desktop players reported missing `meta/x-gst-fourcc-fdsc` and
     `meta/x-gst-fourcc-gpmd` decoders for some MP4 files. The archive was only read; runs used a
     copy in the session scratchpad.

## Implementation

### Method

Read records 0026-0032, the stage-2 specification, the preview sections of the stage-1 contracts,
integrity and split-restore pages, `internal/media` (runner, planner, samples, frames),
`internal/archive` (preview index, planning, worker, restore cleanup, split and restore wiring),
`internal/state` WAL preview events, `internal/report` stub and summary code, and their tests.
Traced a video through split with previews: index replay -> part cleanup -> scan exclusion ->
planning and estimate -> preflight -> commit -> `preview_begin` -> ffmpeg part -> validation ->
publication -> `preview_done` -> stub section -> registry replay; then a crash or cancellation at
each preview event, a rerun, restore with `--previews keep` and `delete`, a second split, and an
archive root rename. Suspected defects were confirmed with throwaway tests (`TestRepro*`, deleted)
before any change. A generation matrix ran every built-in video extension through planning, sample
and frame generation. `deadcode` and `staticcheck` (built in the scratchpad, not added to the
repository) listed unreachable code and vet-level findings.

### Findings

Blocking, repaired in [0034](0034-preview-repair-stage-2-preview-defects.md), each reproduced on
`2bf001b`:

1. MPEG-PS (`.mpg`, `.vob`), Ogg, MXF and DV sources never got a sample: ffmpeg accepts the
   extension, so the muxer fallback did not trigger, but the container rejects H.264/AAC. Every
   rerun failed again (exit 6).
2. An unknown extension (for example `--video-extensions bik`) fell back to `.mp4` at run time, but
   the next run planned the original name, found the `.mp4` occupied and failed on every rerun; its
   part file was not named by the WAL.
3. A crash after a restore commit and before `preview_delete` left the previews forever: the
   resumed run skipped the committed video.
4. Split, restore with kept previews, split: all previews were already recorded, so the new stub
   was never updated and kept the dev-time placeholder text.
5. Renaming or remounting the archive root made every later split and restore exit 5 (preview WAL
   paths "outside archive"); without previews the registry's `stub_rel_path` became
   `../archive/...`.
6. Ctrl+C during a preview logged `preview_failed` and a failure issue; preview failures were
   counted as `videos_failed`, so progress reported more items than the phase total.

Declared run findings (amendment 3), also repaired in 0034: ISO BMFF duration ignored edit lists
(88 of 560 real files), rotation sign differed between parsers (53 files), AppleDouble `._*`
sidecars were moved as videos (42 files), and variable frame rates printed as large rationals.

Nonblocking, fixed with the refactor: a fatal preview-worker error (for example a WAL write) let the
split loop keep moving videos until the end; the worker now stops the loop at the next
transaction. A preview whose moved source vanished stopped the whole run; it is now a failed
preview. Moved videos' preview catch-up re-probed files whose metadata the video registry already
had. `TestFFprobeReaderProcessLimits` failed 3 of 3 runs under `-race`
(`AUD-implement-video-samples-1`).

### Refactor

- `internal/media`: `preview.go` (types, options, `Generate`, published-output validation),
  `preview_plan.go` (a planner struct replacing one 130-line function with closures),
  `samples_codec.go` (container, encoder and quality policy in one table), `samples.go`,
  `samples_args.go`, `frames.go`. One `Quality` field and kind constants replace per-kind fields and
  string literals; `preview_size.go` and `preview_validate.go` merged; unused exports unexported.
- `internal/archive`: `split.go` 355 -> 255 lines; `split_exec.go` holds the split transaction and
  `transfer.go` the placement and its failure handling that split and restore duplicated (restore
  also counted a cross-device fallback as a source-change attempt). `history.go` reads every run's
  WAL once per use with its roots. `previews_split.go` split into `previews_plan.go` and
  `previews_exec.go` (queue and executor); the index uses typed sets instead of a boolean map whose
  value meant "generating". The largest production file is now 298 lines (was 355).
- Removed: the scaffold-only "not implemented" status, error, handler and exit code 70 (no
  operation returned it; `publish` is rejected during validation), `cli.Version`,
  `report.WriteSummaryTo`, `report.RelStubPath`/`MustRelStubPath`, `archive.Session.Op`, the unused
  `preview`/`published` step constants, the stub placeholder text and the code that recognized it,
  the zero-value preview-options defaulting, the value/pointer resolver switch and
  `RestoreConfig.Resolver`, the test-only `scanner.Stats` (now a test helper), and
  `state.FSResolver` from production code (now `crashtest.Resolver` and a state test helper).
  Stale "stage 2 when available" comments were corrected; `StubsDelete`/`StubsKeep` became
  `PolicyDelete`/`PolicyKeep`, since `--previews` uses them too.
- Tests: preview tests were reorganized by seam (`previews_split_test.go`,
  `previews_restore_test.go`) with shared fixtures; removed tests of helpers and of removed code
  (`TestStatsCount`, `TestStripParams`, the timing-based 1e6 committed-set test, the MarkdownStub
  path unit tests duplicated by split-level stub tests, the unknown-extension fallback test) and the
  obsolete not-implemented session assertion. 332 -> 339 test functions; every new one is a
  regression or covers a repaired rule.

### Declared run on real media

Read-only survey of the operator media archive (path omitted; 666 GiB, 32,000 files; 660 video files,
333 GiB) with ffprobe: containers MP4 406, MOV 145, MTS 16, GoPro LRV 9, MPEG-1 elementary `.mpg`
8, WMV 1; video H.264 (Baseline to High, `yuv420p`/`yuvj420p`), HEVC Main and Main 10, MPEG-1,
WMV3; audio AAC mono/stereo, AC-3 5.1, PCM, WMA2 and Apple positional audio (`apac`, 2 files,
always after an AAC track); data tracks GoPro `gpmd` (226), `fdsc` (218), timecode `tmcd` (205),
Apple `mebx`/`mett` (13), PGS subtitles in MTS (16); display rotation -90 (53) and 180 (4). The 42
probe failures were all AppleDouble `._*` sidecars without their companion videos. The GStreamer
notices concern the GoPro `gpmd`/`fdsc` data tracks, which arxgo moves byte for byte and previews
ignore.

The smallest file of each of 43 format classes (container, codec, pixel format, audio layout,
rotation, data tracks; 3.9 GiB) was copied with its directory structure (non-ASCII names) into the
scratchpad and processed by the built binary without `.env`:
`split --metadata media --sample series --sample-every 1m --sample-duration 2s --image middle`,
the same split again, then `restore --previews delete`, then a comparison of path, size, mtime and
SHA-256 of every file.

| Build | Split | Rerun | Restore | Round trip |
| --- | --- | --- | --- | --- |
| `2bf001b` plus findings 1-6 | exit 6: 43 videos moved (3 AppleDouble), 75 previews, 8 failed (3 sidecars, 5 trimmed QuickTime edits planned past their end) | not run | not run | not run |
| Final | exit 0: 40 videos, 80 previews, 0 failed; preflight `previews=34.5MiB` | exit 0, nothing generated, empty preflight needs | exit 0: 40 restored, 80 `preview_deleted` | identical manifests and SHA-256; video archive holds only the retired registries |

A portrait phone video (rotation -90) gave an upright 360x640 PNG (inspected) and a 360x640 sample
without rotation metadata. Comparing the ISO BMFF parser with ffprobe on all 560 MP4/MOV/LRV files:
88 duration and 53 rotation mismatches before 0034, 1 duration difference of 0.113 s and none
otherwise after it; 14 files fall back to ffprobe as specified (files with a box past the end of
file, or without a readable box structure).

### Windows review (reading and cross-compilation only)

Read `media/ffmpeg_process_windows.go` (Job Object, unchanged), the preview publication through
`fsops.Rename`, and the new code's path handling: `scanner.IsPartFile` works on slash paths,
`history.localRel` uses `filepath.Rel` and `scanner.LocalRelPath`, restore deletion compares parent
directories with the archive root string. No Windows-only defect found; `make build-all` and
`make vet-windows` pass. Runtime checks: W7a and W8 (notes below).

## Invariant-to-evidence table

| Invariant | Code | Evidence |
| --- | --- | --- |
| Preview events are version 2, independent of the video transaction; reopening keeps the committed set and rejects a duplicate outcome | `state/wal.go`, `wal_read.go`, `wal_preview.go` | `TestPreviewEventsSurviveWALReopen`, `TestWALUnsupportedVersionIsCorruptEvenOnLastLine` |
| `preview_begin` is durable before ffmpeg writes; `preview_done` records the published size | `archive/previews_exec.go` | `TestPreviewCrashResumesOnlyMissingPreviewsLive`, `TestSplitPreviewsRegistryStubAndRerunLive` |
| Unfinished parts are removed; only missing previews are generated; a rerun after success generates nothing | `previews_index.go`, `previews_exec.go` | `TestPreviewCrashResumesOnlyMissingPreviewsLive`, `TestSplitPreviewsRegistryStubAndRerunLive`, `TestSubstitutedSampleContainerRerunLive`; declared run rerun |
| Every run plans the same names; the container is chosen before the WAL event | `media/preview_plan.go`, `samples_codec.go` | `TestPreviewSampleContainer`, `TestGenerateSampleLiveContainers`, `TestSubstitutedSampleContainerRerunLive` |
| No user file is overwritten: exclusive part, no-replace publication, `-arxgo` names, adoption only of a validated unfinished output | `media/ffmpeg.go`, `preview_plan.go`, `previews_exec.go` | `TestRunnerFailureAndConflicts`, `TestFramePNGValidationAndFailedPublish`, `TestPreviewNamesAndCollisions`, `TestPreviewNamesAcrossVideosLive`, `TestPendingPreviewRejectsInvalidFinalLive` |
| An invalid output never becomes a preview | `media/samples.go`, `frames.go` | `TestSampleEncoderFallbackAndValidation`, `TestFramePNGValidationAndFailedPublish` |
| Previews never become split candidates | `previews_index.go` `skipPaths`, `scanner/relpath.go` | `TestSplitPreviewsRegistryStubAndRerunLive`, `TestLocalRelPath`, `TestStage2Previews` |
| A failed preview never affects the move; it is counted as a preview and exits 6 | `previews_exec.go`, `finish.go` | `TestPreviewFailureIsCountedAndCaughtUpLive` |
| Cancellation or timeout stops the process tree and leaves the preview unfinished | `media/ffmpeg_process*.go`, `previews_exec.go` | `TestRunnerTimeoutKillsProcessTree`, `TestRunnerClosesOrphanChild`, `TestRunnerCancellation`, `TestCanceledPreviewIsRetriedNotFailedLive` |
| Restore deletes only WAL-recorded previews with their recorded size below real directories; the intent is durable first; interrupted cleanup completes | `previews_restore.go`, `restore.go` | `TestRestoreDeletesOnlyRecordedPreviewsLive`, `TestRestoreKeepsChangedPreviewLive`, `TestRestoreCrashAfterCommitDeletesPreviewsLive` |
| The owned stub lists exactly the recorded previews after split, re-split and restore; operator text stays | `previews_exec.go` `refreshPreviewStub`, `report/markdown.go` | `TestReplacePreviewSection`, `TestResplitAfterRestoreLinksKeptPreviewsLive`, `TestRestoreKeptStubDropsDeletedPreviewsLive` |
| Registry `previews` and `stub_rel_path` come from WAL replay against each run's recorded roots | `archive/history.go`, `split_report.go` | `TestRelocatedArchiveKeepsRegistryPathsLive`, `TestSplitRegistryMergesTwoRuns` |
| Preflight counts only previews not yet published | `previews_plan.go` `missingBytes`, `preflight.go` | `TestPlanPreflightTable` (preview row); declared run preflight `previews=34.5MiB`, rerun needs empty |
| Planned duration and orientation match the decoder | `media/isobmff_fields.go`, `ffprobe.go`, `preview_plan.go` | `TestISODurationIsThePresentedMovie`, `TestFFprobeCapturedFormats`, `TestGenerateSampleLiveRotatedSource`; declared run parser comparison and upright frame |
| Only real videos are candidates | `scanner/mimetype.go` | `TestVideoExtensionOptions` (AppleDouble); declared run 43 -> 40 videos |
| Bundles carry the approved GPL v3 tools, verbatim licence, source notice, manual and checksums, never `.env` | `scripts/package-dist.sh`, `packaging/` | `TestPackageDist`; `make dist` on this tree: archive and inner `SHA256SUMS` verify, file lists as specified, `cmp` of GPL text identical, `bin/ffmpeg -version` shows `--enable-gpl --enable-version3` without `--enable-nonfree` (bundles removed afterwards: version `2bf001b-dirty`) |
| Only `media` starts processes; domain code does not print | `internal/media` | `grep` at review time: `os/exec` only in `media/{ffmpeg,ffmpeg_process*,ffmpeg_encoders,ffprobe,tools}.go`; no `fmt.Print` in domain packages |
| Windows specifics type-check | build-tagged files | `make build-all`, `make vet-windows` (in `make ci`); runtime deferred |

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Notes dispositioned | [Audit handoff](#audit-handoff) | pass; Windows-only notes deferred |
| Verdicts recorded | [Verdicts](#verdicts) | refactor (operator-authorized); `proceed-with-nonblocking-notes` |
| Blockers repaired first | [0034](0034-preview-repair-stage-2-preview-defects.md) | pass; each regression reproduced on `2bf001b` |
| `make ci` passes | `PATH=/usr/local/go/bin:$PATH make ci` | pass on Linux: fmt, vet (with Windows), tests, static builds, spec-plan and doc-link lints |
| Live media tests ran, not skipped | `ARXGO_TEST_REQUIRE_TOOLS=1 go test -count=1 ./internal/...` | pass with ffmpeg/ffprobe 6.1.1 |
| End-to-end binary tests | `ARXGO_TEST_REQUIRE_TOOLS=1 make test-integration` | pass (`TestStage1GeneratedArchive`, `TestStage2Previews`, `TestPackageDist`) |
| Race detector | `CGO_ENABLED=1 ARXGO_TEST_REQUIRE_TOOLS=1 go test -race -count=1 ./internal/archive ./internal/media ./internal/state ./internal/report ./internal/scanner ./internal/cli` | pass, Linux |
| Real-media declared run | [Declared run on real media](#declared-run-on-real-media) | pass; operator data only read, runs on a scratch copy |

## Audit handoff

### Incoming notes

| Note | Disposition |
| --- | --- |
| `AUD-implement-ffmpeg-runner-1` | Windows only: deferred to W7a (already listed). |
| `AUD-implement-video-samples-1` | Reproduced: `TestFFprobeReaderProcessLimits` failed 3 of 3 race runs; only the hung-helper case keeps the 250 ms timeout, the others get 30 s. Passes 3 of 3 under `-race`. Closed. |
| `AUD-implement-release-bundle-with-ffmpeg-1` | Windows only: deferred to W8, now listed in the deferred items. |
| `AUD-review-stage-1-integrity-3` | Verified closed by 0030: an unparsable `arxgo-videos.csv` stops split and restore with exit 5 before a move (`TestCorruptVideoRegistryStopsBeforeMove`), and archive bytes written count stubs, previews and stub refreshes. |
| 0030 "broader review of preview WAL and registry invariants" | Done above; defects 2-6 came from it. |

### New notes

- `AUD-review-stage-2-previews-1`: nonblocking. FFmpeg 6.1.1 cannot decode Apple positional audio
  (`apac`), and its concat filter rejects mono MPEG-1 Layer II audio after input seeking ("Changing
  audio frame properties on the fly is not supported"; stereo works). A sample of a file whose
  first audio stream is `apac`, or a series sample of such a mono MP2 source, fails as a preview on
  every run. On the archive both `apac` files carry AAC first and no MP2 source has audio.
  Next check: a later FFmpeg pin or a per-fragment encode if operators hit it. Owner: this
  checkpoint; disposition: documented limitation in the
  [current page](../current/media-previews.md#real-media-support) and manuals, closed.
- `AUD-review-stage-2-previews-2`: nonblocking, Windows only. Root resolution of earlier runs'
  WAL paths with `filepath.Rel` (drive-letter case, UNC spelling, a root moved to another drive)
  and restore deletion's parent walk are cross-compiled only. Next check:
  [W7a](../../guide/windows-verification.md#deferred-items). Owner: this checkpoint; deferred.
- `AUD-review-stage-2-previews-3`: nonblocking. `arxgo scan` registers preview files in
  `arxgo-registry.csv` (samples with `is_video=true`); split's own scan excludes recorded previews.
  This follows the registry specification (every regular file is registered) and nothing reads the
  scan operation's candidates. With `--metadata file`, each split with preview options probes every
  earlier moved video once to plan its names. Owner: this checkpoint; disposition: specified
  behavior, documented, closed.

### Verdicts

- Refactor: **refactor**, authorized by amendment 2 and limited to readability, file balance,
  removal of dead and scaffold code and duplicated placement handling; behavior changes are the
  0034 repairs.
- Decision: **`proceed-with-nonblocking-notes`**. The blocking repairs passed; remaining notes are
  nonblocking with one disposition each. Stage 3 may start.

## Close or resume

All gates pass. The checkpoint and its repair task are removed from the plan; the dependency of
`research-cloud-target-apis` links this record. `media-previews` is marked shipped in the
[registry](../../openspec/spec.md#capability-registry). Current pages updated:
[index](../current.md), [media previews](../current/media-previews.md),
[media metadata](../current/media-metadata.md), [archive registry](../current/archive-registry.md),
[video split](../current/video-split.md), [crash safety](../current/crash-safety.md); manuals,
README, architecture and the Windows scenario too. Plan counts after: 9 open tasks (6 agent, 3
human); next eligible `research-cloud-target-apis`. Temporary repro tests, the archive copy and the
verification bundles were removed from the repository tree; `dist/` was emptied by the verification
run and is rebuilt with `make dist`.

## Addendum: FFmpeg 9.0.1 and undecodable audio

After this checkpoint the operator approved FFmpeg 9.0.1
([0035](0035-preview-approve-ffmpeg-9-distribution.md)). `AUD-review-stage-2-previews-1` is now
resolved rather than only documented: mono MP2 series samples work with the pinned 9.0.1
([0036](0036-preview-upgrade-bundled-ffmpeg-to-9.md)), and samples pass over audio streams without
a decoder, such as Apple positional audio, or are written silent
([0037](0037-preview-handle-undecodable-sample-audio.md)). Verdict unchanged.
