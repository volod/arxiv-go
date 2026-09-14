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

The [Linux manual](docs/guide/manual-linux.md) and
[Windows manual](docs/guide/manual-windows.md) cover extraction, checksum checks, configuration,
command examples and recovery.

## Project documentation

- [Specification](docs/openspec/spec.md) and [current implementation](docs/impl/current.md)
- [Implementation plan](docs/impl/plan.md) and [development guide](docs/guide/development.md)
- [Agent and contributor rules](AGENTS.md)
