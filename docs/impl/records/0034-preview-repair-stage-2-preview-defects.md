# Stage-2 preview repairs

## Task and scope

- Id / capability / checkpoint: `repair-stage-2-preview-defects` / `media-previews` /
  `review-stage-2-previews`
- State: accepted.
- Source: blocking repair task added to the plan by the
  [stage-2 review](0033-preview-review-stage-2-previews.md) (findings 1-6); revision `2bf001b`,
  worktree clean at the start of the review.
- Plan counts at start: 11 open tasks (8 agent, 3 human) after the review added this task; it
  was eligible in parallel with the checkpoint's own review work.
- Accepted task (original block):

```markdown
#### repair-stage-2-preview-defects

The stage-2 review found preview defects: MPEG-PS, Ogg, MXF and DV sources never get a sample, an
unknown-extension sample fails on every rerun, a crash after a restore commit keeps previews that
`--previews delete` should remove, a re-split after restore writes a stub without preview links,
a relocated archive root stops every run, and preview failures are counted as failed videos.

- Serves: `media-previews` -- [Transactions and failures](../openspec/stage-2-previews/previews.md#transactions-and-failures)
- Agent status: CLEAR
- Dependencies: [Preview integration](records/0030-preview-integrate-previews-into-split-and-restore.md).
- User-visible outcome: Every video extension gets a playable sample or a logged `.mp4`
  substitution decided before encoding, reruns after success change nothing, restore cleanup and
  stub links survive crashes and round trips, and a moved archive root keeps working.
- Scope boundary: Sample container and encoders chosen from the source extension at planning time;
  restore deletes previews of every video the run committed, including an interrupted run it
  resumes; stub preview section refreshed for each planned video; WAL paths resolved against the
  recording run's archive root (also the stub column of stage 1); an interrupted preview stays
  unfinished rather than failed; preview counters separate from video counters; preview part
  files reserved; a source without a video stream skips previews with a warning. Spec amendments
  for these rules. No new flags.
- Data and artifact paths: `internal/media/`, `internal/archive/`, `internal/state/`,
  `internal/report/`, `internal/scanner/`, `docs/openspec/`.
- Execution path: Package tests with generated media at the planner, executor, split and restore
  seams; each regression fails before its fix.
- Acceptance gates: `.mpg`, `.vob`, `.ogv` and an unknown extension produce `.mp4` samples and a
  rerun exits 0; crash after a restore commit then resume deletes the previews; split, restore,
  split leaves preview links in the stub; split after renaming the archive root exits 0 with local
  registry paths; cancellation during a preview writes no `preview_failed`; `make ci` passes.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.
```

- Amendments: the review's declared run on real operator media
  ([0033](0033-preview-review-stage-2-previews.md#declared-run-on-real-media)), prompted by the
  operator's request during the review to "scan /media/vola/vola-2tp1/_Drone/ archive and check if
  we support all the required to handle such archive video formats", found four more defects that
  break preview planning or operator output on that archive. They were added under the operator's
  standing request to "Implement, run, fix, improve implementation", with the same regression rule:
  1. The pure-Go ISO BMFF parser took the longest media (`mdhd`) duration across tracks and ignored
     the presented movie duration: a trimmed QuickTime or phone edit reported up to ten times its
     length (88 of 560 real MP4/MOV files), so middle and end previews seeked past the end and
     failed. Duration now comes from `mvhd` (which honors edit lists); fragmented files keep the
     summed video and audio tracks.
  2. Rotation disagreed between parsers for every rotated video (53 files): the ISO track matrix
     gives clockwise 90 for iPhone portrait, ffprobe side data counter-clockwise -90 was normalized
     to 270 while its legacy `rotate` tag path gave 90. Side data is now negated.
  3. macOS AppleDouble sidecars `._<name>.MP4` (4 KiB, magic `00 05 16 07`, 42 files whose
     companions were never copied) were classified as videos by extension, moved with stubs, and
     their previews failed on every rerun. They are now `multipart/appledouble`, never video.
  4. The stub duration line printed a variable frame rate as `12690000/422899 fps`; non-integer
     rates now print with two decimals (`30.01 fps`, `29.97 fps`).

  Gate added for the amendment: the declared real-media round trip exits 0 with identical
  manifests. No other field changed.

## Implementation

`internal/media`: the planner (`preview_plan.go`) picks the sample container from the source
extension with `samples_codec.go` (the extensions whose container holds H.264/AAC, WebM VP9/Opus,
AVI MPEG-4/MP3; anything else `.mp4` with a warning). The runtime muxer fallback, its error-string
matching, the returned-path API and the source-container validation are gone: the output extension
is fixed before the WAL event, so the name recorded, the file published and the name planned on the
next run are the same. A source without a video stream skips with a warning instead of failing on
every rerun. `ReadISO` uses the movie duration; ffprobe rotation is clockwise.

`internal/archive`: `history.go` reads each run's WAL with the roots from its `options.json`;
the preview index (`previews_index.go`) and video registry replay (`split_report.go`) resolve
preview and stub paths against those roots, and restore's directory cleanup against the recorded
video archive. The executor (`previews_exec.go`) returns the context error instead of logging
`preview_failed` when generation was canceled, counts `previews_done`/`previews_failed` (new
additive counters; `videos_failed` no longer includes previews and progress no longer overcounts),
and refreshes the owned stub's preview section once per planned video, writing only on change.
Restore (`restore.go`, `previews_restore.go`) removes unfinished generation parts, finishes
interrupted deletions, and with `--previews delete` first deletes the previews of every video the
run already committed. `report.ReplacePreviewSection` works on the comment markers only: it appends
a section to a stub without one and removes the section when no previews remain; the dev-time
placeholder text is gone from new stubs.

`internal/scanner`: preview part files `<stem>.arxgo-part.<ext>` are reserved like transfer parts;
AppleDouble sidecars are detected by signature. `internal/report`: fractional frame rates.

Spec amendments: previews (naming and container table, stub section, presented duration, no video
stream, identification by name, interruption and counters, relocation, restore deletion scope),
contracts (stub markers, preview counters, WAL root resolution), integrity (WAL versions, preview
events), split-restore (exclusion from WAL events), metadata (rotation convention, duration),
registry (AppleDouble), spec reserved paths. Current pages: [media previews](../current/media-previews.md),
[media metadata](../current/media-metadata.md), [archive registry](../current/archive-registry.md),
[video split](../current/video-split.md). No dependency was added.

## Acceptance evidence

Each defect was reproduced on `2bf001b` before the fix: findings 1-6 with throwaway tests
(`TestRepro*` in `internal/archive` and `internal/media`, deleted), amendments 1 and 3 by reverting
only the fixed file (`git stash -- <file>`) and rerunning the new regression, amendment 2 by the
real-archive parser comparison.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| `.mpg`, `.vob`, `.ogv` and an unknown extension get `.mp4` samples; rerun exits 0 | `TestGenerateSampleLiveContainers`, `TestPreviewSampleContainer`, `TestSubstitutedSampleContainerRerunLive` | pass, Linux, generated media; on `2bf001b` the `.mpg` split was partial and the `.bik` rerun partial |
| Crash after a restore commit, then resume, deletes previews | `TestRestoreCrashAfterCommitDeletesPreviewsLive` (crashes at `wal:commit`, `wal:preview_delete`, `wal:preview_deleted`) | pass; on `2bf001b` the preview stayed after the resumed restore |
| Split, restore, split keeps preview links in the stub | `TestResplitAfterRestoreLinksKeptPreviewsLive` | pass; on `2bf001b` the new stub had only the placeholder |
| Split after renaming the archive root exits 0 with local registry paths | `TestRelocatedArchiveKeepsRegistryPathsLive` | pass; on `2bf001b` exit 5 (`preview WAL path outside archive`), and without previews `stub_rel_path` became `../archive/...` |
| Cancellation during a preview writes no `preview_failed` | `TestCanceledPreviewIsRetriedNotFailedLive` | pass; interrupted (130), resume generates the preview |
| Preview failures counted separately | `TestPreviewFailureIsCountedAndCaughtUpLive` | pass: `previews_failed` > 0, `videos_failed` 0, exit 6, catch-up exit 0 |
| Presented ISO duration (amendment 1) | `TestISODurationIsThePresentedMovie`; parser vs ffprobe on 560 real MP4/MOV/LRV files | pass; fails with the old `isobmff_fields.go`; real files: 88 mismatches before, 1 after (0.113 s) |
| Clockwise rotation (amendment 2) | `TestFFprobeCapturedFormats` (`rotated.json` side data -90 -> 90); real-file comparison | pass; 53 rotation mismatches before, 0 after |
| AppleDouble sidecar (amendment 3) | `TestVideoExtensionOptions`; declared run | pass; declared run moved 43 files before, 40 after |
| Frame-rate text (amendment 4) | `TestMediaLine` | pass |
| Reserved preview parts, kept-stub section removal | `TestLocalRelPath`, `TestReplacePreviewSection`, `TestRestoreKeptStubDropsDeletedPreviewsLive` | pass |
| Real-media round trip (amendment gate) | [declared run](0033-preview-review-stage-2-previews.md#declared-run-on-real-media) | pass: exits 0/0/0, manifests and SHA-256 identical |
| `make ci` passes | `PATH=/usr/local/go/bin:$PATH make ci` | pass on Linux; Windows vetted and cross-built only |

## Audit handoff

None identified beyond the notes recorded by the [checkpoint](0033-preview-review-stage-2-previews.md#audit-handoff),
which owns them: the Windows root-resolution check (`AUD-review-stage-2-previews-2`) and the
FFmpeg decoding limits found on real media (`AUD-review-stage-2-previews-1`).

## Close or resume

All gates pass. The task and the checkpoint left the plan together; the checkpoint's dependency
was this task. Current pages and the record index are updated. Plan counts after: 9 open tasks
(6 agent, 3 human). `media-previews` is shipped with the checkpoint.
