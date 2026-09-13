// Package archive implements the split and restore operations.
//
// Planned files: split.go (move video files into the mirrored video archive,
// write metadata stubs) and restore.go (move or copy them back). Every file
// mutation runs as a write-ahead-logged transaction from package state.
// Specification: docs/openspec/stage-1-core/split-restore.md.
package archive
