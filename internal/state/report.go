package state

import "time"

// ReportVersion is the report.json format version.
const ReportVersion = 1

// Report is the content of report.json, written when a run reaches a final state. Its presence
// marks the run complete; an interrupted or failed run has none and is resumed.
type Report struct {
	V          int          `json:"v"`
	RunID      string       `json:"run_id"`
	Op         string       `json:"op"`
	Version    string       `json:"version"`
	Status     string       `json:"status"` // completed, partial, not_implemented; dry runs may also be interrupted or failed
	DryRun     bool         `json:"dry_run,omitempty"`
	Resumed    bool         `json:"resumed,omitempty"`
	StartedAt  time.Time    `json:"started_at"`
	FinishedAt time.Time    `json:"finished_at"`
	WallS      float64      `json:"wall_s"`
	Counters   Counters     `json:"counters"`
	Phases     []PhaseStats `json:"phases"`
	Roots      []RootStats  `json:"roots"`
	Issues     []Issue      `json:"issues,omitempty"`
	// IssuesOmitted counts issues beyond the in-report cap; the run log lists all of them.
	IssuesOmitted int64 `json:"issues_omitted,omitempty"`
}

// PhaseStats are the counters accumulated during one phase of this process.
type PhaseStats struct {
	Name      string    `json:"name"`
	StartedAt time.Time `json:"started_at"`
	WallS     float64   `json:"wall_s"`
	Counters  Counters  `json:"counters"`
}

// RootStats are the bytes written to and freed from one root.
type RootStats struct {
	Root         string `json:"root"`
	BytesWritten int64  `json:"bytes_written"`
	BytesFreed   int64  `json:"bytes_freed"`
}

// Issue kinds.
const (
	IssueSkipped = "skipped"
	IssueFailed  = "failed"
)

// Issue is one skipped or failed item.
type Issue struct {
	Kind    string `json:"kind"`
	RelPath string `json:"rel_path"`
	Reason  string `json:"reason"`
}

// WriteReport atomically writes report.json.
func WriteReport(path string, r Report) error {
	r.V = ReportVersion
	return WriteJSON(path, r)
}
