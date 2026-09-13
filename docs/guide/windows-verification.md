# Windows Verification Scenario

Status: **deferred; outside the current development scope and never an acceptance gate.**

`arxgo` is implemented for Linux and Windows (amd64), but only Linux is tested as a gate. This page
collects every check that needs a real Windows host, so the Windows specifics stay visible without
blocking tasks. Running it is a separate, operator-initiated effort. Making any part of it a gate
again is a [specification](../openspec/spec.md#development-integrity) amendment followed by plan
tasks.

## What stays in scope

Windows support is still built and reviewed in every task:

- Platform behavior from the [specification](../openspec/spec.md#platforms-and-build) and the
  [cross-platform notes](../openspec/architecture.md#cross-platform-notes) is implemented in
  build-tagged files, never dropped or stubbed.
- `make ci` on Linux cross-compiles the Windows binary (`make build-all`) and type-checks all
  packages and tests for Windows (`make vet-windows`). Both are gates.
- Tests for Windows-only behavior are written next to the code when cheap. They skip on Linux with
  a reason and are evidence only when this scenario runs them.
- Records state Windows behavior as "cross-compiled only" and route Windows-only audit notes to
  this page through the owning checkpoint (see [deferred items](#deferred-items)).

## When to run

Before the first Windows release to operators, after a change to a `_windows.go` file that the
operator wants confirmed on a host, or when a Windows-only defect is reported. Results go into an ad
hoc record (`docs/impl/records/NNNN-<group>-windows-verification.md`); failures become repair tasks
in the plan only when the operator brings Windows into scope.

## Prerequisites

- Windows 10/11 or Server 2019+ amd64 host with Go 1.27+, Git and GNU Make (Git Bash or MSYS2).
- Developer Mode or the `SeCreateSymbolicLinkPrivilege` (otherwise symlink tests skip).
- A second volume (`subst` drive, VHD or USB disk) and, for network checks, an SMB share mounted by
  UNC path and as a mapped drive.
- Optional: `ffmpeg.exe` / `ffprobe.exe` on `PATH` (or `make ffmpeg`) for media checks.
- The on-demand workflow `.github/workflows/windows.yml` (`workflow_dispatch`) covers W1 only.

## Scenario

| Step | Action | Pass signal |
| --- | --- | --- |
| W1 Unit tests | `go vet ./...`; `go test ./...`; `CGO_ENABLED=0 go build -trimpath -o bin/arxgo.exe ./cmd/arxgo`; `bin/arxgo.exe version` | All pass; tests that skip on Linux run here (for example `TestRootCaseVariantOnWindows`) |
| W2 CLI and environment | `make setup` under Git Bash; `ARXGO_EXCLUDE` with `;` separators; `.env` next to `arxgo.exe`; case-variant and `\\?\` long roots; `--exclude 'cache\*'` | Setup completes with `arxgo.exe`; list split on `;`; `.env` read; case variants of one root rejected as the same directory; backslash glob exits 2 |
| W3 Filesystem primitives | Rename and copy within one volume and to the second volume; target held open by another process; path longer than 260 characters; UNC and mapped-drive roots | No-replace rename, `ERROR_NOT_SAME_DEVICE` falls back to copy, sharing-violation retry, long paths work, devices grouped correctly, free space from `GetDiskFreeSpaceEx` |
| W4 Run lock and WAL | Two concurrent `arxgo scan` runs; kill one with Task Manager; resume; lock held from another host name on a share | Second run exits 5; stale lock taken over after process exit; WAL recovery converges; remote lock refused |
| W5 Walker | Tree with a directory symlink, file symlink, junction, a directory denied to the user by ACL, hidden and system files, names differing only in case | Symlinks recorded not followed; junction skipped as special; denied directory counted unreadable; order equals `WalkDir` order |
| W6 Tool discovery | `go test ./internal/media ./internal/cli` (in-memory discovery fakes and test-binary helper process, no fake tools); a real `ffprobe.exe` next to `arxgo.exe` and on `PATH` | Next-to-executable wins; failing tool treated as missing; exit 3 prints the `windows/amd64` link |
| W7 Stage-1 proof | `go test -tags integration ./test/integration/...` on NTFS, then with `ARXGO_TEST_VIDEO_PARENT` naming a directory on the second volume | Manifests equal after split, kill, resume and restore; exit codes as specified |
| W8 Release bundle | Unpack `arxgo-<version>-windows-amd64.zip`; `arxgo.exe split --image start` on a generated video | Previews produced with the bundled `ffmpeg.exe`; no `.env` in the bundle |
| W9 Cloud (stage 3) | Token cache and session state after a publish | Files readable only by the current user (ACL) |

## Deferred items

Windows-only audit notes whose next check needs a Windows host. The owning checkpoint dispositions
each one as deferred to this scenario; the step column shows where it is checked.

| Note | Concern | Step |
| --- | --- | --- |
| `AUD-implement-cli-contract-1` | Case-variant roots on a real filesystem; `EvalSymlinks` on UNC and mapped drives | W1, W2, W3 |
| `AUD-add-env-file-and-setup-1` | `.env` ACL instead of mode 0600; `make setup` under Git Bash | W2, W9 |
| `AUD-implement-filesystem-primitives-1` | Volume serial and serial-0 shares, `GetDiskFreeSpaceEx`, `MoveFileEx` no-replace and retry, `\\?\` prefix, cross-volume copy | W3 |
| `AUD-implement-run-lock-and-checkpoint-1` | `OpenProcess` liveness, case-insensitive roots, `MoveFileEx` during takeover | W4 |
| `AUD-implement-write-ahead-log-and-recovery-1` | WAL open/truncate and atomic stub writes | W4 |
| `AUD-implement-disk-space-preflight-2` | `GetDiskFreeSpaceEx` and UNC zero-total free space | W3 |
| `AUD-implement-directory-walker-1` | Symlink privilege, junctions as special entries, case-sensitive `SkipPaths` | W5 |
| `AUD-implement-scan-operation-and-csv-registry-5` | Registry replace while the CSV is open in another program; `mode` values of Windows files | W5 |
| `AUD-implement-video-split-transactions-1` | Case-fold skip of colliding video names; same-volume rename vs copy | W7 |
| `AUD-implement-file-type-detection-4` | Detection open on files locked by another process or denied by ACL returns an error counted unreadable | W5 |
| `AUD-implement-tool-discovery-1` | `LookPath` on `.exe` candidates, `-version` via `CreateProcess`, timeout kill; only `.exe` names are candidates, so runtime checks use the helper-process tests and a real `ffprobe.exe` | W6 |
| `AUD-review-stage-1-integrity-1` | Registry path validation (`filepath.IsLocal` device names, `\`), replaced-run root comparison by recorded spelling, cleanup `filepath.Rel` case folding | W7 |
