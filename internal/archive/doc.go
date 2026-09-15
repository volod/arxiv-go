// Package archive implements the operations and their run lifecycle.
//
// Files: session.go, resume.go and finish.go (Session: run lock, run directory,
// resume, WAL recovery, run log, checkpoints, issues, final report and outcome),
// progress.go (live counters and throttled progress lines), preflight.go (pure free-space
// requirement model), preflight_run.go (device probe, report lines, Session.Preflight),
// session_state.go (checkpoint hooks used by operation bodies), scan.go and scan_pipeline.go (the
// resumable registry scan: walker, detection pool, ordered writer), candidates.go
// (candidates.jsonl), history.go (the WAL of every run with its recorded roots), transfer.go
// (placement and its failures, shared by split and restore), split.go, split_exec.go,
// split_recovery.go, split_description.go and split_report.go (video transactions, video descriptions, video
// registry), restore.go, restore_exec.go, restore_recovery.go, restore_dirs.go and
// restore_report.go (restore transactions), previews_index.go, previews_plan.go,
// previews_exec.go and previews_restore.go (preview WAL events, planning, generation and cleanup).
// Specifications: docs/openspec/stage-1-core/integrity.md,
// docs/openspec/stage-1-core/split-restore.md and docs/openspec/stage-2-previews/previews.md.
package archive
