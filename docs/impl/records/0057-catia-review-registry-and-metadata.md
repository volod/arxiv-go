# Registry and metadata checkpoint

## Task and scope

- Id / capability / checkpoint: `review-registry-and-metadata` / `catia-archive` / none; this is the
  bounded checkpoint
- State: accepted
- Source: plan task `review-registry-and-metadata`; code revision `3d70363`, clean tree at start.
- Plan counts at start: 10 open tasks (7 agent, 3 human); next eligible
  `review-registry-and-metadata`; also eligible `research-cloud-target-apis`.
- Accepted task:

```markdown
#### review-registry-and-metadata

Review registry schema stability and the metadata a detached reader sees before cloud work starts.

- Serves: `catia-archive` -- [Development integrity](../openspec/spec.md#development-integrity)
- Agent status: CLEAR
- Task kind: checkpoint
- Dependencies: [Registry full schema](records/0052-registry-stabilize-registry-columns.md);
  [Preserved archive registry](records/0053-registry-preserve-archive-registry.md);
  [Registry detection reuse](records/0054-registry-reuse-registry-detection.md);
  [Self-locating metadata](records/0055-catia-record-source-location-in-metadata.md);
  [CATIA text index](records/0056-catia-implement-catia-text-index.md).
- User-visible outcome: Registry schemas and metadata documents are coherent across commands and
  payloads, and stage 3 can start without reopening them.
- Scope boundary: Full-schema writers, preserved rows against the run history, owned-artifact
  exclusion, the reuse rule and stamp lifecycle, description and sidecar field order and escaping, index correctness against the run history, cap accounting. No speculative
  refactor.
- Data and artifact paths: The records of the five dependencies above, `internal/report/`,
  `internal/archive/`.
- Execution path: Invariant-to-evidence table, targeted tests, routed notes.
- Acceptance gates: Notes dispositioned; verdicts recorded; blockers repaired first; `make ci`
  passes.
- Documentation target: `docs/impl/current.md`
- Review checkpoint: none; this is the bounded checkpoint.
```

- Amendments: the operator asked for the checkpoint to be tested on a real archive (a disposable
  copy of the experimental CATIA tree, the tree record 0051 used) and for the file registry and the
  CATIA text index of that run to be prepared as review input for
  `approve-stage-4-on-operator-catia-copy`. During the task the operator also asked that no name,
  path or string of that archive, and no invented value echoing its vocabulary, appear in code,
  tests or documentation. This widened the evidence and the review input, not the scope: the
  repairs below are defects in the listed invariants. The review input is kept outside the
  repository and its location was given to the operator directly.

## Implementation

Four defects repaired, each with a regression test that fails on `3d70363`:

1. **A file restored after a scan reconstructed its row kept the reconstructed values**
   (`internal/archive/scan_reuse.go`, `archive_view.go`, `scan.go`; routed note
   `AUD-reuse-registry-detection-2`). `restore --registry-update` sets `location` back to `archive`
   without detection. When the scan before the restore could not read the mirror, the row came from
   the run history (`registry-row-reconstructed`) or from a non-reusable base (`registry-row-kept`),
   and the next scan reused it for the present file. A raw-XML `.3dxml` then kept `file_type` `3dxml`
   and `is_binary` `true` where detection writes `xml` and `false`, so the registry differed from
   `--redetect`, which the reuse rule forbids. `replayMoves` now returns the commit time of the
   restore that returned each file (`restoreCommitTimes`), the view keeps it
   (`archiveView.restored`), and `baseStream.at` detects a file whose restore committed at or after
   the base scan start, to the second. The first scan after a restore therefore detects the
   restored files once; the scan after it reuses them again. Test
   `TestScanDetectsFilesRestoredSinceBaseScan`.
2. **`moved_at` ignored the session clock** (`internal/archive/split_description.go`; routed note
   `AUD-record-source-location-in-metadata-3`). `NewMarkdownDescription` defaulted `Now` to the wall
   clock, so the branch that gives a writer the session clock never ran for the writer the CLI
   builds. The default is gone; `MarkdownDescription.now` falls back to the wall clock only while no
   session attached one. Production output is unchanged (the session clock is the wall clock);
   descriptions in tests now follow `Config.Now`. Test `TestSplitDescriptionUsesSessionClock`.
3. **Value quoting read single bytes at the edges** (`internal/report/description.go`).
   `quoteValue` tested `unicode.IsSpace(rune(v[0]))` and the same for the last byte. The last byte
   of a two-byte Cyrillic or Latin-extended letter can be `0x85` or `0xA0`, which read as U+0085 or
   U+00A0, so a value ending in such a letter (U+0445, U+0420, U+0105) was quoted, and a value
   starting with a multi-byte space (U+2003) was not. Both edges are now decoded as runes. The
   reader always unquoted, so files written before stay readable and ownership checks are
   unaffected. On the real archive no value was affected (0 unnecessarily quoted of 41,651 value
   lines in the index), because its paths end in ASCII extensions; an archive root or a 3dxml title
   ending in such a letter would have been. Test `TestQuoteValueDecodesEdgeRunes`.
4. **`file_size` was omitted for an empty file** (`internal/report/description.go`). The contract
   writes the field always; the renderer skipped a zero size. CATIA files are selected by name, so an
   empty `.CATPart` is moved and described. It now writes `file_size: 0 (0 B)`, and the sidecar
   identity repeats it. Test `TestRenderDescriptionEmptyFileHasSize`.

Specs amended: [incremental update](../../openspec/stage-1-core/registry.md#incremental-update)
(reuse rule and stability evaluation for restored files),
[video description rules](../../openspec/stage-1-core/contracts.md#video-description) (white space
decoded as Unicode; `file_size` of an empty file),
[registry stamp](../../openspec/stage-1-core/contracts.md#registry-stamp) (written indented like the
other state JSON files).

Docs: [archive registry](../current/archive-registry.md), [CATIA archive](../current/catia-archive.md),
[index](../current.md), both manuals, `README.md`.

No refactor. Rejected alternatives for repair 1: a stamp field that marks registries holding kept
or reconstructed rows (a JSON contract change and a version bump for a rare case); letting restore
clear a cell so reuse fails (a scan after every restore would rewrite the registry). The chosen rule
costs one detection of each restored file; on the archive copy an untraced full `--redetect` with
`--metadata media` of all 4031 files takes 0.45 s.

## Acceptance evidence

Linux amd64, Go 1.27.1. Generated fixtures in `t.TempDir()`; real-archive runs on a disposable copy
(aggregates only: 4031 files, 468 directories, 66.8 GiB; 2255 CATIA files, 1.1 GiB; 149 videos,
60.4 GiB), driven by a script outside the repository with the built binary and the pinned FFmpeg
9.0.1 tools.

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Notes dispositioned | [Audit handoff](#audit-handoff): 12 incoming notes, 3 new | complete |
| Verdicts recorded | [Invariant to evidence](#invariant-to-evidence), refactor verdict, decision | complete |
| Blockers repaired first | 4 defects above with regression tests, each failing on `3d70363` (checked by reverting the fix and running the test) | pass; none was a blocker for stage 3, all repaired before the verdict |
| `make ci` | `make ci` | pass on Linux: fmt, vet (incl. `GOOS=windows` and the integration tag), tests, `build-all`, `lint-spec-plan`, `lint-doc-links`; Windows cross-compiled and vetted only |
| Integration | `make test-integration` | pass (6.1 s) |
| Declared extra: pass 1 (build of `3d70363`) | Scan, rescan, `--redetect`, CATIA split with `--catia-text --verify hash`, index, video split with previews interrupted by SIGINT and resumed, registry rebuild, both restores, `--redetect`, manifest | pass: every registry invariant below held; the planned kills of the CATIA split and restore did not fire (driver fault), so pass 2 repeats them |
| Declared extra: pass 2 (fixed build) | Same sequence with the CATIA split killed (SIGKILL) after 700 commits and resumed, the CATIA restore killed after 903 commits and resumed, a scan killed mid-walk and resumed, files added and removed, scans traced with `strace -e openat` | pass: rows below; the only WARN lines are 5 lock takeovers after the kills, the SIGINT interruption notice and 3 specified preview-name fallbacks (logged by both processes of the interrupted video split); round trip byte-identical |
| Round trip | SHA-256 of 4031 files; path, type, size, mtime (ns) and mode of every file; path and mode of every directory, before and after pass 2 | pass: equal. Directory modification times are not part of the established manifest (they change when a split renames files out); pass 1 recorded 265 changed directory times, routed below |

Pass 2 in detail (counts from run reports, `strace` and a consistency script over the written files):

| Step | Result |
| --- | --- |
| `scan --metadata media`, then a traced rescan | 4031 rows, 41 columns; rescan `reused` 4031, 0 files opened, registry `unchanged` with the same mtime |
| `--redetect`; scan with `--checkpoint-every 1` killed mid-walk (checkpoint held `registry_base`), resumed | both byte-identical to the first registry |
| `split --catia --catia-text --verify hash` killed after 700 commits, resumed | `catia_done` 2255, `texts_done` 2255; registry changed only `location` of 2255 rows; traced scan after it opened 0 files in the archive and both mirrors (`preserved` 2255 from the stamped base); rerun left `arxgo-catia.csv` byte-identical |
| `catia-index`, `--strings`, rerun | 2255 sections = 2255 `moved` rows, walk order, 0 missing text; rerun byte-identical; strings-stripped `--strings` output equal to the default |
| Video split with previews, SIGINT after 30 previews, resumed | `videos_done` 149, `previews_done` 298, 0 failed; only `location` of 149 rows changed; traced scan opened 0 files; index byte-identical after it |
| Registry and stamp moved aside, traced scan | opened 1627 archive files and the 2255 CATIA and 149 video mirror copies; registry byte-identical; `--redetect` identical |
| Consistency of written files | 2255 of 2255 CATIA descriptions: field order, `arxgo`, `archive`, `sha256`, `file_size` and `catia` equal to `arxgo-catia.csv`; 2255 of 2255 sidecars: field order, identity lines byte-identical to the description, `description:` path; 149 of 149 video descriptions: field order, `archive`, `sha256`, `video:` line, preview links equal to `previews`; 2255 of 2255 index sections: identity equal to the description, `text:` equal to `text_rel_path`, `truncated`, `properties:` and `components:` equal to the sidecar, `strings:` equal to the sidecar; 4957 owned artifacts (descriptions, sidecars, previews), 0 with a registry row; 0 values quoted without need; stamp size and SHA-256 equal to the registry |
| Added CATIA copy and text file, scan, `split --catia --catia-text` | +2 rows, no other line changed; the split changed only the added file's `location`; index 2256 sections |
| `restore --catia --descriptions delete --registry-update` killed after 903 commits, resumed; traced scans | `catia_done` 2256; only `location` changed; first scan opened exactly the 2256 restored files and left the registry `unchanged`; second scan opened 0 |
| `restore --descriptions delete --previews delete --registry-update`, scan, additions removed, scan, `--redetect` | first scan opened only the 149 restored videos, `unchanged`; after removing the additions the registry is byte-identical to the first scan of the run, and so is `--redetect` |

## Invariant to evidence

| Invariant | Evidence | Verdict |
| --- | --- | --- |
| Full-schema writers: one header per CSV whatever the command, payload and options | Every registry comparison in both passes had an equal header (41-column file registry); `arxgo-catia.csv` and `arxgo-videos.csv` written by split and restore carry `CatiaHeader` and `VideoHeader`; `TestRegistryHeadersAreStableAcrossCommands`, `TestRegistryWritersWriteFullHeader`, `TestRegistryReadersAcceptCompactedFiles` | holds |
| Preserved rows follow the run history, not the payload registries or the mirror content | `preserved` 2255 after the CATIA split and 2404 after both splits, equal to the replayed moves; rebuild without registry and stamp byte-identical; `TestFileRegistryRebuildsFromMirrors`, `TestFileRegistryWithUnreadableMirrors`, `TestFileRegistryIgnoresDeletedPayloadRegistries` | holds |
| Owned artifacts have no row; everything else does | 4957 owned artifacts, 0 rows; 4031 rows through every split, +2 and -2 exactly for additions and removals; `TestCatiaSplitExcludesTextSidecarsFromFileRegistry`, `TestFileRegistryKeptDescriptionStaysOwned` | holds |
| Reuse rule: a reused row equals the row detection writes | 0 files opened by unchanged rescans; every `--redetect` byte-identical; kill and resume with `registry_base`; restored files detected once (repair 1); `TestScanReusesUnchangedRows`, `TestScanReuseDetectsChanges`, `TestScanResumeRestartsOnChangedBase`, `TestScanDetectsFilesRestoredSinceBaseScan` | holds after repair 1 |
| Stamp lifecycle: stamp names the placed registry after scan, split location update and restore | Stamp size and SHA-256 equal to the registry after the splits; unchanged scans after split and restore prove the stamp matched; `checkStampMatches` in the lifecycle test | holds |
| Description and sidecar field order and escaping | Consistency counts above; `TestRenderDescriptionGoldens`, `TestRenderCatiaTextIdentityMatchesDescription`; repairs 2 to 4 | holds after repairs 2 to 4 |
| Index correctness against the run history | Consistency counts above; sections equal to `moved` rows before and after additions, 0 after restore; `TestCatiaIndexCoversMovedFilesAndListsMissingText`, `TestCatiaIndexReadsRecordedOwnershipNotNames` | holds |
| Cap accounting | 0 of 2255 sidecars over 1 MiB, 1 at the cap with `truncated: true`; identity header median 906 B, maximum 1853 B, below the 4 KiB preflight reserve; preflight estimate 609 MB for 65.3 MB written; `TestRenderCatiaTextOversizedIdentity`, `TestRenderCatiaTextIdentityCountsAgainstCap` | holds |

## Audit handoff

Incoming notes:

- `AUD-preserve-archive-registry-1` (stamp written indented while the contract example is
  compact): **confirmed as consistent, contract clarified.** `options.json`, `checkpoint.json`,
  `report.json` and the stamp share `state.WriteJSON`; only the lock is compact. Readers decode
  either form. The contract now says the stamp is indented and the example is compact. Closed.
- `AUD-preserve-archive-registry-2` (every archive scan replays the whole history): **measured.**
  `loadArchiveView` costs about 2 us and 0.7 KB of memory per WAL record, linearly: the real history
  of one split and restore of both payloads (31,849 records) loads in 78 ms and 45 MB; the same
  history replicated 10 and 50 times loads in 0.62 s / 237 MB and 3.1 s / 1.2 GB. Nonblocking for
  this archive; memory is the limit on a long history. Routed as
  `AUD-review-registry-and-metadata-1`.
- `AUD-reuse-registry-detection-1` (a file made unreadable without changing size or mtime keeps its
  reused row): **accepted, no change.** A portable check needs an open, which the rule exists to
  avoid; `--redetect` reports it. Documented in the archive registry page. Closed.
- `AUD-reuse-registry-detection-2` (restore returns a reconstructed row that is reused):
  **repaired**, repair 1.
- `AUD-reuse-registry-detection-3` (`media metadata unavailable` is logged only on detection):
  **accepted, no change.** The warning reports this run's detection; the reused row still carries
  `media_error`, and on the archive copy no row had one. Documented. Closed.
- `AUD-record-source-location-in-metadata-1` (Windows `archive:` forms): stays with
  [Windows verification](../../guide/windows-verification.md) W10. No change.
- `AUD-record-source-location-in-metadata-2` (4 KiB identity reserve is an estimate): **measured,
  closed.** Largest identity header 1853 B on real paths with percent-encoded Cyrillic `moved_to`
  URLs; the estimate overstates the written sidecars about ninefold because extraction is far
  smaller than the file.
- `AUD-record-source-location-in-metadata-3` (session clock not used): **repaired**, repair 2.
- `AUD-record-source-location-in-metadata-4` (a sidecar copies an operator-edited description):
  **confirmed as specified.** The description is the source of truth for the identity block, so a
  sidecar never disagrees with the description next to it. Closed.
- `AUD-implement-catia-text-index-1` (Windows `--out` checks): stays with W10. No change.
- `AUD-implement-catia-text-index-2` (index accepts a sidecar by its first line, restore also
  checks its size): **confirmed, both correct.** The index copies text and must show what the
  sidecar says now; restore deletes and must not remove an edited file. Closed.
- `AUD-implement-catia-text-index-3` (`history_at` names the history, not the disk):
  **confirmed.** It lets a rerun over the same state be byte-identical; the `Missing text` section
  and the counts show the disk. The manuals say so. Closed.

New notes:

- `AUD-review-registry-and-metadata-1`: nonblocking. Memory of an archive scan grows with the
  WAL history of every split and restore run (about 0.7 KB per record, measured above), because
  `readHistory` keeps every record of every run. Stage 3 adds publish steps to split runs, which
  would grow this history. Location `internal/archive/history.go`, `archive_view.go`. Next check:
  owner `review-stage-3-cloud` (confirm publish records do not enter or are bounded in the archive
  view's replay). Disposition: routed.
- `AUD-review-registry-and-metadata-2`: nonblocking. A split and restore round trip keeps every
  file's content, size, mode and modification time, but not the modification times of directories
  that held moved files (265 on the archive copy), because moving files out and back, and writing
  and deleting descriptions, updates them. No spec promises directory times. Next check: owner
  `approve-stage-4-on-operator-catia-copy` (the operator judges whether directory times matter).
  Disposition: routed.
- `AUD-review-registry-and-metadata-3`: nonblocking, observation for the same owner. Absolute
  workstation paths occur in harvested strings: 244 of 2255 sidecars hold at least one item with a
  drive-letter (`X:\`, `X:/`) or UNC (`\\host\`) path, 1249 items. Record 0051 reported 743 sidecars
  with 5272 items under a broader, unrecorded pattern. Descriptions contain none. Next check: owner
  `approve-stage-4-on-operator-catia-copy`. Disposition: routed.

Refactor verdict: **no refactor.** The repairs are local; the replay-memory note is routed rather
than refactored here.

Decision: **proceed-with-nonblocking-notes.** Registry schemas and metadata documents are coherent
across commands and payloads; stage 3 may proceed without reopening them.

### Review input prepared for `approve-stage-4-on-operator-catia-copy`

Kept outside the repository with a short guide: the file registry after the CATIA and video splits
of pass 2 (4031 rows), `arxgo-catia.csv`, the CATIA text index and the index with `--strings`.
Aggregates for the reviewer: 2255 CATIA files (1641 `CATPart`, 466 `CATProduct`, 148 `CATDrawing`),
895 with `0 components`, 12,330 component items; sidecars 65.3 MB, median 8.9 KB; `strings:`
2,069,391 items, 417,193 distinct, 55 distinct strings in at least 90% of sidecars making up 5.9% of
the items; workstation paths as in note 3; directory times as in note 2.

## Close or resume

All gates passed on Linux; Windows cross-compiled and vetted only. Four defects repaired with
regression tests, twelve incoming notes dispositioned, three new notes routed. The task is removed
from the plan; `approve-stage-4-on-operator-catia-copy` now links this record for its review input.
`catia-archive` stays `planned`: its human task `approve-stage-4-on-operator-catia-copy` is still
open, and `make lint-spec-plan` rejects a shipped capability with open tasks. Plan counts after: 9
open tasks (6 agent, 3 human); next eligible `research-cloud-target-apis`.
