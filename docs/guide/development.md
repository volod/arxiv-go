# Development Guide

## Requirements

- Go 1.27 or newer (`go version`). No cgo toolchain is needed.
- GNU Make. On Windows use Git Bash, MSYS2 or `choco install make`; every target also documents
  the plain `go` command it runs.
- Optional, local only: `ffmpeg`/`ffprobe` to run media tests. GitHub CI does not install them;
  those tests skip there. `make ffmpeg` downloads the approved static FFmpeg 9.0.1 builds into
  `bin/` (next to `arxgo`, where tool discovery looks first). Live tests use those pinned tools
  when present, as tool discovery does, and otherwise `ffmpeg`/`ffprobe` on `PATH`; the
  integration tests also put `bin/` first on the built binary's `PATH`. The target needs network access, `curl`, `tar`, `gzip` and
  `unzip`, and is not part of `make ci`. A local run that must exercise the live media tests sets
  `ARXGO_TEST_REQUIRE_TOOLS=1`, so a missing tool fails instead of skipping.

## First-time setup

```bash
make setup
```

`make setup` builds `bin/arxgo` and `bin/arxgo.exe`, downloads the pinned `ffmpeg`/`ffprobe`
into `bin/` (network access required), creates `bin/.env` from `.env.example` if it does not
exist yet (mode 0600, never overwritten), and prints how to configure and run. Rerunning it is
safe. It needs Go on `PATH` and exits with install guidance if Go is missing.

`bin/.env` is the optional [environment file](../openspec/stage-1-core/cli.md#environment-file)
that `arxgo` reads next to its executable. It holds `ARXGO_*` settings and, with cloud publishing,
credentials. Flags and process environment variables override it. Declared runs (`RUN NEEDED`
tasks) keep their settings and credentials there instead of in shell history. Unit tests never
read it: the test binary lives in a temporary build directory, and tests inject their own file.
After pulling changes, compare `bin/.env` with `.env.example` for new variables.
`TestEnvExampleListsEveryFlag` fails when a flag is added without a matching `.env.example` line.

## Make targets

| Target | Runs | Purpose |
| --- | --- | --- |
| `make setup` | `build-all`, `ffmpeg`, `env`, then prints usage | Ready-to-run `bin/` in one command (network) |
| `make env` | `cp .env.example bin/.env` unless it exists | Optional settings file next to the binary; never overwrites |
| `make build` | `CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -trimpath -ldflags ... -o bin/arxgo ./cmd/arxgo` | Static Linux amd64 binary with version stamp |
| `make build-all` | `build`, then `CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -trimpath -ldflags ... -o bin/arxgo.exe ./cmd/arxgo` | `bin/arxgo` and `bin/arxgo.exe` |
| `make ffmpeg` | `bash scripts/fetch-ffmpeg.sh bin linux/amd64 windows/amd64` | Pinned static ffmpeg/ffprobe 9.0.1 into `bin/` (checksums from `packaging/ffmpeg.lock`; network) |
| `make dist` | `build-all`, `ffmpeg`, then `scripts/package-dist.sh` | Linux `.tar.gz` and Windows `.zip` in `dist/`, each with checksums, matching manual, `.env.example`, GPL v3 text and FFmpeg source notice; also writes archive checksums to `dist/SHA256SUMS` (network) |
| `make test` | `go test ./...` | Package tests and untagged integration tests |
| `make test-race` | `go test -race ./...` | Race detector (needs cgo on the host; not part of `ci`) |
| `make test-integration` | `go test -count=1 -tags integration ./test/integration/...` | Integration tests and tagged end-to-end proofs, including the archive round-trip proof (`TestArchiveSplitRestoreRoundTrip`) and the stage-4 CATIA proof (`TestCatiaSplitRestoreRoundTrip`); a CI step after `make ci` |
| `make fmt` | `gofmt -w` | Format |
| `make fmt-check` | `gofmt -l` | Fail on unformatted files |
| `make vet` | `go vet ./...`, also with `-tags integration` for `./test/integration/...` | Static checks |
| `make vet-windows` | `GOOS=windows GOARCH=amd64 go vet ./...`, also with `-tags integration` for `./test/integration/...` | Type-checks Windows build-tagged code and tests on Linux |
| `make lint-spec-plan` | `go run ./tools/plancheck lint` | Registry, plan and records agree |
| `make lint-doc-links` | `go run ./tools/plancheck links` | Relative Markdown links and anchors resolve; `testdata` skipped |
| `make plan-status` | `go run ./tools/plancheck status` | Open task counts and next eligible task |
| `make coverage` | `go test -coverprofile` | Diagnostic coverage report only |
| `make ci` | fmt-check, vet, vet-windows, test, build-all, lint-spec-plan, lint-doc-links | Required before accepting a task (Linux) |
| `make clean` | removes `bin/` contents except `bin/.env`, `dist/`, coverage files | Keeps local settings and credentials |

## Conventions

- Production code lives in `internal/`; `cmd/arxgo/main.go` only calls `cli.Run`.
- Domain packages return errors; only `internal/cli` maps them to exit codes and writes to
  stdout/stderr. Use `log/slog`, never `fmt.Print*`, for runtime messages in domain packages.
- Platform code goes in `_unix.go` / `_windows.go` files inside `internal/fsops` (or the package
  the architecture assigns); keep both variants compiling (`make build-all`, `make vet-windows`).
- Test gates are Linux only. Windows-only tests skip with a reason on other systems; they run
  only in the deferred [Windows verification scenario](windows-verification.md).
- Files aim for at most about 300 lines; split at real seams, not by line count alone.
- Unit and white-box component tests sit next to code (`foo_test.go`). Cross-package integration
  tests, end-to-end tests, external test applications, reusable test helpers, and committed mock or
  golden data live under root-level [`test/`](../../test/README.md), following the
  [Go project-layout `/test` convention](https://github.com/golang-standards/project-layout/tree/master/test).
  Tests build runtime fixtures in `t.TempDir()` and never touch the network. Media fixtures are
  generated in tests; tests that need ffmpeg skip with a reason when it is missing.
- New dependencies must be listed in the [dependency table](../openspec/spec.md#dependencies) and
  be pure Go; commit `go.mod` and `go.sum` together.
- ASCII in code, logs and docs unless a test needs Unicode input.

## Versioning

The version is the hand-edited `VERSION` file at the repository root, one
[Semantic Versioning](https://semver.org) line such as `0.1.0`. `make build`, `make build-all` and
`make dist` stamp it into `arxgo version` and the bundle names (`arxgo-0.1.0-linux-amd64.tar.gz`); a
plain `go build` reports `dev`. Builds never change it: a developer bumps it in the change that
prepares a release (MAJOR for incompatible CLI or file-format changes, MINOR for new behavior, PATCH
for fixes; `0.y.z` while the project is pre-1.0). `TestVersionFileIsSemver` rejects any other
content. A release is the tag `v<VERSION>`; the release workflow fails when the tag differs.

## CI

`.github/workflows/ci.yml` runs `make ci` on `ubuntu-latest` for pushes to `main` and pull
requests; it is the only CI gate. It does not install `ffmpeg` or `ffprobe`. Tests that need those
tools skip in CI and run only on a local machine that has them on `PATH`.

`.github/workflows/windows.yml` runs vet, tests and a static build on `windows-latest` only when
started manually (`workflow_dispatch`). It is step W1 of the deferred
[Windows verification scenario](windows-verification.md) and never gates a task.

`.github/workflows/release.yml` runs `make ci` and `make dist` on Linux for `v*` tags, verifies
`dist/SHA256SUMS`, and creates a GitHub release containing both bundles and their archive
checksums. It first checks that the tag is `v` plus the `VERSION` file. The
packager verifies the tool pins even when `make ffmpeg` reuses existing files, then copies an
explicit file list; it never copies `bin/.env`. It needs `zip` and `sha256sum` in addition to the
`make ffmpeg` tools. The Windows bundle is cross-built and checked on Linux; its runtime smoke
test is [W8](windows-verification.md#scenario).
