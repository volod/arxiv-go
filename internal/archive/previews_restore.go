package archive

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

// restorePreviewsBeforeExecute removes part files of unfinished preview generation, finishes
// interrupted deletions and, with --previews delete, deletes the previews of videos an earlier
// process of this run already restored.
func restorePreviewsBeforeExecute(s *Session, w *state.WAL, idx *previewIndex, deleteRestored bool) error {
	if err := idx.removeUnfinishedParts(); err != nil {
		return err
	}
	if err := resumePreviewDeletes(s, w, idx); err != nil {
		return err
	}
	if !deleteRestored {
		return nil
	}
	for _, video := range w.Committed().Paths() {
		if err := deletePreviews(s, w, idx, video); err != nil {
			return err
		}
	}
	return nil
}

// resumePreviewDeletes finishes deletions whose preview_delete was logged by an earlier process.
func resumePreviewDeletes(s *Session, w *state.WAL, idx *previewIndex) error {
	for _, video := range sortedKeys(idx.deleting) {
		for _, preview := range idx.deleting.sorted(video) {
			if err := deletePreview(s, w, idx, video, preview); err != nil {
				return err
			}
		}
		if err := refreshPreviewDescription(s, idx, video); err != nil {
			return err
		}
	}
	return nil
}

// deletePreviews removes the recorded previews of a restored video, then updates a kept description.
func deletePreviews(s *Session, w *state.WAL, idx *previewIndex, video string) error {
	for _, preview := range idx.owned.sorted(video) {
		if err := deletePreview(s, w, idx, video, preview); err != nil {
			return err
		}
	}
	return refreshPreviewDescription(s, idx, video)
}

// deletePreview removes one preview only when it is still a regular file with the recorded size;
// a changed file is kept and reported. preview_delete is durable before the removal, and a file
// already gone after a logged intent completes the deletion.
func deletePreview(s *Session, w *state.WAL, idx *previewIndex, video, preview string) error {
	want := idx.owned[video][preview]
	if want <= 0 {
		want = idx.deleting[video][preview]
	}
	abs := idx.abs(preview)
	if err := noSymlinkParents(s.cfg.Archive, abs); err != nil {
		return err
	}
	fi, err := os.Lstat(abs)
	switch {
	case os.IsNotExist(err):
	case err != nil:
		return err
	case !fi.Mode().IsRegular() || fi.Size() != want:
		s.Issue(state.IssueSkipped, video, fmt.Sprintf("preview size or type changed; kept %s", preview))
		return nil
	}
	begin, err := w.BeginEvent(idx.family, video, abs, want, true)
	if err != nil {
		return err
	}
	if fi != nil {
		if err := os.Remove(abs); err != nil && !os.IsNotExist(err) {
			return err
		}
		if err := fsops.SyncDir(filepath.Dir(abs)); err != nil {
			return err
		}
		s.Stats.ArchiveFreed.Add(want)
	}
	if _, err := w.FinishEvent(idx.family, begin.TxID, idx.family.Deleted, "", want, ""); err != nil {
		return err
	}
	idx.owned.remove(video, preview)
	idx.deleting.remove(video, preview)
	return nil
}

// noSymlinkParents requires every parent of path up to root to be a real directory, so a deletion
// never follows a link out of the archive.
func noSymlinkParents(root, path string) error {
	for dir := filepath.Dir(path); dir != root; {
		fi, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("preview parent is not a directory: %s", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("preview path outside archive: %s", path)
		}
		dir = parent
	}
	return nil
}
