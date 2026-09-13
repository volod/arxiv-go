# Task Record

## Task and scope

- Id / capability / checkpoint: `implement-frame-images` / `media-previews` / `review-stage-2-previews`.
- State: accepted.
- Source: plan task at `77c72b2`; clean worktree at start.
- Plan counts at start: 13 open tasks (10 agent, 3 human); next eligible `implement-frame-images`.
- Accepted task:

````markdown
#### implement-frame-images

Generate PNG frames at the planned positions.

- Serves: `media-previews` -- [Position modes](../openspec/stage-2-previews/previews.md#position-modes)
- Agent status: CLEAR
- Dependencies: [FFmpeg runner](records/0026-preview-implement-ffmpeg-runner.md); [Preview planning](records/0027-preview-implement-preview-planning.md).
- User-visible outcome: `--image start|middle|end|series` produces `-imgNN.png` frames at the
  clamped resolution.
- Scope boundary: Frame extraction arguments, compression mapping, PNG validation with
  `image/png` decode.
- Data and artifact paths: `internal/media/frames.go`.
- Execution path: Generated sources as for samples.
- Acceptance gates: Frame count and names, decoded dimensions, rotated source orientation, frame
  near end of a short video.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.
````

- Amendments: none.

## Implementation

`internal/media/frames.go` adds `Runner.GenerateFrame`, which accepts a planned image job,
constructs argument-vector FFmpeg extraction with one selected video stream and the planned
display-oriented size, and maps low/medium/high image quality to PNG compression levels 9/6/3.
The executor uses the existing runner's timeout, exclusive part reservation, non-replacing
publication and cleanup. Before publication, it decodes the part with `image/png` and checks
decoded dimensions. An invalid or empty output cannot become a final preview. A short source
with unknown duration can produce no frame at the planned 1-second start seek; that specific
empty-output case retries once at time zero. The planner now marks image jobs with
`DurationKnown` so known-duration failures do not trigger this fallback.

The owning split integration task still supplies the WAL preview record and archive output path.
The [current-state page](../current/media-previews.md) documents the executor. No dependencies
were added. PNG extraction uses the FFmpeg software path; CUDA support on the test host does not
change the portable output contract.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Frame count and names | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -count=1 -run 'TestFrame|TestGenerateFrames' ./internal/media` (`TestGenerateFramesLiveModesAndNames`) | pass on Linux: start, middle, end and four-frame series have planned names and no extra files |
| Decoded dimensions and compression | Same command (`TestGenerateFramesLiveClampRotationAndShortEnd`, `TestFrameArgsAndInvalidJobs`, `TestFramePNGValidationAndFailedPublish`) | pass: `image/png` decodes exact planned dimensions; low/medium/high map to 9/6/3; malformed PNG is removed before publication; existing user output is unchanged |
| Rotation and short-video end frame | Same command (`TestGenerateFramesLiveClampRotationAndShortEnd`) | pass: rotated source yields portrait 96x128 PNG; 1.3-second clip produces an end frame and a distinct frame at 1.2 seconds |
| Unknown-duration short start | Same command (`TestGenerateFrameLiveUnknownDurationShortStart`) | pass: empty 1-second seek on 0.8-second source retries at zero and publishes a decoded PNG |
| Full local media package | `ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache go test -count=1 ./internal/media` | pass on Linux with installed FFmpeg/ffprobe 6.1.1; generated media only |
| Required CI and Windows cross-checks | `GOCACHE=/tmp/arxgo-go-cache make ci` | pass on Linux: vet, all tests, Linux/Windows static builds and both documentation lints; Windows runtime not run |
| Plan and doc links | `GOCACHE=/tmp/arxgo-go-cache make lint-spec-plan`; `GOCACHE=/tmp/arxgo-go-cache make lint-doc-links` | pass separately after final record update |

## Audit handoff

None identified in the frame executor, planner metadata field and affected tests. Windows
behavior is cross-compiled and vetted in `make ci`; runtime verification remains deferred to
[Windows verification W7a](../../guide/windows-verification.md#scenario).

## Close or resume

All acceptance gates passed. The [current-state page](../current/media-previews.md) and record
index link this record; the completed task is removed from the plan and its dependency replaced
with this record link. Plan counts after removal: 12 open (9 agent, 3 human); next
eligible `integrate-previews-into-split-and-restore`. The `media-previews` capability remains
planned until its remaining tasks pass. No temporary repository files remain.
