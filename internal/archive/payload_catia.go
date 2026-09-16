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
// arxgo-catia.csv. --catia-text generates post-commit text sidecars; restore removes owned
// descriptions and sidecars with --descriptions delete.
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
	newSplit:   func(s *Session) splitHooks { return &catiaSplit{s: s} },
	newRestore: newCatiaRestore,
}

// catiaSplit is the CATIA part of split: arxgo-catia.csv and optional text sidecars.
type catiaSplit struct {
	s       *Session
	rows    []report.CatiaRow
	history []runHistory
	idx     *eventIndex
	text    bool
}

// prepare stops the run on a corrupt operator registry before the first archive move and
// reconstructs text-sidecar events of earlier CATIA runs so skip paths and catch-up are exact.
func (v *catiaSplit) prepare(_ context.Context, c *SplitConfig) error {
	rows, err := loadCatiaRegistry(v.s)
	if err != nil {
		return err
	}
	v.rows, v.text = rows, c.CatiaText
	if v.history, err = readHistory(v.s.cfg.Archive, PayloadCatia); err != nil {
		return err
	}
	if v.idx, err = newTextIndex(v.s.cfg.Archive, v.history); err != nil {
		return err
	}
	if !v.s.cfg.DryRun {
		if err := v.idx.removeUnfinishedParts(); err != nil {
			return err
		}
	}
	// Owned text sidecars have no file-registry row: the scan excludes them with every other owned
	// artifact (archive view).
	return nil
}

func (v *catiaSplit) plan(_ context.Context, c *SplitConfig, remaining *Candidates) error {
	if !c.CatiaText {
		return nil
	}
	n, err := estimateTextBytes(v)
	if err != nil {
		return err
	}
	remaining.TextBytes = n
	return nil
}

func (v *catiaSplit) start(ctx context.Context, w *state.WAL) (postCommit, error) {
	if !v.text {
		return noPostCommit{}, nil
	}
	return startTextSidecars(ctx, v, w)
}

func (v *catiaSplit) writeRegistry(c SplitConfig) error {
	s := v.s
	if err := s.Phase(phaseReport, Totals{}); err != nil {
		return err
	}
	rows, err := collectCatiaRows(s, c, v.idx)
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
func collectCatiaRows(s *Session, c SplitConfig, idx *eventIndex) ([]report.CatiaRow, error) {
	existing, err := loadCatiaRegistry(s)
	if err != nil {
		return nil, err
	}
	history, err := readHistory(s.cfg.Archive, PayloadCatia)
	if err != nil {
		return nil, err
	}
	byReg := fileRegistryRows(s, c)
	rows := replayCatiaRows(existing, history)
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
		if idx != nil {
			rows[i].TextRelPath = idx.ownedPath(rows[i].RelPath)
		}
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
