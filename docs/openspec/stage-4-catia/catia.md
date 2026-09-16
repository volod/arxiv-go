# CATIA files

Owner: `catia-archive`. Flags: [CLI](../stage-1-core/cli.md). Move behavior:
[CATIA split and restore](split-restore.md). File registry: [type detection](../stage-1-core/registry.md#type-detection).

## Operator problem

A mixed archive holds CATIA CAD files that are large and awkward to share with the document tree.
The operator needs those files marked in the file registry, moved with the same safety as video,
and described by Markdown that a person can read without CATIA. Optional extracted text makes
assembly membership and other embedded strings searchable in the document archive.

## Kinds

Stage 4 treats these extensions as CATIA, compared case-insensitively against the last dotted
suffix of the file name (the same rule as video extensions):

| Extension | Kind token | Role |
| --- | --- | --- |
| `.CATPart` | `CATPart` | Individual 3D part |
| `.CATProduct` | `CATProduct` | Assembly that names other CATIA files |
| `.CATDrawing` | `CATDrawing` | 2D drawing |
| `.cgr` | `cgr` | Lightweight graphical representation |
| `.3dxml` | `3dxml` | XML-based 3D exchange (a ZIP of XML members, or raw XML) |

The table is defined once, in `internal/catia` (a leaf package with no internal imports), and the
scanner uses it for classification. There is no `--catia-extensions` flag in this stage.

## Classification

`scan` always classifies CATIA files; it takes no CATIA flag.

- `is_catia` is true when the file's extension is in the table above. The decision does not read
  content, so a damaged or truncated CATIA file is still moved with its siblings.
- A macOS AppleDouble sidecar (`._<name>`, magic `00 05 16 07`) is never CATIA, whatever its
  extension.
- `is_catia` files are never `is_video`. A CATIA extension in `--video-extensions` is a usage error
  (exit 2), so the two payloads can never select the same file.
- `file_mime`, `file_type`, `is_binary`, `is_media` and `is_picture` follow the unchanged stage-1
  rules (a V5 document is `application/octet-stream` with `file_type` from its extension; a ZIP
  `.3dxml` is `application/zip`).

The file registry always has the `is_catia` column, after the other type flags
([column order](../stage-1-core/contracts.md#file-registry-csv)). Scan statistics gain a `catia`
count and byte total, omitted from JSON when zero. There are no stage-1 registries to stay
compatible with, so readers require the current header.

## Format detection

Format is read from the leading bytes at split time, independent of the kind, and only feeds
metadata. It never changes selection:

| Leading bytes | Format token |
| --- | --- |
| `V5_CFV2` followed by NUL | `V5_CFV2` (CATIA V5 container of `.CATPart`, `.CATProduct` and `.CATDrawing`) |
| `PK\x03\x04` | `zip` |
| optional UTF-8 BOM and whitespace, then `<` | `xml` |
| anything else, or an unreadable head | `unknown` |

## Accessible metadata

Scan stays cheap: it only sets `is_catia`. Structured CATIA fields are collected at split time by
`internal/catia`, in pure Go, with no external process, in one streaming pass over the placed
destination copy (recovery that rolls a transaction forward repeats the pass). Memory is bounded
by the caps below, not by file size. An extraction error is logged at warn with the error kind and
yields the empty values below; it never skips or rolls back the move.

The description's `catia:` line is a short summary, never a list of extracted names:

```text
catia: CATProduct | V5_CFV2 | V5R30 SP5 | 12 components
```

| Token | Content |
| --- | --- |
| kind | kind token from the extension |
| format | format token |
| release | V5: `V5R<release>` plus ` SP<service pack>` when present, from the `LastSaveVersion` document property, falling back to `MinimalVersionToRead`; 3dxml: `3DXML <version>` from the header `SchemaVersion` element; otherwise `unknown` |
| components | `<n> components` (`1 component` when n is 1): the number of distinct component names that [text extraction](#components-of-v5-documents) finds, `0 components` when none |

The description holds the video description's filesystem fields (`file_size`, `file_mime`,
`sha256` when hashed, `modified`, `moved_at`, `moved_to`, `url`) plus `catia:`, and no `video:` or
`created:` field. `--metadata file|media` still applies to the file-registry scan of the whole tree
and does not drive CATIA extraction.

### V5 document properties

A V5 container stores its save history as length-prefixed key/value records: a little-endian
uint16 key length, the ASCII key, the uint32 type `0x0000000e` (string), a little-endian uint32
value length (at most 4 KiB), then the value. The extractor reads these keys and ignores others:

| Key | Use |
| --- | --- |
| `LastSaveVersion` | `<Version>`, `<Release>`, `<ServicePack>` elements (written as `<Version>5/<Version>`) give the release token |
| `MinimalVersionToRead` | release fallback, for example `CATIAV5R30` |
| `CATBuildLevel` | text sidecar `properties:` only |

The first occurrence of each key wins. A missing or malformed record leaves the field empty.

## Text extraction

`--catia-text` on `split --catia` writes a second owned Markdown file next to the description:
[`<rel_path>.text.md`](../stage-1-core/contracts.md#catia-text-sidecar). Extraction runs after the
move commits, never affects the original, and a failure logs a warning, records `text_failed`, and
does not roll back the move (the stage-2 preview rule). Transactions, reruns and restore:
[CATIA split and restore](split-restore.md#text-sidecars).

The sidecar is opt-in because harvested strings can include the authoring user id and absolute
paths of the authoring workstation. The description never contains them.

### Components of V5 documents

The published approach in
[sobalvarro/CATProductFiles](https://github.com/sobalvarro/CATProductFiles/blob/master/CATProductFiles/Form1.cs)
treats the file as text and walks a marker window. The extractor applies it to every `V5_CFV2`
document of any kind:

1. Find the first `CATOctetArray`, then the first backspace byte (0x08) followed by `FINJPL` after
   it. If either marker is missing, or the window between them exceeds 4 MiB, the list is empty.
   The window is located while streaming; only the window is buffered.
2. Decode the window as UTF-8 (invalid sequences become U+FFFD) and remove every `;` followed by
   U+0001. Without this step the next split finds nothing.
3. Split the window on the three-character separator U+0001, U+0004, `File`.
4. Skip chunks that start with `CATOctetArray` or whose text after the first character starts with
   `feat`.
5. In each remaining chunk take the text from the first NUL up to the first `"` (U+0022). Skip the
   chunk when either is missing or the quote comes first.
6. Remove NULs, keep the text after the last backslash, and drop the final character, as the
   reference tool does.
7. Discard names that are empty, contain a control character, `/` or U+FFFD, or equal the file's
   own base name (compared case-insensitively). A V5 document normally lists itself.
8. Deduplicate and sort byte-wise.

A `.CATPart` usually yields no components after step 7 and a `.CATDrawing` yields the documents it
draws. An empty list is a valid result, not an error.

### 3dxml

- A ZIP `.3dxml` is read with `archive/zip`. Member names containing `..`, an absolute path or a
  volume are skipped. Limits: 1024 members, 64 MiB uncompressed per member and 256 MiB in total;
  exceeding a limit stops reading, sets `truncated: true` and keeps what was collected.
- `Manifest.xml` names the root member (`<Root>`); every `.xml` and `.3dxml` member is parsed with
  `encoding/xml` (no DTD or external entity is fetched).
- Header elements `SchemaVersion`, `Title`, `Author`, `Generator` and `Created` go to
  `properties:`.
- Components: the `associatedFile` attribute of `ReferenceRep` and the file part of `urn:3DXML:`
  references, reduced to their base names; `http:`, `https:` and other external URLs are ignored.
  The `name` attributes of `Reference3D` and `Instance3D` go to `strings:`.
- An XML syntax error in the root member is `text_failed` with no sidecar; an error in another
  member is logged and that member is skipped.

### Strings

Every kind also harvests printable runs for search:

- ASCII runs of bytes 0x20-0x7E, and UTF-16LE runs of the same characters, at least 6 characters
  long and containing at least 3 ASCII letters, trimmed of surrounding spaces;
- runs already listed under `components:` are excluded; the rest are deduplicated and sorted
  byte-wise under `strings:`.

For a ZIP `.3dxml` the harvest reads the XML text content and attribute values rather than the
compressed bytes.

### Caps and encoding

- The sidecar is UTF-8 without BOM with `\n` line endings. Values are escaped as description values
  are ([contracts](../stage-1-core/contracts.md#video-description)).
- The rendered sidecar is capped at 1 MiB. Blocks are filled in order (`properties:`,
  `components:`, `strings:`); the first item that does not fit ends the file after the last
  complete item and sets `truncated: true`. Collection stops once the cap is reached, so memory
  stays bounded.
- Harvested strings and component names are never logged at info or higher; debug logs record
  counts only.

## Self-locating metadata

A description and a text sidecar are written next to the file they describe, so their own location
supplies the rest of the story. An operator who harvests them into a search index or a vector
database reads them detached from that location, where `arxgo: <rel_path>` alone does not say which
archive the path belongs to, and a sidecar says nothing about the file at all beyond its path.

- Every description and every text sidecar carries `archive: <absolute archive root>`, written
  directly after the marker line. It is the only field that names the root, so a harvested document
  identifies its archive and a reader can rejoin `rel_path` to it.
- A text sidecar repeats the identity block of its description between `archive:` and the
  `properties:` block: `file_name`, `file_size`, `file_mime`, `sha256` when the split recorded one,
  `modified`, `catia`, `moved_to` and `url` when there is one, followed by
  `description: <rel_path of the description>`. The values are the ones the description holds, so a
  sidecar stands alone without being re-extracted.
- Field order stays fixed, values keep the description escaping, and the first line is unchanged:
  `arxgo: <rel_path>` for a description and `arxgo-text: <rel_path>` for a sidecar, so occupancy,
  conflict naming, restore and recovery are untouched.
- The identity block counts against the sidecar's 1 MiB cap before any block is filled. The
  marker, `archive:`, `extracted_at:` and `truncated:` lines are always written; identity fields are
  kept in order while they fit, and the first that does not fit drops itself and every later
  field and block and sets `truncated: true`.
- The owned description is the one the WAL `described` record of an earlier run names while it is
  still owned, otherwise the first owned name in the description naming order. Without one, the
  sidecar holds `file_name` only and split logs a warning; the sidecar is still written.

Evaluation: a split writes `archive:` as the second line of both files; a sidecar of a hash-verified
split carries the same `sha256`, `file_size` and `catia` values as its description; a sidecar whose
identity block alone exceeds the cap is `truncated: true` with no blocks; `restore --descriptions
delete` still removes both files.

Excluded: rewriting descriptions of earlier runs when the archive root moves, and any field that
names the operator's host or user.

## Text index

`--catia-text` leaves one sidecar per file. An operator who wants to read or feed the whole archive
at once needs them in one document, and assembling that in the shell means guessing sidecar names
instead of reading the ownership the WAL recorded.

`arxgo catia-index --archive PATH [--out PATH] [--strings]` writes one Markdown document from the
owned descriptions and sidecars that the CATIA run history records
([contract](../stage-1-core/contracts.md#catia-text-index)):

- a header with `archive`, `catia_archive` (the mirror root of the latest CATIA run), `history_at`
  (the time of the newest record of the CATIA run history, so the document names the state it
  reflects and a rerun over the same state writes the same bytes) and the `files`, `components` and
  `missing_text` counts;
- one `## <rel_path>` section per CATIA file whose replayed CATIA history ends `moved` (the rows
  `arxgo-catia.csv` marks `moved`), in walk order, holding the identity block of its owned
  description (the sidecar identity fields and `description:`), then `text:` and `truncated:` of its
  owned sidecar and the sidecar's `properties:` and `components:` blocks;
- `strings:` only with `--strings`, because harvested runs dominate the size;
- `--out` defaults to `<archive>/arxgo-catia-text.md`, a [reserved path](../spec.md#reserved-paths);
- files without a usable owned sidecar keep their section and are listed once under a final
  `## Missing text` section with a reason, not skipped silently: `not_recorded` (no completed text
  event in the history), `missing` (the recorded sidecar is gone), `foreign` (the file at the
  recorded path no longer starts with `arxgo-text: <rel_path>`), `unreadable`.

Ownership comes from the history, never from file names: the owned description is found with the
[self-locating metadata](#self-locating-metadata) rule and the sidecar is the last completed text
event of that file, so fallback names are read like plain ones. When the description is gone, the
section takes the identity fields the sidecar repeated and has no `description:` line; the index
logs a warning.

It reads only: it starts no run, writes no run log, takes no lock and writes nothing but the output
(through a part file and an atomic replace). A lock held by a live process on this host, or by
another host, exits 5 because that run is changing the history and the sidecars; a stale or
unreadable lock is logged and the recorded history is indexed. Corrupt run state exits 5; an
interrupt exits 130 and leaves an earlier output unchanged. It needs no mirror root and never
re-extracts a CATIA file. Missing text alone does not change the exit code (0).

`--out` is validated before anything is read: it must not be a directory, its parent must exist,
and it must not name run state, a registry, a part file, or an existing arxgo description or text
sidecar (exit 2). Another existing file named by `--out` is replaced. An index written inside the
archive under another name is an ordinary file for the next scan.

Evaluation: the section count equals the `moved` rows of `arxgo-catia.csv`; a file whose sidecar was
deleted appears under `Missing text`; removing the `strings:` blocks from the `--strings` output
gives the default output byte for byte; a rerun writes a byte-identical document.

Excluded: other output formats, chunking for an embedding model, and any write to a description,
sidecar or registry.

## Experimental data

A gitignored tree under `bin/` may hold real CATIA files for operator experiments. Tests, goldens,
spec examples, log fixtures, records and committed docs must not copy names, paths or extracted
strings from that tree; records may report aggregate counts only (files, kinds, how many yielded
components). Tests build synthetic files in `t.TempDir()` with invented names such as
`fixture-part.CATPart` and planted format markers (`V5_CFV2`, `CATOctetArray`, `FINJPL`, the
property keys above, `Manifest.xml`). Those identifiers belong to the published file formats, not
to archive content.

## Exclusions

- No CATIA kernel, CAA, COM, geometry tessellation, or decoding of the embedded preview image
  (a later refinement could extract the V5 `CATPreview` stream as a PNG preview).
- No part number, revision or nomenclature fields: V5 stores them in undocumented structures. They
  may appear under `strings:`.
- No new Go module dependency.
- No cloud upload of the CATIA archive (stage 3 publishes video only).
- No moving CATIA and video in one run; the payload kind is exclusive.

## Acceptance

- Synthetic `.CATPart`, `.CATProduct`, `.CATDrawing`, `.cgr` and `.3dxml` (XML and ZIP) fixtures in
  `t.TempDir()` set `is_catia` and never `is_video`, whatever their content; `._fixture.CATPart`
  AppleDouble is not CATIA; `--video-extensions CATPart` exits 2.
- A V5 fixture with planted markers yields the planted component names sorted and unique, excludes
  its own name, skips malformed chunks and finds names whose separators are interleaved with `;`
  U+0001; a well-formed fixture without markers yields `0 components`.
- Planted `LastSaveVersion` yields `V5R30 SP5`; without it `MinimalVersionToRead` yields `V5R30`;
  without both the token is `unknown`.
- A ZIP `.3dxml` with a `..` member, an oversized member and an external URL reference is read
  within the limits; a broken root member is `text_failed`.
- The sidecar respects the 1 MiB cap and `truncated`; extraction of a large synthetic file keeps
  memory bounded (window and cap, not file size).
- No test or spec file contains strings harvested from the experimental `bin/` tree.
