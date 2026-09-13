# Implement File Type Detection

## Task and scope

- Id / capability / checkpoint: `implement-file-type-detection` / `archive-registry` /
  `review-stage-1-integrity`
- State: accepted (Linux gates pass; Windows is cross-compiled and vetted only, see audit handoff)
- Source: plan task `implement-file-type-detection`, operator request on 2026-09-13
  ("implement, run, fix, improve implementation and then update documentation and plan.md").
  Branch `ag-01-stage-1` at `7624c05`, clean tree.
- Plan counts at start: 26 open (23 agent, 3 human); next agent task
  `implement-file-type-detection`; also eligible `implement-tool-discovery`.
- Accepted task:

```markdown
#### implement-file-type-detection

Classify each file's MIME type and binary, media, picture, video and large flags.

- Serves: `archive-registry` -- [Type detection](../openspec/stage-1-core/registry.md#type-detection)
- Agent status: CLEAR
- Dependencies: [CLI contract](records/0003-foundation-implement-cli-contract.md).
- User-visible outcome: Registry flags reflect file content rather than extensions, with the
  specified extension fallback for ambiguous signatures.
- Scope boundary: `Detect(path, size, opts) FileType` using `github.com/gabriel-vasile/mimetype`,
  text hierarchy for `is_binary`, built-in and extra video extension lists, large threshold. The
  ISO BMFF no-video-track refinement is owned by `implement-iso-bmff-metadata`.
- Data and artifact paths: `internal/scanner/mimetype.go`; `go.mod`, `go.sum`.
- Execution path: `mimetype.DetectReader` over a bounded read; parent traversal for text.
- Acceptance gates: Generated fixtures: plain text, UTF-8 with BOM, JSON, SVG, PDF header, PNG,
  JPEG, WAV, minimal MP4/M4A/MOV `ftyp` headers, Matroska/WebM EBML header, random bytes with a
  video extension, text with `.mp4` extension, empty file, exact large threshold.
- Documentation target: `docs/impl/current/archive-registry.md`
- Review checkpoint: `review-stage-1-integrity`.
```

- Amendments: none to the task. Two interface refinements within scope: `Detect` returns
  `(FileType, error)` so the scan can count open/read failures as `unreadable`
  (`AUD-implement-directory-walker-2`), and options are built with
  `NewDetectOptions(largeThreshold, extraVideoExtensions)`. Detection runs `mimetype.Detect` over
  the bounded head instead of `DetectReader`, which is the same call after the same bounded read
  and lets the pure `Classify` be table-tested. Spec clarifications from measured library
  behavior:
  - [type detection](../../openspec/stage-1-core/registry.md#type-detection): the `mimetype`
    default read limit is 4096 bytes in v1.4.15 (the spec said 3072); open/read failures are
    `unreadable`; opening never blocks on a FIFO; `file_type` fallback ignores a leading dot
    (`.profile` has no extension); M4A is `audio/x-m4a` (the spec said `audio/mp4`, which is
    used for `M4B `/`M4P `/`F4A ` brands); audio-only generic-brand MP4 and audio-only WebM/Matroska
    are video types; MPEG-TS, M2TS, MXF and DV are video only through the extension list.
  - [ISO BMFF parser](../../openspec/stage-1-core/metadata.md#iso-bmff-parser): `audio/x-m4a`
    added to the MIME types it applies to, so M4A files are not missed.

## Implementation

`internal/scanner/mimetype.go` (new) and `github.com/gabriel-vasile/mimetype` v1.4.15 (approved in
the dependency table; pure Go with no module dependencies of its own, so `go.sum` gains only its
two lines).

- `Detect(path, size, opts)`: `readHead` opens with `os.O_RDONLY|syscall.O_NONBLOCK`, checks the
  opened file is regular, reads up to `DetectLimit` bytes into a `sync.Pool` buffer; `IsLarge`
  from the recorded size.
- `Classify(head, name, opts)`: empty head -> `inode/x-empty`; `mimetype.Detect`; parameters
  stripped; canonical extension or name extension for `application/octet-stream`; `isText` walks
  `Parent()` looking for `text/plain`, plus the spec allow-list and `text/*`; video by `video/`
  prefix or octet-stream plus extension list; picture, media.
- `BuiltinVideoExtensions`, `DetectOptions` (zero value uses the built-in list and no large
  threshold), `MIMEEmpty`, `MIMEOctetStream`.

Run, fix and improve:

- Ran on real media generated with ffmpeg on this CUDA host (scratch, deleted): libx264 and
  `h264_nvenc`/`hevc_nvenc`/`av1_nvenc` MP4, fast-start and fragmented MP4, M4V, MOV, 3GP, MKV,
  VP9 WebM, Opus-only WebM, AVI, FLV, WMV, MPEG-PS, VOB, Theora OGV, MPEG-TS, M2TS, MXF, DV, AAC M4A
  and audio-only MP4, WAV, MP3, FLAC, Vorbis Ogg, PNG, JPEG, WebP, GIF, BMP, TIFF, AVIF, and a
  truncated MP4: 36 files, no errors, results as listed in the current page. This run found the
  three spec mismatches amended above (read limit, M4A MIME, audio-only WebM).
- Ran walk plus detection over `/usr/share` (121,820 files): 2 `permission denied` errors
  returned as errors, 35,543 binary, 29,789 pictures; 1.37 s warm on one goroutine, 12.6 s cold.
- Fixed (test): `TestClassifyIgnoresBytesBeyondLimit` first sliced a prefix that contained no NUL
  byte, so the "within the limit" case could not fail for the right reason.
- Improved: pooled head buffers; `BenchmarkDetectFile` (scratch) went from 4.2 us, 4,648 B,
  9 allocs to 2.6 us, 552 B, 8 allocs per cached file. `TestDetectConcurrent` guards buffer reuse
  under `-race`.
- Improved: `O_NONBLOCK` open plus regular-file check, so a file swapped for a FIFO between the
  walk and detection fails fast instead of hanging a detection worker
  (`TestDetectFIFODoesNotBlock`; removing the flag makes it time out).
- Scratch mutations (each restored): no `O_NONBLOCK`; no hierarchy walk; extension fallback for
  every MIME; no leading-dot rule; `>` instead of `>=`; no regular-file check; empty treated as
  content. Each failed named tests. Removing the text allow-list did not fail any test: every
  allow-listed type already descends from `text/plain` in `mimetype` v1.4.15, so the allow-list is
  kept as the specified guard against hierarchy changes in a future library version.

Decisions and rejected alternatives:

- The `syscall.O_NONBLOCK` constant exists on every target and Windows `Open` ignores it, so no
  build-tagged file is needed in `scanner` (the architecture keeps platform files in `fsops`).
- Rejected: `mimetype.SetLimit` (process-global); `mimetype.DetectFile` (unbounded open semantics,
  no FIFO guard, allocates per call).
- Rejected: skipping the open for `size == 0`: the recorded size can be stale, and detection
  decides emptiness from the bytes read.
- The FIFO regression test runs the `mkfifo` utility from the test (skipped when missing) instead
  of `syscall.Mkfifo`, which does not compile for Windows.

Current state: [archive registry](../current/archive-registry.md#file-type-detection-internalscanner).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| Plain text, UTF-8 with BOM, JSON, SVG, PDF header, PNG, JPEG, WAV | `TestClassifyFixtures` (generated bytes; also UTF-16LE BOM, HTML) | pass, Linux |
| Minimal MP4/M4A/MOV `ftyp` headers, Matroska/WebM EBML header | `TestClassifyFixtures`; `TestDetectFiles` (file on disk, lying `.txt` name) | pass, Linux |
| Random bytes with a video extension | `TestClassifyFixtures` (`.MTS`, extra `.dat`, other and no extension, dotfile); `TestVideoExtensionOptions` (every built-in, upper case, extras) | pass, Linux |
| Text with `.mp4` extension | `TestClassifyFixtures`; `TestVideoExtensionOptions` (signature wins) | pass, Linux |
| Empty file | `TestClassifyFixtures`; `TestDetectFiles` | pass, Linux |
| Exact large threshold | `TestDetectLargeThreshold` (below, equal, above, zero threshold, on-disk file of exactly the threshold) | pass, Linux |
| Text hierarchy for `is_binary` | `TestIsBinaryHierarchy`; `TestClassifyIgnoresBytesBeyondLimit` | pass, Linux |
| Bounded read, errors, FIFO | `TestDetectReadsOnlyTheHead`; `TestDetectErrors` (missing, directory, mode 000 as uid 1000); `TestDetectFIFODoesNotBlock` | pass, Linux |
| Concurrency | `CGO_ENABLED=1 go test -race -count=3 ./internal/scanner` incl. `TestDetectConcurrent` | pass, Linux |
| Real media | Scratch probe over 36 ffmpeg/NVENC-generated files; `/usr/share` run | pass (not a committed gate) |
| Tests detect regressions | Seven scratch mutations listed above | each failed named tests; allow-list removal valid-negative |
| Windows build compiles | `GOOS=windows go vet ./internal/scanner`; `GOOS=windows go test -c ./internal/scanner`; `make build-all`, `make vet-windows` in `make ci` | pass (cross-compiled only) |
| `make ci` on Linux | `make ci` (Go 1.27.1) | pass |

## Audit handoff

- `AUD-implement-file-type-detection-1`: nonblocking. `--video-extensions` is a split flag only
  (`cli.SplitOptions`), so a registry written by `arxgo scan` uses only the built-in list while the
  split's scan phase also uses the extras; the same file can get different `is_video` values.
  Next check: decide whether scan accepts the flag or split re-classifies. Owner:
  `implement-scan-operation-and-csv-registry`.
- `AUD-implement-file-type-detection-2`: nonblocking. Audio-only Matroska/WebM files are
  `video/webm`/`video/matroska` and would be split as videos; the spec refines only ISO BMFF.
  Next check: whether the ffprobe path should apply the same no-video-stream refinement (spec
  amendment). Owner: `implement-ffprobe-metadata`.
- `AUD-implement-file-type-detection-3`: nonblocking. The ISO BMFF refinement must route
  `audio/x-m4a` as well as `audio/mp4` (spec amended here), and generic-brand audio-only MP4 is
  `video/mp4` until refined. Owner: `implement-iso-bmff-metadata`.
- `AUD-implement-file-type-detection-4`: nonblocking, Windows only. `O_NONBLOCK` is ignored by the
  Windows open, and opening a file held with exclusive sharing or denied by ACL must return an error
  counted as unreadable. Next check: step W5 of the deferred
  [Windows verification scenario](../../guide/windows-verification.md#deferred-items). Owner:
  `review-stage-1-integrity` (disposition: deferred).
- Incoming `AUD-implement-directory-walker-2`: detection now returns open/read errors; counting them
  as `unreadable` in `Stats` stays with `implement-scan-operation-and-csv-registry`.

## Close or resume

All Linux gates pass. Windows host checks are not a gate. The task was removed from the plan and
`implement-scan-operation-and-csv-registry` links this record. The archive-registry current page,
current index and records index were updated; `archive-registry` stays `planned` (one task
open). Plan counts after: 25 open (22 agent, 3 human); next agent task
`implement-scan-operation-and-csv-registry`, also eligible `implement-tool-discovery`.
