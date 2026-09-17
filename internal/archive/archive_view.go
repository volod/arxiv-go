package archive

import (
	"maps"
	"path/filepath"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// archiveView is what the run history of both payloads says about the archive root: the payload
// files split moved out, which keep preserved registry rows, and the owned artifacts (descriptions,
// previews, text sidecars), which have none. See
// docs/openspec/stage-1-core/registry.md#archive-view.
type archiveView struct {
	archive string
	moved   map[string]movedFile // rel_path -> the move that holds it out of the archive
	// restored maps rel_path to the commit time of the restore that returned a moved file last.
	restored map[string]time.Time
	owned    []string // OS paths of owned artifacts, excluded from traversal
	history  map[PayloadKind][]runHistory
}

// movedFile is a payload file whose replayed status is moved.
type movedFile struct {
	kind   PayloadKind
	mirror string    // mirror root recorded by the run that last moved the file
	size   int64     // size in that run's begin record
	mtime  time.Time // modification time in that run's begin record
}

// viewPayloads are the payload kinds replayed into the archive view, in a fixed order.
var viewPayloads = []PayloadKind{PayloadVideo, PayloadCatia}

// loadArchiveView replays the WAL of every split and restore run of archive. It reads no payload
// registry, so an edited or deleted arxgo-videos.csv or arxgo-catia.csv changes nothing.
func loadArchiveView(archive string) (*archiveView, error) {
	v := &archiveView{archive: archive, moved: map[string]movedFile{}, restored: map[string]time.Time{},
		history: map[PayloadKind][]runHistory{}}
	for _, kind := range viewPayloads {
		history, err := readHistory(archive, kind)
		if err != nil {
			return nil, err
		}
		v.history[kind] = history
		moved, restored := replayMoves(kind, history)
		maps.Copy(v.moved, moved)
		maps.Copy(v.restored, restored)
		v.owned = append(v.owned, ownedDescriptions(archive, history)...)
		idx, err := sidecarIndex(kind, archive, history)
		if err != nil {
			return nil, err
		}
		v.owned = append(v.owned, idx.skipPaths()...)
	}
	return v, nil
}

// sidecarIndex is the post-commit sidecar family of a payload: previews for video, text sidecars
// for CATIA.
func sidecarIndex(kind PayloadKind, archive string, history []runHistory) (*eventIndex, error) {
	if kind == PayloadCatia {
		return newTextIndex(archive, history)
	}
	return newPreviewIndex(archive, history)
}

// replayMoves replays the runs of one payload in start order, with the payload registry rules: a
// committed split makes a file moved (an abort never undoes that), a committed restore takes it
// back. It returns the files still moved and, for the files whose last event was a restore, the
// time that restore committed.
func replayMoves(kind PayloadKind, history []runHistory) (map[string]movedFile, map[string]time.Time) {
	moved := map[string]movedFile{}
	restored := map[string]time.Time{}
	for _, run := range history {
		split, back := splitEvents(run)
		for rel, a := range split {
			if a.status == report.StatusMoved && scanner.LocalRelPath(rel) {
				moved[rel] = movedFile{kind: kind, mirror: run.mirror, size: a.begin.Size, mtime: a.begin.Mtime}
				delete(restored, rel)
			}
		}
		if len(back) == 0 {
			continue
		}
		committed := restoreCommitTimes(run)
		for rel := range back {
			if _, ok := moved[rel]; ok {
				delete(moved, rel)
				restored[rel] = committed[rel]
			}
		}
	}
	return moved, restored
}

// restoreCommitTimes maps each rel_path a restore run committed to the time of its commit record.
func restoreCommitTimes(run runHistory) map[string]time.Time {
	begins := map[string]string{} // txid -> rel_path
	out := map[string]time.Time{}
	for _, rec := range run.records {
		switch {
		case rec.Step == state.StepBegin && rec.Op == opRestore:
			begins[rec.TxID] = rec.RelPath
		case rec.Step == state.StepCommit && begins[rec.TxID] != "":
			out[begins[rec.TxID]] = rec.TS
		}
	}
	return out
}

// ownedDescriptions lists the descriptions the history records as written and not removed since.
// A restore records the owned description both when it removes it and when --descriptions keep
// leaves it, so a description a restore recorded is still owned while its marker names its file.
func ownedDescriptions(archive string, history []runHistory) []string {
	owned := map[string]string{}    // archive-relative description -> owning rel_path
	restored := map[string]string{} // recorded by a restore after the last split wrote it
	for _, run := range history {
		begins := map[string]string{} // txid -> rel_path
		for _, rec := range run.records {
			if rec.Step == state.StepBegin {
				begins[rec.TxID] = rec.RelPath
				continue
			}
			if rec.Description == "" || (rec.Step != state.StepDescribed && rec.Step != state.StepDescriptionRemoved) {
				continue
			}
			desc, ok := run.archiveRel(rec.Description)
			owner := begins[rec.TxID]
			if !ok || owner == "" || !scanner.LocalRelPath(desc) {
				continue
			}
			if rec.Step == state.StepDescribed {
				owned[desc] = owner
				delete(restored, desc)
			} else {
				delete(owned, desc)
				restored[desc] = owner
			}
		}
	}
	out := make([]string, 0, len(owned)+len(restored))
	for desc := range owned {
		out = append(out, osPath(archive, desc))
	}
	for desc, owner := range restored {
		p := osPath(archive, desc)
		if occ, err := report.InspectDescription(p, owner); err == nil && occ == report.DescriptionOwned {
			out = append(out, p)
		}
	}
	return out
}

func osPath(root, rel string) string { return filepath.Join(root, filepath.FromSlash(rel)) }

// locationOf is the file-registry location of a payload's moved files.
func locationOf(kind PayloadKind) string {
	if kind == PayloadCatia {
		return report.LocationCatiaArchive
	}
	return report.LocationVideoArchive
}
