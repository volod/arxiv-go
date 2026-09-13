# Task Records

Copy the [template](template.md) at task start to `NNNN-<group>-<task-id>.md` using the
[record naming rules](../../guide/planning-workflow.md#record-file-naming), then index it here.
Records preserve full scope and evidence after [plan](../plan.md) removal;
[current state](../current.md) links them.

Next unused sequence: `0003`.

| Record | Scope | Result |
| --- | --- | --- |
| [0001 Repository and agent harness](0001-foundation-bootstrap-repository-and-agent-harness.md) | Go module and scaffold, Makefile, CI, agent instructions, staged specification tree, forward plan, planning tooling | Accepted; `make ci` passes on Linux |
| [0002 Approve ffmpeg distribution](0002-preview-approve-ffmpeg-distribution.md) | Operator decision on redistributable ffmpeg/ffprobe builds; pinned lock file and `make ffmpeg` download target | Accepted by the operator: FFmpeg 6.1.1, GPL v3 static builds for linux/windows amd64 |
