// Package report writes the operator-facing outputs: the CSV file registry,
// the video registry, per-video Markdown metadata stubs and the archive
// summary.
//
// csv.go and csv_read.go write and read the file registry. markdown.go renders
// stubs; frontmatter.go parses the machine-readable header; names.go chooses a
// collision-safe stub filename; videos.go and summary.go write arxgo-videos.csv
// and arxgo-videos.md. Specification: docs/openspec/stage-1-core/contracts.md.
package report
