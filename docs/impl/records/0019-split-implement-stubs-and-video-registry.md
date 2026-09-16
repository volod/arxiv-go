# Stubs and video registry

## Task and scope

- Id / capability / checkpoint: `implement-stubs-and-video-registry` / `video-split` / `review-stage-1-integrity`
- State: accepted
- Source: plan task; starting revision includes accepted [0018](0018-split-implement-video-split-transactions.md) work in the tree.
- Plan counts at start: 20 open (17 agent, 3 human); next eligible: this task.
- Accepted task:

```markdown
#### implement-stubs-and-video-registry

Leave a Markdown stub at each former video location and write the video registry and summary.

- Serves: `video-split` -- [Data contracts](../openspec/stage-1-core/contracts.md#markdown-stub)
- Agent status: CLEAR
- Dependencies: [Video split transactions](records/0018-split-implement-video-split-transactions.md);
  [ffprobe metadata](records/0016-metadata-implement-ffprobe-metadata.md).
- User-visible outcome: Every moved video has `<name>.<video_ext>.md` with front matter,
  relative and absolute links, optional base-URL link and metadata; both roots contain
  `arxgo-videos.csv` and `arxgo-videos.md`.
- Scope boundary: Stub renderer and parser (front matter), stub collision rule, URL composition and
  escaping, video registry regeneration from WAL + existing file, summary aggregation. No previews.
- Data and artifact paths: `internal/report/markdown.go`, `internal/report/videos.go`,
  `internal/report/summary.go`.
- Execution path: `text/template` rendering with golden files; front matter parsed with a minimal
  line-based reader (flat keys only, no YAML dependency).
- Acceptance gates: Golden stub for file and media modes, with and without base URL, Unicode and
  space-containing paths; existing foreign `<name>.<video_ext>.md` triggers `<name>.arxgo.md`;
  registry regeneration after two runs merges rows deterministically;
  summary bands and top-100 table. When generating a filename, the system handles potential
  duplicates created by users or the utility during previous runs. If the utility created the file,
  it should be overwritten with the current metadata, so we need a special marker tag at
  the beginning of the .md file. If a file with that name already exists in the archive created by
  a human, a <name>-<idx>.<video_ext>.md format is generated. Operating system limitations on
  filename and full-path lengths are also taken into account; in such cases, the filename is
  truncated to accommodate the index length, the video file extension, the .md file extension,
  and the dots separating them, so we have the filename format <name-prefix>-<idx>.<video_ext>.md,
  which satisfies the operating system's requirements regarding file names and full path lengths.
- Documentation target: `docs/impl/current/video-split.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none.

## Implementation

`internal/report` renders stubs, the video CSV and the summary. `internal/archive` writes them
inside split execute and recovery. No new module dependencies.

- `frontmatter.go` / `markdown.go`: line-based YAML header (`arxgo_stub: 1` first), `text/template`
  body, golden files under `test/testdata/report/`.
- `names.go`: `<name>.<ext>.md`, then `<name>.<ext>.arxgo.md`, then `<prefix>-<idx>.<ext>.md` with
  prefix truncation to NAME_MAX / PATH_MAX (unix and windows build tags). Owned stubs (matching
  `rel_path`) are overwritten.
- `url.go`: per-segment `url.PathEscape`, `ComposeURL`, `filepath.Rel` for the relative link.
- `videos.go` / `summary.go`: RFC 4180 CSV, merge by `rel_path` (moved wins), walk-order sort,
  resolution bands, top-100 table.
- `split_stub.go` / `split_report.go`: same writer for execute and recovery; phase `report`
  regenerates both roots from every run WAL plus the existing file. CLI installs the writer on
  the split resolver before `Start`.

Decisions and rejected alternatives:

- No YAML library; the parser is the contract's "flat keys only" reader.
- Collision keeps the 0018 `.arxgo.md` fallback, then indexed names when that is also foreign.
- Report-time hashing is avoided: SHA-256 comes from the copy `verified` record or a hash of the
  destination only when `--verify hash` and the stub is written on the rename path.
- Empty `--base-url` leaves existing registry URLs; a non-empty flag rewrites `moved` rows.
- Golden stubs contain archive-relative links that leave the repository. `lint-doc-links`
  skips `testdata` rather than rewriting product goldens or weakening the check on docs.

Current state: [video split](../current/video-split.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Golden stub file/media, with/without base URL, Unicode and spaces | `TestRenderStubGoldens` (`test/testdata/report/stub-*.md`) | pass, Linux |
| Foreign `<name>.<ext>.md` uses `<name>.<ext>.arxgo.md` | `TestSplitStubCollisionUsesFallback`; `TestChooseStubPathCollisionAndTruncation`; `TestMarkdownStubChoosesFallbackForForeignFile` | pass, Linux |
| Indexed `<name>-<idx>.<ext>.md` when both primary and `.arxgo.md` are foreign | `TestSplitForeignThenIndexedStub`; `TestMarkdownStubIndexedNameWhenFallbackIsForeign` | pass, Linux |
| Filename truncation for OS limits | `TestChooseStubPathCollisionAndTruncation` (`nameMax=20`) | pass, Linux |
| Registry merge after two runs, walk order | `TestSplitRegistryMergesTwoRuns` | pass, Linux |
| Summary bands and top-100 table | `TestSummaryBandsAndTop100` | pass, Linux |
| Both roots get CSV and summary; `--base-url` in stub | `TestSplitWritesVideoRegistryAndSummaryToBothRoots`; `TestSplitCommandMovesVideo` | pass, Linux |
| Media-mode duration line | `TestSplitMediaModeStubHasDuration` (generated MP4, no ffprobe) | pass, Linux |
| `--dry-run` writes no stubs or video registry | `TestSplitDryRunLeavesVideoAndStubUntouched` | pass, Linux |
| Required repository gates | `GOCACHE=/tmp/arxgo-gocache make ci` | pass, Linux (Go 1.27); Windows runtime cross-compiled only |
| Race | `CGO_ENABLED=1 go test -race -count=1 ./internal/report ./internal/archive ./internal/cli ./internal/state` | pass, Linux |

## Audit handoff

none identified. Reviewed: stub overwrite vs foreign files, WAL+existing merge (moved wins),
identical copies in both roots, dry-run, recovery using the same writer as execute, Windows
name/path limits by cross-compile only.

## Close or resume

All Linux gates pass, including race on the changed packages. The task is removed from the plan;
`implement-video-restore` depends on this record. The video-split current page, current index,
crash-safety, project-foundation, README, architecture tree and the capability registry are
updated. Capability `video-split` is `shipped`. Plan counts after: 19 open (16 agent, 3 human);
next eligible `implement-video-restore`.
