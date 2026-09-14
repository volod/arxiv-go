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

// previewSet maps an owning video to its previews. Both are slash paths relative to the main
// archive; the value is the published byte size, or zero while unknown.
type previewSet map[string]map[string]int64

func (s previewSet) put(video, preview string, size int64) {
	if s[video] == nil {
		s[video] = map[string]int64{}
	}
	s[video][preview] = size
}

func (s previewSet) remove(video, preview string) { delete(s[video], preview) }

func (s previewSet) has(video, preview string) bool {
	_, ok := s[video][preview]
	return ok
}

func (s previewSet) sorted(video string) []string {
	out := make([]string, 0, len(s[video]))
	for p := range s[video] {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}

// previewIndex is reconstructed from durable WAL events of every run, never from a guessed file
// name. owned holds completed previews with their sizes; generating and deleting hold events whose
// outcome was never logged.
type previewIndex struct {
	archive                     string
	owned, generating, deleting previewSet
}

func newPreviewIndex(archive string, history []runHistory) (*previewIndex, error) {
	idx := &previewIndex{archive: archive, owned: previewSet{}, generating: previewSet{}, deleting: previewSet{}}
	for _, run := range history {
		if err := idx.replay(run); err != nil {
			return nil, err
		}
	}
	return idx, nil
}

func (idx *previewIndex) replay(run runHistory) error {
	begins := map[string]string{} // txid -> preview path of an open event
	for _, rec := range run.records {
		if !isPreviewStep(rec.Step) {
			continue
		}
		preview, ok := run.archiveRel(rec.Dst)
		if !ok || !scanner.LocalRelPath(rec.RelPath) {
			return fmt.Errorf("%w: preview WAL path outside archive or reserved: %q", state.ErrStateCorrupt, rec.Dst)
		}
		video := rec.RelPath
		switch rec.Step {
		case state.StepPreviewBegin:
			begins[rec.TxID] = preview
			idx.generating.put(video, preview, 0)
		case state.StepPreviewDelete:
			begins[rec.TxID] = preview
			idx.deleting.put(video, preview, rec.Size)
		case state.StepPreviewDone, state.StepPreviewFailed:
			idx.generating.remove(video, begins[rec.TxID])
			if rec.Step == state.StepPreviewDone && rec.Size > 0 {
				idx.owned.put(video, preview, rec.Size)
			}
		case state.StepPreviewDeleted:
			idx.deleting.remove(video, preview)
			idx.owned.remove(video, preview)
		}
		if rec.Step != state.StepPreviewBegin && rec.Step != state.StepPreviewDelete {
			delete(begins, rec.TxID)
		}
	}
	return nil
}

func isPreviewStep(step state.Step) bool {
	switch step {
	case state.StepPreviewBegin, state.StepPreviewDone, state.StepPreviewFailed,
		state.StepPreviewDelete, state.StepPreviewDeleted:
		return true
	}
	return false
}

func (idx *previewIndex) abs(preview string) string {
	return filepath.Join(idx.archive, filepath.FromSlash(preview))
}

// published reports a completed preview whose file still has its recorded size.
func (idx *previewIndex) published(video, preview string) bool {
	want := idx.owned[video][preview]
	size, ok := nonEmptyFileSize(idx.abs(preview))
	return want > 0 && ok && size == want
}

// encoded is the previews column of the video registry.
func (idx *previewIndex) encoded(video string) string {
	return strings.Join(idx.owned.sorted(video), ";")
}

// skipPaths lists recorded previews, complete or not, so a split scan never makes one a candidate.
func (idx *previewIndex) skipPaths() []string {
	var out []string
	for _, set := range []previewSet{idx.owned, idx.generating, idx.deleting} {
		for video := range set {
			for _, preview := range set.sorted(video) {
				out = append(out, idx.abs(preview))
			}
		}
	}
	return out
}

// removeUnfinishedParts deletes the part files of previews whose generation never logged an
// outcome; the executor retries those previews.
func (idx *previewIndex) removeUnfinishedParts() error {
	for video := range idx.generating {
		for preview := range idx.generating[video] {
			err := os.Remove(media.PreviewPartPath(idx.abs(preview)))
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
