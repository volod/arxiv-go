# Task Record

## Task and scope

- Id / capability / checkpoint: `implement-video-samples` / `media-previews` / `review-stage-2-previews`.
- State: accepted.
- Source: plan task at `cc62366`; clean worktree at start.
- Plan counts at start: 14 open (11 agent, 3 human); next eligible `implement-video-samples`.
- Accepted task:

````markdown
#### implement-video-samples

Generate sample clips in the source container format.

- Serves: `media-previews` -- [Encoding by container](../openspec/stage-2-previews/previews.md#encoding-by-container)
- Agent status: CLEAR
- Dependencies: [FFmpeg runner](records/0026-preview-implement-ffmpeg-runner.md); [Preview planning](records/0027-preview-implement-preview-planning.md).
- User-visible outcome: `--sample start|middle|end|series` produces playable `-smplNN` clips at the
  requested duration and clamped resolution.
- Scope boundary: ffmpeg argument construction for single and chunked series clips, encoder
  fallback, quality mapping, output validation with ffprobe.
- Data and artifact paths: `internal/media/samples.go`.
- Execution path: Tests generate `testsrc2`+`sine` sources in mp4/mov/mkv/webm; skip with reason
  without ffmpeg.
- Acceptance gates: Duration within tolerance, dimensions per clamp, container matches source,
  series with more than 50 fragments concatenates correctly, missing encoder falls back.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.
````

- Amendments: none.

## Implementation

`internal/media/samples.go` and `samples_args.go` add `Runner.GenerateSample`. It consumes the
planner's ranges and encoded size, chooses installed encoders from the runner's cached probe,
maps quality to CRF or quantizer and audio bitrate, and builds argument vectors for single or
series clips. More than 50 ranges are encoded in groups of at most 50, then joined with the
concat demuxer. Chunk outputs and their list live in a private temporary directory and are
removed when the call ends. The final output is passed through the existing no-replace runner;
the split integration task owns the surrounding WAL preview records and invokes this executor
after a committed move.

`Runner.Run` now accepts a read-only validation callback before publication. Samples use ffprobe
to require a video stream, planned dimensions, source container, audio when the source has it,
and duration close to the plan. Invalid parts are removed by the runner and cannot become final
files. An unknown-duration start job accepts a shorter nonempty clip when the source ends before
the requested length. Missing `libx264` falls back to `mpeg4`; missing AVI `libmp3lame` falls back to `aac`.
Unknown extensions retry as MP4 only when FFmpeg reports that it cannot choose an output muxer;
existing-file and other errors do not trigger that fallback. The returned path communicates a
fallback extension to the future registry and WAL integration. CUDA/NVENC is not used because the
container policy specifies software encoders and portable output.

Live tests exposed a pre-existing rotation mismatch: both metadata readers already report
display-oriented dimensions, while the planner swapped them again. The clamp now uses those
dimensions directly, and a rotated-source live test verifies the encoded result. A 3GP probe
also needed extension-based container recognition when MIME is unavailable; `isoContainer` now
handles `.3gp` and `.3g2`. The initial 0.1-second long-series fixture at 10 fps produced a
truncated sample, so the fixture uses decodable 0.2-second ranges and the executor now rejects
large duration errors instead of allowing error to grow with fragment count. No dependencies
were added. [Current state](../current/media-previews.md) describes the executor; the CLI still
guards active preview modes until the later archive integration task.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Single and series duration, dimensions, container, audio and names | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -count=1 -run 'TestGenerateSample|TestSampleEncoder|TestPreviewClamp' ./internal/media` | pass on Linux with local FFmpeg/ffprobe 6.1.1: MP4, MOV, MKV, WebM, M4V, 3GP, AVI; start, middle, end, series; sample names and probe results |
| More than 50 fragments | `TestGenerateSampleLiveLongSeries` in same run | pass: 52 ranges, 2 chunks and concat demuxer, with and without audio; total duration within 0.5 s |
| Encoder and muxer fallback | `TestSampleEncoderFallbackAndValidation`, `TestSampleEncodersAndArguments`, `TestGenerateSampleLiveUnknownExtensionFallback` | pass: live `mpeg4` fallback, AVI audio fallback selection, unknown-extension MP4 fallback; occupied output unchanged |
| Invalid output, rotation and size clamp | `TestSampleEncoderFallbackAndValidation`, `TestGenerateSampleLiveRotatedSource`, `TestGenerateSampleLiveClampAndShortNoAudio`, `TestGenerateSampleLiveUnknownDurationStart` | pass: ffprobe failure leaves no published part or final; display rotation, short landscape/portrait clips and unknown-duration start decode at planned size |
| Required CI and Windows cross-checks | `GOCACHE=/tmp/arxgo-go-cache make ci` | pass on Linux after plan transition: formatting, vet, all tests, Linux/Windows static builds and docs lints. The initial run passed code gates but lint-spec-plan correctly rejected the active record while its task remained in the plan. Windows runtime not run |
| Plan and links | `GOCACHE=/tmp/arxgo-go-cache make lint-spec-plan`; `GOCACHE=/tmp/arxgo-go-cache make lint-doc-links` | pass individually after plan transition |
| Optional race check | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -race -count=1 -run 'TestGenerateSample|TestSampleEncoder|TestPreviewClamp' ./internal/media` | pass. Full `go test -race -count=1 ./internal/media` failed twice in pre-existing `TestFFprobeReaderProcessLimits`: valid and oversize helper processes exceeded its 250 ms deadline under race instrumentation; see audit note |

## Audit handoff

`AUD-implement-video-samples-1`: Optional full-package race runs twice failed in the pre-existing
`internal/media/ffprobe_test.go` `TestFFprobeReaderProcessLimits` valid and oversize cases. They
launch the race-instrumented test binary with a 250 ms timeout and returned `context deadline
exceeded` instead of the expected probe result. The sample-specific race run and required `make
ci` pass; no sample executor failure or race was observed. Nonblocking: the affected invariant is
that helper-process timeout tests distinguish slow startup from a hung process. The
`review-stage-2-previews` checkpoint owns a repeatable race check and a narrow test-timeout repair
if needed; this task does not change the earlier metadata test.

Windows behavior is cross-compiled and vetted in `make ci`; live FFmpeg execution on a Windows
host remains part of [W7a](../../guide/windows-verification.md#scenario).

## Close or resume

All acceptance gates passed. The record index and [current-state page](../current/media-previews.md)
link this record; the completed task block is removed and the dependent task links this record.
Plan counts after: 13 open (10 agent, 3 human); next eligible `implement-frame-images`.
`media-previews` remains planned. `git diff --check` passed and no temporary repository files
remain.
