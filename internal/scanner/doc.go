// Package scanner traverses an archive tree and classifies files.
//
// walker.go and order.go implement deterministic filepath.WalkDir traversal with reserved-path
// and --exclude matching, symlink and special-entry handling, and a resume cursor compared by walk
// order key. Planned: mimetype.go (signature-based type detection and binary/media/video/large
// flags). Specification: docs/openspec/stage-1-core/registry.md.
package scanner
