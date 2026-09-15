# CATIA text sidecars

## Task and scope

- Id / capability / checkpoint: `implement-catia-text-sidecars` / `catia-archive` / `review-stage-4-catia`
- State: accepted
- Source: plan task `implement-catia-text-sidecars`; code revision `7b73a96` with the accepted CATIA
  split working tree.
- Plan counts at start: 13 open tasks (10 agent, 3 human); next eligible `implement-catia-text-sidecars`.
- Accepted task:

```markdown
#### implement-catia-text-sidecars

Operators want searchable text beside each moved CATIA file, also for archives split earlier.

- Serves: `catia-archive` -- [Text sidecars](../openspec/stage-4-catia/split-restore.md#text-sidecars)
- Agent status: CLEAR
- Dependencies: [CATIA split](records/0046-catia-implement-catia-split.md).
- User-visible outcome: `split --catia --catia-text` writes owned `<rel_path>.text.md` sidecars after
  each commit and for previously moved files that lack one; failures never roll back a move.
- Scope boundary: `--catia-text` flag and validation; `text_begin`/`text_done`/`text_failed` events
  on the shared post-commit mechanism; part files and non-replacing rename; sidecar collision naming;
  rerun catch-up; `texts_done`/`texts_failed` counters and exit 6; `text_rel_path` in
  `arxgo-catia.csv`. No restore cleanup.
- Data and artifact paths: `internal/cli/`, `internal/archive/`, `internal/state/`.
- Execution path: Generated trees in `t.TempDir()`; injected extraction failure; crash between
  `text_begin` and `text_done`; split without then with `--catia-text`.
- Acceptance gates: Sidecars match the contract; a foreign `<rel_path>.text.md` is kept and the
  sidecar goes to `<rel_path>.arxgo.text.md`; failure leaves the file moved with exit 6; crash
  recovery leaves no part file and retries; second run moves nothing and writes only missing
  sidecars; `--catia-text` without `--catia` exits 2; `make ci` passes.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none.

## Implementation

**CLI** (`internal/cli`). `--catia-text` / `ARXGO_CATIA_TEXT` is a split flag, default false,
defining, valid only with `--catia` (exit 2 otherwise, before the lock). Help, flag table and
`.env.example` name it; harvested strings can include authoring user ids, so the environment
template leaves it unset unless wanted. `SplitOptions.CatiaText` maps to `archive.SplitConfig`.

**WAL and counters** (`internal/state`). `TextEvents` is a second `EventFamily` on the shared
begin/finish/part-file mechanism (`text_begin` / `text_done` / `text_failed`; `text_delete` /
`text_deleted` reserved for restore). Version 2 envelope. `texts_done` / `texts_failed` on
checkpoint and report; `Finish` treats `texts_failed` as `StatusPartial` (exit 6), never
`catia_failed`.

**Paths** (`internal/report`). `InspectTextSidecar` / `ChooseTextSidecarPath`: `<rel_path>.text.md`,
then `<rel_path>.arxgo.text.md`, then the indexed description name with a `.text.md` suffix.
Occupancy is the first line `arxgo-text: <rel_path>` (64 KiB). `parseField` rejects `-` in keys, so
inspection splits on `: ` and unquotes. `text_rel_path` is the last `text_done` for that file
(`eventIndex.remember` / `ownedPath`), not the lexicographically last owned path.

**Executor** (`internal/archive/text.go`). CATIA `prepare` always rebuilds the text index (skip
paths, unfinished `.arxgo-part` cleanup, registry fill) even without `--catia-text`. With the flag,
`plan` sets `Candidates.TextBytes` (min(1 MiB, file size) per file that still needs a sidecar) as
need `texts` on the archive device. `start` runs sequential post-commit generation (no preview
queue): catch-up of earlier moved files still in the mirror, then each new `commit`. Body is
`RenderCatiaText` written to a part file, fsynced, renamed without replace. Crash points
`wal:text_begin`, `fs:text_part`, `fs:text_sidecar`. An owned file at a generating path is adopted.
Cancellation returns the context error (unfinished, exit 130). `extractCatia` is a test seam.

**Context.** The run context is attached to `MarkdownDescription` (`useContext` / `resolverAttach`)
and to text extraction, so Ctrl+C can stop both passes (`AUD-implement-catia-split-1`).

Rejected: a second copy of the preview WAL; an ffmpeg-style queue (extraction is in-process);
`AtomicWriteFile` (it replaces; the spec is a non-replacing rename); failing the move on text
failure; caching `ExtractPath` between description (`placed`) and text (`commit`).

Compatibility: a CATIA split without `--catia-text` does not resume an interrupted `--catia-text`
run (defining options differ); recovery of that run still uses its recorded flag. The CLI
availability note now lists `--catia-text` as active on split; restore CATIA flags stay unknown.
Restore cleanup is not in this build. Current-state:
[CATIA archive](../current/catia-archive.md), [crash safety](../current/crash-safety.md),
[project foundation](../current/project-foundation.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Sidecars match the contract; rerun moves nothing and writes only missing sidecars | `TestCatiaTextSidecarsMatchContractAndRerunIsNoop` (product sidecar `arxgo-text:` / properties / components; Unicode drawing path; `text_rel_path`; `texts_done`; second run `catia_done=0` `texts_done=0`, byte-identical sidecar) | pass, Linux |
| Foreign `<rel_path>.text.md` kept; owned file at `.arxgo.text.md` | `TestCatiaTextSidecarKeepsForeignName`; `TestChooseTextSidecarPathCollisionAndReuse` (primary, `.arxgo.text.md`, indexed `fixture-1.CATPart.text.md`, reuse owned) | pass, Linux |
| Failure leaves the file moved with exit 6 | `TestCatiaTextFailureLeavesFileMoved` (injected `TextFailed`; `StatusPartial`; `texts_failed`; CATIA files still in the mirror; later catch-up writes the sidecar); `TestBroken3DXMLTextFailureDoesNotRollBackMove` (`<<<` 3dxml) | pass, Linux |
| Crash recovery leaves no part file and retries | `TestCatiaTextCrashRetriesWithoutPartFile` (`wal:text_begin`, `fs:text_part`, `fs:text_sidecar`; resume `StatusCompleted`; no `.arxgo-part`) | pass, Linux |
| Split without then with `--catia-text` | `TestCatiaSplitThenTextCatchUpMovesNothing` (`catia_done=0`, `texts_done` equals the moved set) | pass, Linux |
| `--catia-text` without `--catia` exits 2 | `TestCatiaUsageErrorsExitBeforeLock` (`catia-text without catia`, `catia-text from env without catia`; no `.arxgo` in any root) | pass, Linux |
| `--catia-text` is defining; CLI writes a sidecar | `TestSplitCatiaTextCommandWritesSidecar` | pass, Linux |
| Cancellation is unfinished, not `text_failed` | `TestCanceledTextSidecarIsRetriedNotFailed` | pass, Linux |
| Registry follows the last `text_done` | `TestCatiaTextRegistryUsesLastDoneSidecar` (replace primary with foreign notes; fallback `.arxgo.text.md` is `text_rel_path`) | pass, Linux |
| WAL envelope; preflight `texts` | `TestTextEventsUseSharedEnvelope`; `TestPreflightPlacesCatiaNeedsOnCatiaArchive` / split catia text hook on archive device | pass, Linux |
| `make ci` | `make ci` (fmt-check, vet, vet-windows, `go test ./...`, `make build-all`, lint-spec-plan, lint-doc-links) | pass, Linux; Windows cross-compiled only |
| `make test-integration` (declared extra, video path unchanged) | `make test-integration` | pass, Linux |

## Audit handoff

- Resolved `AUD-implement-catia-split-1`: description extraction used `context.Background()`. The
  run context is now attached to `MarkdownDescription` (`useContext` / `resolverAttach` during
  execute and WAL recovery) and to post-commit text extraction. Location
  `split.go` / `split_description.go` / `text.go`.
- `none identified` in the new work. Reviewed scope: `--catia-text` parsing and defining options,
  WAL text family, occupancy and collision names, sequential post-commit generate/adopt/catch-up,
  part files and non-replacing rename, counters and exit 6, `text_rel_path`, preflight `texts`,
  manuals and `.env.example`. Restore `text_delete` / `text_deleted` remains for
  `implement-catia-restore`.

## Close or resume

All gates passed on Linux; Windows cross-compiled only. Plan task removed and the dependency in
`implement-catia-restore` replaced with this record; current-state pages, operator manuals and the
records index updated; `catia-archive` stays planned. Plan counts after: 12 open tasks (9 agent,
3 human); next eligible `implement-catia-restore`.
