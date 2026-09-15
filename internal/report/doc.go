// Package report writes the operator-facing outputs: the CSV file registry,
// the video registry, per-video Markdown descriptions and the archive
// registries.
//
// csv.go, csv_read.go, csv_compact.go and metadata_csv.go write and read the file registry. description.go writes and reads
// the video description fields and preview links; names.go chooses a collision-safe description filename; videos.go writes arxgo-videos.csv.
// Specification: docs/openspec/stage-1-core/contracts.md.
package report
