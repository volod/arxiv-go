# Task Record

## Task and scope

- Id / capability / checkpoint: `implement-preview-planning` / `media-previews` / `review-stage-2-previews`
- State: accepted.
- Source: plan task at `3769049`; clean worktree at start.
- Plan counts at start: 15 open (12 agent, 3 human); next eligible `implement-preview-planning`.
- Accepted task:

````markdown
#### implement-preview-planning

Compute preview positions, series, resolution clamp and names without running ffmpeg.

- Serves: `media-previews` -- [Position modes](../openspec/stage-2-previews/previews.md#position-modes)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Operators get predictable preview files for every mode and resolution.
- Scope boundary: Pure planner from `MediaInfo` and options to a list of preview jobs (time ranges,
  output size, encoder choice, file names, collision suffix), series cap, space estimate. Enables
  stage-2 flags in the CLI.
- Data and artifact paths: `internal/media/preview.go`, `internal/cli/`.
- Execution path: Table tests only.
- Acceptance gates: Positions for start/middle/end/series including `D <= L`, unknown duration,
  cap; clamp for landscape/portrait below and above each box with even rounding; names with index
  padding and collisions; estimate formula.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: `review-stage-2-previews`.
````

- Amendments: none.

## Implementation

`internal/media/preview.go` and `preview_size.go` add a pure planner from `MediaInfo`,
`PreviewOptions`, source name and a read-only occupancy predicate to ordered jobs. It plans
sample ranges and image times, caps series, clamps display-oriented dimensions without upscaling,
rounds to even dimensions, chooses preferred codecs from the probed container, resolves name
collisions with `-arxgo`, and estimates output bytes. Unknown duration allows start previews and
skips other modes with a warning. An occupied fallback name is an error; the later executor must
provide occupancy for the target directory and handle encoder availability and muxer fallback.

`internal/cli` accepts and validates split preview flags into typed `SplitOptions.Preview`. Active
modes exit 70 before run state opens because sample/frame execution and archive integration are
later tasks; this avoids a successful split that silently omits requested previews. Restore
`--previews` remains reserved for integration. The CLI contract, preview current-state page and
index were updated. No dependency was added. CUDA is not involved: this task is pure Go planning
and its declared execution path is table tests without ffmpeg.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Positions, short and unknown duration, cap | `GOCACHE=/tmp/arxgo-go-cache go test ./internal/media ./internal/cli` (`TestPreviewPositions`) | pass on Linux: all modes, `D <= L`, unknown start/skip, capped series |
| Clamp and even dimensions | Same command (`TestPreviewClamp`) | pass: landscape, portrait, rotated source, below/above sd/hd/4k and odd dimensions |
| Names, codecs and estimate | Same command (`TestPreviewNamesEncodersAndEstimate`, `TestPreviewEstimateFactorsAndInvalid`) | pass: 2/3-digit indices, both collision paths, container precedence, bitrate and image formulas, invalid inputs |
| CLI flag contract | Same command (`TestPreviewFlagsValidateAndReachOptions`, `TestPreviewFlagErrorsAndNoMutation`) | pass: defaults, flags, environment, validation and active-mode no-mutation exit |
| Required CI, static builds and Windows gates | `GOCACHE=/tmp/arxgo-go-cache make ci` | pass on Linux after plan transition; includes `go vet`, Windows cross-vet, `go test ./...`, Linux/Windows static builds and docs lints; Windows runtime not run |
| Plan and links | `GOCACHE=/tmp/arxgo-go-cache make lint-spec-plan`; `GOCACHE=/tmp/arxgo-go-cache make lint-doc-links` | pass |

## Audit handoff

None identified in the planner, CLI contract and affected tests. Windows behavior consists of
platform-independent planning and flag parsing; `make build-all` and `make vet-windows` passed,
with runtime behavior covered by the deferred Windows verification scenario.

## Close or resume

All acceptance gates passed. [Media previews](../current/media-previews.md) and the record index
link this record; dependent plan tasks link it and the completed block was removed. Plan counts
after: 14 open (11 agent, 3 human). Next eligible task: `implement-video-samples`; also eligible:
`implement-frame-images`. No temporary repository files remain.
