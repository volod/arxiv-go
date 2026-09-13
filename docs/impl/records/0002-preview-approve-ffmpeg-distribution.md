# Approve ffmpeg Distribution

## Task and scope

- Id / capability / checkpoint: `approve-ffmpeg-distribution` / `media-previews` / none (human task)
- State: accepted (operator decision, 2026-09-13)
- Source: plan task `approve-ffmpeg-distribution` (Human-Assisted, HUMAN-GATED). Operator request
  in the working session on branch `ag-00-init-specs` (base `c155c76`, harness files uncommitted):
  "For @plan.md task #### approve-ffmpeg-distribution, use FFmpeg version 6.1.1-3ubuntu5+esm13.
  Copyright (c) 2000-2023 the FFmpeg developers. Please create a make target that downloads
  statically linked, standalone ffmpeg and ffprobe files for the defined amd architectures into
  bin, and then mark resolved #### approve-ffmpeg-distribution."
- Plan counts at start: 33 open (29 agent, 4 human); next agent task `implement-cli-contract`.
- Accepted task:

```markdown
#### approve-ffmpeg-distribution

Choose which ffmpeg/ffprobe builds may be redistributed with `arxgo` and under which licence.

- Serves: `media-previews` -- [Release bundle](../openspec/stage-2-previews/previews.md#release-bundle)
- Human status: HUMAN-GATED
- Dependencies: none.
- Requested input or decision: Select the build sources and GPL or LGPL variants per platform,
  confirm the licence notice and source-offer obligations, or decide to ship without bundled tools.
- Unblocks: `implement-release-bundle-with-ffmpeg`.
```

- Amendments: none to the task text. The operator's stated version needed an interpretation,
  confirmed by the operator (see Decision).

## Decision

The human decision was made by the operator; the agent prepared the options, verified them and
recorded the outcome. It did not choose the version or the licence variant itself.

- Version: FFmpeg 6.1.1. The operator named `6.1.1-3ubuntu5+esm13`, the version string of
  their local Ubuntu 24.04 package. That package is dynamically linked (`--enable-shared`,
  distro libraries) and cannot be shipped as a standalone file, so the upstream 6.1.1 release was
  taken as the version to match.
- Variant: GPL v3 (`--enable-gpl --enable-version3`, no `--enable-nonfree`) on both platforms,
  the same GPL family as the operator's Ubuntu build. It is required for `libx264`, which the
  [encoding table](../../openspec/stage-2-previews/previews.md#encoding-by-container) uses.
- Sources (no pinnable static 6.1.1 Linux tarball exists: johnvansickle stops at 6.0.1, BtbN
  offers only 6.1.2+ snapshots that it prunes, ffbinaries has 6.1.0, and the `b6.1.1` Linux asset
  of `eugeneware/ffmpeg-static` actually contains 7.0.2). The operator chose the
  following when asked:

| Platform | Build | Pin |
| --- | --- | --- |
| `linux/amd64` | `mwader/static-ffmpeg:6.1.1`, amd64 manifest `sha256:1a986bde...920bf6`; single layer pulled from the Docker Hub registry by digest, no docker client | layer `sha256:6a50276a...4170d6`; `ffmpeg` `dcf4c806...53edc0`; `ffprobe` `aef7126f...cf9f1` |
| `windows/amd64` | gyan.dev `ffmpeg-6.1.1-essentials_build.zip` from `GyanD/codexffmpeg` release `6.1.1`; source commit `FFmpeg/FFmpeg@e38092ef93` | zip `742e32fc...b94cc5`; `ffmpeg.exe` `04e13079...4ad00`; `ffprobe.exe` `3a7e2dc0...51ba4` |

  Full values are in `packaging/ffmpeg.lock`.
- Obligations confirmed for `implement-release-bundle-with-ffmpeg`: ship the GPL v3 licence text
  with the binaries, state provenance (the recipe `wader/static-ffmpeg` tag `6.1.1` for Linux, the
  gyan.dev README and source commit for Windows), and include a written offer or link to the
  corresponding FFmpeg 6.1.1 source. `arxgo` itself is invoked as a separate process and does not
  link FFmpeg, so its own licence is unaffected.
- Shipping without bundled tools was not chosen.

## Implementation

- `packaging/ffmpeg.lock`: version, licence, sources and SHA-256 pins for the downloaded
  archive or layer and for each extracted binary, as `key=value` lines.
- `tools/fetch-ffmpeg.sh <out-dir> <os/arch>...`: reads the lock and downloads with `curl`. Linux:
  anonymous Docker Hub pull token, blob by digest, `tar -xz` of `ffmpeg` and `ffprobe` only.
  Windows: release zip, `unzip -j` of the two `.exe` files only. It verifies the archive and each
  binary before replacing anything in `<out-dir>` and skips platforms whose binaries already
  match. Unknown platforms fail. Portable to Git Bash (`sha256sum`, else `shasum -a 256`).
- `Makefile`: `make ffmpeg` runs the script for `$(PLATFORMS)` into `bin/`, giving
  `bin/ffmpeg`, `bin/ffprobe`, `bin/ffmpeg.exe` and `bin/ffprobe.exe` next to the `arxgo`
  binaries, where [tool discovery](../../openspec/stage-1-core/metadata.md#tool-discovery) looks
  first. Not part of `make ci`, so GitHub CI stays ffmpeg-free.
- Spec: the [release bundle](../../openspec/stage-2-previews/previews.md#release-bundle) section
  now names the approved builds and obligations. The plan task was removed, and
  `implement-release-bundle-with-ffmpeg` now depends on this record and reuses the lock and script.
  The [development guide](../../guide/development.md#make-targets) documents the target.
- Rejected: `eugeneware/ffmpeg-static` `b6.1.1` (Linux binary is 7.0.2); BtbN n6.1 (not 6.1.1,
  URLs pruned); ffbinaries v6.1 (6.1.0); building 6.1.1 from source in this repository (heavy,
  out of scope for a download target).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Operator decision | session request above; operator selected the mwader 6.1.1 image for Linux | pass: recorded |
| Download and verify | `make ffmpeg` (Linux host, network) | pass: 4 binaries written in about 23 s; all pins verified |
| Idempotent rerun | second `make ffmpeg` | pass: both platforms reported up to date, no download |
| Corrupted local binary | appended a byte to `bin/ffprobe`, reran for `linux/amd64` | pass: re-downloaded; hash back to pin |
| Checksum mismatch | lock copy with a wrong `windows_amd64_archive_sha256` | valid-negative: exit 1, "checksum mismatch", no file written |
| Unknown platform | `tools/fetch-ffmpeg.sh bin darwin/arm64` | valid-negative: exit 1, "no pinned ffmpeg build" |
| Static, correct version (Linux) | `file bin/ffmpeg`; `bin/ffmpeg -version`; `bin/ffprobe -version` | pass: `static-pie linked`; both report `version 6.1.1`; config has `--enable-gpl --enable-version3`, no `nonfree` |
| Required encoders (Linux) | `bin/ffmpeg -hide_banner -encoders` | pass: `libx264`, `mpeg4`, `libvpx-vp9`, `aac`, `libopus`, `libmp3lame`, `png` |
| Windows build | `file bin/ffprobe.exe`; zip `README.txt` | pass for inspection: PE32+ x86-64; README states 6.1.1 static, GPL v3, lists `libx264`, `libvpx`, `libopus`, `libmp3lame`. not-run: executables not run (no Windows host or wine) |
| Independent hash cross-check | GitHub asset digests of `eugeneware/ffmpeg-static` `b6.1.1` `ffmpeg-win32-x64`/`ffprobe-win32-x64` | pass: identical to the gyan.dev `.exe` pins |
| Script lint | `shellcheck tools/fetch-ffmpeg.sh` | pass |
| Required checks | `make ci` (Go 1.27.1, Linux) incl. `lint-spec-plan`, `lint-doc-links`; `make plan-status` | pass; 32 open (29 agent, 3 human) |

## Audit handoff

- `AUD-approve-ffmpeg-distribution-1`: nonblocking. The Linux source is a Docker Hub image. Digest
  pins cannot change, but the image could be deleted or anonymous pulls restricted, and the image
  holds no licence text. Next check: the release bundle adds the GPL v3 text and source offer, and
  decides whether to mirror the pinned binaries as release assets. Owner:
  `implement-release-bundle-with-ffmpeg`.
- `AUD-approve-ffmpeg-distribution-2`: nonblocking. The Windows binaries were inspected and hashed
  but not executed. Next check: run `bin/ffmpeg.exe -version` and the bundle smoke test on
  Windows. Owner: `implement-release-bundle-with-ffmpeg`.
- `AUD-approve-ffmpeg-distribution-3`: nonblocking. `make` evaluates `$(GO) env GOOS` for
  `HOST_EXE` on every target, so `make ffmpeg` prints `go: command not found` when Go is missing,
  even though it works without Go. Next check: make `HOST_EXE` lazy or silence the probe. Owner:
  `implement-cli-contract` (next task that touches the Makefile).

## Close or resume

Operator decision recorded and the download target is verified on Linux. The task was removed from
the plan; `implement-release-bundle-with-ffmpeg` links this record. Capability `media-previews`
stays `planned`. Plan counts after: 32 open (29 agent, 3 human); next agent task
`implement-cli-contract`.
