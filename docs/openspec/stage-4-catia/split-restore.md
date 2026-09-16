# CATIA split and restore

Owner: `catia-archive`. Transaction mechanics: [integrity](../stage-1-core/integrity.md).
Video behavior (unchanged default): [stage-1 split and restore](../stage-1-core/split-restore.md).
Formats: [contracts](../stage-1-core/contracts.md).

## Operator problem

Move every CATIA file out of the main archive into a mirrored CATIA archive, separate from the video
archive, so the document archive stays
small and shareable, while each former CATIA location still says what was there, where it went, and
(optionally) what accessible text it contained. Restore must put the files back under the same
conflict, directory and description policies as video.

## Payload selection

`split` and `restore` take a payload kind. `scan` does not.

| Flags | Payload |
| --- | --- |
| neither `--video` nor `--catia` | video (current behavior) |
| `--video` | video |
| `--catia` | CATIA |
| both | usage error, exit 2 |

- `--catia-text` is a split flag, default false, valid only with `--catia`; otherwise exit 2.
- Each payload has its own mirror root: `--video-archive` for video, `--catia-archive` for CATIA.
  `--catia` requires `--catia-archive`, with the existence, creation and lock rules of the video
  archive. `--video-archive` on the command line together with `--catia`, or `--catia-archive`
  without it, exits 2 naming the right flag, so a CATIA run can never write into the video archive
  by a copied command line. Environment values for the other payload's root do not select or
  record a root; they are only compared by the nesting rule below.
- The archive, the video archive and the CATIA archive are pairwise neither equal nor nested
  whenever they are set ([validation](../stage-1-core/cli.md#validation)). In this page "the mirror"
  means the CATIA archive.
- With `--catia`: `--sample` or `--image` other than `none`, `--publish`, or restore
  `--previews delete` is a usage error (exit 2), reported before the lock.
- The payload kind is written to `options.json` as `payload` (`video` or `catia`) with the matching
  root (`video_archive` or `catia_archive`), and is a defining option together with `--catia-text`. A video run and a
  CATIA run never resume each other; the archive lock still serializes them.

## Run history by payload

The run state of every payload lives in `<archive>/.arxgo/runs`. Split and restore replay the WAL of
every earlier run there to build registries, preview indexes, restore candidates and directory
cleanup ([contracts](../stage-1-core/contracts.md#video-registry-csv)).
Each consumer reads only runs of the selected payload:

- `arxgo-videos.csv`, the preview index, video restore candidates and preview cleanup use video
  runs;
- `arxgo-catia.csv`, CATIA restore candidates and text sidecar ownership use CATIA runs;
- mirror directory cleanup after restore uses runs of the selected payload;
- recovery of an interrupted run uses that run's own payload and `--catia-text` from its
  `options.json`, whatever the current command selects.

## Split

Reuse the [stage-1 split pipeline](../stage-1-core/split-restore.md#split): validate, lock, recover,
scan, preflight, one WAL transaction per candidate, payload registry, unlock. Do not fork a second
executor. Differences when the payload is CATIA:

1. Candidates are present `is_catia=true` rows. Videos stay in the main archive.
2. Destination is `<catia-archive>/<rel_path>`; parent directories follow the video rules.
3. After `placed`, run the [metadata pass](catia.md#accessible-metadata) on the destination and write
   the [CATIA description](../stage-1-core/contracts.md#catia-description) `<archive>/<rel_path>.md`
   with marker `arxgo: <rel_path>`, so description occupancy, conflict naming and recovery are
   reused unchanged.
4. No preview planning. With `--catia-text`, [text sidecars](#text-sidecars) are generated after
   `commit`.
5. Write [`arxgo-catia.csv`](../stage-1-core/contracts.md#catia-registry-csv) into the archive and
   the CATIA archive, and the file registry with the `location` of the moved files. Do not rewrite
   `arxgo-videos.csv`.

Destination conflicts, description conflicts, source-changed retry, transfer modes, `--verify`,
`--base-url` and the case-fold guard are the video rules applied to CATIA candidates. Progress,
checkpoint and report use the `catia_*` counters ([checkpoint](../stage-1-core/contracts.md#checkpoint));
log lines name the payload. A rerun after success finds no CATIA candidates,
generates only missing text sidecars, leaves unchanged registries untouched, and exits 0.

## Text sidecars

Text extraction follows the stage-2 preview model, so it can be added to an already split archive:

- For every CATIA file committed by this or an earlier CATIA split and still present in the mirror,
  a split with `--catia-text` generates the sidecar when no owned sidecar for that `rel_path` exists
  and no `text_done` names an existing file. Extraction reads the mirror copy.
- Events use the version 2 WAL envelope: `text_begin` (sidecar path) then `text_done` (size) or
  `text_failed` (reason). The sidecar is written as `<dst>.arxgo-part` and renamed without replacing
  an existing file. The next split or restore removes part files of unfinished events, and split
  retries them.
- Sidecar path: `<rel_path>.text.md`; when that name holds anything that is not an owned sidecar for
  this `rel_path`, `<rel_path>.arxgo.text.md`, then the indexed name of the description rule. The
  conflict is logged.
- A failure counts in `texts_failed` (never `videos_failed`), is listed in the report and yields exit
  code 6; the CATIA file stays moved. Cancellation leaves the event unfinished and exits 130.
- Without `--catia-text`, split neither creates nor removes sidecars.
- A sidecar has no file-registry row, like a description or a preview: it is an owned artifact of
  the moved CATIA file, listed in `text_rel_path`, and the file registry keeps describing the
  archive as it was before the split ([archive view](../stage-1-core/registry.md#archive-view)).

## Restore

Reuse the [stage-1 restore pipeline](../stage-1-core/split-restore.md#restore). Scan the CATIA
archive. Candidates are `is_catia=true` rows plus every `rel_path` of a `moved` or `restored` row in
`arxgo-catia.csv`. Registry rows are matched by `rel_path`; non-local or reserved rows are ignored
with a warning, as for video.

- `--create-dirs`, `--overwrite`, `--transfer`, `--verify` and mirror directory cleanup are
  unchanged.
- `--registry-update` marks rows in both `arxgo-catia.csv` copies (archive and CATIA archive) `restored`
  and sets `location` `archive` on their file-registry rows. When no `moved` rows
  remain and `--descriptions delete` was used, both copies are renamed to
  `arxgo-catia.restored-<run-id>.csv`. Restore never creates a CATIA registry and never rewrites
  `arxgo-videos.csv`.
- `--descriptions delete` removes `<rel_path>.md` only when its marker names this CATIA file, and
  each owned sidecar only when its first line is `arxgo-text: <rel_path>`, under WAL events
  `text_delete` / `text_deleted` (size-checked like preview cleanup). `--descriptions keep` keeps
  both. A sidecar whose size, type or first line changed is kept and reported (exit 6).
- Sidecars an interrupted restore left behind follow [replaced restores](#replaced-restores).
- A CATIA file present in the mirror while its registry row says `restored` is restored again (the
  filesystem is the source of truth) and logged.

## Replaced restores

Sidecar deletion runs after `commit`, so a restore interrupted between a commit and its sidecar
deletion leaves owned sidecars of a restored file. When the same run resumes, it deletes them
before execute. When another run replaces it instead (other options, `--new-run`, or a command of
the other payload that rolls its transactions forward), the replacing run has no candidate for that
file. The rule is the same for both payloads (video previews and CATIA text sidecars):

- A restore whose own `sidecar_cleanup` is `true` deletes, before execute, the owned sidecars of
  every file whose last transaction in the payload's history is a restore committed by an earlier
  run whose [`options.json`](../stage-1-core/contracts.md#run-options) records
  `sidecar_cleanup: true`. A restore with `sidecar_cleanup: false` never deletes a sidecar.
- A file split again after that restore is excluded (its last transaction is a split), so sidecars
  generated after a later split are never removed by this rule.
- Deletion uses the payload's normal checks (recorded size; for CATIA also the first line) and
  `*_delete` / `*_deleted` events in the current run's WAL. A changed sidecar is kept and logged, not
  reported again, because the run that restored the file already owned that report. A kept video
  description is refreshed as after a normal preview deletion.
- The intent comes only from `sidecar_cleanup`, never from decoding CLI options or a rebuilt
  recovery resolver, so the rule behaves the same with or without `Config.RecovererFor`.

## Reuse

The payload kind (`video` | `catia`) and its mirror root are one value passed from `internal/cli`
into `internal/archive`, which names the root generically (mirror) rather than video. The kind
selects the candidate predicate, the payload registry file name and columns, the counters, the
history filter, the description renderer, and the optional post-commit sidecar (video previews or CATIA
text). Transfer, WAL `begin` through `commit`, recovery, preflight, progress, locks, description
occupancy and conflict naming stay shared in `internal/archive`, `internal/state` and
`internal/report`. `internal/catia` extracts metadata and text from an `io.Reader` and owns the
kind table; it imports no other internal package. `internal/report` renders the CATIA description
and sidecar with the existing value escaping. Post-commit sidecar events share one
begin/finish/part-file mechanism in `internal/state` with previews rather than a copy of it.

## Acceptance

- A generated archive with nested directories, CATIA files at root and deep levels, videos and other
  files, name collisions (`fixture.CATPart` with a foreign `fixture.CATPart.md` and
  `fixture.CATPart.text.md`) and invented Unicode names.
- Same-device and injected cross-device transfers through the existing `fsops` seams.
- Round trip: `split --catia` then `restore --catia` yields identical relative paths, sizes, mtimes
  and SHA-256; videos left in place are untouched.
- Default `split` and `restore` on the same archive with a separate video archive still move only
  videos, and `arxgo-videos.csv` holds no CATIA row after CATIA runs in the same state directory
  (and the reverse); neither mirror root receives the other payload's files or registry.
- `split --catia --video-archive X`, `split --catia-archive X` without `--catia`, and a CATIA archive
  equal to or nested in the video archive exit 2 before the lock; `ARXGO_VIDEO_ARCHIVE` in the
  environment does not break a CATIA run.
- An interrupted CATIA split recovered by a later video split command is rolled forward with a CATIA
  description.
- `split --catia` without `--catia-text`, then again with it, writes sidecars for every moved file
  and moves nothing; `restore --descriptions delete` removes owned sidecars only, `keep` leaves them.
- `--catia` with `--video`, `--sample start`, `--publish gdrive` or `--previews delete`, and
  `--catia-text` without `--catia`, exit 2 before the lock.
- Rerunning CATIA split or restore after success changes nothing and exits 0.
- Crash after `placed`, and crash between `text_begin` and `text_done`, recover without duplicating
  or losing the CATIA original or leaving a part file.
- A `restore --catia` killed after a commit and before its text deletion, then rolled forward by a
  video restore, leaves no owned sidecar after the next `restore --catia`; the same holds for a
  video `restore --previews delete` rolled forward by a CATIA restore or replaced with `--new-run`.
  A restore with `sidecar_cleanup: false` keeps them (a later restore with cleanup still deletes
  them), and so does any restore after a later split of the file; so does a replaced restore whose
  own `sidecar_cleanup` was `false` or missing.
