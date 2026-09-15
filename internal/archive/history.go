package archive

import (
	"encoding/json"
	"path/filepath"
	"sort"
	"time"

	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// runHistory is the durable WAL of one run with the archive and mirror roots the run recorded. WAL
// records hold absolute paths of those roots; root-relative paths stay valid when a root is later
// mounted or renamed elsewhere.
type runHistory struct {
	id, op          string
	archive, mirror string
	options         json.RawMessage // every validated option the run recorded
	records         []state.Record
}

// archiveRel returns the local slash path of abs below the run's archive root.
func (h runHistory) archiveRel(abs string) (string, bool) { return localRel(h.archive, abs) }

// mirrorRel returns the local slash path of abs below the run's mirror root.
func (h runHistory) mirrorRel(abs string) (string, bool) { return localRel(h.mirror, abs) }

func localRel(root, abs string) (string, bool) {
	if root == "" || !filepath.IsAbs(abs) {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	return rel, scanner.LocalRelPath(rel)
}

// readHistory reads the WAL of every run of root whose options.json names payload kind, ordered by
// the creation time in options.json, then by id (run ids alone order only to the second). Runs of
// another payload, scans and runs without readable options are not part of the kind's history.
func readHistory(root string, kind PayloadKind) ([]runHistory, error) {
	ids, err := state.ListRunIDs(root)
	if err != nil {
		return nil, err
	}
	type run struct {
		runHistory
		created time.Time
	}
	runs := make([]run, 0, len(ids))
	for _, id := range ids {
		rd, err := state.OpenRunDir(root, id)
		if err != nil {
			return nil, err
		}
		var o state.RunOptions
		if err := state.ReadJSON(rd.File(state.OptionsFile), &o); err != nil || PayloadKind(o.Payload) != kind {
			continue
		}
		records, err := state.ReadWALRecords(rd.File(state.WALFile))
		if err != nil {
			return nil, err
		}
		archive := o.Archive
		if archive == "" {
			archive = root
		}
		h := runHistory{id: id, op: o.Op, archive: archive, mirror: runPayload(o).Root, options: o.Options, records: records}
		runs = append(runs, run{h, o.CreatedAt})
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].created.Before(runs[j].created) })
	out := make([]runHistory, len(runs))
	for i, r := range runs {
		out[i] = r.runHistory
	}
	return out, nil
}
