package archive

import (
	"path"
	"path/filepath"
	"strings"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

const phaseReport = "report"

type walAcc struct {
	begin  state.Record
	stub   string // archive-relative stub path
	sha    string
	status string
}

func writeVideoOutputs(s *Session, c SplitConfig) error {
	if err := s.Phase(phaseReport, Totals{}); err != nil {
		return err
	}
	rows, err := collectVideoRows(s, c)
	if err != nil {
		return err
	}
	var b strings.Builder
	if err := report.WriteVideoCSV(&b, rows); err != nil {
		return err
	}
	csvBuf := []byte(b.String())
	mdBuf, err := report.RenderSummary(report.SummaryInput{
		Generated:       s.cfg.Now().UTC(),
		Version:         s.cfg.Version,
		RunIDs:          report.UniqueRunIDs(rows),
		Archive:         s.cfg.Archive,
		VideoArchive:    s.cfg.VideoArchive,
		BaseURL:         c.BaseURL,
		Rows:            rows,
		PreviewFailures: previewFailures(s.Issues()),
	})
	if err != nil {
		return err
	}
	for _, root := range []string{s.cfg.Archive, s.cfg.VideoArchive} {
		if root == "" {
			continue
		}
		if err := s.cfg.FS.AtomicWriteFile(filepath.Join(root, scanner.VideoRegistryName), csvBuf, 0o644); err != nil {
			return err
		}
		if err := s.cfg.FS.AtomicWriteFile(filepath.Join(root, scanner.VideoSummaryName), mdBuf, 0o644); err != nil {
			return err
		}
	}
	s.Log.Info("wrote video registry", "videos", len(rows),
		"csv", scanner.VideoRegistryName, "summary", scanner.VideoSummaryName)
	return nil
}

func previewFailures(issues []state.Issue) []string {
	var out []string
	for _, issue := range issues {
		if issue.Kind == state.IssueFailed && strings.HasPrefix(issue.Reason, "preview") {
			out = append(out, issue.RelPath+": "+issue.Reason)
		}
	}
	return out
}

func collectVideoRows(s *Session, c SplitConfig) ([]report.VideoRow, error) {
	existing, err := loadVideoRegistry(s.cfg.Archive, s.cfg.VideoArchive)
	if err != nil {
		return nil, err
	}
	regRows, err := report.LoadRegistry(c.Scan.Registry)
	if err != nil {
		s.Log.Warn("video registry: file registry unavailable", "error", err)
	}
	byReg := report.RegistryByPath(regRows)
	fill := func(row *report.VideoRow) { fillVideoRow(row, byReg, c.BaseURL) }
	merged, err := replayArchive(s, existing, fill)
	if err != nil {
		return nil, err
	}
	issueRows := rowsFromIssues(s.Issues())
	for i := range issueRows {
		fill(&issueRows[i])
	}
	merged = report.MergeVideoRows(merged, issueRows)
	if c.BaseURL != "" {
		for i := range merged {
			if merged[i].Status == report.StatusMoved {
				merged[i].URL = report.ComposeURL(c.BaseURL, merged[i].RelPath)
			}
		}
	}
	return merged, nil
}

// replayArchive reads the current WAL history of the archive and replays it onto existing rows.
func replayArchive(s *Session, existing []report.VideoRow, fill func(*report.VideoRow)) ([]report.VideoRow, error) {
	history, err := readHistory(s.cfg.Archive, s.cfg.VideoArchive)
	if err != nil {
		return nil, err
	}
	idx, err := newPreviewIndex(s.cfg.Archive, history)
	if err != nil {
		return nil, err
	}
	return replayVideoRows(existing, history, idx, fill), nil
}

// replayVideoRows applies the transactions of every run to the existing registry rows, run by run
// in start order, so a later run always wins: a committed split makes a row moved, a split aborted
// at its destination makes it a conflict (other aborts skipped) unless it is moved, and a committed
// restore marks the row restored. fill completes new split rows. The previews column lists the
// completed previews of idx.
func replayVideoRows(existing []report.VideoRow, history []runHistory, idx *previewIndex, fill func(*report.VideoRow)) []report.VideoRow {
	rows := report.MergeVideoRows(nil, existing)
	for _, run := range history {
		split, restored := videoEvents(run)
		if fill != nil {
			for i := range split {
				fill(&split[i])
			}
		}
		rows = report.MarkRestored(report.MergeVideoRows(rows, split), restored, run.id)
	}
	for i := range rows {
		rows[i].Previews = idx.encoded(rows[i].RelPath)
	}
	return rows
}

// videoEvents reduces one run's WAL to its final split rows and the rel_paths it restored.
func videoEvents(run runHistory) ([]report.VideoRow, map[string]struct{}) {
	open := map[string]*walAcc{}
	byRel := map[string]report.VideoRow{}
	restored := map[string]struct{}{}
	for _, rec := range run.records {
		a := open[rec.TxID]
		switch {
		case rec.Step == state.StepBegin:
			open[rec.TxID] = &walAcc{begin: rec}
		case a == nil:
		case a.begin.Op == opRestore:
			if rec.Step == state.StepCommit {
				restored[a.begin.RelPath] = struct{}{}
			}
		case rec.Step == state.StepVerified && rec.SHA256 != "":
			a.sha = rec.SHA256
		case rec.Step == state.StepStubbed && rec.Stub != "":
			a.stub, _ = run.archiveRel(rec.Stub)
		case rec.Step == state.StepCommit:
			a.status = report.StatusMoved
			byRel[a.begin.RelPath] = a.row()
		case rec.Step == state.StepAborted:
			if old, ok := byRel[a.begin.RelPath]; ok && old.Status == report.StatusMoved {
				continue
			}
			a.status = abortStatus(rec.Reason)
			byRel[a.begin.RelPath] = a.row()
		}
	}
	out := make([]report.VideoRow, 0, len(byRel))
	for _, r := range byRel {
		out = append(out, r)
	}
	return out, restored
}

func (a *walAcc) row() report.VideoRow {
	rel := a.begin.RelPath
	return report.VideoRow{
		RelPath: rel, VideoRelPath: rel, StubRelPath: a.stub,
		FileName: path.Base(rel), FileSize: a.begin.Size, SHA256: a.sha,
		Transfer: a.begin.Transfer, Status: a.status, RunID: runIDFromTxID(a.begin.TxID),
	}
}

func abortStatus(reason string) string {
	if strings.Contains(reason, "destination") {
		return report.StatusConflict
	}
	return report.StatusSkipped
}

func rowsFromIssues(issues []state.Issue) []report.VideoRow {
	var out []report.VideoRow
	for _, is := range issues {
		st := report.StatusSkipped
		if strings.Contains(is.Reason, "destination") {
			st = report.StatusConflict
		}
		out = append(out, report.VideoRow{
			RelPath: is.RelPath, VideoRelPath: is.RelPath, FileName: path.Base(is.RelPath), Status: st,
		})
	}
	return out
}

func fillVideoRow(row *report.VideoRow, byReg map[string]report.RegistryRow, baseURL string) {
	if r, ok := byReg[row.RelPath]; ok {
		if row.FileName == "" {
			row.FileName = r.FileName
		}
		if row.FileSize == 0 {
			row.FileSize = r.FileSize
		}
		if row.FileMIME == "" {
			row.FileMIME = r.FileMIME
		}
		if row.Metadata.V == 0 {
			row.Metadata = r.Metadata
		}
	}
	if row.FileName == "" {
		row.FileName = path.Base(row.RelPath)
	}
	if baseURL != "" && row.Status == report.StatusMoved {
		row.URL = report.ComposeURL(baseURL, row.RelPath)
	}
}
