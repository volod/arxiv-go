# Compact Markdown Stub

## Task and scope

- Id / capability / checkpoint: `compact-markdown-stub` / `video-split` / none
- State: accepted.
- Source: ad hoc operator request while preparing `provide-google-drive-test-account`; revision
  `dcb38ed` with the uncommitted version-file change of
  [0038](0038-foundation-version-from-semver-file.md).
- Plan counts at start: 9 open tasks (6 agent, 3 human); next agent task
  `research-cloud-target-apis`.
- Accepted task:

```text
After fisplit archive, we got the following metadata .md for video files:

Please:
Delete ---, provide all information as single block
Use a shorter file stub e.g., arxgo
Keep in mind that the stub file can be used to identify the file type during archive restoration and to check for duplicate entries. There are currently no archives requiring restoration, and backward compatibility is not needed, so do not overcomplicate the implementation.

Provide only the path related to the archive root rel_path, remove video_archive_path
For file_size, also provide a human-readable value, e. g: 1073741824 - 1 TB
Remove run_id if we do not need it.
Remove # filename.MP4 - we alredy have rel_path, remove text "This video was moved to the video archive by arxgo", remove - Size: we have file_size already.
The file should be free of newlines to ensure a compact, machine- and human-readable format.
Include useful, readily available information about the video that requires no further reading or analysis—such as the creation time or, if the video parameters are available, the resolution and duration.
```

- Amendments: "free of newlines" was ambiguous. Asked "What does \"free of newlines\" mean for the new
  stub?", the operator chose "One field per line": `key: value` lines with no blank lines, preview
  links kept as lines. The example "1073741824 - 1 TB" is 1 GiB; sizes use the binary units of
  the rest of arxgo (`1073741824 (1.0 GiB)`).
- Amendment 2, operator follow-up: "png link shoul contain link to the png file next to the md file,
  and mp4 links should be absolute lint to the are moved video file". The `-smpl01` MP4 is a sample
  clip written next to the stub like the PNG, so its relative link stays; asked how to link the
  moved video, the operator chose "Add a video link line": `moved_to: [<name>](file:///<absolute
  video-archive path>)`.

## Implementation

`internal/report/stub.go` replaces `markdown.go` and `frontmatter.go`. `RenderStub` writes the marker
line `arxgo: <rel_path>`, then `file_size` (bytes and binary size), `file_mime`, `sha256`,
`created` (container creation time), `modified` (file mtime from the WAL begin record), `video`
(duration, size, codecs, frame rate), `moved_at`, `moved_to` (Markdown link to the `file://` URL
of the destination, `report.FileURL`; Windows drive and UNC forms tested) and `url`, omitting
unknown values. Removed:
the `---` delimiters, `arxgo_stub: 1`, `rel_path` as a separate key, `video_archive_path`, `run_id`
(never read; the video registry keeps it), the title, the sentence, the video-archive and absolute
path lines and `Size`. `InspectStub` reads only the first line, so ownership for restore, recovery
and name collisions is the marker naming the video. `ParseStub` reads the field block for tests.
`ReplacePreviewLinks` rewrites the `- ` link lines directly after the fields, replacing the HTML
comment section. No compatibility with the old format is kept, as requested.

Spec: [stub contract](../../openspec/stage-1-core/contracts.md#video-description), split and restore
stub rules, previews stub rule. Current pages: [video split](../current/video-split.md),
[video restore](../current/video-restore.md).

## Acceptance evidence

| Gate | Exact command, test or artifact | Result and limit |
| --- | --- | --- |
| New format | `TestRenderStubGoldens` (`test/testdata/report/stub-*.md`: file mode, media with hash and URL, quoted Unicode path) | pass: no `---`, no blank lines, marker first, `file_size: 734003200 (700.0 MiB)` |
| Ownership and duplicates | `TestInspectStubTreatsNonFilesAsForeign`, `TestChooseStubPathCollisionAndTruncation`, split and restore stub tests | pass: other `rel_path`, marker not on the first line, over-long first line, directories and links are foreign |
| Preview links | `TestReplacePreviewLinks`; archive preview tests | pass: add, replace, remove, operator text kept |
| Real binary | built `arxgo split --metadata media --verify hash --sample start --image middle`, then `restore --previews delete`, on a generated H.264 clip with `creation_time` | pass: stub as in the contract with `created`, `video` and two link lines; restore removed stub and previews |
| Suites | `ARXGO_TEST_REQUIRE_TOOLS=1 go test ./internal/...`; `ARXGO_TEST_REQUIRE_TOOLS=1 make test-integration`; `make ci` | pass on Linux |

## Audit handoff

None identified.

## Close or resume

Accepted. The record index and current pages link this record; plan counts unchanged.
