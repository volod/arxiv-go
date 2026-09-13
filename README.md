# [arxiv-go](https://github.com/volod/arxiv-go)

Organize videos in a large archive of files in order to separate video and text data for convenient
storage.

`arxgo(.exe)` is a single static executable for Linux and Windows that:

1. **scan** (default) -- walks an archive and writes a CSV registry of every file, marking binary,
   media, picture, video and large files;
2. **split** -- moves video files into a video archive with the same directory tree (another disk
   or network share), leaves a Markdown metadata stub with a link at each original location, and
   writes a video registry and summary;
3. **restore** -- moves the videos back, optionally recreating deleted directories.

Runs are crash-safe (write-ahead log and checkpoints), resumable and check free disk space before
starting.

Stage 2 adds video samples and PNG frames through `ffmpeg`;
Stage 3 adds Google Drive and SharePoint publishing.

> Status: stage 1 in progress. `scan` writes the resumable file registry (`--metadata media` checks
> for `ffprobe` and exits 3 with download links without it); `split` moves videos transactionally,
> writes Markdown stubs and video registries; `restore` still exits 70.

```bash
arxgo --archive /data/archive
arxgo split --archive /data/archive --video-archive /mnt/nas/video --metadata media
arxgo restore --archive /data/archive --video-archive /mnt/nas/video --create-dirs
```

## Documentation

- [Specification](docs/openspec/spec.md) and [stage tree](docs/openspec/README.md)
- [Implementation plan](docs/impl/plan.md) and [current state](docs/impl/current.md)
- [Development guide](docs/guide/development.md)
- [Agent and contributor rules](AGENTS.md)

## Setup

```bash
make setup        # build bin/arxgo, download ffmpeg/ffprobe, create bin/.env, print usage
```

Settings may come from flags, `ARXGO_*` environment variables, or the optional `bin/.env` file next
to the executable (template: [.env.example](.env.example)), in that order of precedence.

## Build

```bash
make build        # bin/arxgo
make build-all    # static linux/windows amd64 binaries
```

## Develop

```bash
make plan-status  # next eligible task
# implement
make ci           # required checks
```
