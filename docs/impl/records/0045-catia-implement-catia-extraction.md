# CATIA accessible metadata and text extraction

## Task and scope

- Id / capability / checkpoint: `implement-catia-extraction` / `catia-archive` / `review-stage-4-catia`
- State: accepted
- Source: plan task `implement-catia-extraction`; code revision `3cbee10`; working tree already
  holds uncommitted classification and payload-executor work from records 0043 and 0044.
- Plan counts at start: 15 open tasks (12 agent, 3 human); next eligible
  `implement-catia-extraction`.
- Accepted task:

```markdown
#### implement-catia-extraction

Operators need accessible metadata and optional text without a CATIA licence or copied archive names.

- Serves: `catia-archive` -- [Accessible metadata](../openspec/stage-4-catia/catia.md#accessible-metadata)
- Agent status: CLEAR
- Dependencies: [CATIA classification](records/0043-catia-implement-catia-classification.md).
- User-visible outcome: A pure-Go streaming extractor returns format, release, component names,
  properties and strings for every CATIA kind, with bounded memory.
- Scope boundary: `internal/catia` only: format detection, V5 property records, the V5 component
  walk (including the `;` U+0001 removal, self-name exclusion and malformed-chunk skip), 3dxml XML and
  ZIP with member limits, strings harvest, 1 MiB cap and `truncated`. `internal/report` rendering of
  the `catia:` line and the sidecar body. No archive mutation, CLI or WAL. No new module dependency.
- Data and artifact paths: `internal/catia/`, `internal/report/`.
- Execution path: Synthetic V5, cgr and 3dxml (XML and ZIP) fixtures in `t.TempDir()` with invented
  names and planted markers; a large synthetic stream for the memory bound. An optional local run
  against the gitignored experimental tree may record aggregate counts only in the task record.
- Acceptance gates: The [CATIA files acceptance](../openspec/stage-4-catia/catia.md#acceptance)
  extraction items; malformed input never panics (a short fuzz run of the extractor passes);
  `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none.

## Implementation

`internal/catia.Extract` / `ExtractPath` streams one file and returns `Info` (kind, format, release,
V5/3dxml properties, unique-sorted components and strings, `Truncated`, `TextFailed`, `ErrorKind`).
Kind is the extension table; format is the leading-byte table. No new module dependency. Split does
not call this yet.

V5 properties are little-endian uint16 key length, ASCII key, type `0x0000000e`, uint32 value length
at most 4 KiB. First valid occurrence wins. `LastSaveVersion` uses the on-disk
`<Tag>value/<Tag>` form (also accepts well-formed `</Tag>`). Components use the published
CATOctetArray window: strip `;` U+0001, split on U+0001 U+0004 `File`, skip `CATOctetArray` / `feat`
and malformed chunks, take the last path segment, drop the file's own base name.

3dxml is `encoding/xml` for raw XML (64 MiB read cap) and `archive/zip` when the reader is
`io.ReaderAt` (os.File or bytes.Reader). Unsafe member names are skipped. Limits: 1024 members,
64 MiB uncompressed each, 256 MiB total. A syntax error in the Manifest-named root is `TextFailed`;
other members are skipped at debug.

`report.CatiaLine` and `RenderCatiaText` render the description `catia:` field and the `arxgo-text:`
sidecar. `RenderDescription` writes `catia:` instead of `created:` / `video:` when `DescriptionInput.Catia`
is set. Sidecar blocks fill in order until 1 MiB.

Rejected: buffering the whole V5 file (the window and 1 MiB string budget are enough); fetching XML
DTDs.

Limitations: ZIP 3dxml needs random access; the experimental tree used for the optional run has V5
documents only (no `.cgr` / `.3dxml`). Current-state page: [CATIA archive](../current/catia-archive.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| V5 planted components, `;` U+0001, self-name, malformed skip, unique sort | `TestExtractV5ComponentsAndRelease` (synthetic `fixture-product.CATProduct` in `t.TempDir()`) | pass, Linux |
| No markers -> 0 components; LastSaveVersion `V5R30 SP5`; MinimalVersionToRead `V5R30`; neither `unknown` | `TestExtractV5ComponentsAndRelease`; `TestExtractV5MinimalVersionFallbackAndNoMarkers`; `TestExtractV5FirstPropertyWins` | pass, Linux |
| ZIP `..` / volume / oversized member / external URL; broken root `text_failed`; other member skipped | `TestZipNameUnsafe`; `TestExtractZIP3DXMLLimits`; `TestExtractZIPBrokenRoot`; `TestExtractZIPOtherMemberSkipped`; `TestExtractXML3DXML`; `TestExtractXMLBrokenRoot` | pass, Linux |
| 1 MiB sidecar cap and `truncated`; large stream bounded | `TestRenderCatiaTextCap`; `TestExtractLargeStreamBounded` (32 MiB zeros, not loaded as one string); `TestExtractV5OversizeWindowEmpty` (window > 4 MiB yields 0 components) | pass, Linux |
| cgr strings harvest (ASCII and UTF-16LE) | `TestExtractCGRStrings` | pass, Linux |
| `catia:` line and sidecar body | `TestCatiaLine`; `TestRenderCatiaDescription`; `TestRenderCatiaText`; `TestRenderCatiaText3DXMLProperties` | pass, Linux |
| Malformed input never panics | `go test ./internal/catia -fuzz=FuzzExtract -fuzztime=15s` (~9.6e5 execs, 0 crash) | pass, Linux |
| Optional experimental tree (aggregates only) | `go test ./internal/catia -tags catialive -run TestExtractExperimentalAggregates` | pass, Linux: 492 files (329 parts, 157 products, 6 drawings), 492 V5, 309 with components, 1447 component names, 0 unknown release, 1 distinct release token, 0 errors, 0 text_failed, 3.9 s. No names copied into the repo. |
| `make ci` | `make ci` (fmt-check, vet, vet-windows, `go test ./...`, `make build-all`, lint-spec-plan, lint-doc-links) | pass, Linux; Windows cross-compiled only |

## Audit handoff

`none identified`. Reviewed: leaf import rule, format vs kind, V5 window vs file size, ZIP limits
before decompress, experimental-data ban in tests and this record, and that archive/CLI/WAL were
not touched.

## Close or resume

All code gates passed; `make ci` passed on Linux (Windows cross-compiled only). Record indexed;
current pages updated; plan task removed and `implement-catia-split` now depends on this record.
Capability `catia-archive` stays planned. Plan counts after: 14 open (11 agent, 3 human); next
eligible `implement-catia-split`.
