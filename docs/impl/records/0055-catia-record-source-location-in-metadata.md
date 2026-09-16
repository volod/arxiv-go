# Self-locating metadata

## Task and scope

- Id / capability / checkpoint: `record-source-location-in-metadata` / `catia-archive` /
  `review-registry-and-metadata`
- State: accepted
- Source: plan task `record-source-location-in-metadata`; code revision `1d37507`, clean tree at
  start.
- Plan counts at start: 12 open tasks (9 agent, 3 human); next eligible
  `record-source-location-in-metadata`; also eligible `research-cloud-target-apis`.
- Accepted task:

```markdown
#### record-source-location-in-metadata

A description or text sidecar harvested into a search index no longer sits next to its file, and
nothing inside it says which archive the path belongs to; a sidecar says nothing about the file
beyond its path.

- Serves: `catia-archive` -- [Self-locating metadata](../openspec/stage-4-catia/catia.md#self-locating-metadata)
- Agent status: CLEAR
- Dependencies: [Stage-4 checkpoint](records/0051-catia-review-stage-4-catia.md).
- User-visible outcome: Every description and text sidecar names its archive root, and each sidecar
  carries the identity block of its description, so a harvested document stands alone.
- Scope boundary: The shared description renderer and the sidecar renderer, the archive root passed
  into them, and the sidecar cap accounting. Both payloads share the renderer, so the video
  description gains `archive:` in the same change. No marker-line change, no new registry column,
  no rewrite of descriptions earlier runs wrote.
- Data and artifact paths: `internal/report/description.go`, `internal/report/catia.go`,
  `internal/archive/split_description.go`, `internal/archive/text.go`.
- Execution path: Renderer table tests; a CATIA split fixture comparing sidecar and description
  fields; a cap test whose identity block alone exceeds 1 MiB.
- Acceptance gates: `archive:` is the second line of both files; a hash-verified split writes the
  same `sha256`, `file_size` and `catia` values in sidecar and description; an oversized identity
  block yields `truncated: true` with no blocks; `restore --descriptions delete` still removes
  both; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-registry-and-metadata`.
```

- Amendments (specification, within the task's renderer and cap-accounting scope; no task-text
  change):
  1. [Self-locating metadata](../../openspec/stage-4-catia/catia.md#self-locating-metadata): an
     identity block that does not fit keeps its fields in order up to the first that does not fit;
     the marker, `archive`, `extracted_at` and `truncated` lines are always written. The owned
     description is the WAL-recorded one when still owned, otherwise the first owned name in the
     naming order; without one the sidecar holds `file_name` only and split warns. Reason: the spec
     required `truncated: true` with no blocks but did not say which header lines survive or where
     the description comes from on catch-up.
  2. [Contracts](../../openspec/stage-1-core/contracts.md#video-description): `archive` rows and
     examples for the video description, the CATIA description and the sidecar; sidecar identity
     rows. `extracted_at` and `truncated` follow the identity block.
  3. [Integrity preflight](../../openspec/stage-1-core/integrity.md): `--catia-text` estimate is
     min(1 MiB, file size + 4 KiB). Reason: a sidecar of a small file is now mostly header, which
     min(1 MiB, file size) did not count.

## Implementation

- `internal/report`:
  - `description.go`: `DescriptionInput.Archive`, rendered as `archive:` directly after the marker.
    Video and CATIA descriptions share the renderer, so both gain it.
  - `catia.go`: `CatiaTextInput.Archive` and `Identity` (`TextIdentity`); `TextIdentityOf` copies
    `file_size`, `file_mime`, `sha256`, `modified`, `catia`, `moved_to`, `url` from a parsed
    description. `RenderCatiaText` writes marker, `archive`, `file_name`, the identity fields,
    `description`, `extracted_at`, `truncated`, then blocks. Cap: reserve the fixed lines, keep
    identity lines while they fit, stop blocks when one does not. A zero `ExtractedAt` no longer
    reaches `quoteValue("")`, which indexed an empty string (unreachable in production: the
    session clock is always set).
  - `names.go`: `FindDescriptionPath` walks the `ChooseDescriptionPath` order for an owned
    description: both fixed names, then indexed names until the first absent one.
- `internal/archive`:
  - `split_description.go`, `split.go`: the description writer passes its archive root;
    `splitDescriptions` fills an empty root from the session.
  - `text_identity.go` (new): `movedDescriptions` maps rel_path to the description the latest moved
    `described` record of the CATIA history names; `splitTexts.identity` reads that description if
    still owned, else `FindDescriptionPath`, and logs `text sidecar has no description identity`
    when none is found or readable.
  - `text.go`: the sidecar renderer gets the archive root and the identity; `textNeed` is
    min(1 MiB, size + `textIdentityReserve` 4 KiB).
- Decisions:
  - The identity is read from the description file on disk, not carried from the description
    writer, so catch-up of files moved by earlier runs and resumed runs take the same path and the
    values are, by construction, the ones the description holds.
  - The WAL hint comes first because `FindDescriptionPath` stops at the first absent indexed name:
    a foreign file removed after the split would hide a later owned name.
  - `archive` is the native absolute path escaped like every description value, so on Windows it
    is quoted (`"D:\\archive"`). A slash form would read better but would not be the path the
    operator typed.
  - A missing description does not fail the sidecar: the sidecar's own content is still valid, and
    failing would re-extract on every run.
  - Rejected: truncating the rendered header at the byte cap (the previous fallback); it could cut
    through a line and drop `truncated:`. The byte cut now applies only when the marker and archive
    lines alone exceed 1 MiB.
- Compatibility: descriptions and sidecars written by earlier builds are not rewritten; they lack
  the new lines. First lines are unchanged, so occupancy, restore and recovery are unaffected.
- Documentation: [CATIA archive](../current/catia-archive.md#self-locating-metadata-internalreport-internalarchive),
  [current state](../current.md), [crash safety preflight](../current/crash-safety.md), both manuals
  (sidecar description; per-file recipe now copies the sidecar, the assembly index reads `catia:`
  from it; earlier-release fallback kept), [Windows verification](../../guide/windows-verification.md)
  W10 signal, `README.md`.

## Acceptance evidence

Linux amd64, Go toolchain from `go.mod`. Fixtures are generated in `t.TempDir()`; CATIA files are
synthetic.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| `archive:` is the second line of both files | `TestRenderDescriptionGoldens` (goldens regenerated, second line asserted), `TestRenderCatiaTextIdentity`; `TestCatiaTextSidecarRepeatsDescriptionIdentity` (every file of the CATIA fixture, both files); `TestVideoDescriptionNamesArchive` | pass |
| Hash-verified split writes the same `sha256`, `file_size` and `catia` in sidecar and description | `TestCatiaTextSidecarRepeatsDescriptionIdentity` (`--verify hash`; also `file_mime`, `modified`, `moved_to`, `sha256` equal to the CATIA archive copy, `description:` path including the `.arxgo.md` fallback, sidecar field order); `TestRenderCatiaTextIdentityMatchesDescription` (line-for-line with quoting); `TestCatiaTextCatchUpReadsEarlierDescription` (catch-up of an earlier run via the WAL-recorded indexed description; fails with the hint disabled) | pass |
| Oversized identity block yields `truncated: true` with no blocks | `TestRenderCatiaTextOversizedIdentity` (first field, late field, split across fields, near-margin; length <= 1 MiB, fixed lines present, no block); `TestRenderCatiaTextIdentityCountsAgainstCap` | pass |
| `restore --descriptions delete` still removes both | `TestCatiaTextSidecarRepeatsDescriptionIdentity` (restore after split; foreign `.md` kept); existing `internal/archive` CATIA restore tests | pass |
| `make ci` passes | `make ci` | pass on Linux on the final tree: fmt, vet (incl. `GOOS=windows`), tests, `build-all`, `lint-spec-plan`, `lint-doc-links`. An earlier run failed only `lint-doc-links`, on links to this record before it was written |
| Integration | `make test-integration` | pass (11.9 s) |
| Real binary | `go build` of `cmd/arxgo` in a scratch directory: synthetic `cad/fixture-part.CATPart` with a foreign `.md`, `split --catia --catia-text --verify hash --base-url ...`; relative `--archive`; the updated Linux recipes; `restore --catia --descriptions delete` | pass: identical identity lines incl. `url` and `description: cad/fixture-part.CATPart.arxgo.md`, absolute `archive:` from a relative flag, recipe copies and index built from the sidecar, restore removed owned description and sidecar and kept the foreign file |

Windows: cross-compiled and vetted only.

## Audit handoff

- `AUD-record-source-location-in-metadata-1`: nonblocking, Windows-only. `archive:` with a
  drive-letter, UNC or `\\?\` root, its quoting, and the Windows manual recipes need a runtime
  check. Owner: [Windows verification](../../guide/windows-verification.md) W10. Disposition:
  routed.
- `AUD-record-source-location-in-metadata-2`: nonblocking. `textIdentityReserve` (4 KiB) is an
  estimate: an identity block holding several near-`PATH_MAX` paths and a long `--base-url` can
  exceed it, and preflight then underestimates by that excess per sidecar (still bounded by the
  1 MiB cap per file). Owner: `review-registry-and-metadata` (cap accounting). Disposition: routed.
- `AUD-record-source-location-in-metadata-3`: nonblocking. `NewMarkdownDescription` defaults `Now`
  to `time.Now`, so the `if m.cfg.Now == nil` branch in `splitDescriptions` never gives a
  configured writer the session clock: `moved_at` in tests and in writers built by the CLI uses the
  wall clock, not `Config.Now`. Output is still correct in production; test descriptions are not
  clock-deterministic. Owner: `review-registry-and-metadata` (description fields). Disposition:
  routed.
- `AUD-record-source-location-in-metadata-4`: nonblocking. The sidecar copies what the description
  holds at sidecar time; an operator edit to the description between split and a later catch-up is
  copied too. This is the specified source of truth. Owner: `review-registry-and-metadata`.
  Disposition: routed for confirmation.

## Close or resume

All gates pass. Task removed from the plan; its references in `implement-catia-text-index` and
`review-registry-and-metadata` now link this record. `catia-archive` stays planned
(`implement-catia-text-index` and the checkpoint remain).
