# Release bundle with FFmpeg

## Task and scope

- Id / capability / checkpoint: `implement-release-bundle-with-ffmpeg` / `media-previews` / `review-stage-2-previews`
- State: accepted
- Source: plan task; start revision `d225b52`, clean working tree.
- Plan counts at start: 11 open (8 agent, 3 human); next eligible `implement-release-bundle-with-ffmpeg`.
- Accepted task:

```markdown
#### implement-release-bundle-with-ffmpeg

Package `arxgo` with pinned ffmpeg/ffprobe builds per platform.

- Serves: `media-previews` -- [Release bundle](../openspec/stage-2-previews/previews.md#release-bundle)
- Agent status: RUN NEEDED
- Dependencies: [Preview integration](records/0030-preview-integrate-previews-into-split-and-restore.md); [Approve ffmpeg distribution](records/0002-preview-approve-ffmpeg-distribution.md).
- User-visible outcome: Operators download one archive per platform that runs previews with no
  installation.
- Scope boundary: `make dist` packaging on top of the approved pins in `packaging/ffmpeg.lock` and
  the verified download in `scripts/fetch-ffmpeg.sh` (`make ffmpeg`), `SHA256SUMS`, GPLv3 licence
  text and source offer per bundle, CI release job on tags. Each bundle ships `.env.example`
  (operators copy it to `.env`) and the matching practical manual (`docs/guide/manual-linux.md`
  or `docs/guide/manual-windows.md`) next to the executable, and never a `.env`. Binaries never
  committed. Correct the `make ffmpeg` row of the development guide, which still names
  `tools/fetch-ffmpeg.sh` (`AUD-refactor-repository-layout-1`).
- Data and artifact paths: `packaging/`, `scripts/fetch-ffmpeg.sh`, `make/`, `Makefile`,
  `.github/workflows/release.yml`, `docs/guide/manual-linux.md`,
  `docs/guide/manual-windows.md`, `dist/` (ignored).
- Execution path: Declared run on Linux: `make dist` for linux/amd64 and windows/amd64 with network
  access, then smoke-test the Linux bundle (`arxgo split --image start` on a generated video). The
  Windows bundle smoke test is step W8 of the Windows scenario.
- Acceptance gates: Checksums verified before packaging; Linux bundle smoke test passes; both
  bundle file lists (`arxgo`/`arxgo.exe`, `ffmpeg`/`ffmpeg.exe`, `ffprobe`/`ffprobe.exe`, licences,
  `SHA256SUMS`, `.env.example`, and the matching platform manual) are checked on Linux; licence
  files match the approved variant; checksum mismatch fails the build; no bundle contains a `.env`
  file, even when `bin/.env` exists on the build host.
- Documentation target: `docs/guide/development.md`, `docs/guide/manual-linux.md`,
  `docs/guide/manual-windows.md`
- Review checkpoint: `review-stage-2-previews`.
```

- Amendments: none.

## Implementation

`make/dist.mk` adds `make dist` on top of the existing shared `build-all` recipe and pinned
`make ffmpeg` downloader. `scripts/package-dist.sh` checks each FFmpeg executable against
`packaging/ffmpeg.lock` before writing either archive. It rejects missing, symlinked or
non-executable inputs and copies only the named release files into private staging directories.
Each archive gets internal `SHA256SUMS`; `dist/SHA256SUMS` covers the two archives. The explicit
copy list excludes `bin/.env`, including when that file exists on the build host. Build and
download outputs remain ignored, never committed.

Each bundle carries arxgo's MIT licence, full GPL v3 text copied verbatim from the host's
`/usr/share/common-licenses/GPL-3`, and a platform-specific FFmpeg provenance/source notice.
The notices link FFmpeg 6.1.1 corresponding source and the build recipe or release.
`.github/workflows/release.yml` runs `make ci`, then `make dist` for `v*` tags, checks the
archive sums and creates a GitHub release with the two bundles and their outer sums. No release
was published in this task.

The fixture test in `test/integration/package_dist_test.go` builds fake tool binaries and a
fake lock under `t.TempDir()`; it exercises both archive file lists, checksums, a private
`bin/.env`, the selected source notice and the checksum-mismatch failure without network.
The development guide now points `make ffmpeg` to `scripts/fetch-ffmpeg.sh` and documents
`make dist` and the tag workflow. Both operator manuals explain the bundled licences.
[Current media previews](../current/media-previews.md#release-bundles) links this record.

The existing pinned downloader and cross-platform build recipe were reused. The release
packager accepts an alternate lock path only as an explicit argument for fixture tests;
`make dist` always uses the repository lock. Windows runtime remains deferred to W8.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Fresh approved tools | `scripts/fetch-ffmpeg.sh <temporary-dir> linux/amd64 windows/amd64` with network access | Pass: Docker layer and Windows ZIP downloaded, archives and four extracted tools matched pins; temporary directory removed. |
| Real release build | `GOCACHE=/tmp/arxgo-go-cache make dist` | Pass on Linux host: Linux and Windows amd64 bundles produced from pinned tools; `bin/.env` existed. FFmpeg inputs were already cached for this run; fresh fetch was checked separately above. |
| Outer archive checksums | `(cd dist && sha256sum -c SHA256SUMS)` | Pass for both archives after final `make dist`. |
| Bundle contents and internal checksums | Extract both archives to a temporary directory; `(cd <each> && sha256sum -c SHA256SUMS)`; `find . -type f` | Pass: each contained exactly its arxgo, ffmpeg, ffprobe, `.env.example`, matching manual, `LICENSES/{arxgo-MIT,GPL-3.0,FFmpeg-SOURCE}.txt` and `SHA256SUMS`; neither contained `.env`. |
| Linux preview smoke | Bundled `ffmpeg -f lavfi -i testsrc2=...:duration=2` makes MP4; bundled `arxgo split --image start --min-free 0` on temporary archive/video roots | Pass: exit 0, moved MP4 and nonempty `clip-img01.png`; bundled `arxgo version` worked. Generated fixture only, no operator archive touched. |
| Licence variant | `bin/ffmpeg -version`, `strings bin/ffmpeg.exe`, `file` on both platform tools, `cmp packaging/LICENSES/GPL-3.0.txt /usr/share/common-licenses/GPL-3` | Pass: Linux and Windows build strings show `--enable-gpl --enable-version3`, no `--enable-nonfree`; Linux tools are static PIE, Windows tools PE32+ amd64; GPL text is verbatim. Windows binaries were inspected, not run. |
| Checksum mismatch and `.env` regression | `GOCACHE=/tmp/arxgo-go-cache go test ./test/integration -run '^TestPackageDist$' -count=1 -v` | Pass: fixture archives verify, private `bin/.env` is absent, altered `ffmpeg` fails before packaging. |
| Shell and diff checks | `shellcheck scripts/package-dist.sh`; `bash -n scripts/package-dist.sh`; `git diff --check` | Pass after fixing two shellcheck SC2015 findings. |
| Live integration | `PATH="$PWD/bin:$PATH" ARXGO_TEST_REQUIRE_TOOLS=1 GOCACHE=/tmp/arxgo-go-cache make test-integration` | Pass on Linux with FFmpeg 6.1.1. |
| Required checks | `GOCACHE=/tmp/arxgo-go-cache make ci` | Pass: format, vet including Windows, tests, both static builds, plan and doc-link lints. Initial run failed plan lint while the new record and source task coexisted; removing the completed task from the plan resolved it. |
| Tag release and Windows runtime | GitHub tag workflow; W8 Windows scenario | Not run: no tag was pushed and no Windows host was used. Workflow is configured; Windows was cross-built/vetted and its bundle inspected on Linux. |

## Audit handoff

`AUD-implement-release-bundle-with-ffmpeg-1`: nonblocking. Windows executables and the
extracted ZIP have not run on Windows. Linux `file`/build-string inspection and cross-vet cover
format and compilation, but not Windows tool discovery or preview runtime. Next check: execute
the bundled smoke test in [W8](../../guide/windows-verification.md#scenario); owner: deferred
Windows verification scenario, as required by the plan. The earlier distribution decision's
availability note is addressed by publishing the pinned executables in release bundles; its
Windows runtime note is routed here.

## Close or resume

All Linux acceptance gates passed; Windows runtime and the first tag-triggered release remain
unrun external checks. The task was removed from `plan.md`, the dependency of
`review-stage-2-previews` now links this record, the record index and current media-preview page
were updated, and `media-previews` remains `planned` until its review checkpoint. Plan counts
after: 10 open (7 agent, 3 human); next eligible task: `review-stage-2-previews`.
