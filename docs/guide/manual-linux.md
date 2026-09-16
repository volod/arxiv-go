# arxgo practical manual: Linux

`arxgo` catalogs a main archive, moves its videos to a separate video archive (and, with
`--catia`, its CATIA files to a separate CATIA archive), and restores them. Try the workflow on a disposable copy of representative data before using it on an
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
per readable regular file and symlink. It marks binary, media, picture, video, CATIA and large files.
Directories and unreadable or special entries are reported as skipped rather than registered.
Symlinks are recorded, never followed. `--metadata file` (the default) records filesystem
metadata and, for MP4, MOV, M4A, M4V and 3GP files, container duration, size and codecs,
without an external tool. `--metadata media` also records those fields for other audio and
video files and requires `ffprobe` at startup. An unreadable or malformed media file can
still have a row with a media error. Use `--registry PATH` to write the file
elsewhere and `--video-extensions braw,r3d` when a format lacks a recognizable signature.
CATIA extensions (`.CATPart`, `.CATProduct`, `.CATDrawing`, `.cgr`, `.3dxml`) cannot appear in
`--video-extensions`. `--exclude` globs are relative to the archive root; repeat the flag for
multiple patterns.

Every CSV arxgo writes (`arxgo-registry.csv`, `arxgo-videos.csv`, `arxgo-catia.csv`) has all
of its columns on every run of every command, in a fixed order; a column that no row fills is
written with empty cells. A spreadsheet, database or search index therefore sees one schema per
file, whatever the archive holds. A registry written by an earlier build that left out empty
columns is still read, and the next run writes it with every column.

The file registry describes the archive you curated, not only what is currently under its root.
After `split`, each moved video or CATIA file keeps its row, unchanged except for column 12,
`location`: `video-archive` or `catia-archive` instead of `archive`. Descriptions, previews and
text sidecars arxgo wrote never get a row. A later scan adds rows only for files you added, and
drops rows only for files you deleted from the archive yourself. `restore --registry-update` sets
`location` back to `archive`. When a run finds nothing to change, it leaves the CSV untouched, so
its modification time shows when the archive last changed. arxgo keeps a stamp of the last
registry in `.arxgo/registry.json`. If the registry and the stamp are lost, the next scan rebuilds
the moved rows from the video and CATIA archive copies. A copy it cannot read gives a row rebuilt
from the run history and a `registry-row-reconstructed` warning. A registry from an earlier build
without `location` is read as if every file were in the archive. Restore's retired copies,
`arxgo-videos.restored-<run-id>.csv` and `arxgo-catia.restored-<run-id>.csv`, are arxgo files and
have no row.

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
optional previews. Both roots get `arxgo-videos.csv`, whose columns start with `rel_path`,
`file_name`, `status`, `url` and `description_rel_path`, followed by `file_size`, `sha256`,
`transfer`, `run_id`, `file_mime`, `previews` and the metadata columns. Builds before this column
order wrote another order and run state without a payload: such a registry stops split and restore
with exit 5, and an interrupted run of such a build must be finished by that build before upgrading.
`split` never overwrites a different video already at the destination: it reports a conflict and
exits 6.

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

## Move CATIA files to a CATIA archive

CATIA files (`.CATPart`, `.CATProduct`, `.CATDrawing`, `.cgr`, `.3dxml`) move to their own
archive with `--catia`. It is a separate run from the video split; one run moves one payload.

```bash
./arxgo split --catia --archive /data/archive --catia-archive /mnt/catia --dry-run
./arxgo split --catia --archive /data/archive --catia-archive /mnt/catia --verify hash
./arxgo split --catia --catia-text --archive /data/archive --catia-archive /mnt/catia
```

`/data/archive/cad/bracket.CATPart` becomes `/mnt/catia/cad/bracket.CATPart`, and
`cad/bracket.CATPart.md` in the main archive describes it with the usual description fields and a
`catia:` summary line such as `catia: CATProduct | V5_CFV2 | V5R30 SP5 | 12 components` (kind,
format, release, number of referenced documents). It never lists names or other text from inside
the file. `--catia-text` writes a second owned file `cad/bracket.CATPart.text.md` whose first line
is `arxgo-text: cad/bracket.CATPart`, with harvested properties, component names and printable
strings. Those strings can include authoring user ids and workstation paths; leave the flag unset
unless that is wanted. If `<rel_path>.text.md` already holds a file that is not this sidecar, the
owned file is `<rel_path>.arxgo.text.md` (then an indexed name). A failed extraction leaves the
CATIA file moved and exits 6 (`texts_failed`). Rerunning after a successful split moves nothing
and writes only missing sidecars. `--catia-text` without `--catia` exits 2. Videos, the video
archive and `arxgo-videos.csv` are not touched. Both roots get `arxgo-catia.csv`: the same first
ten columns as `arxgo-videos.csv`, then `text_rel_path`, `catia_kind`, `catia_format`,
`catia_release`, `catia_components` and `mtime`, all written on every run.
Transfer modes, `--verify`, `--base-url`, conflicts, `--min-free` and reruns work as for videos.
A damaged or unrecognized CATIA file is still moved; its summary then says `unknown`.

The CATIA archive must not be the main archive, the video archive, or inside either (or contain
them). `--catia` cannot be combined with `--video`, `--video-archive` on the command line,
`--sample` or `--image`; `--catia-archive` without `--catia` is refused too. Each of these exits 2
before anything is written. `ARXGO_VIDEO_ARCHIVE` in `.env` does not affect a CATIA run, and
`ARXGO_CATIA_ARCHIVE` does not affect a video run. `ARXGO_CATIA=true` makes `--catia` the default;
a `--video` or `--catia` flag on the command line overrides it.

## Collect CATIA text for a search index

`--catia-text` leaves one sidecar next to each description. To feed them to a search engine, a
vector database or a language model, assemble them first. These recipes read only; run them while
the descriptions and sidecars are still in the archive, that is before a `restore --descriptions
delete`. Set `ARCHIVE` to the archive root; it is the one thing the files themselves do not name.

One self-contained document per CATIA file, written outside the archive so the next scan does not
register them:

```bash
ARCHIVE=/data/archive
OUT=/data/catia-metadata
cd "$ARCHIVE"
find . -name '*.text.md' -not -path './.arxgo/*' -print0 |
while IFS= read -r -d '' text; do
  rel=${text#./}; rel=${rel%.text.md}
  mkdir -p "$OUT/$(dirname "$rel")"
  { printf 'archive: %s\n' "$ARCHIVE"; cat "$rel.md"; tail -n +2 "$text"; } > "$OUT/$rel.catia.md"
done
```

One aggregated assembly index: each file's `catia:` summary line and its component list, without
the harvested `strings:` blocks. This is the form worth giving to a language model; on an archive
of 2255 CATIA files it is under 1 MB, while the same index with strings is about 65 MB.

```bash
cd "$ARCHIVE"
{ printf '# CATIA assembly index\n\narchive: %s\n' "$ARCHIVE"
  find . -name '*.text.md' -not -path './.arxgo/*' -print0 | LC_ALL=C sort -z |
  while IFS= read -r -d '' text; do
    rel=${text#./}; rel=${rel%.text.md}
    printf '\n## %s\n\n' "$rel"
    grep -m1 '^catia: ' "$rel.md"
    sed -n '/^components:$/,/^strings:$/{/^strings:$/d;p;}' "$text"
  done
} > /data/catia-assembly-index.md
```

Drop the `grep`/`sed` pair and use `tail -n +2 "$rel.md"; tail -n +2 "$text"` instead to keep every
field and every harvested string.

Both recipes assume the plain names `<rel_path>.md` and `<rel_path>.text.md`. When a foreign file
forced a fallback name (`<rel_path>.arxgo.text.md` or an indexed name), the split logged it and the
`text_rel_path` column of `arxgo-catia.csv` holds the real path; those files need the column rather
than the name pattern. A supported `arxgo catia-index` operation that reads the recorded ownership
instead of guessing names is
[specified](../openspec/stage-4-catia/catia.md#text-index) and not yet built.

When the file registry itself feeds an index, scan with `--metadata media` rather than the default
`--metadata file`: the default fills `media_*` columns only for ISO BMFF containers (MP4, MOV, M4A,
M4V, 3GP), so formats such as MPEG-TS leave them empty until `ffprobe` runs.

## Restore CATIA files

```bash
./arxgo restore --catia --archive /data/archive --catia-archive /mnt/catia --dry-run
./arxgo restore --catia --archive /data/archive --catia-archive /mnt/catia --create-dirs --verify hash
```

`restore --catia` scans the CATIA archive and returns CATIA files (and any other file
`arxgo-catia.csv` records as moved) to their original relative paths, with the same
`--create-dirs`, `--overwrite`, `--transfer`, `--verify` and empty-directory cleanup rules as
video restore. The default `--descriptions delete` removes the arxgo-owned `.md` description and the
owned `.text.md` sidecar (first line `arxgo-text: <rel_path>`) of each restored file; a foreign file
at either name, or a sidecar you edited, is kept (an edited sidecar is reported, exit 6).
`--descriptions keep` leaves both. Both `arxgo-catia.csv` copies mark restored rows; after the last
moved row is restored with `--descriptions delete`, both are renamed to
`arxgo-catia.restored-<run-id>.csv`. Videos, the video archive and `arxgo-videos.csv` are not
touched. `--previews delete`, `--catia-text`, `--video-archive` on the command line and
`--catia-archive` without `--catia` exit 2 before anything is written. If an interrupted CATIA
restore that deleted descriptions was finished by another command (for example a video restore),
the next `restore --catia` with `--descriptions delete` deletes the sidecars that run left.

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
command also deletes the previews of videos it had already restored, even when another command
(`--new-run`, other options, or a CATIA restore) finished the interrupted run first. A kept description (`--descriptions keep`)
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
follow its exit-5 message and rerun against those roots first. A video split or restore
recovers an interrupted CATIA run (and a CATIA run an interrupted video run) by locking the
mirror root recorded by that run while it recovers; after a killed process add `--force-unlock`.

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
