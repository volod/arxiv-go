# arxgo practical manual: Windows

`arxgo.exe` catalogs a main archive, moves videos to a separate video archive (and, with
`--catia`, CATIA files to a separate CATIA archive), and restores them. Try the workflow on a disposable copy of representative data before using it on an
irreplaceable archive.

## Unpack and configure the Windows bundle

Download the `arxgo-<version>-windows-amd64.zip` bundle for Windows amd64. It contains
`arxgo.exe`, `ffmpeg.exe`, `ffprobe.exe`, `.env.example`, this manual, `LICENSES/` and
`SHA256SUMS`. The tools are already beside `arxgo.exe`; no separate installation or Go
toolchain is needed. The bundle never contains a configured `.env` file.
`LICENSES/GPL-3.0.txt` is the bundled FFmpeg licence,
`LICENSES/FFmpeg-SOURCE.txt` gives build provenance and the FFmpeg 9.0.1 source link, and
`LICENSES/arxgo-MIT.txt` covers arxgo itself.

In PowerShell, open the directory containing the downloaded ZIP, then extract and inspect it:

```powershell
$bundle = (Get-Item .\arxgo-*-windows-amd64.zip).FullName
Expand-Archive -LiteralPath $bundle -DestinationPath .\arxgo-windows
Set-Location .\arxgo-windows
Get-Content .\SHA256SUMS
Get-FileHash .\arxgo.exe -Algorithm SHA256
.\arxgo.exe version
.\arxgo.exe help
```

Compare the displayed SHA-256 hash with the `arxgo.exe` entry in `SHA256SUMS`, and check the
other bundled files the same way before use. For saved settings, copy the template and edit
only the values you need:

```powershell
Copy-Item .\.env.example .\.env
notepad .\.env
```

The bundled `.env.example` explains every setting. For example, put
`ARXGO_ARCHIVE='D:\archive'` and `ARXGO_VIDEO_ARCHIVE='E:\video'` in `.env` if you do not want
to repeat the roots. Keep `.env` private and out of the archive being processed. `arxgo.exe`
reads it from the executable directory; flags override process variables, which override
`.env`. Use unquoted or single-quoted Windows paths, because double-quoted values treat
backslashes as escapes. Relative paths use PowerShell's current directory, not the `.env`
directory. The included `ffmpeg.exe` and `ffprobe.exe` are used for media metadata and previews.
Ordinary scanning, splitting without previews, and restoring do not invoke them.

Use absolute paths to separate roots. `D:\archive` and `E:\video`, or `D:\archive` and
`\\nas\video`, are examples. Neither root may contain the other. `split` may create a
missing video root if its parent already exists; the main archive must exist. A network share
must be accessible under the account running arxgo.

The examples below run from the extracted bundle directory. If you move the executable, keep
`ffmpeg.exe` and `ffprobe.exe` beside it so preview and media options continue to work.

## Catalog the archive

```powershell
.\arxgo.exe scan --archive 'D:\archive'
.\arxgo.exe scan --archive 'D:\archive' --metadata media `
  --large-threshold 500MiB --exclude 'cache/**'
```

`scan` is the default command and writes `D:\archive\arxgo-registry.csv`. It registers
readable files and symlinks, marking binary, media, picture, video, CATIA and large files. Directories
and unreadable or special entries are reported as skipped. Symlinks are recorded but never
followed. `--metadata file` (default) records filesystem metadata and, for MP4, MOV, M4A, M4V
and 3GP, container duration, size and codecs, with no external tool. `--metadata media` adds
the same fields for other audio and video files and requires `ffprobe.exe` at startup. Use
`--registry PATH` to write the CSV elsewhere, `--video-extensions braw,r3d` for ambiguous
video formats, or repeat `--exclude` for multiple relative patterns. CATIA extensions
(`.CATPart`, `.CATProduct`, `.CATDrawing`, `.cgr`, `.3dxml`) cannot appear in `--video-extensions`.
Glob paths use `/` even
on Windows, as in `cache/**`.

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

A scan or split after an earlier registry does not read unchanged files again. A file keeps its
row without being opened when its size and modification time (to the second) match its row and it
was last modified before the previous scan started; everything else is detected. The first scan
after an upgrade, or after changing `--metadata` or `--video-extensions`, detects every file.
`--large-threshold` changes only `is_large`, so it needs no detection. The `scan summary` line and
the run report count the rows taken over as `reused`. A tool that rewrites a file but keeps its
size and modification time is not noticed: run once with `--redetect` to detect every file again.
The registry it writes is the one a scan without the flag would write for an unchanged archive.

## Move videos to a video archive

```powershell
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' --dry-run
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' `
  --metadata media --verify hash
```

`split` scans the main archive. `D:\archive\projects\demo.mp4` becomes
`E:\video\projects\demo.mp4`, and a video description such as
`D:\archive\projects\demo.mp4.md` points to it. Existing unrelated files are preserved;
an occupied description name gets an alternate name. Non-video files remain in the main archive.
Both roots receive `arxgo-videos.csv`. Its columns start with `rel_path`, `file_name`, `status`,
`url` and `description_rel_path`, followed by `file_size`, `sha256`, `transfer`, `run_id`,
`file_mime`, `previews` and the metadata columns. Builds before this column order wrote another
order and run state without a payload: such a registry stops split and restore with exit 5, and an
interrupted run of such a build must be finished by that build before upgrading. If a different
file already occupies the video destination, split skips it, reports a conflict and exits 6.

The default `--transfer auto` uses a no-replace rename only when both roots are on the same
filesystem/device. Two folders on one volume normally meet this condition. Separate volumes
on the same physical drive do not: arxgo copies there, as it does to another drive or a UNC
share. On the copy path it writes a temporary destination, verifies it, places it without
replacement, writes the description, and only then removes the original. `split --transfer copy`
forces that path even within one volume. `--verify size` compares byte counts;
`--verify hash` verifies SHA-256 after copying but needs more I/O.

The default `--min-free 1GiB` means estimated writes must leave at least 1 GiB available on
each write device. Exit 4 means preflight refused to proceed; free space or adjust the
threshold, then rerun. Some network shares cannot report free space; arxgo warns and
continues. `--dry-run` scans and estimates without moving videos or writing registries,
descriptions or previews, though it can write run state, logs and a report under
`D:\archive\.arxgo`.

If you separately host the mirrored video tree at an HTTP(S) address, set for example
`--base-url https://storage.example.com/video`. Descriptions then include a link formed by
appending each video's escaped relative path. arxgo does not upload or check the URL.
Cloud publishing options are not available in current builds.

## Make previews during split

```powershell
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' `
  --sample middle --sample-duration 8s --image series --image-every 5m
```

Samples are short video clips and images are PNG frames beside the descriptions in the main archive.
`--sample` and `--image` independently accept `none` (default), `start`, `middle`, `end`
and `series`. `series` combines spaced video fragments into one sample or creates multiple
frames; `--preview-max-items` caps each series at 100 by default. `--sample-every` controls
fragment spacing and `--image-every` frame spacing. `sd`, `hd` and `4k` are upper size
bounds. `low`, `medium` and `high` set sample quality or PNG compression. Active modes require
working `ffmpeg.exe` and `ffprobe.exe`.

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

```powershell
.\arxgo.exe split --catia --archive 'D:\archive' --catia-archive 'F:\catia' --dry-run
.\arxgo.exe split --catia --archive 'D:\archive' --catia-archive 'F:\catia' --verify hash
.\arxgo.exe split --catia --catia-text --archive 'D:\archive' --catia-archive 'F:\catia'
```

`D:\archive\cad\bracket.CATPart` becomes `F:\catia\cad\bracket.CATPart`, and
`D:\archive\cad\bracket.CATPart.md` describes it with the usual description fields and a `catia:`
summary line such as `catia: CATProduct | V5_CFV2 | V5R30 SP5 | 12 components` (kind, format,
release, number of referenced documents). It never lists names or other text from inside the file.
`--catia-text` writes a second owned file `cad\bracket.CATPart.text.md` whose first line is
`arxgo-text: cad/bracket.CATPart`, with harvested properties, component names and printable strings.
Those strings can include authoring user ids and workstation paths; leave the flag unset unless that
is wanted. Both files name the archive on their second line (`archive: "D:\\archive"`, quoted
because of the backslash), and the sidecar repeats the description's identity fields (`file_name`,
`file_size`, `file_mime`, `sha256`, `modified`, `catia`, `moved_to`, `url`) and names it
(`description: cad/bracket.CATPart.md`), so either file still says what it is about when copied out
of the archive. Files written by earlier releases are not rewritten and lack these lines. If
`<rel_path>.text.md` already holds a file that is not this sidecar, the owned file is
`<rel_path>.arxgo.text.md` (then an indexed name). A failed extraction leaves the CATIA file moved
and exits 6 (`texts_failed`). Rerunning after a successful split moves nothing and writes only
missing sidecars. `--catia-text` without `--catia` exits 2. Videos, the video archive and
`arxgo-videos.csv` are not touched. Both roots get `arxgo-catia.csv`: the same first ten columns as
`arxgo-videos.csv`, then `text_rel_path`, `catia_kind`, `catia_format`, `catia_release`,
`catia_components` and `mtime`, all written on every run. Transfer modes, `--verify`, `--base-url`,
conflicts, `--min-free` and reruns work as for videos. A damaged or unrecognized CATIA file is still
moved; its summary then says `unknown`.

The CATIA archive must not be the main archive, the video archive, or inside either (or contain
them). `--catia` cannot be combined with `--video`, `--video-archive` on the command line,
`--sample` or `--image`; `--catia-archive` without `--catia` is refused too. Each of these exits 2
before anything is written. `ARXGO_VIDEO_ARCHIVE` in `.env` does not affect a CATIA run, and
`ARXGO_CATIA_ARCHIVE` does not affect a video run. `ARXGO_CATIA=true` makes `--catia` the default;
a `--video` or `--catia` flag on the command line overrides it.

## Collect CATIA text for a search index

`--catia-text` leaves one sidecar next to each description. `arxgo catia-index` assembles them into
one Markdown document, reading the ownership the split recorded instead of guessing sidecar names.
Run it while the descriptions and sidecars are still in the archive, that is before a
`restore --descriptions delete`:

```powershell
.\arxgo.exe catia-index --archive 'D:\archive'
.\arxgo.exe catia-index --archive 'D:\archive' --strings --out 'D:\catia-index-with-strings.md'
```

The document (default `D:\archive\arxgo-catia-text.md`, which `scan` never registers) has a header
with the archive, the CATIA archive and counts, then one `## <rel_path>` section per moved CATIA
file: the description's fields (`catia:` summary, size, hash, `moved_to`), the sidecar path,
`properties:` and `components:`. Without `--strings` it is the form worth giving to a language
model; the harvested `strings:` blocks usually make the document tens of times larger. Files whose
sidecar is missing are listed at the end under `## Missing text` with a reason (`not_recorded`:
split never wrote one, rerun `split --catia --catia-text`; `missing`, `foreign`: the recorded file
was deleted or replaced). The command only reads: it starts no run, and a rerun over an unchanged
archive writes the same bytes. It exits 5 while another arxgo run holds the archive lock.

For a search index that wants one document per file, copy the sidecars instead: each already names
its archive and repeats its description's fields. Copy them outside the archive so the next scan
does not register them:

```powershell
$Archive = 'D:\archive'
$Out     = 'D:\catia-metadata'
Get-ChildItem -LiteralPath $Archive -Recurse -Filter *.text.md |
  Where-Object { $_.FullName -notmatch '\\\.arxgo\\' } | ForEach-Object {
    $rel  = $_.FullName.Substring($Archive.Length + 1) -replace '\.text\.md$',''
    $dest = Join-Path $Out "$rel.catia.md"
    New-Item -ItemType Directory -Force -Path (Split-Path $dest) | Out-Null
    Copy-Item -LiteralPath $_.FullName -Destination $dest
  }
```

A sidecar from an earlier release lacks `archive:` and the description fields; for those, write
`@("archive: $Archive") + (Get-Content -LiteralPath (Join-Path $Archive "$rel.md")) +
(Get-Content -LiteralPath $_.FullName | Select-Object -Skip 1)` to `$dest` with
`Set-Content -Encoding utf8NoBOM` instead of the `Copy-Item`. This recipe derives `rel_path` from
the plain name `<rel_path>.text.md`; when a foreign file forced a fallback name
(`<rel_path>.arxgo.text.md` or an indexed name), the `text_rel_path` column of `arxgo-catia.csv`
holds the real path, and `catia-index` lists it under the right file.

When the file registry itself feeds an index, scan with `--metadata media` rather than the default
`--metadata file`: the default fills `media_*` columns only for ISO BMFF containers (MP4, MOV, M4A,
M4V, 3GP), so formats such as MPEG-TS leave them empty until `ffprobe` runs.

## Restore CATIA files

```powershell
.\arxgo.exe restore --catia --archive 'D:\archive' --catia-archive 'F:\catia' --dry-run
.\arxgo.exe restore --catia --archive 'D:\archive' --catia-archive 'F:\catia' --create-dirs --verify hash
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

```powershell
.\arxgo.exe restore --archive 'D:\archive' --video-archive 'E:\video' --dry-run
.\arxgo.exe restore --archive 'D:\archive' --video-archive 'E:\video' `
  --create-dirs --previews delete --verify hash
```

`restore` returns videos to their original relative paths. Missing parent directories cause
skips (exit 6) unless `--create-dirs` is set. Different existing destination files are
skipped unless `--overwrite` is explicitly set. The default `--descriptions delete` removes only
matching arxgo-owned video descriptions; `--descriptions keep` leaves them. The default
`--previews keep` leaves previews; `--previews delete` removes only recorded previews whose
sizes still match. Changed and unrelated files remain. If a restore with `--previews delete` is
interrupted, rerunning the same command also deletes the previews of videos it had already
restored, even when another command (`--new-run`, other options, or a CATIA restore) finished the
interrupted run first. A kept description (`--descriptions keep`) loses the links of deleted previews. By default, both video registries
record restored rows, and complete registries may be renamed with
`.restored-<run-id>` rather than deleted.

With `--transfer auto`, restore renames within a device or copies, verifies and removes the
video-archive source across devices. `restore --transfer copy` instead keeps the video
archive copy, requiring space for all restored videos in the main archive. An auto restore
cleans up empty video-archive directories that held restored videos; it keeps the root and
unrelated directories.

## Interruptions, locks and reports

Each transfer takes a lock in both roots and keeps a write-ahead log under
`<archive>\.arxgo\runs\<run-id>\`. Arxgo logs placement and description/source changes before
considering a video complete. After power loss or interruption, rerun the same command:
unfinished transfers are recovered and the scan resumes from its checkpoint.
`--new-run` recovers unfinished transfers first, then starts a fresh scan. Do not delete
transaction files or source/destination videos while recovery may need them. If a different
command cannot lock the interrupted run's original roots, follow the exit-5 message and
recover using those roots first. A video split or restore recovers an interrupted CATIA run (and a
CATIA run an interrupted video run) by locking the mirror root recorded by that run while it recovers;
after a killed process add `--force-unlock`.

The run state stores the roots it was started with. Moving or remounting a whole archive (for
example a disk that mounts under another path) keeps previews and registry links working, but
interrupted runs must finish first under their original paths.

Only one run may use an archive at a time. Exit 5 can mean a live lock or one needing operator
action. `--force-unlock` takes over only a stale lock from a dead process on this computer;
it does not take over another host's lock on a share. Check that the remote run has stopped
before manually handling that lock. The run's `report.json` summarizes completed and partial
runs; `run.log.jsonl` contains structured logs. Exit 0 means complete, 3 means a required
tool is missing, 4 means insufficient space, and 6 means skipped items or preview failures.
In PowerShell, `$LASTEXITCODE` contains the last process exit code. Inspect the report's
issues and both video registries after exit 6.

For every option and default, run `.\arxgo.exe help scan`, `help split` or `help restore`.
