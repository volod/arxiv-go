# CATIA restore

## Task and scope

- Id / capability / checkpoint: `implement-catia-restore` / `catia-archive` / `review-stage-4-catia`
- State: accepted
- Source: plan task `implement-catia-restore`; code revision `358656a`, clean tree at start.
- Plan counts at start: 12 open tasks (9 agent, 3 human); next eligible `implement-catia-restore`.
- Accepted task:

```markdown
#### implement-catia-restore

Return CATIA files without touching the video payload or deleting foreign Markdown.

- Serves: `catia-archive` -- [Restore](../openspec/stage-4-catia/split-restore.md#restore)
- Agent status: CLEAR
- Dependencies: [CATIA split](records/0046-catia-implement-catia-split.md); [CATIA text sidecars](records/0047-catia-implement-catia-text-sidecars.md).
- User-visible outcome: `arxgo restore --catia` returns CATIA files, honors directory and conflict
  policies, and deletes or keeps owned descriptions and text sidecars.
- Scope boundary: Restore `--video`, `--catia` and `--catia-archive` with the split validation rules,
  and `--previews delete` refusal; scan of the CATIA archive; CATIA candidates from CATIA history and
  `arxgo-catia.csv`; registry update and rename when empty; `text_delete` /
  `text_deleted`; mirror directory cleanup. No video restore changes except payload selection.
- Data and artifact paths: `internal/archive/`, `internal/cli/`.
- Execution path: Split-then-restore on generated trees with both a video archive and a CATIA
  archive; `--create-dirs`; `--overwrite`; `--descriptions keep` and `delete`.
- Acceptance gates: Round trip of paths, sizes, mtimes and SHA-256; `--descriptions delete` removes
  owned sidecars only; video archive, video descriptions and `arxgo-videos.csv` unchanged; rerun
  changes nothing; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none to the task. Spec clarification in the
  [stage-4 restore section](../../openspec/stage-4-catia/split-restore.md#restore): a changed
  sidecar is kept and reported (exit 6), and a later `restore --catia --descriptions delete` deletes
  the sidecars an earlier restore recorded with `--descriptions delete` left behind because another
  command rolled it forward (found by a failing test, see evidence). The
  [CLI contract](../../openspec/stage-1-core/cli.md) availability note now lists the payload flags
  as active on restore.

## Implementation

**CLI** (`internal/cli`). `--video` and `--catia` gained restore rows in the flag table;
`--catia-archive` applies to every operation (scan still ignores it). Restore reuses
`selectPayload`/`checkRoots` (the selected root must exist). `buildRestoreOptions` rejects
`--previews delete` (flag or `ARXGO_PREVIEWS`) with `--catia`. The restore scan root is the
selected mirror root (was `o.VideoArchive`). `recovererFor` accepts CATIA restore runs. Help,
usage texts and `.env.example` name CATIA.

**Executor** (`internal/archive`). `catiaPayload.newRestore = newCatiaRestore`
(`restore_catia.go`): rows and `include` from `arxgo-catia.csv` (moved or restored rows),
`beforeExecute`/`committed` through `sidecarCleanup`, and `updateRegistry`. The dead
"restore not available" guard in `sessionPayload` is gone. Transfer, WAL, description removal,
directory policies and mirror cleanup were already payload-generic.

**Shared sidecar cleanup** (`previews_restore.go`). Preview deletion became `sidecarCleanup`
(event index, optional `owns` check, optional `refresh`, `quiet`), used by previews (refresh of
preview links) and CATIA text (first-line `arxgo-text: <rel_path>` check). A missing parent
directory now completes a logged deletion instead of failing the run. `noSymlinkParents` messages
say "sidecar". Video behavior is otherwise unchanged (preview tests unchanged and passing).

**Earlier restores.** Text deletion is post-commit, so a CATIA restore killed after `placed` and
rolled forward by a video command committed the file without deleting its sidecar, and the next
CATIA restore had no candidate for it. `catiaRestore.earlierRestored` finds files whose last CATIA
transaction is a restore by an earlier run, asks `Config.RecovererFor` to rebuild that run's
resolver from its `options.json`, and deletes the owned sidecars when it had `KeepDescriptions`
false; changed files are only logged there. `runHistory` now keeps `op` and `options`.

**Registry.** `replayCatiaRows` is shared by split and restore; `writeRestoredRegistry` (atomic
write, optional rename to `<stem>.restored-<run-id>.csv`) is shared with the video registry.
Restored rows get an archive `file:` URL (base-URL links are kept) and `text_rel_path` from the
remaining owned sidecar.

Rejected: a post-recovery hook on `RestoreResolver` run inside `recoverReplaced` (misses a crash
after `commit` with no open transaction, and would report CATIA issues in a video run); deleting
sidecars of every earlier-restored file regardless of that run's policy (a default-policy rerun
would delete sidecars an operator kept with `--descriptions keep`); retiring the registry only when
no owned sidecar remains (spec says rename when no `moved` rows remain).

Compatibility: none for data formats; `runHistory` is internal. Current-state pages:
[CATIA archive](../current/catia-archive.md), [crash safety](../current/crash-safety.md); operator
manuals (Linux, Windows) gained "Restore CATIA files".

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Round trip of paths, sizes, mtimes and SHA-256 | `TestCatiaRoundTripRestoresFilesAndDeletesOwnedMarkdown` (same-device and injected cross-device; video split first into the video archive, `split --catia --catia-text`, restore; whole-archive manifest equal to the pre-CATIA-split manifest except `arxgo-registry.csv` and the retired registry); `TestRestoreCatiaCommandRoundTrip` (CLI, `--verify hash`) | pass, Linux |
| `--descriptions delete` removes owned sidecars only | same round-trip test (foreign `fixture.CATPart.md` and `.text.md` kept, owned `.arxgo.md`/`.arxgo.text.md` removed); `TestCatiaRestoreKeepsChangedSidecarsAndForeignMarkdown` (same-size edited first line and grown sidecar kept, 2 issues, exit 6 status, `text_rel_path` kept) | pass, Linux |
| `--descriptions keep` | `TestCatiaRestoreKeepDescriptionsKeepsMarkdownAndRegistry` (descriptions and sidecars kept, rows `restored`, registry not retired; a file put back in the mirror under a `restored` row is restored again); `TestRestoreCatiaKeepDescriptionsCommand` | pass, Linux |
| `--create-dirs`, `--overwrite`, mirror cleanup, registry rename when empty | `TestCatiaRestoreDirectoryAndConflictPolicies` (skips then restores with both flags; no `cad` left in the CATIA archive; retired copy); `TestCatiaRestoreIncludesRegistryRowsWithoutCatiaExtension` | pass, Linux |
| Video archive, video descriptions and `arxgo-videos.csv` unchanged | round-trip test (video archive manifest, `arxgo-videos.csv` and `clip.mp4.md` bytes); `TestRestoreCatiaCommandRoundTrip` (empty video archive) | pass, Linux |
| Rerun changes nothing | round-trip test (second restore `catia_done=0`, same manifests); CLI round trip rerun exit 0 | pass, Linux |
| Crash points and `text_delete` / `text_deleted` | `TestCatiaRestoreCrashPointsConverge` (`RestoreRenamePoints` plus `wal:text_delete`, `wal:text_deleted`; resumed tree equals the original) | pass, Linux |
| Recovery by the other payload; sidecars of a replaced restore | `TestInterruptedCatiaRestoreRecoveredByVideoRestore`, `TestVideoRestoreCommandRecoversInterruptedCatiaRestore` (real `recovererFor`). Before `earlierRestored` the archive test failed with `unexpected cad/deep/fixture.CATPart.arxgo.text.md` | pass, Linux |
| Exit 2 before the lock | `TestCatiaUsageErrorsExitBeforeLock` restore cases: `--catia --video`, `--catia --video-archive`, `--catia-archive` without `--catia`, missing CATIA archive, equal roots, `--previews delete` (flag and env), `--catia-text` | pass, Linux |
| Preview cleanup unchanged | existing `TestRestore*PreviewsLive`, `TestRestoreCrashAfterCommitDeletesPreviewsLive` | pass, Linux |
| `make ci` | `make ci` (fmt-check, vet, vet-windows, `go test ./...`, `make build-all`, lint-spec-plan, lint-doc-links) | pass, Linux; Windows cross-compiled only |
| `make test-integration` (declared extra, video path) | `make test-integration` | pass, Linux |
| Real-process kill/resume run (declared extra) | Scratch driver outside the repository with the built binary: generated archive of 120 synthetic CATIA files of all five kinds, two 150 MiB V5 files, Cyrillic directories, foreign description and foreign `.text.md`, two NVENC H.264 clips; video and CATIA archives on `/dev/shm` (cross-device copies, `--verify hash`). Per seed: video split, `split --catia --catia-text` SIGKILLed then resumed, `restore --catia` SIGKILLed 1-3 times, then either CATIA restore first or video restore first (recovering the CATIA run), then the other; manifests (path, size, mtime ns, SHA-256), mirror roots empty except retired registries and `.arxgo`, no directories left, rerun unchanged. Seeds 1-3 and 10-49 with the build before a no-behavior refactor (history reuse), seeds 100-104 with the final build | pass: 48 of 48 seeds; every seed SIGKILLed the CATIA split and 1-3 CATIA restore attempts (a few late restore attempts had already exited 0), 12 seeds had a video restore roll an open CATIA transaction forward; no part files, owned sidecars or directories left, foreign Markdown kept. The kill point inside a run is timing-based, not a seeded WAL point |
| Memory bound on a large file (declared extra) | `/usr/bin/time -v arxgo restore --catia --verify hash` of one generated 1 GiB V5 file from `/dev/shm` | pass: max RSS 13 MiB, 2.5 s, SHA-256 identical, description and sidecar removed, registry retired |

## Audit handoff

- `AUD-implement-catia-restore-1`: video preview cleanup has the same post-commit gap this task
  closed for CATIA text: a `restore --previews delete` interrupted after a commit and replaced by
  another run (`--new-run`, other options, or a CATIA command) never deletes the previews of the
  video it committed, because the next restore only revisits its own run's commits. Pre-existing;
  nonblocking; location `videoRestore.beforeExecute` / `restorePreviewsBeforeExecute`. Out of this
  task's scope ("no video restore changes except payload selection"). Next check: owner
  `repair-replaced-restore-sidecar-cleanup` (routed after acceptance; spec amended in
  [replaced restores](../../openspec/stage-4-catia/split-restore.md#replaced-restores)).
- `AUD-implement-catia-restore-2`: earlier-run sidecar cleanup depends on `Config.RecovererFor` to
  read that run's `--descriptions` policy; without it (only possible in tests) earlier runs are
  skipped. Nonblocking; location `catiaRestore.deletedDescriptions`. It also couples the archive
  layer to CLI option decoding. Next check: owner `repair-replaced-restore-sidecar-cleanup`, which
  replaces it with the archive-written `sidecar_cleanup` run option.
- Resolved from earlier records: `AUD-implement-catia-split-2` (lock and exit-5 behavior for a
  restore replacing a CATIA run) is now exercised by the CATIA-restore recovery tests and the kill
  sweep; the checkpoint still confirms it.

Reviewed scope: CLI payload flags on restore, validation, recovery mapping, restore hooks,
shared sidecar cleanup, earlier-run cleanup, registry replay and retirement, manuals and
environment template.

## Close or resume

All gates passed on Linux; Windows cross-compiled only. Plan task removed and the dependency in
`prove-stage-4-on-generated-archive` replaced with this record; current-state pages, operator
manuals and the records index updated; `catia-archive` stays planned (proof and checkpoint remain).
Plan counts after: 11 open tasks (8 agent, 3 human); next eligible
`prove-stage-4-on-generated-archive`. After routing the audit notes, the repair task
`repair-replaced-restore-sidecar-cleanup` was added before the proof: 12 open tasks (9 agent,
3 human); next eligible `repair-replaced-restore-sidecar-cleanup`.
