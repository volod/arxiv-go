# [arxiv-go](https://github.com/volod/arxiv-go)

`arxgo` is a static Linux and Windows command-line tool for cataloging a file archive, moving
videos to a mirrored archive with Markdown stubs and optional FFmpeg previews, and restoring
them. Runs can recover after interruption. Cloud publishing is not available in current builds.

## Quick start

To build from source on Linux, install Go 1.27+ and GNU Make, then:

```bash
git clone https://github.com/volod/arxiv-go.git
cd arxiv-go
make setup
```

`make setup` builds `bin/arxgo` and `bin/arxgo.exe`, fetches the pinned FFmpeg tools, and
creates an optional `bin/.env` from [.env.example](.env.example). `make build-all` builds just
the two static executables.

The planned distribution command is `make dist`. It will create a Linux tarball and Windows
ZIP in `dist/`, each containing `arxgo`, the matching FFmpeg tools, `.env.example`, a practical
manual, licences and checksums. **`make dist` is not implemented yet**, so no distribution
bundle can be produced from the current checkout.

After a bundle is available, extract the archive for the target host and run its executable
from the extracted directory:

```text
Linux:              ./arxgo scan --archive /path/to/archive
Windows PowerShell: .\arxgo.exe scan --archive 'D:\archive'
```

The [Linux manual](docs/guide/manual-linux.md) and
[Windows manual](docs/guide/manual-windows.md) cover extraction, checksum checks, configuration,
command examples and recovery.

## Project documentation

- [Specification](docs/openspec/spec.md) and [current implementation](docs/impl/current.md)
- [Implementation plan](docs/impl/plan.md) and [development guide](docs/guide/development.md)
- [Agent and contributor rules](AGENTS.md)
