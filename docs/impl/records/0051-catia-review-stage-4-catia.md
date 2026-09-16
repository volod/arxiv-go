# Stage-4 checkpoint

## Task and scope

- Id / capability / checkpoint: `review-stage-4-catia` / `catia-archive` / none; this is the bounded
  checkpoint
- State: accepted
- Source: plan task `review-stage-4-catia`; code revision `9d9505b`, clean tree at start.
- Plan counts at start: 10 open tasks (7 agent, 3 human); next eligible `review-stage-4-catia`.
- Accepted task:

```markdown
#### review-stage-4-catia

Review CATIA payload reuse, mirror-root separation, registry layout and extraction boundaries before cloud work starts.

- Serves: `catia-archive` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: [Stage-4 proof](records/0050-catia-prove-stage-4-on-generated-archive.md).
- User-visible outcome: Stage 4 is coherent, video default is intact, and stage 3 can start without
  CATIA cloud scope.
- Scope boundary: Payload kind, mirror-root separation and history filter, registry column order,
  WAL text events,
  description marker reuse, extractor memory bounds, experimental-data ban (grep of committed files
  for strings from the gitignored tree). No speculative refactor.
- Data and artifact paths: Stage-4 records, `internal/catia/`, `internal/archive/`.
- Execution path: Invariant-to-evidence table, targeted tests, routed notes.
- Acceptance gates: Notes dispositioned; verdicts recorded; blockers repaired first; `make ci` passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.
```

- Amendments: the operator asked for the checkpoint to be run against a disposable copy of the real
  archive rather than generated fixtures only, and for registry and metadata completeness to be
  judged from the viewpoint of indexing the outputs into a search or vector database. That widened
  the evidence, not the scope: every repair below is a defect in the invariants already listed, and
  the four design gaps the wider viewing angle exposed are routed to new tasks rather than fixed
  here.

## Implementation

Five defects were found by running the built binary over a disposable copy of the operator's
archive (aggregates only: 4031 files, 66.8 GiB, 2255 CATIA files in three kinds, 149 videos) and
repaired. Each has a regression test that fails on the previous build.

1. **`--verify hash` recorded no hash on the rename path** (`internal/archive/transfer.go`). The
   copy path writes `verified` with the SHA-256, and the payload registry is rebuilt from the WAL,
   so a same-device rename left `sha256` empty in every row of `arxgo-videos.csv` and
   `arxgo-catia.csv`. The description writer hashed the destination itself, so one run produced a
   description with a hash and a registry row without one. `hashRenamed` now reads the file once
   before the rename and appends the `verified` record, which is also where the description writer
   takes its value from, so the read is not duplicated. Split only: restore keeps no hash column.
   Written before the rename, so a crash either aborts the transaction or rolls it forward with the
   hash already durable. Tests `TestSplitRecordsHashOfRenamedPayload`,
   `TestSplitWithoutHashVerifyRecordsNoHash`.
2. **CATIA text sidecars were erased from the file registry** (`internal/archive/payload_catia.go`).
   `prepare` reused the preview mechanism and added every owned sidecar to `Scan.SkipPaths`, which
   the walker treats like a reserved path. Preview clips must be hidden because a preview is a video
   and would become a candidate of the next split; a sidecar is Markdown and can never be a CATIA
   candidate, so hiding it only dropped real archive files from the inventory. On the archive copy a
   second `split --catia` wrote 4031 rows where a `scan` of the same tree wrote 6286. Test
   `TestCatiaSplitKeepsTextSidecarsInFileRegistry`.
3. **`file_type` was empty for files with a usable extension** (`internal/scanner/mimetype.go`).
   The column fell back to the file name only for `application/octet-stream`; a MIME that detection
   knows but has no canonical extension for left it empty. On the archive copy that was 194 of 4031
   rows, all `application/x-ole-storage` (CAD parts and assemblies of another vendor, and legacy
   Office documents). The fallback now applies whenever detection yields no extension. AppleDouble
   sidecars stay empty: their extension belongs to the file they describe. Test
   `TestClassifyFallsBackToNameExtension`.
4. **Every WAL step after `begin` serialized a zero `mtime`** (`internal/state/wal.go`).
   `encoding/json` ignores `omitempty` on a `time.Time`, so each later record carried
   `"mtime":"0001-01-01T00:00:00Z"`, which the WAL contract does not list among a later step's
   fields. `Record.MarshalJSON` omits it. The marshaler encodes with `SetEscapeHTML(false)` so a
   path holding `&`, `<` or `>` is written exactly as before. Test `TestRecordOmitsZeroMtime`
   (routed note `AUD-implement-catia-split-3`).
5. **A scan's finish line reported three zeroed video counters** (`internal/archive/finish.go`).
   A scan has no payload and moves nothing; `payloadFinishAttrs` now returns none for it.

No refactor was made. `internal/catia` was not touched: the extraction audit below found nothing to
repair.

Specs amended: [type detection](../../openspec/stage-1-core/registry.md#type-detection),
[`--verify`](../../openspec/stage-1-core/cli.md), [split step 5](../../openspec/stage-1-core/split-restore.md#split),
[WAL record and run report](../../openspec/stage-1-core/contracts.md#wal-record),
[text sidecars](../../openspec/stage-4-catia/split-restore.md#text-sidecars). New behavior specified
for the routed work: [column stability](../../openspec/stage-1-core/registry.md#column-stability),
[self-locating metadata](../../openspec/stage-4-catia/catia.md#self-locating-metadata),
[text index](../../openspec/stage-4-catia/catia.md#text-index).

Docs: [CATIA archive](../current/catia-archive.md), [archive registry](../current/archive-registry.md),
[video split](../current/video-split.md), [crash safety](../current/crash-safety.md),
[index](../current.md), both manuals, README.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Invariant-to-evidence table | Below | complete |
| Every routed note dispositioned | Below: 5 incoming notes | complete |
| Blockers repaired first | 5 defects repaired with regression tests before the verdict; each fails on `9d9505b` | pass |
| `make ci` | `make ci` | pass, Linux; Windows cross-compiled and vetted only |
| Declared extra: operator archive copy | Built `arxgo` over a disposable copy of the real archive (aggregates only: 4031 files, 468 directories, 66.8 GiB; 2255 CATIA in 3 kinds, 1.1 GiB; 149 videos, 60.4 GiB; all paths non-ASCII, 12 needing CSV quoting). Sequence: `scan --metadata media`, `split --catia --catia-text --verify hash`, `split --verify hash`, both reruns, `restore --catia --descriptions delete`, `restore --descriptions delete`, both reruns | pass: `catia_done` 2255, `texts_done` 2255, `texts_failed` 0, `videos_done` 149, zero WARN or ERROR lines in any run, both reruns exit 0 and leave `arxgo-catia.csv` byte-identical |
| Round trip is lossless | SHA-256, size and nanosecond mtime manifest of all 4031 files and 468 directories before and after the full sequence | pass: 0 differences, twice (once before the repairs, once after); mirror roots left holding only their retired registry |
| Payload separation | `arxgo-videos.csv` after CATIA runs, `arxgo-catia.csv` after video runs, both mirror roots | pass: 0 CATIA rows in the video registry, each mirror holds only its own payload, registry copies in archive and mirror byte-identical |

## Invariant to evidence

| Invariant | Evidence | Verdict |
| --- | --- | --- |
| Payload kind is exclusive and selects candidates | Archive copy: 2255 `is_catia` rows and 149 `is_video` rows, 0 rows with both; CATIA split moved 2255 and no video, video split moved 149 and no CATIA. `TestScanClassifiesCatiaAndNotVideo` | holds |
| Mirror roots stay separate; a copied command line cannot cross them | Each mirror root held only its own payload and registry after both splits; `internal/cli` exit-2 tests for `--catia --video-archive`, `--catia-archive` without `--catia`, and nesting | holds |
| History filter reads only the selected payload's runs | `readHistory(root, kind)` filters on `options.json` `payload`; both payload registries stayed correct across four interleaved runs in one state directory; `payload_test.go` | holds |
| Registry column order is fixed and readers accept omitted optional columns | `CatiaHeader` / `VideoHeader` / `RegistryHeader` order asserted in `internal/report` tests; all 16 CATIA columns filled on real data | holds; the *set* of columns is not stable, routed to `stabilize-registry-columns` |
| WAL text events are complete and ordered | 2255 `text_begin`/`text_done` pairs, 0 `text_failed`, 0 part files left; `text_delete`/`text_deleted` on restore removed 2255 sidecars; record 0050 kill sweep | holds |
| Description marker reuse is sound | 2255 CATIA descriptions and 149 video descriptions all began `arxgo: <rel_path>`; restore removed exactly the owned ones; no fallback name was needed on this tree | holds |
| Extractor memory is bounded by the caps, not file size | 1.1 GiB of CATIA read in a 37 s split at steady memory; window cap 4 MiB, sidecar cap 1 MiB; 1 sidecar of 2255 hit the cap and set `truncated: true`; `internal/catia` fuzz and cap tests | holds |
| Text extraction reports no silent errors | Cross-check of all 2255 files: 0 files reported `0 components` while their raw bytes held more than two CATIA name references; 8365 of 12330 component names resolve to a file present in the same archive; on a 25-file sample the extractor found every name a byte-level scan found plus 340 it did not, because it decodes UTF-16 names a byte scan misses | holds |
| Experimental-data ban | `git grep` over all committed files for base names (5+ characters) of every file and directory of the gitignored tree and of the archive copy | holds: only generic words match; no file name, path or extracted string is committed |

## Audit handoff

Incoming notes:

- `AUD-generalize-payload-split-restore-3` (`Progress.payloadOf` falls back to video totals without a
  payload): **confirmed, no output can come from the fallback.** The fallback is reached only from
  `emit` and `measure` when `p.totals.Items > 0`, and `Totals.Items` is set only by a payload phase,
  which always has the session's spec (`spec != nil` for split and restore). Scan phases report
  `entries`. Disposition: closed, no change. The related scan finish line that did print video
  counters is repair 5 above.
- `AUD-generalize-payload-split-restore-4` (runs without `payload` are silently excluded from
  history): **accepted, documented, no change.** A run directory without a readable `options.json`
  can only come from an earlier build or from state corruption; its rows survive in the existing
  registry file, which every split merges, so the loss needs a deleted registry *and* an unreadable
  options file together. Logging it would mean threading a logger through nine call sites for a
  corruption path. Disposition: closed; noted in both manuals.
- `AUD-implement-catia-split-2` (lock and exit-5 when recovering the other payload's run):
  **confirmed.** `recoverReplaced`/`lockOtherMirror` take the recorded mirror root's lock;
  record 0050's kill sweep rolled an open transaction of the other payload forward in 37 of 50 seeds,
  including restores replacing splits. The archive copy added no kills, so this rests on 0050.
  Disposition: closed, no change.
- `AUD-implement-catia-split-3` (zero `mtime` in later WAL steps): **repaired**, repair 4 above. The
  contract keeps the field; the encoder omits it.
- `AUD-prove-stage-4-on-generated-archive-1` (byte and failure counters of a resumed run are
  restored from the last checkpoint only): **contract narrowed, not reconstructed.** The item
  counters are already re-derived from the run's own WAL. The byte counters would need every
  committed transaction's size replayed and the failure counters cannot be derived at all, because a
  file skipped before its transaction began leaves no record. They are progress telemetry: no
  registry, description or recovery decision reads them. [Run report](../../openspec/stage-1-core/contracts.md#run-report)
  now says so. Disposition: closed.

New notes, all nonblocking, all routed:

- `AUD-review-stage-4-catia-1`: a registry's column set depends on the archive's current content.
  On the archive copy the same tree produced a 28-column file registry from `scan --metadata media`,
  a 12-column one from a `scan` after the videos moved out, and a video registry whose `previews`
  column is written empty while the CATIA registry's `text_rel_path` is droppable. Location
  `report.dropEmptyColumns`, `WriteVideoCSV`, `WriteCatiaCSV`. Next check: owner
  `stabilize-registry-columns`.
- `AUD-review-stage-4-catia-2`: a description or sidecar read away from its location does not name
  its archive, and a sidecar carries no identity fields at all. Location `report.RenderDescription`,
  `report.RenderCatiaText`. Next check: owner `record-source-location-in-metadata`.
- `AUD-review-stage-4-catia-3`: there is no supported way to read the extracted text of a whole
  archive at once. Shell recipes assembling the sidecars are documented in both manuals but guess
  sidecar names instead of reading recorded ownership, and break on the collision fallback names.
  Next check: owner `implement-catia-text-index`.
- `AUD-review-stage-4-catia-4`: `strings:` carries format boilerplate and decoded thumbnail bytes.
  Measured over the archive copy: 2 069 427 items, 417 229 distinct; the runs that appear in at
  least 2000 of the 2255 sidecars are 5.9% of items and 3.4% of bytes. Nonblocking and bounded, but
  an embedding index would pay for it. Location `internal/catia/strings.go`. Next check: owner
  `approve-stage-4-on-operator-catia-copy`, whose operator judges sidecar usefulness.
- `AUD-review-stage-4-catia-5`: `--metadata file` leaves media columns empty for video containers
  that are not ISO BMFF. On the archive copy 16 of 149 videos had no media fields until
  `--metadata media` ran ffprobe. Specified behavior, not a defect; the manuals now recommend
  `--metadata media` when the registry feeds an index. Next check: owner
  `approve-stage-4-on-operator-catia-copy`.

Refactor verdict: **no refactor.** The payload spec, shared event mechanism and description
renderer carried a real archive without a structural problem; the one place where sharing went too
far (preview skip paths applied to text sidecars) is repaired in place.

Decision: **proceed-with-nonblocking-notes.** Stage 3 may start. `catia-archive` stays `planned`
until `record-source-location-in-metadata` and `implement-catia-text-index` are accepted.

### Operator input prepared for `approve-stage-4-on-operator-catia-copy`

Aggregates from the archive copy, for the reviewer's judgment; no names or extracted strings:

- 2255 CATIA files: 1641 `CATPart`, 466 `CATProduct`, 148 `CATDrawing`; all `V5_CFV2`;
  release `V5R30 SP5` on 1797 and `V5R30 SP6` on 458, none unknown.
- Components: 895 files report `0 components`, and the split is by kind, as the format predicts:
  892 of 1641 `CATPart` (54.4%), 3 of 466 `CATProduct` (0.6%), 0 of 148 `CATDrawing`. The three
  empty assemblies contain no CATIA name reference at all in their raw bytes. This answers
  `AUD-prove-stage-4-on-generated-archive-2`: the zero counts are a property of parts, not a
  failure of extraction.
- Component names reference other documents beyond CATIA files, including spreadsheets used as
  design tables, which the reviewer may or may not want indexed.
- Sidecars: 2255 written, 63.5 MB total, median 8.1 KB, largest at the 1 MiB cap (1 of 2255
  `truncated: true`). Every sidecar has `properties:` and `strings:`.
- Privacy, the question the task asks about: 743 of 2255 sidecars (32.9%) contain at least one
  absolute Windows or UNC path from the authoring workstation, 5272 such items in total. No
  authoring user id was observed next to the `V5USERID` marker. Descriptions contain none of this.

## Close or resume

All gates passed on Linux; Windows cross-compiled and vetted only. Five defects repaired with
regression tests; five incoming notes dispositioned; five new notes routed. Plan task removed and
its references replaced with this record; `research-cloud-target-apis` now depends on it. Four
tasks added (`stabilize-registry-columns`, `record-source-location-in-metadata`,
`implement-catia-text-index`, `review-registry-and-metadata`) with their behavior specified first.
`archive-registry` returns from `shipped` to `planned` in the capability registry, because
`stabilize-registry-columns` adds scope to it; `catia-archive` stays `planned`.
Plan counts after: 13 open tasks (10 agent, 3 human); next eligible `stabilize-registry-columns`,
with `record-source-location-in-metadata` and `research-cloud-target-apis` eligible in parallel.

Not covered here: no kills were injected on the archive copy, so crash recovery evidence remains
record 0050's generated sweep; and the operator decision in
`approve-stage-4-on-operator-catia-copy` is still open.
