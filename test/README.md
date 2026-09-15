# Test Layout

Root-level `test/` holds external test applications and test data, following the
[Go project-layout `/test` convention](https://github.com/golang-standards/project-layout/tree/master/test).

| Path | Purpose |
| --- | --- |
| `integration/` | Cross-package and end-to-end tests using public interfaces; use the `integration` build tag for heavier future proofs. |
| `apps/` | Standalone helper applications used only by external-process tests, when needed. |
| `fixtures/` | Reusable test-only Go builders and mocks (`crashtest` fake transactions and filesystem resolver, `testmp4`, `tooltest`); never import these from production code. |
| `testdata/` | Captured mock data and golden outputs; Go ignores this directory during `go test ./...`. Markdown goldens are product samples, not repository docs (`make lint-doc-links` skips them). |

Keep unit and white-box component tests beside their packages. A test requiring unexported hooks
stays package-local. Generate runtime archive trees and media in `t.TempDir()`; do not commit
binary media. Static text fixtures in `testdata/` are reviewable and do not contain secrets.
`go test ./...` runs the untagged integration tests and `make ci` includes them. `make
test-integration` also runs the `integration`-tagged end-to-end proofs: the archive round-trip proof
`TestArchiveSplitRestoreRoundTrip` (`archive_*_test.go`) and the preview proof `TestPreviewSplitRestoreRoundTrip`
(`previews_roundtrip_test.go`, skipped without ffmpeg). They build `arxgo` into a temporary directory and drive
it as subprocesses; CI runs them after `make ci`. It logs its seed: `ARXGO_TEST_SEED=<n>`
replays archive content and kill points, and `ARXGO_TEST_VIDEO_PARENT=<dir>` puts the video
archive in that directory (for example on another device). Tagged tests stay portable (no shell,
`os.Process.Kill`, `.exe` from `GOOS`); `make vet-windows` type-checks them for Windows.

Live ffmpeg/ffprobe tests find the tools with `tooltest.LookPath`: the pinned build that `make
ffmpeg` writes to `bin/` when present, otherwise `PATH`. It skips with the reason when a tool is
missing. Set `ARXGO_TEST_REQUIRE_TOOLS=1` for a local run that must exercise them:
a missing tool then fails the test instead of skipping it. GitHub CI installs no ffmpeg and does
not set the variable.
