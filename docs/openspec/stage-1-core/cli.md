# CLI contract

Owner: `project-foundation`. Split preview settings and restore preview cleanup are active.
Cloud publishing flags are listed so the parser recognizes their names; using them exits 2 with
`option not available in this build`. CATIA flags are specified for
[stage 4](../stage-4-catia/README.md): `--video`, `--catia` and `--catia-archive` are active for
`split` and `restore`, and `--catia-text` for `split` (`--catia-archive` is accepted and ignored by
`scan`; `--catia-text` is unknown on `restore`, exit 2).

## Synopsis

```text
arxgo [scan]  --archive PATH [common flags] [scan flags]
arxgo split   --archive PATH --video-archive PATH [common flags] [scan flags] [split flags]
arxgo split   --catia --archive PATH --catia-archive PATH [--catia-text] [common flags] [scan flags] [split flags]
arxgo restore --archive PATH --video-archive PATH [common flags] [restore flags]
arxgo restore --catia --archive PATH --catia-archive PATH [common flags] [restore flags]
arxgo version
arxgo help [operation]
```

The operation name `publish` is reserved for cloud publishing.

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
- A flag of another operation (for example `--descriptions` on `scan`) is an unknown flag (exit 2).
  Split preview flags and restore `--previews` are active. Stage-3 flags and
  `--follow-symlinks=true` exit 2 with `option not available in this build` whether they come
  from the command line or the environment.
- `scan` accepts `--video-archive` and `--catia-archive` (including `ARXGO_VIDEO_ARCHIVE` and
  `ARXGO_CATIA_ARCHIVE`) and ignores them.
- The payload flags `--video` and `--catia` resolve as one setting: when either appears on the
  command line, `ARXGO_VIDEO` and `ARXGO_CATIA` from the environment or environment file are both
  ignored, so `--video` on the command line overrides `ARXGO_CATIA=true` instead of conflicting.
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
| `--video-archive PATH` | required for video split/restore | Root of the video archive; may be on another drive or a mounted/UNC network share |
| `--catia-archive PATH` | required for `--catia` split/restore | Stage 4. Root of the CATIA archive; same placement rules as `--video-archive` |
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
| `--metadata MODE` | `file` | `file`: filesystem metadata plus ISO BMFF container/stream fields for MP4, MOV, M4A, M4V and 3GP (no external tool). `media`: also ffprobe for other audio/video and ISO failures (see [metadata](metadata.md)) |
| `--video-extensions LIST` | none | Extra comma-separated extensions treated as video when signature detection is inconclusive (`application/octet-stream`), added to the built-in list |
| `--exclude GLOB` | none, repeatable | Relative-path glob (`path.Match` per segment, `**` for any depth) anchored at the archive root and excluded from traversal with its subtree; see [traversal](registry.md#traversal) |
| `--follow-symlinks` | `false` | Reserved; symlinks are recorded but never followed by scan |

## Split flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--transfer MODE` | `auto` | `auto`: rename on the same device, copy+verify+delete otherwise. `copy`: always copy+verify+delete |
| `--verify MODE` | `size` | `size` or `hash` (SHA-256 computed while copying and re-read from the destination; on a same-device rename there is nothing to compare, so split reads the file once to record the same hash) |
| `--base-url URL` | none | Base URL of the cloud location the selected mirror (video or CATIA archive) is published at; descriptions link to `URL/<rel_path>` |
| `--video` | `false` | Stage 4. Select the video payload. Default when `--catia` is also unset |
| `--catia` | `false` | Stage 4. Select the CATIA payload. Mutually exclusive with `--video` |
| `--catia-text` | `false` | Stage 4. With `--catia`, write a text sidecar of extracted accessible text for moved CATIA files that lack one |
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
| `--publish TARGET` | none | Cloud publishing. `gdrive` or `sharepoint`; target-specific flags are in [cloud targets](../stage-3-cloud/cloud-targets.md) |

`--metadata` doubles as the "type of metadata" option from the requirements: `file` needs no
external tool; `media` needs `ffprobe` for non-ISO-BMFF containers.

## Restore flags

| Flag | Default | Meaning |
| --- | --- | --- |
| `--video` | `false` | Stage 4. Select the video payload. Default when `--catia` is also unset |
| `--catia` | `false` | Stage 4. Select the CATIA payload. Mutually exclusive with `--video` |
| `--transfer MODE` | `auto` | `auto`: rename on the same device, copy into the archive then delete from the video or CATIA archive otherwise. `copy`: copy and keep the mirror copy |
| `--verify MODE` | `size` | As for split |
| `--descriptions POLICY` | `delete` | `delete` or `keep` the descriptions at restored locations (CATIA also the owned text sidecar) |
| `--previews POLICY` | `keep` | Stage 2. `delete` or `keep` preview files generated for restored videos; `delete` with `--catia` exits 2 |
| `--create-dirs` | `false` | Recreate a missing parent directory in the archive; default skips the file with a warning |
| `--overwrite` | `false` | Replace an existing, different destination file instead of skipping it |
| `--registry-update` | `true` | Mark restored rows in `arxgo-videos.csv` or, with `--catia`, `arxgo-catia.csv` |

## Validation

Validation happens before the lock is taken and before any filesystem write.

- The selected mirror root is `--video-archive` for the video payload and `--catia-archive` for
  `--catia`; a missing selected root exits 2. `--archive` and the selected root exist and are
  directories. For `split` the selected root is created if missing and its parent exists.
- The payload and its root must match: `--catia-archive` on the command line without `--catia`, or
  `--video-archive` on the command line with `--catia`, exits 2 naming the flag to use. A value that
  comes only from the environment or environment file for the other payload is ignored.
- The archive and every mirror root that is set (both mirror roots when both come from any source)
  are pairwise neither equal nor nested in either direction (after resolving symlinks and, on
  Windows, case-folding). The root that is not selected is compared by path only; it need not
  exist.
- Enumerated values, durations and sizes parse; `--sample-*`/`--image-*` flags other than `none`
  require `split`.
- `--video` and `--catia` are booleans. If both are set, exit 2. If neither is set, the payload is
  video. `--catia-text` requires `--catia` and `split`; otherwise exit 2.
- `--catia` together with `--sample` or `--image` other than `none`, `--publish`, or restore
  `--previews delete` exits 2.
- A built-in CATIA extension in `--video-extensions` exits 2.
- `--base-url` is an absolute `http`/`https` URL with a host and without credentials, query or
  fragment, because descriptions append `/<rel_path>`. Trailing slashes are removed.
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
| 6 | Completed with skipped items (conflicts, missing directories, unreadable files) or failed previews or CATIA text extraction; see report |
| 130 | Interrupted by signal after writing a checkpoint |

## Examples

```bash
arxgo --archive /data/archive
arxgo split --archive /data/archive --video-archive /mnt/nas/video --metadata media \
  --base-url https://storage.example.com/video
arxgo split --catia --archive /data/archive --catia-archive /mnt/nas/catia --catia-text
arxgo restore --archive D:\archive --video-archive \\nas\video --create-dirs
arxgo restore --catia --archive /data/archive --catia-archive /mnt/nas/catia
```
