# Task Record

## Task and scope

- Id / capability / checkpoint: `implement-ffprobe-metadata` / `media-metadata` / `review-stage-1-integrity`
- State: accepted.
- Source: `docs/impl/plan.md` at `0df4049`; worktree clean at start.
- Plan counts at start: 22 open tasks (19 agent, 3 human); next eligible `implement-ffprobe-metadata`.
- Accepted task:

```markdown
#### implement-ffprobe-metadata

Read metadata for other containers through ffprobe JSON output.

- Serves: `media-metadata` -- [ffprobe parser](../openspec/stage-1-core/metadata.md#ffprobe-parser)
- Agent status: CLEAR
- Dependencies: [Tool discovery](records/0013-metadata-implement-tool-discovery.md);
  [ISO BMFF metadata](records/0015-metadata-implement-iso-bmff-metadata.md).
- User-visible outcome: Matroska, WebM, AVI, MPEG-TS and other media get the same metadata fields
  as MP4 when ffprobe is available; ISO BMFF parse failures fall back to ffprobe.
- Scope boundary: `exec.CommandContext` invocation, timeout, output limit, typed JSON decode,
  normalization to `MediaInfo`, fallback routing. No ffmpeg.
- Data and artifact paths: `internal/media/ffprobe.go`, `internal/media/testdata/ffprobe/*.json`.
- Execution path: Captured ffprobe JSON text fixtures for normalization; live test gated on tool
  presence with a generated `lavfi` clip.
- Acceptance gates: Normalization of captured JSON for mkv, webm, avi, ts, audio-only, rotated
  side data, missing duration; timeout kills the process; oversized output rejected; live test runs
  only locally when ffmpeg is on PATH.
- Documentation target: `docs/impl/current/media-metadata.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

`internal/media/ffprobe.go` runs the discovery-validated executable with the specified argument
vector, a 60-second per-file deadline, 16 MiB stdout cap, 64 KiB stderr capture and a 1-second
wait delay. Typed JSON normalization fills the shared `MediaInfo`: format or stream duration,
container, bit rate, first video dimensions and rotation, frame rate, codecs, audio presence,
stream counts, creation time and capped container tags. It excludes attached cover pictures from
video counts. Process and parse failures are non-fatal metadata errors without archive paths.

`ReadMetadata` tries the existing pure-Go ISO parser first and falls back to ffprobe only on ISO
failure. `ScanConfig` receives the validated ffprobe path from CLI options without persisting it
in `options.json`. The scan worker probes other detected audio/video files, and the ordered writer
clears MIME-based `is_video` for a successful result with no video stream before statistics,
registry rows and candidates. Pictures keep file-only metadata. This reuses discovery, the scan
pipeline, `MediaInfo`, ISO parser and candidate writer. No dependency or data contract changed.

Fixtures were captured from locally generated clips using ffprobe 6.1.1: MKV, WebM, AVI,
MPEG-TS and audio-only Ogg. Rotated side data and missing duration are derived variants of those
captured JSON documents. The test binary serves as a child ffprobe for bounded invocation,
timeout, command failure and scan routing tests; no fake executable is installed. A live lavfi AVI
test runs only outside CI when both tools are available. The current-state page is
[media metadata](../current/media-metadata.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Captured JSON normalization: MKV, WebM, AVI, TS, audio-only, rotation, missing duration | `TestFFprobeCapturedFormats`, `TestFFprobeNormalizationCorners` in `go test ./internal/media` | Pass on Linux; typed decode ignores unknown fields, invalid/trailing JSON rejected. |
| Timeout kills process; output larger than 16 MiB rejected | `TestFFprobeReaderProcessLimits` | Pass on Linux with test binary child; deadline returned promptly and oversized stdout produced an error. |
| ISO fallback and scan classification | `TestReadMetadataISOFallback`, `TestScanFFprobeMetadataAndISOFallback` | Pass on Linux; successful ISO stays pure Go, broken ISO falls back, audio-only AVI leaves candidates. |
| Live ffprobe on generated lavfi clip | `TestFFprobeLive` via `go test ./internal/media` | Pass locally with ffmpeg and ffprobe 6.1.1; skips with reason when tools are absent or `CI` is set. |
| Real CLI run | Build `arxgo`, then `scan --metadata media --min-free 0` on five generated clips | Completed on Linux; 5 media rows (MKV, WebM, AVI, TS, Ogg), expected codecs and durations, 4 video candidates and no metadata errors. CUDA was not used by ffprobe metadata extraction. |
| Required repository gates | `GOCACHE=/tmp/arxgo-gocache make ci` | Pass on Linux: fmt, vet, Windows vet, all tests, static Linux/Windows builds, spec-plan and doc-link lints. Windows runtime not tested; deferred scenario W6 applies. |
| Documentation and hygiene | `make lint-spec-plan`, `make lint-doc-links`, `git diff --check`, `make plan-status`, `git status --short` | Pass; only task files changed. Initial doc-link lint found one stale archive-registry link after plan removal; fixed and reran. Initial direct `go test` hit the read-only default Go cache; reran with `GOCACHE` under `/tmp`. |

## Audit handoff

None identified after reviewing command invocation, output and timeout bounds, path-free errors,
typed normalization, ISO fallback, worker routing, ordered classification and docs. Windows-only
runtime behavior remains in the deferred verification scenario.

## Close or resume

All acceptance gates passed. The plan task was removed, dependent tasks link this record, and
the current-state page, its index, archive-registry page and capability registry are updated.
`media-metadata` is shipped. Plan counts after: 21 open tasks (18 agent, 3 human); next eligible
`implement-video-split-transactions`. Record index marks 0016 accepted; next sequence is 0017.
