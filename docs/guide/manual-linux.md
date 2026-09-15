# arxgo practical manual: Linux

`arxgo` catalogs a main archive, moves its videos to a separate video archive, and restores
them. Try the workflow on a disposable copy of representative data before using it on an
irreplaceable archive.

## Unpack and configure the Linux bundle

Download the `arxgo-<version>-linux-amd64.tar.gz` bundle for Linux amd64. It contains `arxgo`,
`ffmpeg`, `ffprobe`, `.env.example`, this manual, `LICENSES/` and `SHA256SUMS`. The tools are
already beside `arxgo`; no separate installation or Go toolchain is needed. The bundle never
contains a configured `.env` file.
`LICENSES/GPL-3.0.txt` is the bundled FFmpeg licence,
`LICENSES/FFmpeg-SOURCE.txt` gives build provenance and the FFmpeg 9.0.1 source link, and
`LICENSES/arxgo-MIT.txt` covers arxgo itself.

From the directory containing the downloaded archive, extract and verify it:

```bash
mkdir arxgo-linux
tar -xzf arxgo-*-linux-amd64.tar.gz -C arxgo-linux
cd arxgo-linux
sha256sum -c SHA256SUMS
./arxgo version
./arxgo help
```

If you prefer saved settings, copy the template beside the executable and edit only the values
you need:

```bash
cp .env.example .env
chmod 600 .env
```

The bundled `.env.example` explains every setting. For example, set
`ARXGO_ARCHIVE=/data/archive` and `ARXGO_VIDEO_ARCHIVE=/mnt/video` in `.env` if you do not want
to repeat the roots. Keep `.env` private and do not put it in the archive being processed.
`arxgo` reads `.env` from its executable directory; flags override process variables, which
override `.env`. Paths in `.env` are resolved from the working directory. The included
`ffmpeg` and `ffprobe` are used for media metadata and previews. Ordinary scanning, splitting
without previews, and restoring do not invoke them.

Use absolute archive paths. The main and video roots must be separate, neither nested in the
other. For example, `/data/archive` and `/mnt/video` are valid if `/mnt/video` is not inside
`/data/archive`. `split` can create a missing video root when its parent exists. The source
archive must already exist.

The examples below run from the extracted bundle directory. If you move the executable, keep
`ffmpeg` and `ffprobe` beside it so preview and media options continue to work.

## Catalog the archive

```bash
./arxgo scan --archive /data/archive
./arxgo scan --archive /data/archive --metadata media \
  --large-threshold 500MiB --exclude 'cache/**'
```

`scan` is also the default command. It writes `/data/archive/arxgo-registry.csv` with one row
per readable regular file and symlink. It marks binary, media, picture, video and large files.
Directories and unreadable or special entries are reported as skipped rather than registered.
Symlinks are recorded, never followed. `--metadata file` (the default) records filesystem
metadata and, for MP4, MOV, M4A, M4V and 3GP files, container duration, size and codecs,
without an external tool. `--metadata media` also records those fields for other audio and
video files and requires `ffprobe` at startup. An unreadable or malformed media file can
still have a row with a media error. Use `--registry PATH` to write the file
elsewhere and `--video-extensions braw,r3d` when a format lacks a recognizable signature.
`--exclude` globs are relative to the archive root; repeat the flag for multiple patterns.

## Move videos to a video archive

Check the plan first, then run the transfer:

```bash
./arxgo split --archive /data/archive --video-archive /mnt/video --dry-run
./arxgo split --archive /data/archive --video-archive /mnt/video \
  --metadata media --verify hash
```

`split` scans the main archive and moves video candidates to matching relative paths under
`/mnt/video`. A source such as `/data/archive/projects/demo.mp4` becomes
`/mnt/video/projects/demo.mp4`. In the main archive, an arxgo-owned video description such as
`projects/demo.mp4.md` points to it. Existing unrelated files are preserved; if the usual description
name is occupied, arxgo chooses an alternate name. The main archive retains non-video files and
optional previews. Both roots get `arxgo-videos.csv`. `split` never
overwrites a different video already at the destination: it reports a conflict and exits 6.

With the default `--transfer auto`, roots on the same filesystem/device use a no-replace
rename. That is a move of the directory entry, so no second full video copy is needed. Two
paths on the same physical disk can still be on different partitions or filesystems; they then
use the copy path. A different disk or network share also uses the copy path: arxgo writes a
temporary destination file, verifies it, places it without replacement, writes the description, then
removes the source. `--transfer copy` forces this path even on one device. During that path,
space for a temporary second copy is needed. `--verify size` compares sizes; `--verify hash`
also verifies SHA-256 and reads more data.

`--min-free 1GiB` is the default: estimated writes must leave at least that much free on each
write device. Exit 4 means the preflight refused the operation; free space or adjust this
threshold and rerun. Some network shares report unknown space, in which case arxgo warns and
continues. `--dry-run` scans and estimates without moving videos or writing registries, descriptions
or previews, but it can write run state, a log and report under `/data/archive/.arxgo`.

If the video archive has a separately hosted HTTP(S) address, add for example
`--base-url https://storage.example.com/video`. Each description then includes that URL plus the
escaped video-relative path. `arxgo` does not upload files or test that the URL works; set it
only when the same tree is actually reachable at that address.

## Make previews during split

```bash
./arxgo split --archive /data/archive --video-archive /mnt/video \
  --sample middle --sample-duration 8s --image series --image-every 5m
```

Samples are short video clips; images are PNG frames. Both are stored beside the description in the
main archive and linked from it. Each option accepts `none` (default), `start`, `middle`, `end`
or `series`. `series` makes one combined sample from spaced fragments or multiple frames;
`--preview-max-items` caps each series at 100 by default. `--sample-every` controls fragment
spacing, while `--image-every` controls frame spacing. `sd`, `hd` and `4k` are upper resolution
bounds; `low`, `medium` and `high` select quality. Active preview modes require working
`ffmpeg` and `ffprobe`, discovered beside `arxgo` or on `PATH`.

Samples keep the source container when it holds H.264/AAC (MP4, M4V, MOV, 3GP, 3G2, F4V, FLV, MKV,
TS, M2TS, MTS), WebM uses VP9/Opus and AVI MPEG-4/MP3. Other sources, such as MPEG-PS `.mpg`,
`.vob`, `.ogv`, `.wmv`, `.mxf` or GoPro `.LRV`, get an `.mp4` sample and a warning. Only the first
video and audio streams are encoded. Metadata tracks that cameras and phones add (GoPro telemetry
`gpmd`, GoPro SOS `fdsc`, timecode, Apple metadata) stay untouched in the moved video; a desktop
player that asks for `meta/x-gst-fourcc-gpmd` or similar "codecs" is reporting those tracks, not a
problem arxgo needs to solve. Rotated phone videos produce upright previews. Samples use the first
audio track that the bundled FFmpeg 9.0.1 can decode. Apple spatial audio (`apac`) decodes only on
Apple systems: iPhone videos also carry a normal AAC track that samples use, and a video with no
decodable audio gets a silent sample and a warning. A source without a video stream gets no
previews and a warning.

Previews are generated from the moved video after its transfer commits. A failed preview leaves the
video moved, is counted as `previews_failed` in `report.json` and makes the run exit 6; rerun
`split` with the same preview options to create
the missing ones. A rerun never regenerates a preview that still has its recorded size, and it
keeps an existing preview name even when you change modes or quality; delete a preview file to
have the next split regenerate it with the new options. Recorded previews and their
temporary `*.arxgo-part.*` files are never treated as videos by later scans. macOS `._<name>`
sidecar files are recognized as metadata, not videos.

## Restore videos

```bash
./arxgo restore --archive /data/archive --video-archive /mnt/video --dry-run
./arxgo restore --archive /data/archive --video-archive /mnt/video \
  --create-dirs --previews delete --verify hash
```

`restore` scans the video archive and returns videos to their original relative paths. If a
parent directory in the main archive was removed, the default skips that video (exit 6);
`--create-dirs` recreates it. A different file already at the destination is skipped unless
you explicitly use `--overwrite`. The default `--descriptions delete` removes only matching
arxgo-owned descriptions; `--descriptions keep` preserves them. The default `--previews keep` leaves
previews; `--previews delete` removes only recorded previews whose sizes still match, preserving
changed or unrelated files. If a restore with `--previews delete` is interrupted, rerunning the same
command also deletes the previews of videos it had already restored. A kept description (`--descriptions keep`)
loses the links of deleted previews. The default updates both video registries to show restored rows;
completed registries may be renamed with `.restored-<run-id>` rather than deleted.

Restore's default `--transfer auto` renames on one device or copies, verifies and removes the
video-archive source across devices. In contrast, `restore --transfer copy` keeps the
video-archive copy; allow enough space for all restored videos. Empty video-archive directories
created for moved videos are cleaned up after an auto restore, while the root and unrelated
directories stay.

## Interruptions, locks and reports

Each real transfer takes locks in both roots and writes a durable transaction log under
`<archive>/.arxgo/runs/<run-id>/`. A transaction records placement and description/source changes
before it is considered complete. If power loss or Ctrl+C interrupts a run, rerun the same
command to recover unfinished transfers and resume at its checkpoint. `--new-run` recovers
unfinished transfers first, then scans anew. Do not edit the WAL or remove a video that recovery
may need. If options changed or a new command cannot lock the interrupted run's original roots,
follow its exit-5 message and rerun against those roots first.

The run state stores the roots it was started with. Moving or remounting a whole archive (for
example a disk that mounts under another path) keeps previews and registry links working, but
interrupted runs must finish first under their original paths.

Only one run may use an archive at a time. Exit 5 can mean a live lock or one requiring operator
action. `--force-unlock` is for a stale lock from a dead process on this host; it will not take
over a lock owned by another host. Check that a remote run has stopped before manually handling
its lock. Successful and partial runs write `report.json` in the run directory; `run.log.jsonl`
holds the structured log. Exit 0 means complete, 3 means a required tool is missing, 4 means
insufficient space, 6 means some items were skipped or previews failed, and 130 means interrupted.
For exit 6, inspect the report's issues and the registries before rerunning.

For all options and defaults, run `./arxgo help scan`, `help split` or `help restore`.
