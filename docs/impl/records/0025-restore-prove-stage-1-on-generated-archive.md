# Stage-1 proof on a generated archive

## Task and scope

- Id / capability / checkpoint: `prove-stage-1-on-generated-archive` / `video-restore` /
  [Stage-1 integrity review](0021-restore-review-stage-1-integrity.md) addendum
- State: accepted.
- Source: plan task; revision `f1ec66f`, worktree clean at start. Host: Linux amd64, Go 1.27.1,
  ffmpeg/ffprobe 6.1.1 on `PATH`, NVIDIA GPU (not used: the proof encodes its clips with
  ffmpeg's built-in `mpeg4`, `mpeg2video`, `aac` and `mp2` encoders so any ffmpeg build works).
- Plan counts at start: 17 open tasks (14 agent, 3 human); next eligible
  `prove-stage-1-on-generated-archive`.
- Accepted task:

````markdown
#### prove-stage-1-on-generated-archive

Run the complete stage-1 workflow end to end on a generated archive on Linux.

- Serves: `video-restore` -- [Evaluation and acceptance](../openspec/spec.md#evaluation-and-acceptance)
- Agent status: CLEAR
- Dependencies: [Stage-1 integrity review](records/0021-restore-review-stage-1-integrity.md).
- User-visible outcome: A single integration test proves scan, split, kill, resume, restore and the
  round-trip gate through the built `arxgo` binary.
- Scope boundary: Build the binary in a test temporary directory (so a developer's `bin/.env` is
  never read) and run it with a scrubbed `ARXGO_*` environment, generate a multi-level archive (hundreds of files,
  generated MP4 headers and optional ffmpeg clips), run operations as subprocesses, kill the split
  process mid-run, resume, restore, compare tree manifests. Not run against operator data. The
  test stays portable (no shell, `os.Process.Kill`, `.exe` suffix from `GOOS`) so step W7 of the
  Windows scenario can run it unchanged; running it on Windows is not part of this task.
- Data and artifact paths: `test/integration/stage1_test.go` (build tag `integration`),
  `make test-integration`, CI job on `ubuntu-latest`.
- Execution path: `go test -tags integration ./test/integration/...`; manifest of path/size/
  mtime/SHA-256 before split and after restore.
- Acceptance gates: Manifests equal; registries and stubs validate against contracts; process kill
  at three random points (seeded, seed logged) converges; exit codes match the contract; CI passes
  on `ubuntu-latest`; `GOOS=windows go vet -tags integration ./test/integration/...` passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: [Stage-1 integrity review](records/0021-restore-review-stage-1-integrity.md) addendum.
````

- Amendments: none to the task. The finding below narrowed the `is_video` refinement rule in
  [registry](../../openspec/stage-1-core/registry.md#type-detection); the spec row previously named
  ISO BMFF only, while 0016 had already applied the rule to ffprobe results.

## Implementation

### Test (`test/integration/`, build tag `integration`)

- `stage1_test.go`: `TestStage1GeneratedArchive` scenario and the refusal exit codes.
- `stage1_archive_test.go`: seeded archive generator, optional ffmpeg clips, manifest of path,
  size, mtime (ns) and SHA-256 of every file plus every directory, excluding only arxgo's outputs
  in the root (`.arxgo/`, `arxgo-registry.csv`, `arxgo-videos.*`).
- `stage1_proc_test.go`: `go build -trimpath -buildvcs=false` of `./cmd/arxgo` into
  `t.TempDir()` with `CGO_ENABLED=0` and an `.exe` suffix from `runtime.GOOS`, `ARXGO_*`
  scrubbed from the child environment, subprocess runs with a 5-minute timeout, and
  `runKilled`, which polls `.arxgo/current` and the run's `wal.jsonl` every 200 us and calls
  `os.Process.Kill` once this process appended the seeded number of complete records. A process
  that finishes before its kill fails the test, so a kill point can never pass unexercised.
- `stage1_check_test.go`: contract checks for the file registry, video registry (both copies
  byte-identical, one row per video, `moved`, `copy`, size, SHA-256, run id), stubs (front matter,
  `.arxgo.md` fallback next to a foreign `.md` file and a directory), retired registries after
  restore, run reports, locks and part files.

Scenario: exit 2, 3 and 4 refusals (archive unchanged, no lock) -> `scan` -> `split --transfer
copy --verify hash` killed at `1 + rng.Intn(7V/3)` records -> rerun without `--force-unlock` exits 5
naming the lock -> resumed split killed at `1 + rng.Intn(remaining/2)` -> resumed split completes
(same run, `resumed` report) -> split rerun exit 0 -> `restore --transfer copy --verify hash` killed
at `1 + rng.Intn(6V/2)` -> `restore --new-run` (auto transfer; recovers the replaced run) exit 0 ->
restore rerun exit 0 -> manifests equal. The copy transfer keeps each transaction long enough for a
kill between records on any disk; the kill thresholds leave at least half the remaining work.

Decisions: kills are triggered by WAL progress rather than wall-clock delays, so a seed reproduces
the same kill targets on fast and slow hosts (a kill may overshoot by a record, which the log shows).
No scan-phase kill: its landing cannot be verified from outside the process, and scan resume after
every entry is proven in `internal/archive`. Exit 6 and 130 are not driven here: exit 6 needs an
unreadable file or a conflict (not portable, and a conflict breaks the empty-video-archive gate);
both are covered by `internal/archive` and `internal/cli` tests. `ARXGO_TEST_SEED` replays a run;
`ARXGO_TEST_VIDEO_PARENT` puts the video archive on another device; `ARXGO_TEST_REQUIRE_TOOLS=1`
fails instead of generating no ffmpeg clips.

Make and CI: `make test-integration` adds `-count=1`; `make vet` and `make vet-windows` also vet
`-tags integration ./test/integration/...`; the `ubuntu-latest` job runs `make test-integration`
after `make ci`.

### Defect found and repaired

Seed 222 failed deterministically: `top.avi` (random bytes, so a signature-less `.avi`) was
missing from `arxgo-videos.csv` and had never been moved, while every exit code was 0 and the
round trip matched. With `--metadata media`, ffprobe 6.1.1 reads those bytes as `lrc` (LRC lyrics,
one subtitle stream, `probe_score` 5). `scanPipeline` cleared `is_video` for any successful parse
with no video stream, so a damaged video silently stayed in the archive and was never reported,
contradicting the [media metadata page](../current/media-metadata.md), which described an
audio-only rule. Repair (`internal/archive/scan_pipeline.go`): only a result with at least one
audio stream and no video stream clears the flag. Regression: `damaged.avi` in
`TestScanFFprobeMetadataAndISOFallback` with the captured `test/testdata/ffprobe/lrc-misprobe.json`
fails on `f1ec66f` (`is_video=false`, not a candidate) and passes now; the stage-1 proof with seed
222 fails on the old rule at the file-registry check and passes now. The test now also scans in the
same metadata mode as split and checks the video row set by path, so a missing video is named.

Compatibility: a file that parses with neither audio nor video streams (for example a video whose
container is damaged beyond recognition) is now moved by split like any other extension-matched
video instead of being kept silently. Audio-only files are unchanged.

Current pages: [video restore](../current/video-restore.md#stage-1-proof),
[media metadata](../current/media-metadata.md), [index](../current.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Manifests equal | `TestStage1GeneratedArchive` final `diffManifests` (365 files and directories, path, size, mtime ns, SHA-256) | pass, Linux, generated data |
| Registries and stubs validate against contracts | `checkFileRegistry`, `checkSplitOutputs`, `checkStub`, `checkRestoreOutputs`, `runReport` | pass |
| Kill at three seeded points converges, seed logged | two split kills and one restore kill per run; `seed N (replay with ARXGO_TEST_SEED=N)` | pass: 30 verbose seeds landed after every WAL step (split `begin` 10, `verified` 15, `placed` 12, `stubbed` 8, `source_removed` 7, `commit` 8; restore `begin` 4, `verified` 8, `placed` 7, `stub_removed` 5, `commit` 6) |
| Robustness of kill timing | seeds 222 and 300-339 with and without ffmpeg on `PATH` (82 runs); seeds 400-431 with 16 runs in parallel; seeds 100-124 both modes (50 runs, before the repair: no failure) | pass after the repair; seed 222 failed before it (defect above) |
| Cross-device video archive | `ARXGO_TEST_REQUIRE_TOOLS=1 ARXGO_TEST_VIDEO_PARENT=/dev/shm`, seeds 4242, 777, 91, 1001, 1002 | pass; restore takes the copy+delete path |
| Exit codes match the contract | 2, 3 (with download link), 4, 5 (stale lock names the lock), 0 for completed runs and reruns | pass; 6 and 130 covered by package tests only |
| CI passes on `ubuntu-latest` | `.github/workflows/ci.yml` step `make test-integration` | not-run here: no push in this task. Local equivalent without ffmpeg on `PATH` passes |
| `GOOS=windows go vet -tags integration ./test/integration/...` | `make vet-windows` (in `make ci`) | pass; Windows runtime is W7 of the deferred scenario |
| `make ci` | `PATH=/usr/local/go/bin:$PATH make ci` | pass |
| Repair regression and neighbors | `go test ./internal/archive ./internal/media ./internal/scanner`; `CGO_ENABLED=1 go test -race ./internal/archive ./internal/scanner`; `ARXGO_TEST_REQUIRE_TOOLS=1 go test ./internal/media` | pass |

## Audit handoff

- `AUD-prove-stage-1-on-generated-archive-1`: nonblocking. The GitHub `ubuntu-latest` run of
  `make test-integration` has not been observed; the job installs no ffmpeg, so CI proves the
  file-metadata variant only. Next check: the first CI run after this change is pushed. Owner:
  `review-stage-2-previews`, which also extends this test with preview flags through
  `integrate-previews-into-split-and-restore`.

Incoming note `AUD-implement-write-ahead-log-and-recovery-2` (formal real-process kill gate, left
here by 0021): closed by the kill evidence above. No Windows-only concern beyond W7, which already
lists this test.

## Close or resume

All gates pass except the observed GitHub CI run (routed above). The task is removed from the plan
and its references link this record. `video-restore` stays `planned` in the capability registry:
its human task `approve-stage-1-on-operator-archive-copy` is still open (`make lint-spec-plan`
rejects a shipped capability with open tasks). The
[Stage-1 integrity review](0021-restore-review-stage-1-integrity.md#addendum-stage-1-proof) has the
addendum. Plan counts after: 16 open tasks (13 agent, 3 human).
