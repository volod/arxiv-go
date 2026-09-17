# CATIA notes and properties

## Task and scope

- Id / capability / checkpoint: `extract-catia-notes-and-properties` / `catia-archive` /
  `approve-stage-4-on-operator-catia-copy`
- State: accepted
- Source: operator request after reading the CATIA text index of a disposable copy of the operator
  archive (harvested strings were uninformative: repeated patterns, symbol tags, default fonts);
  code revision `d9d4af6`, clean tree at start apart from the operator's own `README.md` and
  `VERSION` edits, which this task leaves alone.
- Plan counts at start: 9 open tasks (6 agent, 3 human); next eligible
  `research-cloud-target-apis`. After adding this task: 10 open tasks (7 agent, 3 human); next
  eligible `extract-catia-notes-and-properties`.
- Accepted task:

```markdown
#### extract-catia-notes-and-properties

Text sidecars and the CATIA text index are dominated by printable runs harvested from binary CATIA
content (format vocabulary, fonts, line types, decoded binary data), while the product properties
and the notes written on drawings, the text an operator searches for, are missing or unreadable.

- Serves: `catia-archive` -- [Descriptive text](../openspec/stage-4-catia/catia.md#descriptive-text)
- Agent status: CLEAR
- Dependencies: [CATIA text index](records/0056-catia-implement-catia-text-index.md);
  [Registry and metadata checkpoint](records/0057-catia-review-registry-and-metadata.md).
- User-visible outcome: A text sidecar and the index section of a V5 file hold its product
  properties, material and plain-text notes instead of harvested strings, and `catia-index` has no
  `--strings` option.
- Scope boundary: V5 dictionary string reader, root product properties, material, RTF notes and the
  note filter in `internal/catia`; the `notes:` block in the sidecar and the index; `catia-index`
  and `.env.example` without `--strings`; 3dxml without the name harvest; manuals with a way to
  rewrite earlier sidecars. No title-block template detection, no instance or user-defined
  properties, no automatic rewrite of existing sidecars, no new dependency.
- Data and artifact paths: `internal/catia/`, `internal/report/catia.go`,
  `internal/report/catia_index.go`, `internal/archive/catia_index.go`, `internal/cli/`,
  `.env.example`, `docs/guide/manual-linux.md`, `docs/guide/manual-windows.md`.
- Execution path: Synthetic V5 fixtures with planted dictionary strings, product runs and RTF notes
  in `t.TempDir()`, read in chunks that split every marker; split with `--catia-text` and
  `catia-index` through the binary in the integration round trip; an aggregate-only run on a
  disposable copy of the operator archive, kept outside the repository.
- Acceptance gates: Planted properties, material and notes are extracted as specified at every
  chunk boundary; numbers, single letters, placeholders and parameter names are dropped; no
  `strings:` block is written; index blocks equal sidecar blocks byte for byte and a rerun is
  byte-identical; `--strings` exits 2; `make ci` and `make test-integration` pass.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `approve-stage-4-on-operator-catia-copy`.
```

- Amendments: none to the task text. The specification was written for this task before planning:
  [Descriptive text](../../openspec/stage-4-catia/catia.md#descriptive-text) (new section: V5
  dictionary strings, product properties, material, notes, caps), the 3dxml bullet (object names no
  longer extracted), [Text index](../../openspec/stage-4-catia/catia.md#text-index) (no `--strings`,
  `notes:` repeated, earlier sidecars), Exclusions, Experimental data and Acceptance;
  [contracts](../../openspec/stage-1-core/contracts.md#catia-text-sidecar) (sidecar and index
  examples and tables); [CLI](../../openspec/stage-1-core/cli.md) (synopsis and `Index flags`);
  architecture file list. The human task `approve-stage-4-on-operator-catia-copy` now depends on
  this task and no longer asks about `strings:`.

## Investigation

Run on a disposable copy of the operator archive after `split --catia --catia-text` and
`catia-index --strings`; scripts and outputs stayed outside the repository, and only aggregate
counts are recorded here.

- The `--strings` index was 539 MB for 8036 CATIA files (4568 `CATPart`, 1903 `CATProduct`, 1565
  `CATDrawing`, all V5): 14.2 million `strings:` items, 2.3 million distinct. Strings found in at
  least 100 files made up 71% of the items (format vocabulary, style catalogs, line types, fonts);
  1.65 million strings occurred in one file only, and a sample of them was almost entirely decoded
  binary data (repeated short patterns, no vowels, punctuation runs).
- The harvest took only ASCII and UTF-16LE runs. Most files hold UTF-8 text in other scripts (in a
  sample of 300 files, 260 had UTF-8 runs of at least three such letters), which the harvest never
  saw; the UTF-16LE runs of that script in the sample were binary data.
- A V5 container keeps object names and string values as length-prefixed UTF-8 strings: a length
  byte `n` of 1-49 for `n - 1` bytes, or 0 and a uint32 count. Over every `ASMPRODUCT` run of the
  copy the short form never exceeded 48 bytes and the long form never went below 49.
- Every `.CATPart` and `.CATProduct` but 5 has one root product run: `ASMPRODUCT`, the part number,
  then attribute names each followed by a value when set. `_PartNumber` never carried a following
  value except where a design-table parameter drives it (3 files), where the run holds CATIA's
  parameter names instead of values.
- Drawing texts are RTF in dictionary strings: 116109 strings starting `{{\fonttbl`, all in the
  long form, all ending `}`, all valid UTF-8, in 1535 of 1565 drawings (the other 30 have an empty
  text list) and 175 parts. Their control words were `\f`, `\fs`, `\cf`, `\expnd`, `\i`, `\b`,
  `\ul`, `\ql`/`\qc`/`\qr`, `\charscalex`, `\par`, `\field` with `{\*\dsindex}` and `\fldrslt`,
  `\symb` before `<TAG>` symbols, `\sub`/`\super`; no `\'hh` or `\uN` escapes.
- The material name follows the pair `String`, `Material` in 2507 parts (60 of them `None`).
  About 10 values are format keywords rather than materials, because the pair is found by its
  bytes.
- After converting RTF and dropping texts with fewer than three letters or only one distinct letter,
  a drawing keeps a median of 38 notes (59 before the filter). Texts found in at least 10% of the
  drawings (title-block captions, the organization line, standard notes) make up 65% of those note
  occurrences; see note 1.
- Rejected: keeping a filtered harvest (a per-file filter cannot tell format vocabulary from text
  without a stop list, and the list would echo archive vocabulary); corpus frequency inside
  extraction (a sidecar would depend on other files); instance names of product children (the key
  of a value is known only for the first object that uses it); UTF-16LE runs (binary data in the
  sample).

## Implementation

- `internal/catia`:
  - `v5dict.go` (new): `dictString` reads one dictionary string (short or long form, a maximum,
    valid UTF-8 without control characters other than tab and line breaks) and reports `dictShort`
    when the buffer ends inside it. `markerWindow` finds the first occurrence of a marker in a
    stream and buffers a bounded window from it. `productReader` parses the root product run from
    `\x0bASMPRODUCT` to `_BagRepsList` (64 strings, 64 KiB) incrementally, so one-byte reads do not
    re-parse; `productProperties` maps it to `part_number`, `revision`, `definition`,
    `nomenclature`, `source` and `description`, leaving out a following attribute name, the
    property's parameter name, empty values and the `Unknown` source. `materialReader` reads the
    string after the first `String`, `Material` pair within 64 KiB, unless `None` or an attribute.
  - `notes.go` (new): `noteReader` finds `{{\fonttbl` and `{\rtf` with the length prefix in front of
    them, keeping a 14-byte tail between chunks and collecting a string that crosses chunks. A
    candidate is abandoned at its first ASCII control byte, or when it does not end with `}` or is
    not a text string, and is then searched again from its second byte: a false length prefix
    (for example four bytes that happen to precede a marker) would otherwise swallow the notes that
    follow. `nextNote` caches the next position of each marker per scan, which keeps adversarial
    input linear. `noteText` is the letter filter with symbol tags skipped. Notes are unique in file
    order under the 1 MiB cap.
  - `rtf.go` (new): `rtfPlain` per the specification's three steps.
  - `stream.go`: V5 streams feed properties, the component window, product, material and notes; a
    file of another format returns after its head, since nothing else is read from it.
  - `extract.go`: `Info.Strings` replaced by `Info.Product` (`catia.Product`) and `Info.Notes`.
    `strings.go` (the printable-run harvest) is deleted; `sortByte` moved to `v5comp.go`.
  - `xml3d.go`, `zip3d.go`: the attribute and text harvest and the `Reference3D`/`Instance3D` name
    collection are removed; header properties and components are unchanged.
  - `extract_live_test.go` (build tag `catialive`): `ARXGO_CATIA_LIVE_DIR` names the tree, and the
    aggregates include properties and notes.
- `internal/report`: `catiaPropertyLines` writes the product properties after `release` and
  `build_level`; `RenderCatiaText` writes `notes:` in place of `strings:`. `CatiaText.Notes`
  replaces `Strings`; `ReadCatiaText(path, rel, withNotes)` reads `notes:`, and a `strings:` block of
  an earlier sidecar ends the blocks. `WriteCatiaIndexSection` writes `notes:`.
- `internal/archive/catia_index.go`: `CatiaIndexConfig.Strings` removed; the write pass always
  re-reads each sidecar for its `notes:` block only, so notes are never held in memory.
- `internal/cli`: flag `strings`, `includeStrings`, `CatiaIndexOptions.Strings`, the help synopsis
  and `ARXGO_STRINGS` in `.env.example` removed; `--strings` is now an unknown flag (exit 2).
- Decisions:
  - Structure over heuristics: text is read only at markers of the dictionary format, never by
    scanning for printable bytes, so binary data cannot appear; the cost is that text stored in
    another structure is missed (Exclusions).
  - Notes keep file order rather than byte order, so the numbered paragraphs of a drawing's
    requirements read in sequence; the order is deterministic per file.
  - Title-block captions stay notes: removing them needs either the drawing's object graph or
    statistics across files, and both are outside this task (note 1).
  - Existing sidecars are not rewritten automatically: split's catch-up rule already writes a
    missing sidecar, so the manuals give a recipe that deletes owned sidecars holding `strings:`
    and reruns the split. The recipe was run on Linux.
  - `--strings` is removed rather than kept as a no-op: nothing would be left for it to add.
- Compatibility: sidecars written from now on have `notes:` and more properties and never
  `strings:`; `catia-index` over earlier sidecars shows their properties and components without
  notes; `catia-index --strings` and `ARXGO_STRINGS` are gone. Descriptions, `arxgo-catia.csv`,
  restore and recovery are unchanged. The component count and release are unchanged.
- Documentation: [CATIA archive](../current/catia-archive.md#extraction-internalcatia-internalreport),
  [current state](../current.md), both manuals (sidecar content, index usage, rewrite recipe),
  `README.md` (the index paragraph only; the operator's own edits in the same file were left as
  they were), [Windows verification](../../guide/windows-verification.md) W10.

## Acceptance evidence

Linux amd64, Go toolchain from `go.mod`. Fixtures are generated in `t.TempDir()` with invented
names, values and notes; real-data runs used a disposable copy of the operator archive and kept
their outputs outside the repository.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Planted properties, material and notes are extracted as specified at every chunk boundary | `TestExtractV5ProductProperties`, `TestExtractV5ProductRunLimits`, `TestExtractV5Notes` (short and long form, `\par`, field result, symbol tag, escaped braces and backslash, `\uN`, formatting duplicates, invalid prefix, unterminated and invalid strings), `TestExtractV5TextAtEveryChunkBoundary` (one-byte, half and 2-17 byte reads equal the whole read, components and release included), `TestExtractV5NotesCap`, `TestExtractLargeStreamBounded` (a note after 32 MiB), `TestRTFPlain`; a throwaway randomized check outside the committed tests: 5000 random V5 inputs built from markers, prefixes and noise, each read in chunks of 1, 2, 3, 5, 7, 11 and 64 bytes | pass |
| Numbers, single letters, placeholders and parameter names are dropped | `TestNoteText`, `TestExtractV5Notes`, `TestExtractV5ProductPropertiesLeftOut` (parameter names, `Unknown` source, attributes without value, invalid and cut runs, control character, no marker, `None` and attribute-like material) | pass |
| No `strings:` block is written | `TestRenderCatiaText` (all properties in order, quoting, `notes:`), `TestRenderCatiaTextOversizedIdentity`, `TestExtractOtherFormatsHaveNoText` and `TestExtractCGRMeta` (`.cgr` and unknown format with planted markers), `TestExtractXML3DXML`; integration `checkCatiaSplitOutputs` (no sidecar has `strings:`, every V5 sidecar has the planted description and note, other kinds none) | pass |
| Index blocks equal sidecar blocks byte for byte and a rerun is byte-identical | `TestCatiaIndexCoversMovedFilesAndListsMissingText` (product section properties, components and notes equal to the sidecar), `TestReadCatiaTextRoundTripsRenderedSidecar`, `TestReadCatiaTextIgnoresEarlierStringsBlock`, `TestWriteCatiaIndexDocument`; integration `checkCatiaIndex` (every section of the killed-and-resumed generated archive) | pass |
| `--strings` exits 2 | `TestCatiaIndexUsageErrors`, `TestCatiaIndexHelp` (not listed), integration `checkCatiaIndex` through the binary | pass |
| `make ci` and `make test-integration` pass | `make ci`; `make test-integration` | see close |
| Robustness | `go test ./internal/catia -fuzz FuzzExtract -fuzztime 60s` with two new seeds (2.8 million executions); a throwaway timing check: 16 MiB of fake short-form notes, fake long-form prefixes, bare markers, and long text full of short markers each extract in 25-103 ms (6.0 s for the long-form case before `nextNote` cached marker positions) | pass; the fuzzer spends most wall time minimizing, as on the parent commit |
| Real data: extraction | `ARXGO_CATIA_LIVE_DIR=<copy> go test ./internal/catia -tags catialive -run TestExtractExperimentalAggregates -v` | 8036 files in 8.2 s, 0 errors, 0 `text_failed`, 53011 component names (unchanged); 6460 with product properties, 502 descriptions, 3097 definitions, 2447 materials; 1702 files with notes, 61544 notes, 1.9 MB of note text, 0 truncated. The counts match the prototype used for the investigation |
| Real data: binary | `bin/arxgo split --catia --catia-text` and `catia-index` on 400 files sampled from the copy (150 drawings, 150 parts, 100 products, 413 MiB) in a scratch archive | split exit 0 in 4.6 s, 19 MB max RSS; index 0.85 MB in 0.03 s, 400 sections, 0 missing, 148 sections with notes, 0 `strings:`. Composition: identity lines 50%, notes 24%, components 13%, headings 7%, properties 5%. At the previous density the index would be about 27 MB |
| Real data: rewrite recipe | the Linux manual recipe on that scratch archive with three sidecars given a `strings:` block and a foreign `operator.text.md` holding `strings:` | the three sidecars were deleted and rewritten by the rerun split (exit 0), the foreign file kept; index 400 sections, 0 missing |

Windows: cross-compiled and vetted only; the PowerShell rewrite snippet was written on Linux and
never executed (routed to W10).

## Audit handoff

- `AUD-extract-catia-notes-and-properties-1`: nonblocking. Title-block captions, the organization
  line and standard notes repeat in almost every drawing: on the copy, texts found in at least 10%
  of drawings are 65% of note occurrences, while designer names in signatures are notes too. A
  lossless follow-up is possible at index level (list repeated notes once with their file count and
  leave them out of sections), but it trades per-file completeness and needs a threshold. Next
  check: owner `approve-stage-4-on-operator-catia-copy` (the operator judges whether repeated
  captions and names in notes need removing). Disposition: routed.
- `AUD-extract-catia-notes-and-properties-2`: nonblocking. Product properties and material rely on
  byte patterns of an undocumented format. On the copy, about 10 of 2447 materials are format
  keywords, and 5 part or product files have no product run. Next check: owner
  `approve-stage-4-on-operator-catia-copy`. Disposition: routed.
- `AUD-extract-catia-notes-and-properties-3`: nonblocking, Windows-only. The PowerShell snippet that
  deletes owned sidecars holding `strings:` was not executed. Owner:
  [Windows verification](../../guide/windows-verification.md) W10. Disposition: routed.
- `AUD-review-registry-and-metadata-3` (workstation paths in harvested strings): the harvest is
  removed, so no sidecar written from now on carries those strings. Its owner remains the operator
  approval, which now reviews properties and notes instead.

## Close or resume

All gates pass on the final tree: `make ci` (fmt, vet including `GOOS=windows` and the integration
tag, tests, `build-all`, `lint-spec-plan`, `lint-doc-links`) and `make test-integration` (12.2 s).
Windows is cross-compiled and vetted only. Task removed from the plan; the operator approval
`approve-stage-4-on-operator-catia-copy` now links this record as a dependency and receives notes 1
and 2. `catia-archive` stays planned until that approval is recorded. Plan counts after: 9 open
tasks (6 agent, 3 human); next eligible `research-cloud-target-apis`.

Operator follow-up outside this task: an archive split with an earlier release keeps its
`strings:` sidecars until they are deleted and the split is rerun with `--catia-text` (manual
recipe), after which `catia-index` shows properties and notes.
