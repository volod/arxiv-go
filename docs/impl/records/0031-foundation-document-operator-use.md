# Document operator use

## Task and scope

- Id / capability / checkpoint: `document-operator-use` / `project-foundation` / none.
- State: accepted.
- Source: ad hoc operator request; clean working tree at start.
- Plan counts at start: 11 open tasks (8 agent, 3 human); next eligible agent task
  `implement-release-bundle-with-ffmpeg`.
- Accepted task:

```text
We have implemented stage 1 and 2 functional tasks. Update README.md according to the final implementation.
Before task "#### implement-release-bundle-with-ffmpeg", create a separate system-end-user practical manual for targets: how to use arxgo utils with use-case examples and nuances (e.g., if the source and target are on the same physical device, then files are moved; transactional specifics, etc.), with names  docs/gude/manual-linux.md and manual-windows.md
Refactor .env.example. For now, variables are grouped by implementation stage, which is irrelevant to end users. Regroup variables logically by utility command group and by function, and provide clear comments for new end users. E.g., clarify the impact of setting "ARXGO_DRY_RUN=true", "ARXGO_MIN_FREE=1GiB", "ARXGO_METADATA=file", and what will be used for "ARXGO_BASE_URL," etc. Do not mention the development stage; explain the usage to an end-user. We have comments like "# Reserved in stage 1; only false is accepted." Remove such parameters or explain the purpose. For variables such as "ARXGO_SAMPLE=none" in comments, provide acceptable options to simplify end-user setup. For stage 3 variables, "cloud publishing," do not mention stages; just say that, for current builds, it is a future implementation parameter.
Update task "#### implement-release-bundle-with-ffmpeg" to include related manuals in the related distribution bundle.
```

- Amendments: the manuals use the existing `docs/guide/` directory. The original request named
  `docs/gude/`; this was treated as a directory typo to keep the operator guides together. The
  operator then refined the manuals' setup scope:

```text
Please remove from the practical manuals the description of a source checkout, describe the distribution bundle, and explain how to work with it
```

The operator then refined the README scope:

```text
In README.md, provide a description and Quick Start information on how to build from source, create a distribution bundle, and run it on the target host, and links to the manuals. Do not repeat information in the README and manuals
```

## Implementation

No packages changed. Reused the CLI flag table and the accepted current-state pages to describe
the shipped scan, split, restore and preview behavior. Replaced the README's stale status,
regrouped every `.env.example` key by command and function, and wrote separate Linux and Windows
manuals with commands, file placement, transfer and recovery details. Both manuals now start
with the platform distribution archive, its file list, extraction, checksum inspection,
copying `.env.example` to `.env`, and running the bundled executable; neither requires a source
checkout. The Linux manual distinguishes same-device rename from copy across filesystems,
including partitions on one physical disk.

Amended the release-bundle behavior in the preview specification and the open bundle task so
both archive file lists must contain the matching manual. Updated the guide index and the narrow
[project foundation current-state page](../current/project-foundation.md), including its stale
claim that preview flags are unavailable. Corrected the CLI spec opening for the same reason.
The README now has a compact source-build and target-host Quick Start, links to the manuals for
operational detail, and labels `make dist` as planned because that target is not implemented yet.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Available behavior and examples | `README.md`, `.env.example`, `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md` compared with CLI flag table, current-state pages and bundle spec | Pass; no application code changed. Manual commands use bundle-root paths |
| Bundle scope and file lists | Preview spec release-bundle section and `implement-release-bundle-with-ffmpeg` in `docs/impl/plan.md` | Pass; each platform has its matching manual |
| Repository checks | `GOCACHE=/tmp/arxgo-doc-gocache make ci` | Pass on Linux: tests, Linux/Windows static builds, vet including Windows, plan and link linters; Windows host runtime not run |
| Diff integrity | `git diff --check` | Pass |

## Audit handoff

None identified. Self-review covered document links, end-user claims, command-specific transfer
semantics, preview failure handling, `.env.example` options and the bundle task scope.

## Close or resume

All gates passed. The first targeted Go test invocation could not write the host's default
`~/.cache/go-build`; the full `make ci` run passed with `GOCACHE` under `/tmp`, so no repository
change was needed. Plan counts remain 11 open tasks (8 agent, 3 human); next eligible agent task
is `implement-release-bundle-with-ffmpeg`. The current-state and record indexes are updated.
