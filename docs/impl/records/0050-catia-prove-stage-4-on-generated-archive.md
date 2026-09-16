# Stage-4 proof on a generated archive

## Task and scope

- Id / capability / checkpoint: `prove-stage-4-on-generated-archive` / `catia-archive` /
  `review-stage-4-catia`
- State: accepted
- Source: plan task `prove-stage-4-on-generated-archive`; code revision `a54d613`, clean tree at
  start.
- Plan counts at start: 11 open tasks (8 agent, 3 human); next eligible
  `prove-stage-4-on-generated-archive`.
- Accepted task:

```markdown
#### prove-stage-4-on-generated-archive

Prove CATIA split and restore through the built binary the way stage 1 proved video.

- Serves: `catia-archive` -- [Exit criteria](../openspec/stage-4-catia/README.md#exit-criteria)
- Agent status: CLEAR
- Dependencies: [Replaced restore sidecar cleanup](records/0049-catia-repair-replaced-restore-sidecar-cleanup.md).
- User-visible outcome: A generated mixed archive survives killed CATIA split (with `--catia-text`)
  and restore, resume, and a byte-identical round trip, alongside a video split of the same archive
  into a separate video archive.
- Scope boundary: `test/integration` through the built `arxgo`; synthetic CATIA-like files only.
  No experimental-tree content. Linux only as a gate.
- Data and artifact paths: `test/integration/`.
- Execution path: `make test-integration` driving `split --catia --catia-text`, `split` (video),
  `restore --catia` and `restore` with seeded kills.
- Acceptance gates: Round trip manifests match; resume after kill completes; each mirror root and
  payload registry contains only its own payload; `make ci` and `make test-integration` pass.
- Documentation target: `docs/impl/current/catia-archive.md`
- Review checkpoint: `review-stage-4-catia`.
```

- Amendments: none. One product repair outside `test/integration` was needed for the proof to
  pass its contract checks (defect below); it is recorded here rather than as a separate task
  because the proof's report assertion is its regression test.

## Implementation

### Test (`test/integration/`, build tag `integration`)

- `catia_fixture_test.go`: `genArchive.addCatia` adds 22 synthetic CATIA files to the stage-1
  generator: V5 `CATPart`, `CATProduct` and `CATDrawing` (magic, `LastSaveVersion`, component window
  with invented names, 256 KiB to 2 MiB seeded payload), raw-XML and ZIP `3dxml`, `cgr`, the
  extensions in canonical, lower and upper case, at the root and in Cyrillic, space and comma
  directories, plus a foreign `<part>.md` and `<part>.text.md` at one part. Checks:
  `checkCatiaSplitOutputs` (bytes in the CATIA archive, owned descriptions with `catia:` and no
  `video:`, fallback names next to the foreign files, `arxgo-text:` sidecars, identical registry
  copies with kind, text path, and release and component count of one product, foreign files
  unchanged), `checkMirrorPayloads` (each mirror root holds only its payload and registry; the video
  registry has no CATIA row), `checkCatiaRestoreOutputs` (retired copies, rows `restored`, empty
  CATIA archive).
- `catia_roundtrip_test.go`: `TestCatiaSplitRestoreRoundTrip`, reusing the stage-1 build, kill
  (`runKilled` on WAL growth), report and manifest helpers. Scenario: five exit-2 refusals (archive
  unchanged, mirror roots not created, no lock) -> `scan` (`is_catia` per file) -> video split killed
  once and resumed -> `split --catia --catia-text --transfer copy --verify hash` killed at
  `1 + rng.Intn(9N/3)` records, killed again in the resumed run, completed by a third process (same
  run, `resumed`, `catia_done` and `texts_done` = N) -> split rerun writes nothing and leaves
  `arxgo-catia.csv` byte-identical -> `restore --catia --transfer copy` killed -> video
  `restore --transfer copy` killed (its start rolls the CATIA run forward, so the kill may land in
  that recovery) -> `restore --catia` (rolls the video run forward, completes, retires) ->
  `restore` completes -> reruns exit 0 -> manifests equal.
- Shared helpers: `isArxgoOutput` also skips root-level `arxgo-catia.*`; `checkFileRegistry`
  compares `is_catia` with the generator instead of requiring false.

### Defect found and repaired

The first real-sample run (below) reported `texts_done` 447, 459 and 408 for 492 sidecars after a
killed and resumed `split --catia --catia-text`; the contract says report counters are cumulative
for the run. A resumed process restores counters from the last throttled checkpoint; only the
payload `*_done` counter was re-derived from the WAL (`syncCommittedCounter`). Sidecars whose
`text_done` was durable after that checkpoint were lost from `texts_done` (same for
`previews_done`). Repair (`internal/archive/split.go`): `syncSidecarCounters` raises
`previews_done` and `texts_done` to the number of `preview_done` / `text_done` records in the run's
own WAL. Regressions: `TestResumedSplitCountsDurableTextDone` (crash right after the first
`text_done`: 3 of 4 before, 4 after) and the proof's report assertion, which failed on 6 of 6 seeds
built without the repair. Byte and failure counters are still not reconstructed (audit note 1).

Decisions: CATIA restore kills use `--transfer copy` so each transaction is long enough on any disk;
the completing restores use the default transfer, which also makes them runs with other defining
options that replace the killed ones. Text extraction failure (exit 6) is not driven here: it makes
every later split exit 6 while the file keeps failing, and it is covered by
`TestCatiaTextFailureLeavesFileMoved` and `TestBroken3DXMLTextFailureDoesNotRollBackMove`.
`ARXGO_TEST_VIDEO_PARENT` now places both mirror roots.

Docs: [CATIA archive](../current/catia-archive.md#stage-4-proof-testintegration),
[crash safety](../current/crash-safety.md), [index](../current.md), development guide and
Windows scenario W7.

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Round trip manifests match | `TestCatiaSplitRestoreRoundTrip` final `diffManifests` (395 files and directories: path, size, mtime ns, SHA-256) | pass, Linux, generated data |
| Resume after kill completes | same test: video split 1 kill, CATIA split 2 kills (same run, `resumed`), CATIA and video restores 1 kill each with cross-payload roll-forward | pass. Final build, seeds 3000-3029 local and 4000-4019 with `ARXGO_TEST_VIDEO_PARENT=/dev/shm`, `ARXGO_TEST_REQUIRE_TOOLS=1`, 4 in parallel: 50 of 50. Kill landings (last WAL step): CATIA split `begin` 9, `verified` 15, `placed` 16, `described` 14, `source_removed` 10, `commit` 18, `text_begin` 8, `text_done` 10; CATIA restore `begin` 8, `verified` 13, `placed` 4, `description_removed` 6, `commit` 12, `text_delete` 4, `text_deleted` 3; video split and restore across all steps. The completing CATIA restore rolled an open video transaction forward in 37 of 50. An earlier sweep (seeds 1000-1039, 2000-2019, before the counter assertion) passed 60 of 60, including one kill right after recovery aborted a transaction killed at `begin` |
| Each mirror root and payload registry contains only its own payload | `checkMirrorPayloads`, `checkCatiaSplitOutputs`, byte-equal `arxgo-videos.csv` across the CATIA split | pass |
| Report counters cumulative after kills | proof assertion `catia_done` = `texts_done` = 22 on the resumed report; `TestResumedSplitCountsDurableTextDone` | pass after the repair; 6 of 6 seeds and the unit test failed before it |
| Synthetic CATIA only, no experimental content | `test/integration/catia_*.go` uses invented names; base names (at least 5 characters) of every file and directory of the gitignored tree grepped in all committed files and the new files | pass: only generic words used as directory names there (`catia`, `Payload`, `Forms`) match; no file name, path or extracted string |
| `make ci` | `make ci` | pass, Linux; Windows cross-compiled and vetted only (W7 of the deferred scenario) |
| `make test-integration` | `make test-integration` (whole package, about 10 s) | pass, Linux |
| Declared extra: real samples | Scratch driver outside the repository on a copy of the gitignored experimental tree (aggregates only): 492 CATIA files (329 `CATPart`, 157 `CATProduct`, 6 `CATDrawing`; 447 MiB) and 19 other files, plus one generated NVENC clip; both mirror roots on `/dev/shm`. `split` (video), `split --catia --catia-text --transfer copy --verify hash` SIGKILLed twice on WAL growth and resumed, rerun, `restore --catia --transfer copy --verify hash` and video restore killed, both restores completed and rerun; seeds 1-5 | pass: 5 of 5 round trips with 0 manifest differences, mirror roots empty, no SHA-256 mismatch in the CATIA archive, 492 rows `moved` with `text_rel_path`, release present for all, component count above zero for 309, rerun `catia_done=0 texts_done=0`, `texts_failed=0`. Seeds 1-3 exposed the counter defect; seeds 4-5 with the repair report `texts_done=492`. Resumed CATIA split 15-24 s, CATIA restore 10-19 s. This is agent evidence only; `approve-stage-4-on-operator-catia-copy` stays open |

## Audit handoff

- `AUD-prove-stage-4-on-generated-archive-1`: after a kill, the byte counters
  (`*_bytes_written`/`*_bytes_freed`, `video_bytes`, `catia_bytes`) and the failure counters of a
  resumed run are restored from the last checkpoint only, so `report.json` can undercount them,
  while the contract calls report counters cumulative. Pre-existing since stage 1; nonblocking;
  location `Stats.Restore` / `syncCommittedCounter` / `syncSidecarCounters`. Next check: owner
  `review-stage-4-catia`, decide between reconstructing them from the WAL or narrowing the contract.
- `AUD-prove-stage-4-on-generated-archive-2`: on the real samples, 183 of 492 V5 documents report
  0 components. Extraction is bounded by design and a missing window is a valid `0 components`; the
  operator should judge usefulness. Nonblocking; location `internal/catia` component window. Next
  check: owner `approve-stage-4-on-operator-catia-copy`.

Reviewed scope: generator, contract checks, kill scenario and its landing distribution,
cross-payload recovery, counter sync, data ban, docs.

## Close or resume

All gates passed on Linux; Windows vetted and cross-compiled only. Plan task removed; the
dependencies of `review-stage-4-catia` and `approve-stage-4-on-operator-catia-copy` replaced with
this record; current-state pages, development guide, Windows scenario and records index updated;
`catia-archive` stays planned until the checkpoint. Plan counts after: 10 open tasks (7 agent,
3 human); next eligible `review-stage-4-catia`.
