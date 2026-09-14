# CLI contract

Owner: `project-foundation`. Split preview settings and restore preview cleanup are active.
Cloud publishing flags are listed so the parser recognizes their names; using them exits 2 with
`option not available in this build`.

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

Precedence, highest first: command line, process environment, [environment file](#environment-file),
built-in default.

Parsing details:

- The operation must come first. Any positional argument after the flags is a usage error (exit 2).
  `publish` exits 2 with `option not available in this build`.
- `arxgo help <operation>` and `arxgo <operation> --help` print that operation's flags with
  defaults and environment names to stdout, and exit 0.
- An environment variable applies only to flags of the selected operation, and an empty value is
  ignored. For the repeatable `--exclude`, the variable holds several globs separated by the
  platform path list separator (`:` on Linux, `;` on Windows). If the flag appears on the
  command line, the variable is ignored.
- A flag of another operation (for example `--stubs` on `scan`) is an unknown flag (exit 2).
  Split preview flags and restore `--previews` are active. Stage-3 flags and
  `--follow-symlinks=true` exit 2 with `option not available in this build` whether they come
  from the command line or the environment.
- `scan` accepts `--video-archive` (including `ARXGO_VIDEO_ARCHIVE`) and ignores it.
- All validation errors are printed together, one per line, followed by a pointer to
  `arxgo help <operation>`.

## Environment file

`arxgo` reads an optional file named `.env` in the directory of the running executable (symlinks
resolved), for example `bin/.env` next to `bin/arxgo`. It uses the same `ARXGO_<NAME>` variables
and may also hold credentials for later-stage targets, so settings and secrets can be kept without
repeating flags.

- The file is optional. When it is missing, `arxgo` behaves exactly as without it. Mandatory
  values can always come from flags or the process environment.
- Syntax is the common dotenv format parsed by `godotenv`: `KEY=value`, `#` comments, single or
  double quotes, optional `export ` prefix, and `${VAR}` expansion. The file only supplies values
  for lookups. It never changes the process environment, so child processes such as `ffprobe`
  do not inherit it.
- A process environment variable with a non-empty value wins over the file. An empty or absent
  process value falls through to the file.
- Relative paths in the file are resolved against the current working directory, as on the
  command line.
- An unreadable or malformed file exits 2, naming the file. Errors about a value from the file
  name the variable and the file.
- The file is read before any other validation and is never written. The repository ships
  `.env.example` with every variable commented out; `make setup` copies it to `bin/.env` when that
  file does not exist yet.

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
1024); a bare number is bytes. Units are case-insensitive and may be separated from the number by
spaces. A decimal fraction (`1.5GiB`) is allowed with a multi-byte unit and rounds down to whole
bytes. Negative, fractional-byte and larger-than-int64 sizes are rejected. Durations use Go syntax
(`90s`, `5m`, `1h30m`); `--progress-interval`, `--checkpoint-interval`, `--large-threshold` and
`--checkpoint-every` must be positive.

## Scan flags

Used by `scan`, and by `split` for its scan phase.

| Flag | Default | Meaning |
| --- | --- | --- |
| `--large-threshold SIZE` | `1GiB` | Files with `file_size >= SIZE` get `is_large=true` |
| `--registry PATH` | `<archive>/arxgo-registry.csv` | CSV registry output path |
| `--metadata MODE` | `file` | `file`: file-system metadata only. `media`: also container/stream metadata for media files (see [metadata](metadata.md)) |
| `--video-extensions LIST` | none | Extra comma-separated extensions treated as video when signature detection is inconclusive (`application/octet-stream`), added to the built-in list |
| `--exclude GLOB` | none, repeatable | Relative-path glob (`path.Match` per segment, `**` for any depth) anchored at the archive root and excluded from traversal with its subtree; see [traversal](registry.md#traversal) |
| `--follow-symlinks` | `false` | Reserved; symlinks are recorded but never followed in stage 1 |

## Split flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--transfer MODE` | `auto` | `auto`: rename on the same device, copy+verify+delete otherwise. `copy`: always copy+verify+delete |
| `--verify MODE` | `size` | `size` or `hash` (SHA-256 computed while copying and re-read from the destination) |
| `--base-url URL` | none | Base URL of the cloud location the video archive will be uploaded to; stubs link to `URL/<rel_path>` |
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
- `--base-url` is an absolute `http`/`https` URL with a host and without credentials, query or
  fragment, because stubs append `/<rel_path>`. Trailing slashes are removed.
- `--video-extensions` items are letters, digits, `_` or `-`, with an optional leading dot. They
  are normalized to lower case with a leading dot, and duplicates are dropped.
- `--exclude` globs are relative, use `/` on every platform, and contain no empty or `..` segments.
  On Windows a glob containing `\` is rejected, because `\` is a `path.Match` escape rather than a
  separator there (a literal `[` is matched with `[[]`).
- An explicit `--registry` is made absolute. It must not be a directory, and its parent directory
  must exist.
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
