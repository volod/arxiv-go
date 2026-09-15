# Approve FFmpeg 9.0.1 Distribution

## Task and scope

- Id / capability / checkpoint: `approve-ffmpeg-9-distribution` / `media-previews` / none (human
  task)
- State: accepted (operator decision, 2026-09-14).
- Source: ad hoc operator request after the [stage-2 review](0033-preview-review-stage-2-previews.md)
  documented FFmpeg 6.1.1 limits (`AUD-review-stage-2-previews-1`), on branch `ag-02-stage-2` with the
  review changes uncommitted: "The bundled FFmpeg 6.1.1 has a limits. Can we approve implementation to
  handle iPhone spatial audio and mono MP2 audio for example increasing versions or use additional
  standalone ready to use exacutables?"
- Plan counts at start: 9 open tasks (6 agent, 3 human); next agent task
  `research-cloud-target-apis`.
- Accepted task (recorded in human-task shape; the decision was requested in the session, not
  planned in advance):

```markdown
#### approve-ffmpeg-9-distribution

- Serves: `media-previews` -- [Release bundle](../openspec/stage-2-previews/previews.md#release-bundle)
- Human status: HUMAN-GATED
- Dependencies: [Approve ffmpeg distribution](records/0002-preview-approve-ffmpeg-distribution.md);
  [Stage-2 review](records/0033-preview-review-stage-2-previews.md).
- Requested input or decision: Choose whether to replace the approved FFmpeg 6.1.1 builds with a
  newer version, or keep 6.1.1, given evidence on mono MP2 series samples and Apple positional audio.
- Unblocks: `upgrade-bundled-ffmpeg-to-9`.
```

- Amendments: none.

## Decision

The operator decided; the agent prepared and verified the options and did not choose the version.
Options were offered in the session with this evidence, and the operator answered "9.0.1
(Recommended)" to "Which FFmpeg build should arxgo pin and bundle (a distribution/licensing
decision recorded as yours)?" and "Implement both now (Recommended)" to implementing the pin update
and the undecodable-audio fallback.

- Version: FFmpeg 9.0.1, replacing 6.1.1.
- Sources, variant and obligations are unchanged from
  [0002](0002-preview-approve-ffmpeg-distribution.md): GPL v3 (`--enable-gpl --enable-version3`,
  no `--enable-nonfree`) static builds from the same publishers, licence text, provenance and a
  source link in every bundle.

| Platform | Build | Pin |
| --- | --- | --- |
| `linux/amd64` | `mwader/static-ffmpeg:9.0.1`, amd64 manifest `sha256:532d8d39...e3d40d1`; single layer by digest | layer `sha256:1cf03589...e404d5`; `ffmpeg` `bafab020...0a2c`; `ffprobe` `6d6e9d95...13aa5` |
| `windows/amd64` | gyan.dev `ffmpeg-9.0.1-essentials_build.zip` from `GyanD/codexffmpeg` release `9.0.1`; source commit `FFmpeg/FFmpeg@bf1b838f2a` | zip `fec81ae0...5da2e9`; `ffmpeg.exe` `72a489ec...6445e6aa3`; `ffprobe.exe` `19202b23...cec52f` |

Full values are in `packaging/ffmpeg.lock`.

- Apple positional audio is not solved by any version or bundled tool: FFmpeg's `apac` decoder is
  an unrelated codec ("Marian's A-pac audio"), FFmpeg 9.0.1 only identifies Apple's codec as
  `apple_apac`, and Apple lists its decoder only for macOS 14, iOS/iPadOS/tvOS 17 and visionOS 1
  (`afconvert`, AudioToolbox). The operator therefore also approved the code fallback of
  [0037](0037-preview-handle-undecodable-sample-audio.md).
- Rejected: keeping 6.1.1 (mono MP2 series samples keep failing); 7.1.1 (same failure); 8.1.2
  (fixes mono MP2 and passes the tests, but older than 9.0.1 with no advantage found); an extra
  standalone APAC decoder (none exists for Linux or Windows).

## Acceptance evidence

All probes ran in the session scratchpad, outside the repository; operator files were only read.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Builds available for both platforms | Docker Hub tags of `mwader/static-ffmpeg`; GitHub releases of `GyanD/codexffmpeg` | pass: 7.1.1, 8.1.2 and 9.0.1 published for Linux and Windows |
| Mono MP2 series sample | arxgo's concat-filter command on a generated mono MP2 `.mpg`, per version | 6.1.1 and 7.1.1 exit 234; 8.1.2 and 9.0.1 exit 0 with a 2.0 s clip |
| Apple positional audio | `ffmpeg -i <iPhone .MOV copy> -map 0:a:1 -t 2 -f null -`, per version | valid-negative: every version exits 234 (no decoder); 9.0.1 names `apple_apac`. [FFmpeg apac.c](https://github.com/FFmpeg/FFmpeg/blob/master/libavcodec/apac.c) is "Marian's A-pac audio"; the [FFmpeg Changelog](https://github.com/FFmpeg/FFmpeg/blob/master/Changelog) through 9.0 adds no Apple APAC decoder; Apple's [APAC document](https://developer.apple.com/av-foundation/Apple-Positional-Audio-Codec.pdf) lists platform decoders only |
| arxgo compatibility | `PATH=<build>:$PATH ARXGO_TEST_REQUIRE_TOOLS=1 go test ./internal/media ./internal/archive` and `TestStage2Previews` | 9.0.1 pass; 8.1.2 pass except one helper-process timing test under CPU load, 5/5 in isolation (it does not run FFmpeg) |
| Licence variant and encoders | `ffmpeg -version`, `ffmpeg -encoders` (Linux); `strings ffmpeg.exe` and zip `README.txt` (Windows) | pass: `--enable-gpl --enable-version3`, no nonfree; libx264, aac, libvpx-vp9, libopus, libmp3lame, mpeg4 on both; Windows README "License: GPL v3", source commit bf1b838f2a. Windows executables inspected, not run |
| Corresponding source | `curl -I https://ffmpeg.org/releases/ffmpeg-9.0.1.tar.xz` | pass: HTTP 200 |
| Size | static executables | Linux `ffmpeg` 93 -> 134 MiB, Windows `ffmpeg.exe` 79 -> 98 MiB |

## Audit handoff

None identified beyond [0036](0036-preview-upgrade-bundled-ffmpeg-to-9.md): the Windows 9.0.1
executables still need the runtime check of W8.

## Close or resume

Decision recorded. `upgrade-bundled-ffmpeg-to-9` depended on this record and is accepted in
[0036](0036-preview-upgrade-bundled-ffmpeg-to-9.md); the undecodable-audio fallback is
[0037](0037-preview-handle-undecodable-sample-audio.md).
