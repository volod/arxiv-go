# arxgo practical manual: Windows

`arxgo.exe` catalogs a main archive, moves videos to a separate video archive, and restores
them. Try the workflow on a disposable copy of representative data before using it on an
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
readable files and symlinks, marking binary, media, picture, video and large files. Directories
and unreadable or special entries are reported as skipped. Symlinks are recorded but never
followed. `--metadata file` (default) needs no external tool; `--metadata media` adds
container, duration and stream details and requires `ffprobe.exe` at startup. Use
`--registry PATH` to write the CSV elsewhere, `--video-extensions braw,r3d` for ambiguous
video formats, or repeat `--exclude` for multiple relative patterns. Glob paths use `/` even
on Windows, as in `cache/**`.

## Move videos to a video archive

```powershell
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' --dry-run
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' `
  --metadata media --verify hash
```

`split` scans the main archive. `D:\archive\projects\demo.mp4` becomes
`E:\video\projects\demo.mp4`, and a Markdown stub such as
`D:\archive\projects\demo.mp4.md` points to it. Existing unrelated files are preserved;
an occupied stub name gets an alternate name. Non-video files remain in the main archive.
Both roots receive `arxgo-videos.csv` and `arxgo-videos.md`. If a different file already
occupies the video destination, split skips it, reports a conflict and exits 6.

The default `--transfer auto` uses a no-replace rename only when both roots are on the same
filesystem/device. Two folders on one volume normally meet this condition. Separate volumes
on the same physical drive do not: arxgo copies there, as it does to another drive or a UNC
share. On the copy path it writes a temporary destination, verifies it, places it without
replacement, writes the stub, and only then removes the original. `split --transfer copy`
forces that path even within one volume. `--verify size` compares byte counts;
`--verify hash` verifies SHA-256 after copying but needs more I/O.

The default `--min-free 1GiB` means estimated writes must leave at least 1 GiB available on
each write device. Exit 4 means preflight refused to proceed; free space or adjust the
threshold, then rerun. Some network shares cannot report free space; arxgo warns and
continues. `--dry-run` scans and estimates without moving videos or writing registries,
stubs or previews, though it can write run state, logs and a report under
`D:\archive\.arxgo`.

If you separately host the mirrored video tree at an HTTP(S) address, set for example
`--base-url https://storage.example.com/video`. Stubs then include a link formed by
appending each video's escaped relative path. arxgo does not upload or check the URL.
Cloud publishing options are not available in current builds.

## Make previews during split

```powershell
.\arxgo.exe split --archive 'D:\archive' --video-archive 'E:\video' `
  --sample middle --sample-duration 8s --image series --image-every 5m
```

Samples are short video clips and images are PNG frames beside the stubs in the main archive.
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
video moved, is counted as `previews_failed` in `report.json`, listed under "Preview failures" in
`arxgo-videos.md`, and makes the run exit 6; rerun `split` with the same preview options to create
the missing ones. A rerun never regenerates a preview that still has its recorded size, and it
keeps an existing preview name even when you change modes or quality; delete a preview file to
have the next split regenerate it with the new options. Recorded previews and their
temporary `*.arxgo-part.*` files are never treated as videos by later scans. macOS `._<name>`
sidecar files are recognized as metadata, not videos.

## Restore videos

```powershell
.\arxgo.exe restore --archive 'D:\archive' --video-archive 'E:\video' --dry-run
.\arxgo.exe restore --archive 'D:\archive' --video-archive 'E:\video' `
  --create-dirs --previews delete --verify hash
```

`restore` returns videos to their original relative paths. Missing parent directories cause
skips (exit 6) unless `--create-dirs` is set. Different existing destination files are
skipped unless `--overwrite` is explicitly set. The default `--stubs delete` removes only
matching arxgo-owned Markdown stubs; `--stubs keep` leaves them. The default
`--previews keep` leaves previews; `--previews delete` removes only recorded previews whose
sizes still match. Changed and unrelated files remain. If a restore with `--previews delete` is
interrupted, rerunning the same command also deletes the previews of videos it had already
restored. A kept stub (`--stubs keep`) loses the links of deleted previews. By default, both video registries
record restored rows, and complete registries may be renamed with
`.restored-<run-id>` rather than deleted.

With `--transfer auto`, restore renames within a device or copies, verifies and removes the
video-archive source across devices. `restore --transfer copy` instead keeps the video
archive copy, requiring space for all restored videos in the main archive. An auto restore
cleans up empty video-archive directories that held restored videos; it keeps the root and
unrelated directories.

## Interruptions, locks and reports

Each transfer takes a lock in both roots and keeps a write-ahead log under
`<archive>\.arxgo\runs\<run-id>\`. Arxgo logs placement and stub/source changes before
considering a video complete. After power loss or interruption, rerun the same command:
unfinished transfers are recovered and the scan resumes from its checkpoint.
`--new-run` recovers unfinished transfers first, then starts a fresh scan. Do not delete
transaction files or source/destination videos while recovery may need them. If a different
command cannot lock the interrupted run's original roots, follow the exit-5 message and
recover using those roots first.

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
