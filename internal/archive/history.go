package archive

import (
	"path/filepath"
	"sort"
	"time"

	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// runHistory is the durable WAL of one run with the archive root the run recorded. WAL records
// hold absolute paths of that root; archive-relative paths stay valid when the root is later
// mounted or renamed elsewhere.
type runHistory struct {
	id                    string
	archive, videoArchive string
	records               []state.Record
}

// archiveRel returns the local slash path of abs below the run's archive root.
func (h runHistory) archiveRel(abs string) (string, bool) { return localRel(h.archive, abs) }

// videoRel returns the local slash path of abs below the run's video archive root.
func (h runHistory) videoRel(abs string) (string, bool) { return localRel(h.videoArchive, abs) }

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

// readHistory reads the WAL of every run of root, ordered by the creation time in options.json,
// then by id (run ids alone order only to the second). A run without options sorts first and uses
// the current roots.
func readHistory(root, videoArchive string) ([]runHistory, error) {
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
		_ = state.ReadJSON(rd.File(state.OptionsFile), &o)
		records, err := state.ReadWALRecords(rd.File(state.WALFile))
		if err != nil {
			return nil, err
		}
		h := runHistory{id: id, archive: o.Archive, videoArchive: o.VideoArchive, records: records}
		if h.archive == "" {
			h.archive, h.videoArchive = root, videoArchive
		}
		runs = append(runs, run{h, o.CreatedAt})
	}
	sort.SliceStable(runs, func(i, j int) bool { return runs[i].created.Before(runs[j].created) })
	out := make([]runHistory, len(runs))
	for i, r := range runs {
		out[i] = r.runHistory
	}
	return out, nil
}
