# Development Guide

## Requirements

- Go 1.27 or newer (`go version`). No cgo toolchain is needed.
- GNU Make. On Windows use Git Bash, MSYS2 or `choco install make`; every target also documents
  the plain `go` command it runs.
- Optional, local only: `ffmpeg`/`ffprobe` on `PATH` to run media tests. GitHub CI does not
  install them; those tests skip there. `make ffmpeg` downloads the approved static 6.1.1 builds
  into `bin/` (next to `arxgo`, where tool discovery looks first); for `go test` put them on the
  path with `PATH="$PWD/bin:$PATH"`. The target needs network access, `curl`, `tar`, `gzip` and
  `unzip`, and is not part of `make ci`.

## First-time setup

```bash
make setup
```

`make setup` builds `bin/arxgo`, downloads the pinned `ffmpeg`/`ffprobe` into `bin/` (network
access required), creates `bin/.env` from `.env.example` if it does not exist yet (mode 0600,
never overwritten), and prints how to configure and run. Rerunning it is safe. It needs Go on
`PATH` and exits with install guidance if Go is missing.

`bin/.env` is the optional [environment file](../openspec/stage-1-core/cli.md#environment-file)
that `arxgo` reads next to its executable. It holds `ARXGO_*` settings and, from stage 3,
credentials. Flags and process environment variables override it. Declared runs (`RUN NEEDED`
tasks) keep their settings and credentials there instead of in shell history. Unit tests never
read it: the test binary lives in a temporary build directory, and tests inject their own file.
After pulling changes, compare `bin/.env` with `.env.example` for new variables.
`TestEnvExampleListsEveryFlag` fails when a flag is added without a matching `.env.example` line.

## Make targets

| Target | Runs | Purpose |
| --- | --- | --- |
| `make setup` | `build`, `ffmpeg`, `env`, then prints usage | Ready-to-run `bin/` in one command (network) |
| `make env` | `cp .env.example bin/.env` unless it exists | Optional settings file next to the binary; never overwrites |
| `make build` | `go build -trimpath -o bin/arxgo ./cmd/arxgo` | Host binary with version stamp |
| `make build-all` | `GOOS/GOARCH` loop, `CGO_ENABLED=0` | `bin/arxgo-<os>-<arch>[.exe]` for linux/windows amd64 |
| `make ffmpeg` | `bash tools/fetch-ffmpeg.sh bin linux/amd64 windows/amd64` | Pinned static ffmpeg/ffprobe 6.1.1 into `bin/` (checksums from `packaging/ffmpeg.lock`; network) |
| `make test` | `go test ./...` | Unit tests |
| `make test-race` | `go test -race ./...` | Race detector (needs cgo on the host; not part of `ci`) |
| `make test-integration` | `go test -tags integration ./test/integration/...` | End-to-end tests (from `prove-stage-1-on-generated-archive`) |
| `make fmt` | `gofmt -w` | Format |
| `make fmt-check` | `gofmt -l` | Fail on unformatted files |
| `make vet` | `go vet ./...` | Static checks |
| `make vet-windows` | `GOOS=windows GOARCH=amd64 go vet ./...` | Type-checks Windows build-tagged code and tests on Linux |
| `make lint-spec-plan` | `go run ./tools/plancheck lint` | Registry, plan and records agree |
| `make lint-doc-links` | `go run ./tools/plancheck links` | Relative Markdown links and anchors resolve |
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
- Tests sit next to code (`foo_test.go`), build fixtures in `t.TempDir()`, and never touch the
  network. Media fixtures are generated in tests; tests that need ffmpeg call a shared helper that
  skips with a reason when it is missing.
- New dependencies must be listed in the [dependency table](../openspec/spec.md#dependencies) and
  be pure Go; commit `go.mod` and `go.sum` together.
- ASCII in code, logs and docs unless a test needs Unicode input.

## CI

`.github/workflows/ci.yml` runs `make ci` on `ubuntu-latest` for pushes to `main` and pull
requests; it is the only CI gate. It does not install `ffmpeg` or `ffprobe`. Tests that need those
tools skip in CI and run only on a local machine that has them on `PATH`.

`.github/workflows/windows.yml` runs vet, tests and a static build on `windows-latest` only when
started manually (`workflow_dispatch`). It is step W1 of the deferred
[Windows verification scenario](windows-verification.md) and never gates a task.