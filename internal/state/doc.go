// Package state provides crash-safe run state: the run lock, periodic
// checkpoints and the JSONL write-ahead log used to roll interrupted
// transactions forward or back.
//
// Planned files: checkpoint.go and wal.go. Specification:
// docs/openspec/stage-1-core/integrity.md.
package state
