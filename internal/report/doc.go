// Package report writes the operator-facing outputs: the CSV file registry,
// the video registry, per-video Markdown metadata stubs and the archive
// summary.
//
// csv.go writes the file registry through a part file with durable offsets. Planned: markdown.go
// and the video registry and summary. Specification:
// docs/openspec/stage-1-core/contracts.md.
package report
