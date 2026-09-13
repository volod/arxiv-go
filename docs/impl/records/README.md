# Task Records

Copy the [template](template.md) at task start to `NNNN-<group>-<task-id>.md` using the
[record naming rules](../../guide/planning-workflow.md#record-file-naming), then index it here.
Records preserve full scope and evidence after [plan](../plan.md) removal;
[current state](../current.md) links them.

Next unused sequence: `0006`.

| Record | Scope | Result |
| --- | --- | --- |
| [0001 Repository and agent harness](0001-foundation-bootstrap-repository-and-agent-harness.md) | Go module and scaffold, Makefile, CI, agent instructions, staged specification tree, forward plan, planning tooling | Accepted; `make ci` passes on Linux |
| [0002 Approve ffmpeg distribution](0002-preview-approve-ffmpeg-distribution.md) | Operator decision on redistributable ffmpeg/ffprobe builds; pinned lock file and `make ffmpeg` download target | Accepted by the operator: FFmpeg 6.1.1, GPL v3 static builds for linux/windows amd64 |
| [0003 Implement CLI contract](0003-foundation-implement-cli-contract.md) | Validated operation and flag contract, typed options, env overrides, root checks, reserved stage-2/3 flags, slog console logger, signal context, exit codes | Accepted; `make ci` passes on Linux; Windows CI pending |
| [0004 Environment file and setup](0004-foundation-add-env-file-and-setup.md) | Optional `bin/.env` read next to the executable via `godotenv`, `.env.example`, `make setup`/`make env`, `clean` keeps `bin/.env` | Accepted; `make ci` passes on Linux; Windows pending |
| [0005 Filesystem primitives](0005-safety-implement-filesystem-primitives.md) | `internal/fsops`: same-device and free-space queries, no-replace and replacing renames with cross-device classification, durable verified copy, atomic write, `Ops` interface; `golang.org/x/sys` | Accepted; `make ci` passes on Linux; Windows CI pending |
