// Package fsops holds platform-aware filesystem primitives: same-device
// detection, free-space queries, durable copy/rename with fsync and
// verification, and Windows path handling.
//
// Planned files: device_*.go, space_*.go and transfer.go. Specification:
// docs/openspec/stage-1-core/integrity.md.
package fsops
