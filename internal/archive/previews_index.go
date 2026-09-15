package archive

import (
	"fmt"
	"os"
	"path"
	"path/filepath"
	"slices"
	"strings"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// eventSet maps an owning file to its sidecars. Both are slash paths relative to the main archive;
// the value is the published byte size, or zero while unknown.
type eventSet map[string]map[string]int64

func (s eventSet) put(owner, sidecar string, size int64) {
	if s[owner] == nil {
		s[owner] = map[string]int64{}
	}
	s[owner][sidecar] = size
}

func (s eventSet) remove(owner, sidecar string) { delete(s[owner], sidecar) }

func (s eventSet) has(owner, sidecar string) bool {
	_, ok := s[owner][sidecar]
	return ok
}

func (s eventSet) sorted(owner string) []string {
	out := make([]string, 0, len(s[owner]))
	for p := range s[owner] {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// eventIndex is the state of one post-commit sidecar family (for example previews), reconstructed
// from the durable WAL events of every run of a payload, never from a guessed file name. owned
// holds completed sidecars with their sizes; generating and deleting hold events whose outcome was
// never logged. partPath names the part file a generation writes before publishing.
type eventIndex struct {
	family                      state.EventFamily
	archive                     string
	partPath                    func(string) string
	owned, generating, deleting eventSet
}

// previewIndex is the event index of video previews.
type previewIndex = eventIndex

func newPreviewIndex(archive string, history []runHistory) (*previewIndex, error) {
	return newEventIndex(state.PreviewEvents, archive, media.PreviewPartPath, history)
}

func newEventIndex(family state.EventFamily, archive string, partPath func(string) string, history []runHistory) (*eventIndex, error) {
	idx := &eventIndex{family: family, archive: archive, partPath: partPath,
		owned: eventSet{}, generating: eventSet{}, deleting: eventSet{}}
	for _, run := range history {
		if err := idx.replay(run); err != nil {
			return nil, err
		}
	}
	return idx, nil
}

func (idx *eventIndex) replay(run runHistory) error {
	f := idx.family
	begins := map[string]string{} // txid -> sidecar path of an open event
	for _, rec := range run.records {
		if !f.Has(rec.Step) {
			continue
		}
		sidecar, ok := run.archiveRel(rec.Dst)
		if !ok || !scanner.LocalRelPath(rec.RelPath) {
			return fmt.Errorf("%w: %s WAL path outside archive or reserved: %q", state.ErrStateCorrupt, f.Name, rec.Dst)
		}
		owner := rec.RelPath
		switch rec.Step {
		case f.Begin:
			begins[rec.TxID] = sidecar
			idx.generating.put(owner, sidecar, 0)
		case f.Delete:
			begins[rec.TxID] = sidecar
			idx.deleting.put(owner, sidecar, rec.Size)
		case f.Done, f.Failed:
			idx.generating.remove(owner, begins[rec.TxID])
			if rec.Step == f.Done && rec.Size > 0 {
				idx.owned.put(owner, sidecar, rec.Size)
			}
		case f.Deleted:
			idx.deleting.remove(owner, sidecar)
			idx.owned.remove(owner, sidecar)
		}
		if !f.Opens(rec.Step) {
			delete(begins, rec.TxID)
		}
	}
	return nil
}

func (idx *eventIndex) abs(preview string) string {
	return filepath.Join(idx.archive, filepath.FromSlash(preview))
}

// published reports a completed sidecar whose file still has its recorded size.
func (idx *eventIndex) published(video, preview string) bool {
	want := idx.owned[video][preview]
	size, ok := nonEmptyFileSize(idx.abs(preview))
	return want > 0 && ok && size == want
}

// encoded is the registry column of the owner's completed sidecars (previews of a video).
func (idx *eventIndex) encoded(video string) string {
	return strings.Join(idx.owned.sorted(video), ";")
}

// skipPaths lists recorded sidecars, complete or not, so a split scan never makes one a candidate.
func (idx *eventIndex) skipPaths() []string {
	var out []string
	for _, set := range []eventSet{idx.owned, idx.generating, idx.deleting} {
		for video := range set {
			for _, preview := range set.sorted(video) {
				out = append(out, idx.abs(preview))
			}
		}
	}
	return out
}

// removeUnfinishedParts deletes the part files of sidecars whose generation never logged an
// outcome; the executor retries them.
func (idx *eventIndex) removeUnfinishedParts() error {
	for video := range idx.generating {
		for preview := range idx.generating[video] {
			err := os.Remove(idx.partPath(idx.abs(preview)))
			if err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}

// previewPathFor returns the archive-relative path of a planned preview next to its video.
func previewPathFor(video, name string) string { return path.Join(path.Dir(video), name) }

func nonEmptyFileSize(p string) (int64, bool) {
	fi, err := os.Lstat(p)
	if err != nil || !fi.Mode().IsRegular() || fi.Size() == 0 {
		return 0, false
	}
	return fi.Size(), true
}
