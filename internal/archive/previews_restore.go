package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// sidecarCleanup deletes the owned sidecars of one event family (video previews or CATIA text) at
// restored locations under delete and deleted WAL events.
type sidecarCleanup struct {
	s   *Session
	w   *state.WAL
	idx *eventIndex
	// owns, when set, must confirm that a present file with the recorded size is still the owned
	// sidecar of owner; a file it rejects is kept and reported.
	owns func(abs, owner string) (bool, error)
	// refresh, when set, runs after the sidecars of owner were deleted.
	refresh func(owner string) error
	// quiet logs a kept changed sidecar instead of reporting an issue, for files an earlier run
	// restored and already reported.
	quiet bool
}

// previewCleanup deletes previews and then updates the kept description's preview links.
func previewCleanup(s *Session, w *state.WAL, idx *previewIndex) sidecarCleanup {
	return sidecarCleanup{s: s, w: w, idx: idx,
		refresh: func(video string) error { return refreshPreviewDescription(s, idx, video) }}
}

// restorePreviewsBeforeExecute removes part files of unfinished preview generation, finishes
// interrupted deletions and, with --previews delete, deletes the previews of videos an earlier
// process of this run already restored and of videos a replaced earlier restore returned.
func restorePreviewsBeforeExecute(s *Session, w *state.WAL, idx *previewIndex, history []runHistory, deleteRestored bool) error {
	c := previewCleanup(s, w, idx)
	if err := c.beforeExecute(deleteRestored); err != nil || !deleteRestored {
		return err
	}
	return c.deleteEarlierRestored(history)
}

// deletePreviews removes the recorded previews of a restored video, then updates a kept description.
func deletePreviews(s *Session, w *state.WAL, idx *previewIndex, video string) error {
	return previewCleanup(s, w, idx).deleteAll(video)
}

// beforeExecute removes part files of unfinished generation, finishes deletions whose delete event
// an earlier process logged and, with deleteRestored, deletes the sidecars of files this run
// already restored before it was interrupted.
func (c sidecarCleanup) beforeExecute(deleteRestored bool) error {
	if err := c.idx.removeUnfinishedParts(); err != nil {
		return err
	}
	for _, owner := range sortedKeys(c.idx.deleting) {
		for _, sidecar := range c.idx.deleting.sorted(owner) {
			if err := c.deleteOne(owner, sidecar); err != nil {
				return err
			}
		}
		if err := c.afterDelete(owner); err != nil {
			return err
		}
	}
	if !deleteRestored {
		return nil
	}
	for _, owner := range c.w.Committed().Paths() {
		if err := c.deleteAll(owner); err != nil {
			return err
		}
	}
	return nil
}

// deleteEarlierRestored deletes, quietly, the owned sidecars of every file whose last transaction
// in history is a restore committed by an earlier run that recorded sidecar_cleanup: true. Such a
// run was interrupted between the commit and its sidecar deletion and then replaced by another run
// (other options, --new-run, or a command of the other payload), so no restore candidate remains
// for the file. A file split again after that restore is excluded.
func (c sidecarCleanup) deleteEarlierRestored(history []runHistory) error {
	last := map[string]*runHistory{}
	for i := range history {
		run := &history[i]
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
	var owners []string
	for rel, run := range last {
		if run != nil && run.id != c.s.Run.ID && run.sidecarCleanup && len(c.idx.owned[rel]) > 0 {
			owners = append(owners, rel)
		}
	}
	slices.Sort(owners)
	c.quiet = true
	for _, owner := range owners {
		c.s.Log.Info(c.idx.family.Name+" cleanup of a file an earlier interrupted restore returned", "rel_path", owner)
		if err := c.deleteAll(owner); err != nil {
			return err
		}
	}
	return nil
}

// deleteAll removes the recorded sidecars of a restored file.
func (c sidecarCleanup) deleteAll(owner string) error {
	for _, sidecar := range c.idx.owned.sorted(owner) {
		if err := c.deleteOne(owner, sidecar); err != nil {
			return err
		}
	}
	return c.afterDelete(owner)
}

func (c sidecarCleanup) afterDelete(owner string) error {
	if c.refresh == nil {
		return nil
	}
	return c.refresh(owner)
}

// deleteOne removes one sidecar only when it is still a regular file with the recorded size (and
// owned, for a family that checks it); a changed file is kept and reported. The delete event is
// durable before the removal, and a file already gone after a logged intent completes the deletion.
func (c sidecarCleanup) deleteOne(owner, sidecar string) error {
	s, idx, name := c.s, c.idx, c.idx.family.Name
	want := idx.owned[owner][sidecar]
	if want <= 0 {
		want = idx.deleting[owner][sidecar]
	}
	abs := idx.abs(sidecar)
	err := noSymlinkParents(s.cfg.Archive, abs)
	var fi os.FileInfo
	if err == nil {
		fi, err = os.Lstat(abs)
	}
	switch {
	case os.IsNotExist(err): // the sidecar or a parent directory is gone
		fi = nil
	case err != nil:
		return err
	case !fi.Mode().IsRegular() || fi.Size() != want:
		c.kept(owner, fmt.Sprintf("%s size or type changed; kept %s", name, sidecar))
		return nil
	case c.owns != nil:
		owned, err := c.owns(abs, owner)
		if err != nil {
			return err
		}
		if !owned {
			c.kept(owner, fmt.Sprintf("%s is no longer owned; kept %s", name, sidecar))
			return nil
		}
	}
	begin, err := c.w.BeginEvent(idx.family, owner, abs, want, true)
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
	if _, err := c.w.FinishEvent(idx.family, begin.TxID, idx.family.Deleted, "", want, ""); err != nil {
		return err
	}
	idx.owned.remove(owner, sidecar)
	idx.deleting.remove(owner, sidecar)
	if idx.last[owner] == sidecar {
		delete(idx.last, owner)
	}
	return nil
}

func (c sidecarCleanup) kept(owner, reason string) {
	if c.quiet {
		c.s.Log.Info(reason, "rel_path", owner)
		return
	}
	c.s.Issue(state.IssueSkipped, owner, reason)
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
			return fmt.Errorf("sidecar parent is not a directory: %s", dir)
		}
		parent := filepath.Dir(dir)
		if parent == dir {
			return fmt.Errorf("sidecar path outside archive: %s", path)
		}
		dir = parent
	}
	return nil
}
