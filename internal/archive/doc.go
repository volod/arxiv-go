// Package archive implements the operations and their run lifecycle.
//
// Files: session.go, resume.go and finish.go (Session: run lock, run directory,
// resume, WAL recovery, run log, checkpoints, issues, final report and outcome),
// progress.go (live counters and throttled progress lines), preflight.go (pure free-space
// requirement model), preflight_run.go (device probe, report lines, Session.Preflight),
// session_state.go (checkpoint hooks used by operation bodies), scan.go and scan_pipeline.go (the
// resumable registry scan: walker, detection pool, ordered writer), candidates.go
// (candidates.jsonl), split.go, split_transfer.go, split_recovery.go, split_stub.go and
// split_report.go (video transactions, Markdown stubs, video registry), restore.go,
// restore_recovery.go, restore_dirs.go and restore_report.go (restore transactions).
// Specifications:
// docs/openspec/stage-1-core/integrity.md and docs/openspec/stage-1-core/split-restore.md.
package archive
