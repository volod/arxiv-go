# CATIA Archive

Accepted work: [0043 CATIA classification](../records/0043-catia-implement-catia-classification.md);
[0044 Payload split and restore](../records/0044-catia-generalize-payload-split-restore.md);
[0045 CATIA extraction](../records/0045-catia-implement-catia-extraction.md);
[0046 CATIA split](../records/0046-catia-implement-catia-split.md);
[0047 CATIA text sidecars](../records/0047-catia-implement-catia-text-sidecars.md);
[0048 CATIA restore](../records/0048-catia-implement-catia-restore.md);
[0049 replaced restore sidecar cleanup](../records/0049-catia-repair-replaced-restore-sidecar-cleanup.md);
[0050 stage-4 proof](../records/0050-catia-prove-stage-4-on-generated-archive.md);
[0055 self-locating metadata](../records/0055-catia-record-source-location-in-metadata.md);
[0056 CATIA text index](../records/0056-catia-implement-catia-text-index.md);
[0057 registry and metadata checkpoint](../records/0057-catia-review-registry-and-metadata.md).
Specification: [CATIA files](../../openspec/stage-4-catia/catia.md);
[split and restore](../../openspec/stage-4-catia/split-restore.md). CATIA split, `--catia-text`
sidecars, `catia-index` and CATIA restore ship and are proven through the built binary on a generated archive and
on a disposable copy of the operator archive. Every agent task and both checkpoints are accepted; the
capability stays planned until the operator approval `approve-stage-4-on-operator-catia-copy` is
recorded.

## Classification (`internal/catia`, `internal/scanner`)

`scan` always classifies CATIA files. There is no CATIA flag on scan.

`internal/catia` is a leaf package that owns the built-in kind table. The scanner sets `is_catia`
from the last dotted suffix, compared case-insensitively:

| Extension | Kind token |
| --- | --- |
| `.CATPart` | `CATPart` |
| `.CATProduct` | `CATProduct` |
| `.CATDrawing` | `CATDrawing` |
| `.cgr` | `cgr` |
| `.3dxml` | `3dxml` |

Content is not read for this flag, so a truncated or empty CATIA file is still marked. A macOS
AppleDouble sidecar (`._<name>`, magic `00 05 16 07`) is never CATIA, whatever its extension.
`is_catia` files are never `is_video`. MIME, `file_type`, `is_binary`, `is_picture` and `is_media`
follow the stage-1 rules after that override (`is_media` is video, picture or `audio/`).

A built-in CATIA extension in `--video-extensions` is a usage error (exit 2), on both `scan` and
`split`.

The file registry always has `is_catia` as required column 11, after the other type flags, in the
[column order](../../openspec/stage-1-core/contracts.md#file-registry-csv). Readers require this
header; registries written in the previous order do not load. Scan statistics include a `catia`
count and byte total, omitted from report JSON when zero. Root-level `arxgo-catia.csv` is reserved
the same way as `arxgo-videos.csv`.

## Extraction (`internal/catia`, `internal/report`)

`catia.Extract` / `ExtractPath` reads one CATIA file in pure Go. Kind comes from the file name;
format from the leading bytes (`V5_CFV2`, `zip`, `xml`, or `unknown`). Memory is bounded by the
component window (4 MiB), ZIP member caps (1024 members, 64 MiB each, 256 MiB total) and the 1 MiB
sidecar collection cap, not by file size. An extraction error fills empty/unknown fields and sets
`ErrorKind`; it never panics. A broken 3dxml root is `TextFailed` (no sidecar body).

- **V5.** Length-prefixed string properties: `LastSaveVersion` (first wins; `<Release>` / `<ServicePack>`
  written as `<Release>30/<Release>`), then `MinimalVersionToRead`, plus `CATBuildLevel` for the
  sidecar. Components: stream to the first `CATOctetArray` ... `0x08FINJPL` window, strip `;` U+0001,
  split on U+0001 U+0004 `File`, skip malformed and `feat` chunks, drop the file's own base name,
  unique-sort. A missing window is `0 components`.
- **3dxml.** Raw XML or `archive/zip`. Unsafe ZIP names (`..`, absolute, volume) are skipped. Header
  `SchemaVersion` becomes release `3DXML <version>`. `ReferenceRep` `associatedFile` and `urn:3DXML:`
  file parts become components (external URLs ignored); `Reference3D` / `Instance3D` `name` values
  go to strings.
- **Strings.** ASCII 0x20-0x7E and UTF-16LE runs, at least 6 characters with 3 ASCII letters,
  excluding component names, unique-sorted. ZIP 3dxml harvests XML text and attributes, not
  compressed bytes.
- **Report.** `RenderDescription` with `Catia` set writes `catia:` instead of `created:` / `video:`.
  `CatiaLine` is `kind | format | release | N component(s)`. `RenderCatiaText` writes the
  `arxgo-text:` sidecar (archive, identity block, properties, components, strings) with the 1 MiB
  cap and `truncated`; see [self-locating metadata](#self-locating-metadata-internalreport-internalarchive).

CATIA split calls `ExtractPath` on the placed destination for the description (the run context,
so Ctrl+C can stop a large-file pass). With `--catia-text`, a second `ExtractPath` after `commit`
writes the sidecar.

## Payload executor (`internal/archive`, `internal/state`, `internal/report`)

Split and restore run one executor for every payload kind. `archive.Config.Payload` holds the kind
and its mirror root; `cli` sets `video` or `catia` and the matching archive. Both kinds split and
restore.

- **Payload spec** (`payload.go`, `payload_video.go`). The kind selects the candidate predicate
  (`ScanConfig.Candidate`; video: `is_video`), the preflight role and need names (`video_archive`,
  `video_registry`, `videos`, `largest_video`), the payload registry name and loader, the counters
  (video: `videos_*`, `video_bytes`, `video_archive_bytes_*`) used by execution, progress, the
  partial status and the report roots, and two hook sets. Split hooks prepare (registry, history,
  preview index, skip paths), plan (preview bytes for preflight), start the post-commit sidecar
  (preview queue with catch-up, or CATIA text sidecars) and write the registry. Restore hooks provide the registry rows and
  extra candidates, finish interrupted sidecar work, run per-commit cleanup (`--previews delete`)
  and update the registry. Transfer, WAL `begin` through `commit`, recovery, preflight, progress,
  locks, description occupancy and conflict naming are shared code that names the mirror root
  generically.
- **Run history by payload** (`history.go`). `readHistory(archive, kind)` returns only runs whose
  `options.json` names that payload, with that run's archive and mirror roots. The video registry,
  the preview index, restore description hints (through the payload registry) and mirror directory
  cleanup use video runs only; the CATIA registry, text index and CATIA mirror cleanup use CATIA
  runs. A history entry also keeps the run's operation and recorded options. Scans and run directories without readable options are not history.
- **Run options.** Split and restore write `payload` and the matching root (`video_archive`, or
  `catia_archive` for a CATIA run). The payload is a defining option. Replacing an interrupted run
  rebuilds its resolver from the recorded payload (`RecovererFor(op, payload, options)`); another
  payload or mirror root exits 5 naming that payload's mirror flag, and a run without a payload is
  corrupt state.
- **Payload registry columns** (`report.PayloadHeader`, `report.PayloadRow`). Columns 1-10 are
  `rel_path`, `file_name`, `status`, `url`, `description_rel_path`, `file_size`, `sha256`,
  `transfer`, `run_id`, `file_mime`, shared by `arxgo-videos.csv` (then `previews` and metadata)
  and `arxgo-catia.csv`. Registries in the earlier video order do not load.
- **Sidecar event families** (`state.EventFamily`, `wal_event.go`). Begin, done or failed, delete
  and deleted steps of a family use the version 2 envelope through `WAL.BeginEvent` and
  `WAL.FinishEvent`; a family's outcome for another family's begin is rejected on write and is
  corrupt on replay. `archive.eventIndex` replays one family (owned, generating, deleting) and
  removes part files of unfinished generations with the family's part-path rule. Previews and
  CATIA text sidecars (`state.TextEvents`) are the registered families. `ownedPath` is the last
  successful `Done` for that owner (`text_rel_path`).

## CATIA split (`internal/cli`, `internal/archive`, `internal/report`, `internal/state`)

`arxgo split --catia --archive PATH --catia-archive PATH` moves every `is_catia` file into the
CATIA archive through the shared executor. `--catia-text` (defining; `ARXGO_CATIA_TEXT`) writes
owned text sidecars after each commit and for earlier moved files that still lack one. Default
`split` (or `--video`) still moves only videos.

- **Flags and validation** (`cli.selectPayload`, `cli.checkRoots`). `--video` and `--catia` are
  split flags; `--catia-archive` is a common flag of split and scan (scan ignores it). A payload
  flag on the command line ignores both `ARXGO_VIDEO` and `ARXGO_CATIA`. Exit 2 before the lock:
  both payloads; `--video-archive` on the command line with `--catia`; `--catia-archive` on the
  command line without it; a missing selected root; `--sample` or `--image` other than `none` with
  `--catia`; `--catia-text` without `--catia`; `--publish` (still reserved). The archive, the selected root and the other payload's
  root when it is set from any source are pairwise neither equal nor nested (the other root is
  compared by path and need not exist). Only the selected root is stored in the options
  (`Common.VideoArchive` or `Common.CatiaArchive`), so an `ARXGO_VIDEO_ARCHIVE` value never becomes
  a defining option of a CATIA run. `SplitOptions.CreateMirror` replaces `CreateVideoArchive`.
  Restore flags are covered in [CATIA restore](#catia-restore-internalcli-internalarchive).
- **Payload spec** (`payload_catia.go`). Candidate `is_catia`; preflight role `catia_archive` with
  needs `catia_registry`, `catia`, `largest_catia`; counters `catia_done`, `catia_skipped`,
  `catia_failed`, `catia_bytes`, `catia_archive_bytes_written`/`_freed` (checkpoint, report,
  progress, finish log keys); post-commit text sidecars when `--catia-text`; restore hooks in
  `restore_catia.go`. A resumed run must also match the
  recorded payload, mirror root and `--catia-text`. Prepare always rebuilds the text event index
  (skip paths, unfinished `.arxgo-part` cleanup, `text_rel_path`) even when this run does not
  generate sidecars.
- **Description.** `DescriptionConfig.Payload` `catia` makes `MarkdownDescription.Write` run
  `catia.ExtractPath` on `tx.Begin.Dst` after `placed` (execute and recovery), render the `catia:`
  line without `video:`/`created:`, and keep a `state.CatiaSummary` (kind, format, release empty
  when unknown, component count) for the `described` record. The run context is attached
  (`useContext` / `resolverAttach`) so a cancel stops the metadata pass. Recovery gets the summary
  through the optional `state.DescribedAnnotator` (`SplitResolver.DescribedCatia`). An extraction
  error is logged at warn with its kind (`CATIA metadata extraction failed; description has empty
  values`) and never fails the move. Occupancy, fallback names (`.arxgo.md`), conflicts, adoption
  and source-changed retry are the video rules.
- **Registry** (`report.CatiaHeader`, `CatiaRow`, `WriteCatiaCSV`, `LoadCatiaCSV`,
  `MergeCatiaRows`). `arxgo-catia.csv` in both roots: payload columns 1-10, then `text_rel_path`
  (last `text_done` for that `rel_path`, empty when none), `catia_kind`, `catia_format`,
  `catia_release`, `catia_components`, `mtime`; every column is always written and readers accept
  any canonical-order subset from earlier builds
  ([0052](../records/0052-registry-stabilize-registry-columns.md)). Rows replay CATIA runs only
  (`splitEvents`, shared
  with video) with the video status rules; the summary comes from the `described` record, so
  regeneration never re-reads CATIA files; `mtime`, name, size and MIME are completed from the file
  registry; URLs follow the video rules with the CATIA archive as mirror. A corrupt registry stops
  before the first move (exit 5). CATIA split never writes `arxgo-videos.csv`, and video split
  never writes `arxgo-catia.csv`.
- **Recovery by the other payload** (`recoverReplaced`, `lockOtherMirror`). When a split or restore
  replaces an interrupted run of the other payload in the same archive, it takes the lock of the
  mirror root that run recorded (with the current `--force-unlock` setting), rebuilds its resolver
  (`cli.recovererFor` accepts CATIA split runs) and releases the lock after recovery. A missing root
  exits 5 naming it. A run of the same payload on another mirror root, or a scan, still exits 5.

## CATIA text sidecars (`internal/archive`, `internal/report`, `internal/state`, `internal/cli`)

`split --catia --catia-text` generates owned Markdown sidecars after `commit`, sequentially (no
preview-style queue), reading the mirror copy. Catch-up covers CATIA files earlier CATIA splits
moved that are still in the mirror and still lack a published sidecar.

- **Path and occupancy.** `<rel_path>.text.md`; when that name holds anything that is not an owned
  sidecar for this `rel_path`, `<rel_path>.arxgo.text.md`, then the indexed description name with a
  `.text.md` suffix. Occupancy reads the first line (`arxgo-text: <rel_path>`, first 64 KiB) with
  `InspectTextSidecar`; a hyphenated marker cannot go through `parseField`, so inspection splits on
  `: ` and unquotes. A foreign file is kept; the conflict is logged.
- **WAL.** `state.TextEvents`: `text_begin` (sidecar path) then `text_done` (size) or `text_failed`
  (reason), version 2 envelope, `BeginEvent`/`FinishEvent`. The body is written as
  `<dst>.arxgo-part`, fsynced, and renamed without replacing. Crash points `wal:text_begin`,
  `fs:text_part` and `fs:text_sidecar`. The next CATIA split removes unfinished part files and
  retries; an existing owned file at the generating path is adopted. Cancellation leaves the event
  unfinished (exit 130), not `text_failed`.
- **Failures.** Extraction errors (`TextFailed` or `Err`, including a broken 3dxml root) and an
  occupied rename never roll back the move. They count in `texts_failed` (never `catia_failed` /
  `videos_failed`), appear as issues, and finish as `StatusPartial` (exit 6). A later
  `--catia-text` run writes the missing sidecar. Without `--catia-text`, split neither creates nor
  removes sidecars.
- **Preflight.** `Candidates.TextBytes` is min(1 MiB, file size + 4 KiB) per moved or remaining
  file that still needs a sidecar, as need `texts` on the archive device; the 4 KiB
  (`textIdentityReserve`) covers the archive and identity lines.

## Self-locating metadata (`internal/report`, `internal/archive`)

A description or sidecar copied into a search index still says which archive and file it is about
([0055](../records/0055-catia-record-source-location-in-metadata.md)).

- **`archive:`** is the second line of every description (video and CATIA, the shared
  `RenderDescription`, from `DescriptionConfig.Archive`) and of every sidecar (`CatiaTextInput.Archive`,
  the split's archive root). The CLI passes an absolute root; `splitDescriptions` fills an empty
  writer root from the session. Marker lines, occupancy, restore and recovery are unchanged, and
  descriptions of earlier runs are not rewritten.
- **Identity block.** After `archive:` a sidecar writes `file_name` (base of `rel_path`), then
  `file_size`, `file_mime`, `sha256`, `modified`, `catia`, `moved_to`, `url` copied verbatim from
  the parsed owned description (`report.TextIdentityOf`), and `description:` (its archive-relative
  path), then `extracted_at` and `truncated`. Values re-quote with the description escaping, so each
  line is byte-identical to the description's.
- **Finding the description** (`text_identity.go`). The path the `described` WAL record of an
  earlier CATIA run names (`movedDescriptions` over the run history), when still owned; otherwise
  `report.FindDescriptionPath`, which walks the description naming order (both fixed names, then
  indexed names up to the first absent one). With no owned or readable description the sidecar keeps
  `file_name` only and split warns `text sidecar has no description identity`.
- **Cap.** The marker, `archive`, `extracted_at` and `truncated` lines are reserved first. Identity
  lines are kept in order while they fit; the first that does not fit drops it, every later identity
  line and all blocks, and sets `truncated: true`. Blocks then fill the remainder as before, so the
  identity block reduces the room for components and strings.
- **Values** (`report.quoteValue`). A value is quoted when it holds a quote, a backslash or a line
  break, or starts or ends with a white-space character. The edges are decoded as runes: before
  [0057](../records/0057-catia-review-registry-and-metadata.md) the last byte of a two-byte letter
  such as U+0445 read as U+0085 and quoted the value. `file_size` is written for an empty file too
  (`0 (0 B)`), and `moved_at` follows the session clock of the run that writes the description.

## CATIA text index (`internal/archive`, `internal/report`, `internal/cli`)

`arxgo catia-index --archive PATH [--out PATH] [--strings]` writes one Markdown document of every
moved CATIA file ([0056](../records/0056-catia-implement-catia-text-index.md); contract:
[CATIA text index](../../openspec/stage-1-core/contracts.md#catia-text-index)).

- **Source** (`archive.CatiaIndex`, `catia_index.go`). The CATIA run history (`readHistory`) is
  replayed with `replayCatiaRows`, the function that writes `arxgo-catia.csv`; local `moved` rows
  become sections in walk order (`scanner.Compare`). The owned description is
  `ownedDescriptionPath` (the `described` WAL path while still owned, then the naming order, shared
  with sidecar identity); the owned sidecar is the last `text_done` of the text event index, used
  when its first line is `arxgo-text: <rel_path>`. No file name is guessed, so fallback names index
  under the right file.
- **Document.** Header `archive`, `catia_archive` (latest CATIA run's mirror), `history_at` (newest
  history record, so reruns are byte-identical), `files`, `components`, `missing_text`. A section
  holds `file_name` and the description identity lines (from the sidecar, without `description:`,
  when the description is gone; logged), `text:` and `truncated:`, then the sidecar's
  `properties:` and `components:` items byte-identical, and `strings:` only with `--strings` (read
  in a second pass per sidecar, so strings are never held in memory). Files without a usable
  sidecar keep their section and are listed once under a final `## Missing text` as
  `- <reason>: <rel_path>` (`not_recorded`, `missing`, `foreign`, `unreadable`), with one warning.
- **Read-only.** No run directory, run log or lock; the output goes through `fsops.AtomicWrite`.
  `state.InspectLock` classifies an existing lock: live or remote exits 5, stale or unreadable is
  logged. Corrupt history exits 5, an interrupt 130 with an earlier output unchanged, a write
  failure 1. Missing text keeps exit 0.
- **CLI** (`cli/catia_index.go`). Only `--archive`, `--log-level`, `--log-format`, `--out` and
  `--strings` (`ARXGO_OUT`, `ARXGO_STRINGS`) are accepted. `--out` is absolute, not a directory,
  with an existing parent, and not run state, a registry, a part file or an existing arxgo
  description or sidecar (case-folded on Windows); exit 2 otherwise. An operator file named by
  `--out` is replaced.
- **Reserved name.** `arxgo-catia-text.md` directly under a walked root is excluded from scans
  (`scanner.CatiaIndexName`), so the default output never becomes a registry row or a candidate.
- **Scale.** On 2000 generated V5 files the index takes 0.10 s and 26 MB RSS (1.4 MB document;
  20.6 MB with `--strings` in 0.19 s).

## CATIA restore (`internal/cli`, `internal/archive`)

`arxgo restore --catia --archive PATH --catia-archive PATH` returns CATIA files through the shared
restore executor. Default `restore` (or `--video`) still returns only videos.

- **Flags and validation.** `--video`, `--catia` and `--catia-archive` are active on restore with
  the split payload rules (`selectPayload`, `checkRoots`; the selected root must exist). Exit 2
  before the lock: both payloads, `--video-archive` on the command line with `--catia`,
  `--catia-archive` without `--catia`, equal or nested roots, `--previews delete` (command line or
  `ARXGO_PREVIEWS`) with `--catia`; `--catia-text` is unknown on restore. The restore scan root is
  the selected mirror root. `cli.recovererFor` rebuilds CATIA restore resolvers, so a video command
  rolls an interrupted CATIA restore forward (locking the CATIA archive) and the reverse.
- **Candidates and transactions** (`catiaRestore`). The CATIA archive scan selects `is_catia` files
  plus every `rel_path` of a `moved` or `restored` row in `arxgo-catia.csv` (rows with non-local
  paths are ignored with a warning). Transfer, `--verify`, `--create-dirs`, `--overwrite`, the
  case-fold guard, owned description removal (`RestoreResolver` with payload `catia` for registry
  hints), `catia_*` counters and CATIA mirror directory cleanup are the shared video code. A file in
  the mirror whose row says `restored` is restored again and logged.
- **Text sidecar cleanup** (`sidecarCleanup` in `previews_restore.go`, shared with previews). With
  `--descriptions delete`, each owned sidecar of a restored file (from the CATIA text event index)
  is deleted under `text_delete` / `text_deleted` only when it is still a regular file with the
  recorded size and its first line is `arxgo-text: <rel_path>`; otherwise it is kept and reported
  as a skipped issue (exit 6). A missing sidecar or parent directory completes the event. Before
  execute, unfinished text part files are removed, logged deletions are finished, and sidecars of
  files this run already committed are deleted, then the sidecars of
  [replaced restores](#replaced-restore-sidecar-cleanup-internalarchive-internalstate).
  `--descriptions keep` keeps descriptions and sidecars.
- **Registry** (`updateRegistry`, `replayCatiaRows`, `writeRestoredRegistry`). With
  `--registry-update`, both `arxgo-catia.csv` copies replay CATIA history onto the existing rows
  (restored rows get the run id and an archive `file:` URL; `text_rel_path` is the remaining owned
  sidecar, empty after deletion). When no row is `moved` and descriptions were deleted, both copies
  become `arxgo-catia.restored-<run-id>.csv` (the helper is shared with the video registry). Restore
  never creates a CATIA registry and never writes `arxgo-videos.csv`.

## Replaced restore sidecar cleanup (`internal/archive`, `internal/state`)

Sidecar deletion runs after `commit`. A restore interrupted in between and replaced by another run
(`--new-run`, other defining options, or a command of the other payload that rolls it forward)
used to leave those owned sidecars, because the replacing run has no candidate for the file.

- **Intent** (`state.RunOptions.SidecarCleanup`, `archive.Config.SidecarCleanup`,
  `archive.RestoreSidecarCleanup`). Restore runs write `sidecar_cleanup` (`true`/`false`) into
  `options.json`: `--previews delete` for video, `--descriptions delete` for CATIA. The CLI sets
  the config from the same function the executor uses; `Restore` refuses a configuration whose
  policy differs from the recorded one, and `Start` refuses the field on scan and split. Split and
  scan runs omit it; a restore written by an earlier build has none and counts as `false`.
  `runHistory.sidecarCleanup` exposes it; history no longer keeps raw options.
- **Rule** (`sidecarCleanup.deleteEarlierRestored`, shared by `videoRestore` and `catiaRestore`).
  With cleanup on, before execute and after the current run's own resumed commits, the payload's
  history (read at restore start) is reduced to the last transaction per file. Files whose last
  transaction is a restore committed by another run with `sidecar_cleanup: true` and that still
  own sidecars lose them under the family's delete events, with the usual size (and CATIA
  first-line) checks and, for video, description link refresh. A split after that restore makes
  the file ineligible. A kept changed sidecar is logged at info (`quiet`), not reported. The rule
  never uses `Config.RecovererFor`; `catiaRestore.deletedDescriptions` is gone.

## Stage-4 proof (`test/integration`)

`make test-integration` (a CI step on `ubuntu-latest`) also runs `TestCatiaSplitRestoreRoundTrip`
([0050](../records/0050-catia-prove-stage-4-on-generated-archive.md)). It uses the stage-1
generator (videos, documents, Unicode and space names, optional ffmpeg clips) plus 22 synthetic
CATIA files: V5 `CATPart`/`CATProduct`/`CATDrawing` with a `LastSaveVersion` property, a component
window and 256 KiB to 2 MiB seeded payloads, raw-XML and ZIP `3dxml`, `cgr`, lower- and upper-case
extensions at the root and deep levels, and a foreign `<part>.md` and `<part>.text.md` at one CATIA
file. Names and properties are invented. Through the binary only:

- exit 2 before anything is written for `--catia --video-archive`, `--catia-text` without
  `--catia`, `--catia --video`, a CATIA archive inside the archive and `restore --catia --previews
  delete` (mirror roots not created, no lock);
- `scan` writes `is_catia` for exactly the CATIA files;
- a video split (`--transfer copy --verify hash`) killed after a seeded number of WAL records and
  resumed;
- `split --catia --catia-text --transfer copy --verify hash` killed twice and completed by a third
  process in the same run: CATIA files byte-identical in the CATIA archive, descriptions with the
  `catia:` line (and `.arxgo.md` / `.arxgo.text.md` next to the foreign files), owned sidecars,
  identical `arxgo-catia.csv` copies (kind, and release and component count of one V5 product),
  `catia_done` and `texts_done` equal to the file count in the resumed report, `arxgo-videos.csv`
  unchanged, each mirror root holding only its own payload and registry; a rerun moves and writes
  nothing and leaves `arxgo-catia.csv` byte-identical;
- `catia-index` after the rerun: sections equal to the `moved` rows, no missing text, a
  byte-identical rerun, and a `--strings --out` document that differs only by `strings:` blocks;
- `restore --catia --transfer copy` killed, then `restore --transfer copy` (video) killed, which
  first rolls the CATIA run forward; `restore --catia` rolls the video run forward and completes
  (registries retired, CATIA archive empty); `restore` completes; reruns exit 0;
- the manifest of every file and directory (path, size, mtime ns, SHA-256) equals the one taken
  before the first operation.

`ARXGO_TEST_SEED` replays a run; `ARXGO_TEST_VIDEO_PARENT` puts both mirror roots on another
device. A run takes about 5 s.

A resumed split now takes `previews_done` and `texts_done` from the durable `preview_done` /
`text_done` records of its own WAL when they exceed the checkpointed values
(`syncSidecarCounters`), as it already did for the payload's `*_done` counter, so report counters
stay cumulative after a kill.


## Stage-4 checkpoint on an operator archive copy

The checkpoint ([0051](../records/0051-catia-review-stage-4-catia.md)) ran the built binary over a
disposable copy of the operator archive (4031 files, 66.8 GiB; 2255 CATIA files in three kinds,
149 videos): `scan`, `split --catia --catia-text --verify hash`, a video `split`, both reruns, both
restores with `--descriptions delete`, and their reruns. All 2255 CATIA files moved with 2255
sidecars and no failure, no run logged a warning, and a SHA-256 manifest of every file and
directory taken before and after the sequence was unchanged.

It repaired two defects in this area and routed the rest:

- Owned text sidecars kept their file-registry row. The archive view of
  [0053](../records/0053-registry-preserve-archive-registry.md) later reversed this: descriptions,
  previews and text sidecars are owned artifacts without a row, and the registry keeps the moved
  files' rows instead.
- `--verify hash` records the SHA-256 on the same-device rename path, so `arxgo-catia.csv` carries
  the hash its descriptions already had.

Extraction was audited against the real files and found truthful: no file reported `0 components`
while its raw bytes held more than two CATIA name references, 8365 of 12330 component names resolve
to a file in the same archive, and on a sampled comparison the extractor found every name a
byte-level scan found plus 340 more, because it decodes UTF-16 names such a scan misses. The 895
files reporting `0 components` are 892 of 1641 `CATPart` (a part normally lists only itself), 3 of
466 `CATProduct` and none of the 148 `CATDrawing`.

Descriptions and sidecars now name their archive and sidecars repeat the description identity
([self-locating metadata](#self-locating-metadata-internalreport-internalarchive)). `catia-index`
assembles the sidecars into one document
([text index](#catia-text-index-internalarchive-internalreport-internalcli)); the manuals keep a
shell recipe that copies the sidecars out one document per file.

## Registry and metadata checkpoint

The checkpoint ([0057](../records/0057-catia-review-registry-and-metadata.md)) ran the built binary
over the same archive copy twice (before and after its repairs): scans, a CATIA split with
`--catia-text` killed and resumed, `catia-index` with and without `--strings`, a video split with
previews interrupted and resumed, a registry rebuild, a killed and resumed CATIA restore, the video
restore and a file-manifest comparison. On the written files, every CATIA description, text sidecar
and index section agreed with `arxgo-catia.csv` and the run history (2255 of 2255 each; identity
lines byte-identical, blocks equal), every video description with `arxgo-videos.csv` (149 of 149),
and none of the 4957 descriptions, sidecars and previews had a file-registry row. The largest
sidecar identity header was 1.9 KB against the 4 KiB preflight reserve. It repaired detection of
restored files ([archive registry](archive-registry.md#incremental-update)), the session clock of
`moved_at`, value quoting at multi-byte edges and `file_size` of empty files, and prepared the file
registry and the CATIA text index of that run as review input for the operator approval, kept
outside the repository.
