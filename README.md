# [arxiv-go](https://github.com/volod/arxiv-go)

`arxgo` is a static Linux and Windows command-line tool for cataloging a file archive, moving
videos to a mirrored archive with video descriptions and optional FFmpeg previews, and restoring
them. It does the same for CATIA CAD files with `--catia`, into a separate CATIA archive, and can
extract their accessible text into searchable Markdown sidecars. Runs can recover after
interruption. Every CSV it writes keeps the same full set of columns from run to run, so a
spreadsheet, database or search index loads it with one schema. Cloud publishing is not
available in current builds.

## Quick start

To build from source on Linux, install Go 1.27+ and GNU Make, then:

```bash
git clone https://github.com/volod/arxiv-go.git
cd arxiv-go
make setup
```

`make setup` builds `bin/arxgo` and `bin/arxgo.exe`, fetches the pinned FFmpeg tools, and
creates an optional `bin/.env` from [.env.example](.env.example). `make build-all` builds 
the two static executables for amd64 Linux and Windows.

To create distribution bundles, run `make dist`. It writes a Linux tarball and a Windows ZIP to
`dist/`, each containing the static executable, the matching pinned FFmpeg 9.0.1 tools,
`.env.example`, the practical manual, licences and checksums (see the
[development guide](docs/guide/development.md) for release details).

Copy the bundle for the target host, extract it and run the executable from the extracted
directory:

```text
Linux:              ./arxgo split --archive /data/archive --video-archive /mnt/video --dry-run
Windows PowerShell: .\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' --dry-run
```

CATIA files move in their own run, into their own archive, and `--catia-text` writes a searchable
`<file>.text.md` next to each description:

```text
./arxgo split --catia --catia-text --archive /data/archive --catia-archive /mnt/catia
./arxgo restore --catia --archive /data/archive --catia-archive /mnt/catia
```

Both manuals show how to assemble those sidecars into one document for a search engine, a vector
database or a language model.

The [Linux manual](docs/guide/manual-linux.md) and
[Windows manual](docs/guide/manual-windows.md) cover extraction, checksum checks, configuration,
command examples and recovery.

## Project documentation

- [Specification](docs/openspec/spec.md) and [current implementation](docs/impl/current.md)
- [Implementation plan](docs/impl/plan.md) and [development guide](docs/guide/development.md)
- [Agent and contributor rules](AGENTS.md)
