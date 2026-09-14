# Upgrade Bundled FFmpeg to 9.0.1

## Task and scope

- Id / capability / checkpoint: `upgrade-bundled-ffmpeg-to-9` / `media-previews` / none (stage 2 is
  accepted; evidence is recorded here)
- State: accepted.
- Source: plan task added after the operator's decision in
  [0035](0035-preview-approve-ffmpeg-9-distribution.md); working tree with the uncommitted stage-2
  review changes of 0033 and 0034.
- Plan counts at start: 11 open tasks (8 agent, 3 human) with this task and
  `handle-undecodable-sample-audio` added.
- Accepted task:

```markdown
#### upgrade-bundled-ffmpeg-to-9

FFmpeg 6.1.1 cannot build a series sample from a source with mono MPEG-1 Layer II audio; the
operator approved bundling FFmpeg 9.0.1, which can.

- Serves: `media-previews` -- [Release bundle](../openspec/stage-2-previews/previews.md#release-bundle)
- Agent status: CLEAR
- Dependencies: [Approve FFmpeg 9.0.1](records/0035-preview-approve-ffmpeg-9-distribution.md).
- User-visible outcome: Bundles and `make ffmpeg` ship FFmpeg 9.0.1, and mono MP2 sources get
  series samples.
- Scope boundary: `packaging/ffmpeg.lock` pins, source notices, live tests preferring the pinned
  tools in `bin/`, a mono MP2 series regression, bundle and documentation updates. No change to the
  encoding table.
- Data and artifact paths: `packaging/`, `test/fixtures/tooltest/`, `internal/media/`,
  `test/integration/`, `docs/`.
- Execution path: `make ffmpeg`, live media and archive tests and `make test-integration` with the
  pinned tools, `make dist` and bundle inspection, real-media round trip on the drone copy.
- Acceptance gates: Downloads verify against the new pins; `ffmpeg -version` is 9.0.1 GPL v3
  without nonfree; mono MP2 series sample passes and fails with 6.1.1; live tests, integration and
  `make ci` pass; bundle notices name 9.0.1.
- Documentation target: `docs/impl/current/media-previews.md`
- Review checkpoint: none; stage 2 is accepted, evidence recorded in the task record.
```

- Amendments: none.

## Implementation

`packaging/ffmpeg.lock` pins FFmpeg 9.0.1: the `mwader/static-ffmpeg:9.0.1` amd64 layer and
executables for Linux, the gyan.dev `ffmpeg-9.0.1-essentials_build.zip` archive, directory and
executables for Windows. `scripts/fetch-ffmpeg.sh` needed no change. Both
`packaging/LICENSES/FFmpeg-SOURCE-*.txt` notices name 9.0.1, the new provenance and
`ffmpeg-9.0.1.tar.xz`.

`tooltest.LookPath` now prefers the pinned tools in the repository's `bin/`, as tool discovery
prefers the executable directory, and falls back to `PATH`; before, live tests ran whatever
`ffmpeg` was on `PATH` (here Ubuntu's 6.1.1), not the shipped build. The integration tests route
their tool checks through the same helper and put `bin/` first on the built binary's `PATH`.
`TestPackageDist` reads the expected notice version from the lock. The live sample fixtures for
`.mpg` and `.vob` use mono MP2 again, which is the regression for the fixed concat failure.
Specification (release bundle), development guide, test layout, manuals, README and the
[current page](../current/media-previews.md) name 9.0.1. The encoding table is unchanged.

Bundle sizes grow: Linux archive 75.3 -> 110.7 MB, Windows archive 62.2 -> 78.1 MB.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Downloads verify against the new pins | `make ffmpeg` with network; second run | pass: layer and zip downloaded in 10 s, all four executables verified; rerun "up to date"; `shellcheck scripts/fetch-ffmpeg.sh` pass |
| 9.0.1 GPL v3 without nonfree | `bin/ffmpeg -version`, `bin/ffprobe -version`; bundle `./ffmpeg -version` | pass: `version 9.0.1`, `--enable-gpl --enable-version3`; Windows `ffmpeg.exe` strings the same, not run |
| Mono MP2 series regression | `ARXGO_TEST_REQUIRE_TOOLS=1 go test -run TestGenerateSampleLiveContainers ./internal/media` | pass with 9.0.1 (`.mpg`, `.vob`); fails with the 6.1.1 tools placed in `bin/` |
| Live tests | `ARXGO_TEST_REQUIRE_TOOLS=1 go test -count=1 ./internal/...` | pass with the pinned 9.0.1 |
| Integration | `ARXGO_TEST_REQUIRE_TOOLS=1 go test -count=1 -tags integration -v ./test/integration/...` | pass: `TestStage1GeneratedArchive`, `TestStage2Previews`, `TestPackageDist`, crash tests; none skipped |
| Race | `CGO_ENABLED=1 ARXGO_TEST_REQUIRE_TOOLS=1 go test -race -count=1 ./internal/archive ./internal/media` | pass |
| Bundle notices name 9.0.1 | `make dist`; `sha256sum -c` of `dist/SHA256SUMS` and the extracted Linux bundle; notice of both archives | pass; bundles built from the uncommitted tree (`2bf001b-dirty`) were removed afterwards |
| Real media with 9.0.1 | drone-copy round trip (0033 declared run) with the pinned tools beside `arxgo` | pass: split 40 videos, 80 previews, 0 failed; rerun exit 0; restore exit 0; manifests and SHA-256 identical |
| `make ci` passes | `PATH=/usr/local/go/bin:$PATH make ci` | pass on Linux after the plan transition; the first run stopped only at `lint-spec-plan` while this task was still open |

## Audit handoff

`AUD-upgrade-bundled-ffmpeg-to-9-1`: nonblocking, Windows only. The 9.0.1 `ffmpeg.exe` and
`ffprobe.exe` were hashed and inspected but not run. Next check: W8 of the
[Windows verification scenario](../../guide/windows-verification.md#deferred-items), which already
covers the bundled tools (`AUD-implement-release-bundle-with-ffmpeg-1`). Owner: that scenario;
deferred.

## Close or resume

All Linux gates pass. The task left the plan; [media previews](../current/media-previews.md) links
this record. Plan counts after: 9 open tasks (6 agent, 3 human).
