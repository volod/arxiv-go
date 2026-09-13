package archive

import "github.com/volod/arxiv-go/internal/state"

// Checkpointed returns a copy of the checkpoint fields as they will be written next: after Start
// these are the fields of the resumed checkpoint.
func (s *Session) Checkpointed() state.Checkpoint {
	s.mu.Lock()
	defer s.mu.Unlock()
	cp := s.cp
	cp.ScanCursor = append([]string(nil), cp.ScanCursor...)
	if cp.Scan != nil {
		cp.Scan = cp.Scan.Clone()
	}
	return cp
}

// AdvanceSync records n processed items like Advance. When a checkpoint is due it first calls sync,
// which must make the operation's output durable and store the matching cursor and offsets with
// Update, so a checkpoint never names data that is not on disk.
func (s *Session) AdvanceSync(n int64, sync func() error) error {
	s.mu.Lock()
	due := s.throttle.Add(n)
	s.mu.Unlock()
	if !due {
		return nil
	}
	if err := sync(); err != nil {
		return err
	}
	return s.Checkpoint()
}

// RecordIssue adds a skipped or failed item to the report without logging it, for items whose
// warning was already logged.
func (s *Session) RecordIssue(kind, relPath, reason string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.issues) >= maxIssues {
		s.issuesOmitted++
		return
	}
	s.issues = append(s.issues, state.Issue{Kind: kind, RelPath: relPath, Reason: reason})
}

// Issues returns a copy of the recorded issues.
func (s *Session) Issues() []state.Issue {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]state.Issue(nil), s.issues...)
}

// MarkPartial makes a successful run end as partial (exit 6) although this process may have
// recorded no issue, for items skipped by an earlier process of a resumed run.
func (s *Session) MarkPartial() {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.partial = true
}

// SetScanSummary sets the scan section of report.json.
func (s *Session) SetScanSummary(sum *state.ScanSummary) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.scanSummary = sum
}

// ElapsedS returns the run time in seconds accumulated across resumed processes.
func (s *Session) ElapsedS() float64 {
	return s.elapsedBase + s.cfg.Now().Sub(s.started).Seconds()
}
