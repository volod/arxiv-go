# CATIA Archive

Accepted work: [0043 CATIA classification](../records/0043-catia-implement-catia-classification.md).
Specification: [CATIA files](../../openspec/stage-4-catia/catia.md);
[split and restore](../../openspec/stage-4-catia/split-restore.md) is specified and not yet
implemented. The capability remains planned until those tasks ship.

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
the same way as `arxgo-videos.csv`. Split and restore of CATIA files are not available yet.
