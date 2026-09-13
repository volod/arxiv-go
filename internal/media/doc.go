// Package media reads video metadata and drives external ffmpeg tools.
//
// tools.go locates ffprobe/ffmpeg next to the executable or on PATH and validates them with
// "-version"; guidance.go holds the per-platform download message. metadata.go and isobmff.go
// read ISO BMFF metadata and normalize bounded ffprobe JSON for other media or ISO failures.
// Planned for stage 2: ffmpeg previews.
// Specifications: docs/openspec/stage-1-core/metadata.md and
// docs/openspec/stage-2-previews/previews.md.
package media
