// Package archive implements the operations and their run lifecycle.
//
// Files: session.go, resume.go and finish.go (Session: run lock, run directory,
// resume, WAL recovery, run log, checkpoints, issues, final report and outcome),
// progress.go (live counters and throttled progress lines), preflight.go (pure free-space
// requirement model) and preflight_run.go (device probe, report lines, Session.Preflight).
// Planned: split.go, restore.go. Specifications:
// docs/openspec/stage-1-core/integrity.md and docs/openspec/stage-1-core/split-restore.md.
package archive
