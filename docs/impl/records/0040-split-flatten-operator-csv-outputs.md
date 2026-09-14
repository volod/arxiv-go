# Flat operator CSV outputs

## Task and scope

- Id / capability / checkpoint: `flatten-operator-csv-outputs` / `video-split` / none
- State: accepted.
- Source: operator stage-1 archive-copy trial; working tree already contained extensive unrelated edits.
- Plan counts at start: 9 open (6 agent, 3 human); next eligible `research-cloud-target-apis`.
- Accepted task:

```markdown
#### flatten-operator-csv-outputs

Correct the registry outputs reported by the operator's stage-1 archive-copy trial.

- Serves: `video-split` -- [CSV contracts](../openspec/stage-1-core/contracts.md#video-registry-csv)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Both CSV registries have separate metadata columns, video rows have one
  relative path and a usable local URL by default, and split does not create a Markdown summary.
- Scope boundary: CSV format amendment, read compatibility for old video registries, output and
  related documentation/tests. Do not alter video transfer or preview generation.
- Data and artifact paths: `arxgo-registry.csv`, `arxgo-videos.csv`, `internal/report/`,
  `internal/archive/`, stage-1 contracts and operator guides.
- Execution path: Generated scan/split/restore fixtures, old-header video CSV fixture, Linux CI.
- Acceptance gates: New column headers and values match the contract; split/restore preserve old
  rows and use local file URLs without a base URL; no new `arxgo-videos.md`; `make ci` passes.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: none; this is a bounded operator-reported repair.
```

- Amendments: operator confirmed no existing archive needs compatibility; the old CSV reader and tests are out of scope. Revised full task:

```markdown
#### flatten-operator-csv-outputs

Correct the registry outputs reported by the operator's stage-1 archive-copy trial.

- Serves: `video-split` -- [CSV contracts](../openspec/stage-1-core/contracts.md#video-registry-csv)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Both CSV registries have separate metadata columns, video rows have one
  relative path and a usable local URL by default, and split does not create a Markdown summary.
- Scope boundary: CSV format amendment, output and
  related documentation/tests. Do not alter video transfer or preview generation.
- Data and artifact paths: `arxgo-registry.csv`, `arxgo-videos.csv`, `internal/report/`,
  `internal/archive/`, stage-1 contracts and operator guides.
- Execution path: Generated scan/split/restore fixtures and Linux CI.
- Acceptance gates: New column headers and values match the contract; split/restore retain rows
  across runs and use local file URLs without a base URL; no new `arxgo-videos.md`; `make ci` passes.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: none; this is a bounded operator-reported repair.
```

- Amendments: operator requested feature/domain naming in code and tests. Revised full task:

```markdown
#### flatten-operator-csv-outputs

Correct the registry outputs reported by the operator's stage-1 archive-copy trial.

- Serves: `video-split` -- [CSV contracts](../openspec/stage-1-core/contracts.md#video-registry-csv)
- Agent status: CLEAR
- Dependencies: [Stage-1 proof](records/0025-restore-prove-stage-1-on-generated-archive.md).
- User-visible outcome: Both CSV registries have separate metadata columns, video rows have one
  relative path and a usable local URL by default, and split does not create a Markdown summary.
- Scope boundary: CSV format amendment, output and related documentation/tests; rename stage-based
  code and test identifiers to feature or domain terms. Do not alter video transfer or preview generation.
- Data and artifact paths: `arxgo-registry.csv`, `arxgo-videos.csv`, `internal/report/`,
  `internal/archive/`, `internal/cli/`, `test/integration/`, stage-1 contracts and operator guides.
- Execution path: Generated scan/split/restore fixtures and Linux CI.
- Acceptance gates: New column headers and values match the contract; split/restore retain rows
  across runs and use local file URLs without a base URL; no new `arxgo-videos.md`; code and test
  names describe behavior rather than stage numbers; `make ci` passes.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: none; this is a bounded operator-reported repair.
```

## Implementation

`internal/report` writes a fixed, flat metadata header shared by the file and video registries.
The old `v`, `mode`, `metadata` and duplicate `video_rel_path` CSV fields are gone. Media details
(duration, dimensions, codecs, frame rate, stream counts and selected container text tags) are
individual columns. ffprobe collection now selects the same text tags as the ISO parser; unknown
tags are ignored so a streaming registry has a stable header. No old-header parser or migration
path remains, per the operator's explicit direction.

Split writes a local `file://` URL when no base URL is set; restore points restored rows at the
main archive. Both still keep `sha256` empty unless hash verification ran and `previews` empty unless
preview generation ran. The Markdown summary writer and its tests were removed; the scanner no
longer reserves that obsolete filename. Registry writes remain atomic through the existing report
phase. Code and test identifiers now name archive round trips, previews and planned cloud
publishing instead of stage numbers. Historical record names and specification paths retain their
original references.

Current-state pages: [video split](../current/video-split.md),
[archive registry](../current/archive-registry.md), [media metadata](../current/media-metadata.md).


## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Flat CSV contract | `TestScanGoldenRegistry`, `TestScanISOMetadataAndAudioOnlyRefinement`, `TestSplitMediaModeDescriptionHasDuration`; `test/testdata/archive/scan-registry.golden.csv` | pass on generated Linux fixtures; headers have no `v`, `mode`, or `metadata`, with media fields and selected tags in separate columns |
| Local URL, row replay and summary removal | `TestSplitRegistryMergesTwoRuns`, `TestSplitAfterRestoreKeepsRestoredStatus`, `TestSplitWritesVideoRegistryToBothRoots`, `TestPreviewFailureIsCountedAndCaughtUpLive` | pass on generated Linux fixtures; both registry copies have local links and no Markdown summary is generated |
| Archive and preview binary round trips | `GOCACHE=/tmp/arxgo-gocache make test-integration` | pass on Linux; generated archive, subprocess kill/resume and preview fixtures; no operator archive rerun |
| Required quality and cross-build gates | `GOCACHE=/tmp/arxgo-gocache make ci` | pass on Linux: format, vet, Windows vet, tests, Linux/Windows static builds, spec-plan and link lint; Windows behavior cross-compiled only |
| Feature/domain names | `rg --files test internal` and identifier search for `Stage1`, `Stage2`, `stage1_`, `stage2_` | no stage-number code/test identifiers or filenames remain; canonical specification directory names remain |

The first `make ci` stopped at `fmt-check` for `scan_media_test.go`; formatting was fixed and the
full rerun passed. `git diff --check` still reports trailing whitespace in an unrelated README
line that was already modified at task start; this task did not edit that line. The operator's
archive copy was not rerun with this build.

## Audit handoff

None identified in the changed behavior. The CSV schema is intentionally incompatible with the
prior header because the operator confirmed no archive needs migration. Fixed tag columns retain
the selected metadata fields, while unrecognized ffprobe tags are omitted. The operator's separate
human approval task remains open until they decide whether the repaired build is fit for use.

## Close or resume

All gates passed. The record is indexed and linked from the current-state pages; the repair task
was removed from the forward plan. Open task counts were 9 (6 agent, 3 human) before the new
repair, 10 while it was active, and 9 (6 agent, 3 human) after acceptance. The next eligible agent
task is `research-cloud-target-apis`; `approve-stage-1-on-operator-archive-copy` remains the
available human action.
