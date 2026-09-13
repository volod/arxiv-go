// Package state provides crash-safe run state under <root>/.arxgo: run
// directories and the current-run pointer, the run lock, atomic checkpoints,
// the final report and the JSON Lines run log with a console fan-out handler.
//
// Files: rundir.go, lock.go, checkpoint.go, report.go and runlog.go. The
// write-ahead log (wal.go) and recovery are added by their plan task.
// Specification: docs/openspec/stage-1-core/integrity.md.
package state
