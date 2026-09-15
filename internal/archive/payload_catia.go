package archive

import (
	"context"
	"strings"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// RoleCatiaArchive is the preflight role of the CATIA payload's mirror root.
const RoleCatiaArchive Role = "catia_archive"

// catiaPayload moves CATIA files into the CATIA archive with CATIA descriptions and writes
// arxgo-catia.csv. It has no post-commit sidecar yet and no restore.
var catiaPayload = &payloadSpec{
	kind: PayloadCatia, noun: "catia", plural: "catia", role: RoleCatiaArchive, registry: scanner.CatiaRegistryName,
	candidate: func(ft scanner.FileType) bool { return ft.IsCatia },
	counters: func(st *Stats) payloadCounters {
		return payloadCounters{done: &st.CatiaDone, skipped: &st.CatiaSkipped, failed: &st.CatiaFailed,
			bytes: &st.CatiaBytes, mirrorWritten: &st.CatiaArchiveWritten, mirrorFreed: &st.CatiaArchiveFreed}
	},
	totals: func(c state.Counters) payloadTotals {
		return payloadTotals{done: c.CatiaDone, skipped: c.CatiaSkipped, failed: c.CatiaFailed,
			bytes: c.CatiaBytes, mirrorWritten: c.CatiaWritten, mirrorFreed: c.CatiaFreed}
	},
	loadRows: func(path string) ([]report.PayloadRow, error) {
		rows, err := report.LoadCatiaFile(path)
		if rows == nil || err != nil {
			return nil, err
		}
		out := make([]report.PayloadRow, len(rows))
		for i, r := range rows {
			out[i] = r.PayloadRow
		}
		return out, nil
	},
	newSplit: func(s *Session) splitHooks { return &catiaSplit{s: s} },
}

// catiaSplit is the CATIA part of split: arxgo-catia.csv. Descriptions carry the catia: line
// through the description writer of the run's payload.
type catiaSplit struct{ s *Session }

// prepare stops the run on a corrupt operator registry before the first archive move.
func (v *catiaSplit) prepare(context.Context, *SplitConfig) error {
	_, err := loadCatiaRegistry(v.s)
	return err
}

func (v *catiaSplit) plan(context.Context, *SplitConfig, *Candidates) error { return nil }

func (v *catiaSplit) start(context.Context, *state.WAL) (postCommit, error) {
	return noPostCommit{}, nil
}

func (v *catiaSplit) writeRegistry(c SplitConfig) error {
	s := v.s
	if err := s.Phase(phaseReport, Totals{}); err != nil {
		return err
	}
	rows, err := collectCatiaRows(s, c)
	if err != nil {
		return err
	}
	var b strings.Builder
	if err := report.WriteCatiaCSV(&b, rows); err != nil {
		return err
	}
	if err := catiaPayload.writeRegistryCopies(s, []byte(b.String())); err != nil {
		return err
	}
	s.Log.Info("wrote catia registry", "catia", len(rows), "csv", scanner.CatiaRegistryName)
	return nil
}

// noPostCommit is the post-commit step of a payload without sidecars.
type noPostCommit struct{}

func (noPostCommit) committed(string) error { return nil }
func (noPostCommit) stop(err error) error   { return err }

// loadCatiaRegistry reads arxgo-catia.csv from the archive, or from the CATIA archive when the
// archive has none.
func loadCatiaRegistry(s *Session) ([]report.CatiaRow, error) {
	for _, path := range registryPaths(s, scanner.CatiaRegistryName) {
		rows, err := report.LoadCatiaFile(path)
		if err != nil {
			return nil, corruptRegistry(path, err)
		}
		if rows != nil {
			return rows, nil
		}
	}
	return nil, nil
}

// collectCatiaRows replays the CATIA run history onto the existing registry, adds the files this
// run skipped before a transaction began and completes cells from the file registry.
func collectCatiaRows(s *Session, c SplitConfig) ([]report.CatiaRow, error) {
	existing, err := loadCatiaRegistry(s)
	if err != nil {
		return nil, err
	}
	history, err := readHistory(s.cfg.Archive, PayloadCatia)
	if err != nil {
		return nil, err
	}
	byReg := fileRegistryRows(s, c)
	rows := report.MergeCatiaRows(nil, existing)
	for _, run := range history {
		split, restored := splitEvents(run)
		incoming := make([]report.CatiaRow, 0, len(split))
		for _, a := range split {
			row := report.CatiaRow{PayloadRow: a.payloadRow()}
			if a.catia != nil {
				row.Kind, row.Format, row.Release, row.Components = a.catia.Kind, a.catia.Format, a.catia.Release, a.catia.Components
			}
			incoming = append(incoming, row)
		}
		rows = report.MarkCatiaRestored(report.MergeCatiaRows(rows, incoming), restored, run.id)
	}
	var skipped []report.CatiaRow
	for _, p := range rowsFromIssues(s.Issues()) {
		skipped = append(skipped, report.CatiaRow{PayloadRow: p})
	}
	rows = report.MergeCatiaRows(rows, skipped)
	for i := range rows {
		if r, ok := byReg[rows[i].RelPath]; ok {
			fillPayloadRow(&rows[i].PayloadRow, r)
			if rows[i].MTime == "" {
				rows[i].MTime = r.Metadata.MTime
			}
		}
		finishURL(s, c.BaseURL, &rows[i].PayloadRow)
	}
	return rows, nil
}

// fillPayloadRow completes empty payload cells from the file registry row of the same file.
func fillPayloadRow(row *report.PayloadRow, r report.RegistryRow) {
	if row.FileName == "" {
		row.FileName = r.FileName
	}
	if row.FileSize == 0 {
		row.FileSize = r.FileSize
	}
	if row.FileMIME == "" {
		row.FileMIME = r.FileMIME
	}
}
