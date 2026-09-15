package archive

import (
	"context"
	"slices"
	"strings"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// catiaRestore is the CATIA part of restore: arxgo-catia.csv rows and, with --descriptions delete,
// removal of owned text sidecars.
type catiaRestore struct {
	s        *Session
	history  []runHistory // CATIA runs before this restore started executing
	idx      *eventIndex
	existing []report.CatiaRow
	byRel    map[string]report.PayloadRow
	deletion bool // --descriptions delete
}

func newCatiaRestore(_ context.Context, s *Session, c *RestoreConfig) (restoreHooks, error) {
	existing, err := loadCatiaRegistry(s)
	if err != nil {
		return nil, err
	}
	history, err := readHistory(s.cfg.Archive, PayloadCatia)
	if err != nil {
		return nil, err
	}
	idx, err := newTextIndex(s.cfg.Archive, history)
	if err != nil {
		return nil, err
	}
	rows := make([]report.PayloadRow, len(existing))
	for i, r := range existing {
		rows[i] = r.PayloadRow
	}
	return &catiaRestore{s: s, history: history, idx: idx, existing: existing, byRel: payloadRowsByPath(s, rows),
		deletion: !c.KeepDescriptions}, nil
}

func (v *catiaRestore) rows() map[string]report.PayloadRow { return v.byRel }

// include keeps every file the CATIA registry records as moved or restored a candidate, whatever
// its extension now classifies as.
func (v *catiaRestore) include(rel string) bool {
	row, ok := v.byRel[rel]
	return ok && (row.Status == report.StatusMoved || row.Status == report.StatusRestored)
}

func (v *catiaRestore) cleanup(w *state.WAL) sidecarCleanup {
	return sidecarCleanup{s: v.s, w: w, idx: v.idx, owns: func(abs, owner string) (bool, error) {
		occ, err := report.InspectTextSidecar(abs, owner)
		return occ == report.DescriptionOwned, err
	}}
}

// beforeExecute finishes interrupted text sidecar work and, with --descriptions delete, deletes the
// sidecars of files this run already restored. It also deletes the sidecars of files whose last
// transaction is a restore committed by an earlier run that used --descriptions delete but did not
// clean up, because it was interrupted and another run (possibly of the video payload) recovered it.
func (v *catiaRestore) beforeExecute(w *state.WAL) error {
	c := v.cleanup(w)
	if err := c.beforeExecute(v.deletion); err != nil {
		return err
	}
	if !v.deletion {
		return nil
	}
	c.quiet = true
	for _, rel := range v.earlierRestored() {
		if err := c.deleteAll(rel); err != nil {
			return err
		}
	}
	return nil
}

// earlierRestored lists the files with owned sidecars whose last committed transaction in CATIA
// history is a restore by an earlier run whose recorded options delete descriptions.
func (v *catiaRestore) earlierRestored() []string {
	s := v.s
	last := map[string]*runHistory{}
	for i := range v.history {
		run := &v.history[i]
		split, restored := splitEvents(*run)
		for rel, a := range split {
			if a.status == report.StatusMoved {
				last[rel] = nil
			}
		}
		for rel := range restored {
			last[rel] = run
		}
	}
	deletes := map[string]bool{} // run id -> its restore deleted descriptions
	var out []string
	for rel, run := range last {
		if run == nil || run.id == s.Run.ID || len(v.idx.owned[rel]) == 0 {
			continue
		}
		del, ok := deletes[run.id]
		if !ok {
			del = v.deletedDescriptions(*run)
			deletes[run.id] = del
		}
		if del {
			out = append(out, rel)
		}
	}
	slices.Sort(out)
	return out
}

// deletedDescriptions reports whether an earlier restore run was recorded with --descriptions
// delete, from the resolver its options rebuild.
func (v *catiaRestore) deletedDescriptions(run runHistory) bool {
	if v.s.cfg.RecovererFor == nil || run.op != opRestore {
		return false
	}
	res, err := v.s.cfg.RecovererFor(run.op, PayloadCatia, run.options)
	if err != nil {
		v.s.Log.Warn("text sidecar cleanup: options of an earlier restore unreadable", "previous_run", run.id, "error", err)
		return false
	}
	r, ok := res.(RestoreResolver)
	return ok && !r.KeepDescriptions
}

func (v *catiaRestore) committed(w *state.WAL, rel string) error {
	if !v.deletion {
		return nil
	}
	return v.cleanup(w).deleteAll(rel)
}

// updateRegistry marks restored rows in both arxgo-catia.csv copies by replaying every CATIA run,
// and retires both copies when nothing is moved any more and descriptions were deleted. Restore
// never creates a CATIA registry.
func (v *catiaRestore) updateRegistry(c RestoreConfig) error {
	s := v.s
	if err := s.Phase(phaseReport, Totals{}); err != nil {
		return err
	}
	if v.existing == nil {
		s.Log.Info("no catia registry to update")
		return nil
	}
	history, err := readHistory(s.cfg.Archive, PayloadCatia)
	if err != nil {
		return err
	}
	rows := replayCatiaRows(v.existing, history)
	moved := false
	for i := range rows {
		finishURL(s, "", &rows[i].PayloadRow)
		if scanner.LocalRelPath(rows[i].RelPath) {
			rows[i].TextRelPath = v.idx.ownedPath(rows[i].RelPath)
		}
		moved = moved || rows[i].Status == report.StatusMoved
	}
	var b strings.Builder
	if err := report.WriteCatiaCSV(&b, rows); err != nil {
		return err
	}
	retire := !c.KeepDescriptions && !moved
	if err := writeRestoredRegistry(s, scanner.CatiaRegistryName, []byte(b.String()), retire); err != nil {
		return err
	}
	s.Log.Info("updated catia registry", "catia", len(rows), "retired", retire)
	return nil
}

// replayCatiaRows applies the transactions of every CATIA run to the existing registry rows, run
// by run in start order, with the video status rules and the CATIA summary of described records.
func replayCatiaRows(existing []report.CatiaRow, history []runHistory) []report.CatiaRow {
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
	return rows
}
