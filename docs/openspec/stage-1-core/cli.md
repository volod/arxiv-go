# CLI contract

Owner: `project-foundation`. Stage-2 and stage-3 flags are listed so the parser reserves their
names; until their capability ships, using them exits 2 with `option not available in this build`.

## Synopsis

```text
arxgo [scan]  --archive PATH [common flags] [scan flags]
arxgo split   --archive PATH --video-archive PATH [common flags] [scan flags] [split flags]
arxgo restore --archive PATH --video-archive PATH [common flags] [restore flags]
arxgo version
arxgo help [operation]
```

The operation name `publish` is reserved for stage 3.

The operation is the first non-flag argument; when absent, the operation is `scan`. Flags use the
standard library `flag` syntax (`--name value` or `--name=value`). Every flag may also be given as
an environment variable `ARXGO_<NAME>` with dashes replaced by underscores; command-line values win.

## Common flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--archive PATH` | required | Root of the main archive |
| `--video-archive PATH` | required for split/restore | Root of the video archive; may be on another drive or a mounted/UNC network share |
| `--log-level LEVEL` | `info` | `debug`, `info`, `warn`, `error` |
| `--log-format FORMAT` | `text` | Console format `text` or `json`; the run log file is always JSON lines |
| `--progress-interval DURATION` | `10s` | Minimum interval between progress lines |
| `--checkpoint-every N` | `500` | Checkpoint after this many processed files ... |
| `--checkpoint-interval DURATION` | `30s` | ... or after this much time, whichever comes first |
| `--dry-run` | `false` | Run validation, scan and preflight; print the plan; mutate nothing except the run log |
| `--min-free SIZE` | `1GiB` | Free space that must remain on every written device after the operation |
| `--new-run` | `false` | Ignore an incomplete previous run after recovering it, and start a fresh scan |
| `--force-unlock` | `false` | Take over a lock whose owner process is not alive (see [lock](integrity.md#run-lock)) |

Sizes accept `B`, `KB`, `MB`, `GB`, `TB` (powers of 1000) and `KiB`, `MiB`, `GiB`, `TiB` (powers of
1024); a bare number is bytes.

## Scan flags

Used by `scan`, and by `split` for its scan phase.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--large-threshold SIZE` | `1GiB` | Files with `file_size >= SIZE` get `is_large=true` |
| `--registry PATH` | `<archive>/arxgo-registry.csv` | CSV registry output path |
| `--metadata MODE` | `file` | `file`: file-system metadata only. `media`: also container/stream metadata for media files (see [metadata](metadata.md)) |
| `--exclude GLOB` | none, repeatable | Relative-path glob (`path.Match` per segment, `**` for any depth) excluded from traversal |
| `--follow-symlinks` | `false` | Reserved; symlinks are recorded but never followed in stage 1 |

## Split flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--transfer MODE` | `auto` | `auto`: rename on the same device, copy+verify+delete otherwise. `copy`: always copy+verify+delete |
| `--verify MODE` | `size` | `size` or `hash` (SHA-256 computed while copying and re-read from the destination) |
| `--base-url URL` | none | Base URL of the cloud location the video archive will be uploaded to; stubs link to `URL/<rel_path>` |
| `--video-extensions LIST` | none | Extra comma-separated extensions treated as video when signature detection is inconclusive |
| `--sample MODE` | `none` | Stage 2. `none`, `start`, `middle`, `end`, `series` |
| `--sample-duration DURATION` | `5s` | Stage 2. Clip length, or fragment length for `series` |
| `--sample-every DURATION` | `5m` | Stage 2. Fragment/frame spacing for `series` |
| `--sample-resolution RES` | `sd` | Stage 2. `sd` (640x360), `hd` (1920x1080), `4k` (3840x2160) upper bound |
| `--sample-quality Q` | `medium` | Stage 2. `low`, `medium`, `high` |
| `--image MODE` | `none` | Stage 2. `none`, `start`, `middle`, `end`, `series` |
| `--image-every DURATION` | `5m` | Stage 2. Frame spacing for `series` |
| `--image-resolution RES` | `sd` | Stage 2. Same values as `--sample-resolution` |
| `--image-quality Q` | `medium` | Stage 2. PNG compression level mapping |
| `--preview-max-items N` | `100` | Stage 2. Cap on fragments per series clip and frames per series |
| `--publish TARGET` | none | Stage 3. `gdrive` or `sharepoint`; target-specific flags are in [cloud targets](../stage-3-cloud/cloud-targets.md) |

`--metadata` doubles as the "type of metadata" option from the requirements: `file` needs no
external tool; `media` needs `ffprobe` for non-ISO-BMFF containers.

## Restore flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--transfer MODE` | `auto` | `auto`: rename on the same device, copy into the archive then delete from the video archive otherwise. `copy`: copy and keep the video archive copy |
| `--verify MODE` | `size` | As for split |
| `--stubs POLICY` | `delete` | `delete` or `keep` the Markdown stubs at restored locations |
| `--previews POLICY` | `keep` | Stage 2. `delete` or `keep` preview files generated for restored videos |
| `--create-dirs` | `false` | Recreate a missing parent directory in the archive; default skips the video with a warning |
| `--overwrite` | `false` | Replace an existing, different file at the destination; default skips with a conflict entry |
| `--registry-update` | `true` | Mark restored rows in `arxgo-videos.csv` and regenerate the summary |

## Validation

Validation happens before the lock is taken and before any filesystem write.

- `--archive` and, when required, `--video-archive` exist and are directories. For `split` the
  video archive root is created if missing and its parent exists.
- The two roots are neither equal nor nested in either direction (after resolving symlinks and,
  on Windows, case-folding).
- Enumerated values, durations and sizes parse; `--sample-*`/`--image-*` flags other than `none`
  require `split`.
- `--base-url` is an absolute `http`/`https` URL.
- Tool-backed options are checked through [tool discovery](metadata.md#tool-discovery).

## Exit codes

| Code | Meaning |
| --- | --- |
| 0 | Success; nothing skipped |
| 1 | Unexpected failure; state is recoverable by rerunning |
| 2 | Usage or validation error |
| 3 | Required external tool missing; download link printed |
| 4 | Insufficient free space found by preflight |
| 5 | Run lock held by a live process, or recovery needs operator action |
| 6 | Completed with skipped items (conflicts, missing directories, unreadable files); see report |
| 70 | Operation not implemented in this build (scaffold only) |
| 130 | Interrupted by signal after writing a checkpoint |

## Examples

```bash
arxgo --archive /data/archive
arxgo split --archive /data/archive --video-archive /mnt/nas/video --metadata media \
  --base-url https://storage.example.com/video
arxgo restore --archive D:\archive --video-archive \\nas\video --create-dirs
```
