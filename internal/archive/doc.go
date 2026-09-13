// Package archive implements the operations and their run lifecycle.
//
// Files: session.go, resume.go and finish.go (Session: run lock, run directory,
// resume, WAL recovery, run log, checkpoints, issues, final report and outcome),
// progress.go (live counters and throttled progress lines). Planned: split.go,
// restore.go, preflight.go. Specifications:
// docs/openspec/stage-1-core/integrity.md and docs/openspec/stage-1-core/split-restore.md.
package archive
