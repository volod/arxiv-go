# Task Record

## Task and scope

- Id / capability / checkpoint: `implement-iso-bmff-metadata` / `media-metadata` / `review-stage-1-integrity`
- State: accepted.
- Source: plan task id; code revision `4e2dbb8`, clean working tree at start.
- Plan counts at start: 23 open tasks (20 agent, 3 human); next eligible `implement-iso-bmff-metadata`.
- Accepted task: original plan block follows.

```markdown
#### implement-iso-bmff-metadata

Read MP4/MOV/M4A container and stream metadata in pure Go.

- Serves: `media-metadata` -- [ISO BMFF parser](../openspec/stage-1-core/metadata.md#iso-bmff-parser)
- Agent status: CLEAR
- Dependencies: [Scan operation and CSV registry](records/0012-registry-implement-scan-operation-and-csv-registry.md).
- User-visible outcome: `arxgo scan --metadata media` fills duration, resolution, codecs, audio
  presence and tags for ISO BMFF files without any external tool, and audio-only MP4 files are not
  flagged as video.
- Scope boundary: Normalized `MediaInfo` type and JSON, `github.com/abema/go-mp4` probe, codec
  fourcc mapping, rotation, fragmented files, tags, non-fatal errors, registry integration and the
  `is_video` refinement.
- Data and artifact paths: `internal/media/metadata.go`, `internal/media/isobmff.go`,
  `internal/media/testmp4/` (box builder test helper); `go.mod`, `go.sum`.
- Execution path: `mp4.Probe` plus targeted `ReadBoxStructure` for `udta/meta/ilst` and `tkhd`
  matrices.
- Acceptance gates: Generated fixtures: video+audio, audio-only `.mp4`, M4A, MOV with rotation
  matrix, `moov` at end, fragmented with `mehd`, truncated/corrupt box -> `error` field and scan
  continues; parse reads a bounded number of bytes regardless of `mdat` size.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

`internal/media` now exposes normalized `MediaInfo` JSON and an ISO BMFF reader. It uses a
bounded `ReadBoxStructure` walk for movie, track, sample-entry, tag and fragment fields. An optional
`Probe` runs only for small, non-fragmented, validated sample tables; its result is not needed for
normalization because it expands samples and has limited codec coverage. Box reads are capped at
32 MiB total and 1 MiB per decoded metadata box; `mdat` is only sought over. The parser handles
late `moov`, `mehd`, summed fragments including `trex` defaults, display-matrix rotation, text
tags, fourcc codec mapping and RFC 3339 creation time. File-size bit rate is an estimate.

The scan worker attaches `media` only for detected ISO BMFF MIME types in media mode. A successful
parse with no video track clears `is_video` before statistics, registry and candidate writes.
Parse failures stay in `metadata.media.error` and do not fail the scan or change its video flag.
`report.Metadata` gained the shared media field. Generated fixtures live in
`internal/media/testmp4/`. `go-mp4` is the approved direct dependency; its pure-Go indirect
`github.com/google/uuid` dependency was added to the spec table. Current behavior is described in
[media metadata](../current/media-metadata.md). ffprobe fallback remains in its existing task.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Generated ISO BMFF fixtures | `GOCACHE=/tmp/arxgo-gocache go test ./internal/media -run 'TestISO' -v` | Pass: video plus audio, audio-only MP4 and M4A, rotated MOV, late `moov`, `mehd` and summed fragments with `trex`, tags, corrupt box, sparse 1 GiB `mdat` with under 4 KiB read. |
| Scan integration | `GOCACHE=/tmp/arxgo-gocache go test ./internal/archive -run TestScanISOMetadataAndAudioOnlyRefinement -v` | Pass: media JSON, non-fatal corrupt-file error, audio-only video flag and candidate list. |
| Live generated MP4 | `GOCACHE=/tmp/arxgo-gocache go test ./internal/media -run TestISOLiveFFmpeg -v` | Pass on this Linux host with ffmpeg; test skips with a reason when it is absent. |
| Relevant packages | `GOCACHE=/tmp/arxgo-gocache go test ./internal/media ./internal/archive ./internal/report` | Pass. |
| Required Linux and cross-build gates | `GOCACHE=/tmp/arxgo-gocache make ci` | Pass: format, Linux vet/tests, Windows vet, static Linux and Windows builds, plan and link lint. Windows runtime behavior is cross-compiled only; deferred scenario W6 remains. |
| Module integrity | `GOCACHE=/tmp/arxgo-gocache go mod verify` | Pass: all modules verified. |
| Initial gate failure | `GOCACHE=/tmp/arxgo-gocache make ci` before plan removal | `lint-spec-plan` rejected the active record while its task still appeared in the plan; removed the accepted task and reran successfully. |

## Audit handoff

None identified in the reviewed ISO BMFF parser, registry integration, generated/live fixtures and
dependency changes. ffprobe fallback is already owned by `implement-ffprobe-metadata`.

## Close or resume

All acceptance gates passed. The record and index are updated, the task was removed from the
forward plan, and the remaining dependency was replaced by this record link. The current-state
page and its index now describe the parser. Plan counts after: 22 open tasks (19 agent, 3 human);
next eligible `implement-ffprobe-metadata` (also eligible:
`implement-video-split-transactions`). `media-metadata` remains planned until its remaining work
is accepted.
