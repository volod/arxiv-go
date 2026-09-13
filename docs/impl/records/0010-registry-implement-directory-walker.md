# Implement Directory Walker

## Task and scope

- Id / capability / checkpoint: `implement-directory-walker` / `archive-registry` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; Windows is cross-compiled and vetted only, see audit handoff)
- Source: plan task `implement-directory-walker`, operator request on 2026-09-13
  ("implement, run, fix, improve implementation and then update documentation and plan.md").
  Branch `ag-01-stage-1` at `2b5add5`, clean tree.
- Plan counts at start: 27 open (24 agent, 3 human); next agent task
  `implement-directory-walker`; also eligible `implement-file-type-detection`,
  `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-directory-walker

Traverse an archive deterministically with exclusions, special-entry handling and a resume cursor.

- Serves: `archive-registry` -- [Traversal](../openspec/stage-1-core/registry.md#traversal)
- Agent status: CLEAR
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md).
- User-visible outcome: Scans visit every regular file once in a stable order, skip arxgo's own
  files and excluded globs, report unreadable entries without aborting, and can restart after a
  cursor.
- Scope boundary: `Walk(ctx, root, opts, fn)` over `filepath.WalkDir`, walk order key and
  comparison, reserved-path and `--exclude` matching (including `**`), symlink/special handling,
  error accounting. No type detection or output.
- Data and artifact paths: `internal/scanner/walker.go`, `internal/scanner/order.go`.
- Execution path: Relative slash paths; cursor skip by key comparison with directory pruning when a
  whole subtree precedes the cursor.
- Acceptance gates: Order equals `WalkDir` order for trees where string order differs; resume after
  every possible cursor in a fixture yields the uninterrupted sequence suffix; reserved paths and
  globs excluded; symlink reported not followed; unreadable directory counted (Linux).
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Spec clarifications made while implementing it:
  - [traversal](../../openspec/stage-1-core/registry.md#traversal): resume rule with directory
    pruning; `--exclude` globs are anchored at the walked root and match the whole relative path
    (`*.tmp` top level only, `**/*.tmp` any depth, `**` zero or more segments, a matching
    directory is pruned); reserved names only directly under the root, `.arxgo-part` at any depth;
    an explicit `--registry` inside the archive is excluded; excluded entries are not counted; a
    symlinked root is walked through its target; an unlistable directory counts as a directory and
    one skipped entry; an unlistable root fails the run.
  - [CLI validation](../../openspec/stage-1-core/cli.md#validation): on Windows a glob containing
    `\` is rejected (resolves `AUD-implement-cli-contract-3`).
  - [architecture](../../openspec/architecture.md#repository-layout): scanner file list.

## Implementation

`internal/scanner` (new code) and `internal/cli` (glob validation); no new dependencies.

- `order.go`: `Key`, `KeyOf`, `Compare` (segment-wise byte order), `IsPrefix`; the cursor
  classifier (visit, descend silently, prune); `Glob`/`CompileGlob` with `path.Match` per
  segment and `**`, matched in O(pattern x path) by star backtracking.
- `walker.go`: `Walk(ctx, root, Options, fn)` over `filepath.WalkDir`. Reserved-path,
  `SkipPaths` and glob exclusion before cursor handling, both pruning directories with
  `SkipDir`. Each directory is held back until `WalkDir` either reports its listing error (second
  callback) or moves on, so it is delivered exactly once with the final `Err`. Relative paths are
  sliced from the cleaned walk root instead of `filepath.Rel`.
- `entry.go`: reserved names, `Kind`, `Entry`, `SkipReason`, `Stats`.
- `cli.validateGlob` now delegates to `scanner.CompileGlob` (one grammar) and rejects `\` on
  Windows.

Run, fix and improve:

- Fixed: resuming with the cursor on an unreadable directory reported it a second time
  (`TestWalkCountsUnreadableDirectoryAndContinues` failed). A listing error for the directory equal
  to the cursor is now suppressed, since it was delivered with that error before; one on a strict
  prefix of the cursor is still reported (`TestWalkUnreadableDirectoryOnCursorPathIsStillReported`).
- Fixed: cancellation inside the callback of a held-back directory still delivered the following
  entry; `deliver` now checks the context.
- Fixed while optimizing: slicing relative paths broke roots spelled with a trailing separator, a
  `.` segment or `.` itself (`WalkDir` cleans through `filepath.Join`); the walk root is now
  cleaned first and `TestWalkRootSpellings` covers the spellings.
- Ran on this host (scratch test, deleted): `/usr/share` (171,567 entries: 17,980 directories,
  121,820 files, 31,767 symlinks, 2 unreadable directories) and `/usr/lib` (71,099 entries)
  delivered exactly the `WalkDir` order with strictly increasing keys; resuming at 200 evenly spaced
  cursors each yielded the exact suffix; resuming after the last entry took 139 us (full walk
  617 ms).
- Improved: `BenchmarkWalk` (20,420 generated entries) went from 43.6 ms / 188k allocs to
  32.7 ms / 168k allocs per walk, against 27.5 ms for bare `WalkDir` plus `Info`, by replacing
  `filepath.Rel`/`Join` with slicing and reusing `WalkDir`'s joined path. Resume near the end:
  0.09 ms.
- Scratch mutations (each restored, `cmp` checked): string comparison for keys; descend only
  top-level prefixes; never prune directories before the cursor; `**` requiring a segment;
  reserved names at any depth; listing error dropped; cursor-equal suppression removed. Each made
  named tests fail.

Decisions and rejected alternatives:

- Directories are delivered to the callback (the scan statistics count them), so a checkpoint
  cursor may be a directory; resume then walks its contents only.
- The walker does not open files, so it cannot see an unreadable regular file whose `Lstat`
  succeeds; detection reports that (audit note 2).
- Globs are anchored rather than gitignore-style, because the CLI contract calls them relative-path
  globs with `path.Match` per segment; `**/` gives any-depth matching explicitly.
- Rejected: a custom recursive walker (the task names `filepath.WalkDir`, whose order is the
  contract); calling `os.Lstat` on held-back directories (`DirEntry.Info` suffices).
- Rejected: letting the callback return `fs.SkipDir`; with held-back directories it would prune
  the wrong entry, so it is an error that names `ErrStop`.

Current state: [archive registry](../current/archive-registry.md#directory-walker-internalscanner).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Order equals `WalkDir` order where string order differs | `TestWalkOrderEqualsWalkDirOrder` (asserts the fixture's string order differs); `TestCompareFollowsSegmentsNotStrings`; scratch run on `/usr/share`, `/usr/lib` | pass, Linux |
| Resume after every possible cursor yields the suffix | `TestWalkResumeAfterEveryCursorYieldsSuffix` (every delivered key plus six absent cursors); `TestCursorActions`; `TestWalkResumeMidDeepDirectoryPrunesEarlierSubtrees` (pruned unreadable subtree never listed) | pass, Linux |
| Reserved paths and globs excluded | `TestWalkExcludesReservedPathsSkipPathsAndGlobs`; `TestGlobMatch`; `TestGlobManyDoubleStarsStaysLinear`; `TestCompileGlobRejectsInvalid`; `TestReservedPartSuffixMatchesFsops`; `TestValidateGlob` (cli, incl. Windows backslash) | pass, Linux |
| Symlink reported, not followed | `TestWalkReportsSymlinksWithoutFollowing` (to dir outside root, to file, dangling, loop); `TestWalkThroughSymlinkedRoot` | pass, Linux |
| Unreadable directory counted (Linux) | `TestWalkCountsUnreadableDirectoryAndContinues` (one entry, one warning, siblings walked, resume at it); `TestWalkListableButUnsearchableDirectory`; `TestWalkUnreadableDirectoryOnCursorPathIsStillReported` | pass, Linux, uid 1000 (skipped as root) |
| Special entries skipped | `TestWalkSkipsSpecialEntries` (Unix socket); `TestStatsCount` | pass, Linux |
| Callback, context and root errors | `TestWalkCallbackControl`; `TestWalkRootErrors`; `TestWalkRootSpellings`; `TestWalkRejectsInvalidExclude` | pass, Linux |
| Tests detect regressions | Seven scratch mutations listed above | each failed named tests; sources restored |
| Race and repeat | `CGO_ENABLED=1 go test -race -count=3 ./internal/scanner ./internal/cli` | pass, Linux |
| Windows build compiles | `GOOS=windows go vet ./internal/scanner ./internal/cli`; `GOOS=windows go test -c` for both | pass (cross-compiled only) |
| Windows tests on a Windows host | Deferred [Windows verification scenario](../../guide/windows-verification.md) step W5 | not a gate (Linux-only test gates adopted after acceptance) |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

## Audit handoff

- `AUD-implement-directory-walker-1`: nonblocking. Windows behavior is cross-compiled only: symlink
  tests skip without symlink privilege, junctions are expected as `special` (Go reports them as
  irregular) but no fixture creates one, and `SkipPaths` compares paths case-sensitively. Next
  check: step W5 of the deferred
  [Windows verification scenario](../../guide/windows-verification.md#deferred-items). Owner:
  `review-stage-1-integrity` (disposition: deferred).
- `AUD-implement-directory-walker-2`: nonblocking. An unreadable regular file is visible only
  when opened; the scan must count detection open/read failures as `unreadable` in the same
  `Stats` and exit 6. Owner: `implement-scan-operation-and-csv-registry`.
- `AUD-implement-directory-walker-3`: nonblocking. `state.Checkpoint.Counters` has no directory,
  symlink or skipped-by-reason counters, and the scan must pass `--registry` (when inside the
  archive) through `SkipPaths`, use the last delivered key as `scan_cursor`, and never move the
  cursor backwards for the rare re-reported directory on the cursor path. Owner:
  `implement-scan-operation-and-csv-registry`.
- `AUD-implement-directory-walker-4`: nonblocking. By the cursor rule, entries added before the
  cursor between an interrupted scan and its resume are not registered until the next full scan.
  This is the specified behavior; next check: whether the stage-1 operator trial needs a note in
  the operator guide. Owner: `review-stage-1-integrity`.
- Incoming `AUD-implement-cli-contract-3` (backslash in `--exclude` on Windows): resolved by
  rejecting `\` on Windows with a message pointing to `/` and `[[]`; spec amended.

## Close or resume

All Linux gates pass. Windows host checks are not a gate; they are listed in the deferred Windows
verification scenario. The task
was removed from the plan and `implement-scan-operation-and-csv-registry` links this record. The
new archive-registry current page, current index and records index were updated;
`archive-registry` stays `planned` (two tasks open). Plan counts after: 26 open (23 agent,
3 human); next agent task `implement-file-type-detection`, also eligible
`implement-tool-discovery`.
