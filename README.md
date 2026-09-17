# [arxiv-go](https://github.com/volod/arxiv-go)

`arxgo` is a command-line tool for cataloging a file archive, moving videos to a mirrored archive
with video descriptions and optional FFmpeg previews, and restoring them.
It does the same for CATIA CAD files with `--catia`, into a separate CATIA archive, and can
extract their accessible text into searchable Markdown sidecars.

## Quick start

Download a release bundle, extract it and run the executable from the extracted directory. Each
release `v<version>` on the [releases page](https://github.com/volod/arxiv-go/releases) has a Linux
bundle `arxgo-<version>-linux-amd64.tar.gz`, a Windows bundle `arxgo-<version>-windows-amd64.zip`
and `SHA256SUMS` for both. A bundle holds the static executable, the pinned FFmpeg 9.0.1 `ffmpeg`
and `ffprobe`, `.env.example`, the manual, licences and its own `SHA256SUMS`; nothing else needs to
be installed.

### Linux

```bash
# 1. Download the bundle and the release checksums
VERSION=0.2.0
BASE=https://github.com/volod/arxiv-go/releases/download/v$VERSION
curl -fLO "$BASE/arxgo-$VERSION-linux-amd64.tar.gz"
curl -fLO "$BASE/SHA256SUMS"
sha256sum -c --ignore-missing SHA256SUMS

# 2. Extract it and check its files
mkdir "arxgo-$VERSION"
tar -xzf "arxgo-$VERSION-linux-amd64.tar.gz" -C "arxgo-$VERSION"
cd "arxgo-$VERSION"
sha256sum -c SHA256SUMS

# 3. Run
./arxgo version
./arxgo scan --archive /data/archive
./arxgo split --archive /data/archive --video-archive /mnt/video --dry-run
./arxgo split --archive /data/archive --video-archive /mnt/video
./arxgo restore --archive /data/archive --video-archive /mnt/video
```

### Windows (PowerShell)

```powershell
# 1. Download the bundle and the release checksums
$Version = '0.2.0'
$Base = "https://github.com/volod/arxiv-go/releases/download/v$Version"
$Zip = "arxgo-$Version-windows-amd64.zip"
$ProgressPreference = 'SilentlyContinue'
Invoke-WebRequest "$Base/$Zip" -OutFile $Zip
Invoke-WebRequest "$Base/SHA256SUMS" -OutFile SHA256SUMS
$Expected = ((Select-String -Path .\SHA256SUMS -SimpleMatch $Zip).Line -split '\s+')[0]
if ((Get-FileHash $Zip -Algorithm SHA256).Hash -ne $Expected) { throw "checksum mismatch: $Zip" }

# 2. Extract it
Expand-Archive -LiteralPath $Zip -DestinationPath "arxgo-$Version"
Set-Location "arxgo-$Version"

# 3. Run
.\arxgo.exe version
.\arxgo.exe scan --archive 'D:\archive'
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' --dry-run
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video'
.\arxgo.exe restore --archive 'D:\archive' --video-archive 'E:\video'
```

Set `VERSION` to the release you want. If the repository requires signing in, download the two
files from the releases page in a browser, or with
`gh release download v<version> -R volod/arxiv-go`.

`scan` writes the file registry `arxgo-registry.csv` into the archive. `split --dry-run` shows
which videos would move; `split` moves them into the video archive and leaves a Markdown
description at each original location; `restore` moves them back. The main and video archives must
be separate directories, neither inside the other. Try the workflow on a disposable copy first.

CATIA files move in their own run, into their own archive, and `--catia-text` writes a searchable
`<file>.text.md` next to each description, with the file's product properties, referenced
documents and the notes written on its drawings. `catia-index` gathers them into one Markdown
document, `arxgo-catia-text.md`, for a search engine, a vector database or a language model:

```text
./arxgo split --catia --catia-text --archive /data/archive --catia-archive /mnt/catia
./arxgo catia-index --archive /data/archive
./arxgo restore --catia --archive /data/archive --catia-archive /mnt/catia
```

The [Linux manual](docs/guide/manual-linux.md) and [Windows manual](docs/guide/manual-windows.md)
cover download and checksum checks, building from source (including installing Go), settings,
every operation, previews, CATIA text and recovery. To build from a clone on Linux, install Go 1.27+
and GNU Make and run `make setup`; `make dist` builds the release bundles
([development guide](docs/guide/development.md)).

## Project documentation

- [Specification](docs/openspec/spec.md) and [current implementation](docs/impl/current.md)
- [Implementation plan](docs/impl/plan.md) and [development guide](docs/guide/development.md)
- [Agent and contributor rules](AGENTS.md)
