// Package state provides crash-safe run state under <root>/.arxgo: run
// directories and the current-run pointer, the run lock, atomic checkpoints,
// the final report, the JSON Lines run log with a console fan-out handler,
// the write-ahead log and recovery.
//
// Files: rundir.go, lock.go, checkpoint.go, report.go, runlog.go, wal.go,
// wal_read.go, committed.go and recovery.go. Tests inject crashes through
// crashtest/. Specification: docs/openspec/stage-1-core/integrity.md.
package state
