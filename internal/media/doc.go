// Package media reads video metadata and drives the external ffmpeg tools.
//
// tools.go locates ffprobe/ffmpeg next to the executable or on PATH and validates them with
// "-version"; guidance.go holds the per-platform download message. metadata.go and isobmff*.go
// read ISO BMFF metadata; ffprobe.go normalizes bounded ffprobe JSON for other media or ISO
// failures. ffmpeg*.go run ffmpeg with progress, timeouts, process-tree cleanup and no-replace
// publication. preview*.go plan preview jobs without running a tool; samples*.go and frames.go
// encode and validate them.
// Specifications: docs/openspec/stage-1-core/metadata.md and
// docs/openspec/stage-2-previews/previews.md.
package media
