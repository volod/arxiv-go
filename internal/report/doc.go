// Package report writes the operator-facing outputs: the CSV file registry,
// the video registry, per-video Markdown descriptions and the archive
// registries.
//
// csv.go, csv_read.go, csv_compact.go and metadata_csv.go write and read the file registry. description.go writes and reads
// the video and CATIA description fields, preview links and text-sidecar occupancy; catia.go renders the catia: line and the text sidecar; names.go chooses a collision-safe description or text-sidecar filename; videos.go writes arxgo-videos.csv; catia_registry.go writes arxgo-catia.csv.
// Specification: docs/openspec/stage-1-core/contracts.md.
package report
