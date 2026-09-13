// Package scanner traverses an archive tree and classifies files.
//
// Planned files: walker.go (deterministic filepath.WalkDir traversal with
// reserved-path exclusion and resume cursor) and mimetype.go (signature-based
// type detection and binary/media/video/large flags). Specification:
// docs/openspec/stage-1-core/registry.md.
package scanner
