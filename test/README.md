# Test Layout

Root-level `test/` holds external test applications and test data, following the
[Go project-layout `/test` convention](https://github.com/golang-standards/project-layout/tree/master/test).

| Path | Purpose |
| --- | --- |
| `integration/` | Cross-package and end-to-end tests using public interfaces; use the `integration` build tag for heavier future proofs. |
| `apps/` | Standalone helper applications used only by external-process tests, when needed. |
| `fixtures/` | Reusable test-only Go builders and in-memory mocks (`crashtest`, `testmp4`, `tooltest`); never import these from production code. |
| `testdata/` | Captured mock data and golden outputs; Go ignores this directory during `go test ./...`. Markdown goldens are product samples, not repository docs (`make lint-doc-links` skips them). |

Keep unit and white-box component tests beside their packages. A test requiring unexported hooks
stays package-local. Generate runtime archive trees and media in `t.TempDir()`; do not commit
binary media. Static text fixtures in `testdata/` are reviewable and do not contain secrets.
`go test ./...` runs the current integration tests; `make test-integration` also runs the tagged
end-to-end proofs as they are added. `make ci` includes the current integration tests.

Live ffmpeg/ffprobe tests find the tools with `tooltest.LookPath`, which skips with the reason
when a tool is missing. Set `ARXGO_TEST_REQUIRE_TOOLS=1` for a local run that must exercise them:
a missing tool then fails the test instead of skipping it. GitHub CI installs no ffmpeg and does
not set the variable.
