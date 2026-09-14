package archive

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

func resumePreviewDeletes(s *Session, w *state.WAL, idx *previewIndex) error {
	for rel, byPath := range idx.pending {
		for path, generating := range byPath {
			if generating {
				continue
			}
			if err := deleteRecordedPreview(s, w, idx, rel, path); err != nil {
				return err
			}
		}
	}
	return nil
}

func deleteRecordedPreviews(s *Session, w *state.WAL, idx *previewIndex, rel string) error {
	for _, path := range idx.list(rel) {
		if err := deleteRecordedPreview(s, w, idx, rel, path); err != nil {
			return err
		}
	}
	return nil
}

// deleteRecordedPreview refuses directories and links, and checks the recorded byte size before
// removal. The begin event is durable before the delete and a repeated deletion is idempotent.
func deleteRecordedPreview(s *Session, w *state.WAL, idx *previewIndex, rel, path string) error {
	want := idx.owned[rel][path]
	if want <= 0 {
		return nil
	}
	abs := filepath.Join(s.cfg.Archive, filepath.FromSlash(path))
	if err := noSymlinkParents(s.cfg.Archive, abs); err != nil {
		return err
	}
	fi, err := os.Lstat(abs)
	if os.IsNotExist(err) {
		// The previous process may have removed the file after preview_delete was logged.
		return recordPreviewDeletion(s, w, idx, rel, path, want, false)
	}
	if err != nil {
		return err
	}
	if !fi.Mode().IsRegular() || fi.Size() != want {
		s.Issue(state.IssueSkipped, rel, fmt.Sprintf("preview size or type changed; kept %s", path))
		return nil
	}
	return recordPreviewDeletion(s, w, idx, rel, path, want, true)
}

func recordPreviewDeletion(s *Session, w *state.WAL, idx *previewIndex, rel, path string, want int64, remove bool) error {
	abs := filepath.Join(s.cfg.Archive, filepath.FromSlash(path))
	begin, err := w.BeginPreview(rel, abs, want, true)
	if err != nil {
		return err
	}
	if remove {
		if err := os.Remove(abs); err != nil {
			return err
		}
		if err := fsops.SyncDir(filepath.Dir(abs)); err != nil {
			return err
		}
		s.Stats.ArchiveFreed.Add(want)
	}
	delete(idx.owned[rel], path)
	if err := refreshPreviewStub(s, rel, idx.list(rel)); err != nil {
		return err
	}
	if _, err := w.FinishPreview(begin.TxID, state.StepPreviewDeleted, abs, want, ""); err != nil {
		return err
	}
	delete(idx.pending[rel], path)
	return nil
}

func noSymlinkParents(root, path string) error {
	dir := filepath.Dir(path)
	for {
		fi, err := os.Lstat(dir)
		if err != nil {
			return err
		}
		if !fi.IsDir() {
			return fmt.Errorf("preview parent is not a directory: %s", dir)
		}
		if dir == root {
			return nil
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("preview path outside archive: %s", path)
		}
		dir = parent
	}
}
