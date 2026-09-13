// Package media reads video metadata and drives external ffmpeg tools.
//
// Planned files: tools.go (locate ffprobe/ffmpeg next to the executable or on
// PATH and report download links), metadata.go (pure-Go ISO BMFF parsing with
// ffprobe JSON fallback) and, in stage 2, ffmpeg.go and preview.go.
// Specifications: docs/openspec/stage-1-core/metadata.md and
// docs/openspec/stage-2-previews/previews.md.
package media
