# Version from a Semantic Versioning File

## Task and scope

- Id / capability / checkpoint: `version-from-semver-file` / `project-foundation` / none
- State: accepted.
- Source: ad hoc operator request; revision `dcb38ed`, clean working tree at start.
- Plan counts at start: 9 open tasks (6 agent, 3 human); next agent task
  `research-cloud-target-apis`.
- Accepted task:

```text
./arxgo version
arxgo dcb38ed
Please provide a version number based on Semantic Versioning. Start from version 0.1.0.  Do not bump the version number on every build; the developers will provide it manually, e.g., by editing the version file
```

- Amendments: none.

## Implementation

A root `VERSION` file holds `0.1.0`. `make/config.mk` reads it instead of `git describe`, so
`make build`, `make build-all` and `make dist` stamp the same value into `arxgo version` and the
bundle names, and no build changes it. `TestVersionFileIsSemver` (untagged, in `test/integration`,
so it runs in `make ci`) requires exactly one Semantic Versioning 2.0.0 line. The tag release
workflow no longer passes the tag as the version: it fails unless the tag is `v` plus the file, then
runs `make dist`. The [development guide](../../guide/development.md#versioning) documents bumping;
the [project foundation](../current/project-foundation.md) page links it. A plain `go build` still
reports `dev`.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Version starts at 0.1.0 | `make build && bin/arxgo version` | pass: `arxgo 0.1.0` |
| Only an edit of the file changes it | `VERSION` set to `0.2.0-rc.1`, `make build`, `bin/arxgo version`, then restored | pass: `arxgo 0.2.0-rc.1`, then `arxgo 0.1.0` |
| Invalid versions rejected | `go test -run TestVersionFileIsSemver ./test/integration` | pass; `v0.1.0`, `0.1`, `01.0.0`, `0.1.0-` and a commit id are rejected |
| Release tag must match | `.github/workflows/release.yml` tag check | not run: no tag pushed |
| `make ci` passes | `PATH=/usr/local/go/bin:$PATH make ci` | pass on Linux |

## Audit handoff

None identified.

## Close or resume

Accepted. The record index and the project foundation page link this record; plan counts unchanged.
