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
	existing, err := report.LoadVideoFile(filepath.Join(s.cfg.Archive, scanner.VideoRegistryName))
	if err != nil {
		return nil, err
	}
	if existing == nil && s.cfg.VideoArchive != "" {
		existing, err = report.LoadVideoFile(filepath.Join(s.cfg.VideoArchive, scanner.VideoRegistryName))
		if err != nil {
			return nil, err
		}
	}
	regRows, err := report.LoadRegistry(c.Scan.Registry)
	if err != nil {
		s.Log.Warn("video registry: file registry unavailable", "error", err)
	}
	byReg := report.RegistryByPath(regRows)
	walRows, err := rowsFromAllWALs(s.cfg.Archive)
	if err != nil {
		return nil, err
	}
	for i := range walRows {
		fillVideoRow(&walRows[i], byReg, c.BaseURL)
	}
	issueRows := rowsFromIssues(s.Issues())
	for i := range issueRows {
		fillVideoRow(&issueRows[i], byReg, c.BaseURL)
	}
	merged := report.MergeVideoRows(existing, walRows)
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

func rowsFromAllWALs(archiveRoot string) ([]report.VideoRow, error) {
	ids, err := state.ListRunIDs(archiveRoot)
	if err != nil {
		return nil, err
	}
	var incoming []report.VideoRow
	for _, id := range ids {
		rd, err := state.OpenRunDir(archiveRoot, id)
		if err != nil {
			return nil, err
		}
		recs, err := state.ReadWALRecords(rd.File(state.WALFile))
		if err != nil {
			return nil, err
		}
		incoming = append(incoming, rowsFromWAL(recs, archiveRoot)...)
	}
	return incoming, nil
}

func rowsFromWAL(recs []state.Record, archiveRoot string) []report.VideoRow {
	open := map[string]*walAcc{}
	byRel := map[string]report.VideoRow{}
	for _, rec := range recs {
		switch rec.Step {
		case state.StepBegin:
			open[rec.TxID] = &walAcc{begin: rec}
		case state.StepVerified:
			if a := open[rec.TxID]; a != nil && rec.SHA256 != "" {
				a.sha = rec.SHA256
			}
		case state.StepStubbed:
			if a := open[rec.TxID]; a != nil && rec.Stub != "" {
				a.stub = rec.Stub
			}
		case state.StepCommit:
			if a := open[rec.TxID]; a != nil {
				a.status = report.StatusMoved
				byRel[a.begin.RelPath] = rowFromAcc(a, archiveRoot)
			}
		case state.StepAborted:
			if a := open[rec.TxID]; a != nil {
				if old, ok := byRel[a.begin.RelPath]; ok && old.Status == report.StatusMoved {
					continue
				}
				a.status = abortStatus(rec.Reason)
				byRel[a.begin.RelPath] = rowFromAcc(a, archiveRoot)
			}
		}
	}
	out := make([]report.VideoRow, 0, len(byRel))
	for _, r := range byRel {
		out = append(out, r)
	}
	return out
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
