// Package fsops holds platform-aware filesystem primitives: same-device
// detection, free-space queries, atomic file writes, durable verified copies
// and no-replace renames with cross-device error classification.
//
// Files: ops.go (Ops interface and System), device_*.go, space_*.go,
// rename*.go, sys_*.go, transfer.go, copyloop.go and atomic.go. This is the only package with
// build-tagged platform files. Specification:
// docs/openspec/stage-1-core/integrity.md and
// docs/openspec/stage-1-core/split-restore.md.
package fsops
