# Handle Undecodable Sample Audio

## Task and scope

- Id / capability / checkpoint: `handle-undecodable-sample-audio` / `media-previews` / none (stage 2
  is accepted; evidence is recorded here)
- State: accepted.
- Source: plan task added with the operator's approval in
  [0035](0035-preview-approve-ffmpeg-9-distribution.md) ("Implement both now"); working tree with the
  uncommitted changes of 0033, 0034 and 0036.
- Plan counts at start: 11 open tasks (8 agent, 3 human).
- Accepted task:

```markdown
#### handle-undecodable-sample-audio

A sample fails on every run when the source's first audio stream has no FFmpeg decoder, such as
Apple positional audio (`apac`), which no FFmpeg version decodes.

- Serves: `media-previews` -- [Encoding by container](../openspec/stage-2-previews/previews.md#encoding-by-container)
- Agent status: CLEAR
- Dependencies: [Stage-2 repairs](records/0034-preview-repair-stage-2-preview-defects.md).
- User-visible outcome: Such a sample uses the first decodable audio stream, or is written without
  sound with a warning, instead of failing.
- Scope boundary: Decoder probe, per-stream audio selection from ffprobe, sample arguments and
  validation including adoption of a published sample; spec amendment. Frames unchanged.
- Data and artifact paths: `internal/media/`, `docs/openspec/stage-2-previews/previews.md`.
- Execution path: Unit tests of selection and arguments; live test on a generated file whose first
  audio stream has no decoder; drone-copy run on the iPhone `apac` files.
- Acceptance gates: First undecodable plus decodable stream uses the decodable one; only
  undecodable audio gives a silent sample and a warning; decodable-first files unchanged;
  `make ci` passes.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: none; stage 2 is accepted, evidence recorded in the task record.
```

- Amendments: none.

## Implementation

`media.Runner.GenerateSample` resolves the audio stream before encoding (`samples_audio.go`): it
decodes one second of each source audio stream in order (`ffmpeg -nostdin -v error -i SRC -map
0:a:N -t 1 -f null -`, the preview timeout applies) and encodes the first that exits 0. A later
stream is logged with a warning; with none the sample is written with `-an` and a warning. The
planner records the source's audio stream count in the job, and the single-clip and concat-filter
arguments map the chosen `0:a:N`. `ValidatePublishedPreview` takes the source and resolves the
stream the same way, so adopting a sample published before a crash applies the same audio rule.

A test decode was chosen over comparing ffprobe codec names with `ffmpeg -decoders`: names differ
for some codecs (ffprobe `amr_nb`, decoder `amrnb`), which would silence 3GP phone videos. It costs
one short ffmpeg process per audio stream tried, per sample. Frames are unchanged. The
[previews specification](../../openspec/stage-2-previews/previews.md#encoding-by-container) and
manuals describe the rule.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| First undecodable plus decodable stream uses the decodable one | `TestGenerateSampleLiveUndecodableAudio/later_stream_decodes` (generated MOV, first AAC entry renamed `apac` without `esds`, so FFmpeg has no decoder) | pass: sample has audio and validates as a published preview; fails without the fallback (`no decoder found for: apple_apac`) |
| Only undecodable audio gives a silent sample and a warning | `TestGenerateSampleLiveUndecodableAudio/no_stream_decodes` | pass: video-only sample; fails without the fallback |
| Stream index in arguments | `TestSampleEncodersAndArguments` | pass: `-map 0:a:1` and `[n:a:1]` in the concat filter |
| Decodable-first files unchanged | live sample tests; drone-copy round trip | pass: 80 previews, no audio warning |
| Real iPhone files | copies of the two iPhone `.MOV`/`.mov` recordings (AAC, then `apple_apac`), a remux with `apac` first and an `apac`-only remux; built `arxgo split --metadata media --sample series --image middle` with pinned 9.0.1 | pass: exit 0, 8 previews; samples of the recordings and the `apac`-first remux have AAC, the `apac`-only sample has video only; one warning each for the two remuxes; rerun exit 0 |
| `make ci` passes | `PATH=/usr/local/go/bin:$PATH make ci` | pass on Linux; Windows vetted and cross-built only |

## Audit handoff

None identified. Reviewed: cancellation during the decode check returns the context error, a
missing ffmpeg is an error, and a stream index beyond the count cannot be chosen.

## Close or resume

All gates pass. The task left the plan; [media previews](../current/media-previews.md) links this
record. Plan counts after: 9 open tasks (6 agent, 3 human).
