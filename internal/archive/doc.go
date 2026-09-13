// Package archive implements the operations and their run lifecycle.
//
// Files: session.go and finish.go (Session: run lock, run directory, resume, run log,
// checkpoints, issues, final report and outcome), progress.go (live counters and throttled
// progress lines). Planned: split.go, restore.go, preflight.go. File mutations in those
// operations are write-ahead-logged by a later task. Specifications:
// docs/openspec/stage-1-core/integrity.md and docs/openspec/stage-1-core/split-restore.md.
package archive
