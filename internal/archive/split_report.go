package archive

import (
	"path"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

const phaseReport = "report"

type walAcc struct {
	begin  state.Record
	stub   string
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
		Generated:    s.cfg.Now().UTC(),
		Version:      s.cfg.Version,
		RunIDs:       report.UniqueRunIDs(rows),
		Archive:      s.cfg.Archive,
		VideoArchive: s.cfg.VideoArchive,
		BaseURL:      c.BaseURL,
		Rows:         rows,
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
	merged, err := replayVideoRows(existing, s.cfg.Archive, fill)
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

// replayVideoRows applies the transactions of every run in the archive to the existing registry
// rows, run by run in start order, so a later run always wins: a committed split makes a row
// moved, a split aborted at its destination makes it a conflict (other aborts skipped) unless it
// is moved, and a committed restore marks the row restored. fill completes new split rows.
func replayVideoRows(existing []report.VideoRow, archiveRoot string, fill func(*report.VideoRow)) ([]report.VideoRow, error) {
	runs, err := runsInStartOrder(archiveRoot)
	if err != nil {
		return nil, err
	}
	rows := report.MergeVideoRows(nil, existing)
	for _, rd := range runs {
		recs, err := state.ReadWALRecords(rd.File(state.WALFile))
		if err != nil {
			return nil, err
		}
		if len(recs) == 0 {
			continue
		}
		split, restored := videoEvents(recs, archiveRoot)
		if fill != nil {
			for i := range split {
				fill(&split[i])
			}
		}
		rows = report.MarkRestored(report.MergeVideoRows(rows, split), restored, rd.ID)
	}
	return rows, nil
}

// runsInStartOrder lists the run directories of root ordered by the creation time in their
// options.json, then by id. Run ids alone order only to the second.
func runsInStartOrder(root string) ([]state.RunDir, error) {
	ids, err := state.ListRunIDs(root)
	if err != nil {
		return nil, err
	}
	type run struct {
		dir     state.RunDir
		created time.Time
	}
	runs := make([]run, 0, len(ids))
	for _, id := range ids {
		rd, err := state.OpenRunDir(root, id)
		if err != nil {
			return nil, err
		}
		var o state.RunOptions
		_ = state.ReadJSON(rd.File(state.OptionsFile), &o) // a run without options sorts first
		runs = append(runs, run{rd, o.CreatedAt})
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].created.Before(runs[j].created) })
	out := make([]state.RunDir, len(runs))
	for i, r := range runs {
		out[i] = r.dir
	}
	return out, nil
}

// videoEvents reduces one run's WAL to its final split rows and the rel_paths it restored.
func videoEvents(recs []state.Record, archiveRoot string) ([]report.VideoRow, map[string]struct{}) {
	open := map[string]*walAcc{}
	byRel := map[string]report.VideoRow{}
	restored := map[string]struct{}{}
	for _, rec := range recs {
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
			a.stub = rec.Stub
		case rec.Step == state.StepCommit:
			a.status = report.StatusMoved
			byRel[a.begin.RelPath] = rowFromAcc(a, archiveRoot)
		case rec.Step == state.StepAborted:
			if old, ok := byRel[a.begin.RelPath]; ok && old.Status == report.StatusMoved {
				continue
			}
			a.status = abortStatus(rec.Reason)
			byRel[a.begin.RelPath] = rowFromAcc(a, archiveRoot)
		}
	}
	out := make([]report.VideoRow, 0, len(byRel))
	for _, r := range byRel {
		out = append(out, r)
	}
	return out, restored
}

func rowFromAcc(a *walAcc, archiveRoot string) report.VideoRow {
	rel := a.begin.RelPath
	stubRel := ""
	if a.stub != "" {
		stubRel = report.MustRelStubPath(archiveRoot, a.stub)
	}
	return report.VideoRow{
		RelPath: rel, VideoRelPath: rel, StubRelPath: stubRel,
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
