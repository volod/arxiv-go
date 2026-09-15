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
	begin       state.Record
	description string // archive-relative description path
	sha         string
	status      string
}

// writeVideoOutputs writes arxgo-videos.csv into the archive and the video archive.
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
	if err := videoPayload.writeRegistryCopies(s, []byte(b.String())); err != nil {
		return err
	}
	s.Log.Info("wrote video registry", "videos", len(rows), "csv", scanner.VideoRegistryName)
	return nil
}

func collectVideoRows(s *Session, c SplitConfig) ([]report.VideoRow, error) {
	existing, err := loadVideoRegistry(s)
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
	for i := range merged {
		if !scanner.LocalRelPath(merged[i].RelPath) {
			continue
		}
		if merged[i].URL == "" || (merged[i].Status == report.StatusRestored && strings.HasPrefix(merged[i].URL, "file:")) {
			root := s.cfg.Archive
			if merged[i].Status == report.StatusMoved {
				root = s.cfg.Payload.Root
			}
			merged[i].URL = report.FileURL(filepath.ToSlash(filepath.Join(root, filepath.FromSlash(merged[i].RelPath))))
		}
	}
	return merged, nil
}

// replayArchive reads the video run history of the archive and replays it onto existing rows. Runs
// of another payload never contribute rows.
func replayArchive(s *Session, existing []report.VideoRow, fill func(*report.VideoRow)) ([]report.VideoRow, error) {
	history, err := readHistory(s.cfg.Archive, PayloadVideo)
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
		case rec.Step == state.StepDescribed && rec.Description != "":
			a.description, _ = run.archiveRel(rec.Description)
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
		RelPath: rel, DescriptionRelPath: a.description,
		FileName: path.Base(rel), FileSize: a.begin.Size, SHA256: a.sha,
		Transfer: a.begin.Transfer, Status: a.status, RunID: runIDFromTxID(a.begin.TxID),
	}
}

// abortStatus is the registry status of an aborted split: conflict at the destination, otherwise
// skipped.
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
			RelPath: is.RelPath, FileName: path.Base(is.RelPath), Status: st,
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
		if !report.HasMetadata(row.Metadata) {
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
