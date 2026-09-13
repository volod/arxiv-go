package archive

import (
	"errors"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"

	"github.com/volod/arxiv-go/internal/fsops"
	"github.com/volod/arxiv-go/internal/state"
)

func ensureRestoreDirs(s *Session, dst string, create bool) (missing bool, err error) {
	dir := filepath.Dir(dst)
	fi, err := os.Lstat(dir)
	if err == nil {
		if !fi.IsDir() {
			return false, errors.New("destination parent is not a directory: " + dir)
		}
		return false, nil
	}
	if !errors.Is(err, fs.ErrNotExist) {
		return false, err
	}
	if !create {
		return true, nil
	}
	rel, err := filepath.Rel(s.cfg.Archive, dir)
	if err != nil {
		return false, err
	}
	if rel == "." {
		return false, nil
	}
	var chain []string
	for p := rel; p != "."; p = filepath.Dir(p) {
		chain = append(chain, p)
	}
	for i := len(chain) - 1; i >= 0; i-- {
		path := filepath.Join(s.cfg.Archive, chain[i])
		if st, e := os.Lstat(path); e == nil {
			if !st.IsDir() {
				return false, errors.New("destination parent is not a directory: " + path)
			}
			continue
		} else if !errors.Is(e, fs.ErrNotExist) {
			return false, e
		}
		perm := fs.FileMode(0o755)
		if srcDir := filepath.Join(s.cfg.VideoArchive, chain[i]); srcDir != "" {
			if sf, e := os.Stat(srcDir); e == nil && sf.IsDir() {
				perm = sf.Mode().Perm()
			}
		}
		if e := os.Mkdir(path, perm); e != nil && !errors.Is(e, fs.ErrExist) {
			return false, e
		}
		if runtime.GOOS != "windows" {
			if e := os.Chmod(path, perm); e != nil && !errors.Is(e, fs.ErrPermission) && !errors.Is(e, errors.ErrUnsupported) {
				return false, e
			}
		}
		if e := fsops.SyncDir(filepath.Dir(path)); e != nil {
			return false, e
		}
		if e := hitSplit(s.cfg.Crash, "fs:mkdir"); e != nil {
			return false, e
		}
	}
	return false, nil
}

// pruneRestoredDirs removes the video-archive directories that held restored videos and are now
// empty, deepest first, and their ancestors that become empty, never the video archive root.
// Restores committed by any run count, so a run that replaced an interrupted restore also cleans
// up after it. The cleanup is best effort: a directory that cannot be read or removed is logged and
// kept, so an unrelated unreadable directory never fails the run.
func pruneRestoredDirs(s *Session) error {
	runs, err := runsInStartOrder(s.cfg.Archive)
	if err != nil {
		return err
	}
	dirs := map[string]struct{}{}
	for _, rd := range runs {
		recs, err := state.ReadWALRecords(rd.File(state.WALFile))
		if err != nil {
			s.Log.Warn("video archive cleanup: run log unreadable", "run_id", rd.ID, "error", err)
			continue
		}
		src := map[string]string{}
		for _, rec := range recs {
			switch {
			case rec.Step == state.StepBegin && rec.Op == opRestore:
				src[rec.TxID] = rec.Src
			case rec.Step == state.StepCommit && src[rec.TxID] != "":
				rel, err := filepath.Rel(s.cfg.VideoArchive, filepath.Dir(src[rec.TxID]))
				if err != nil || rel == "." || !filepath.IsLocal(rel) {
					continue // a video at the root, or one restored from another video archive
				}
				for d := filepath.ToSlash(rel); d != "."; d = path.Dir(d) {
					dirs[d] = struct{}{}
				}
			}
		}
	}
	ordered := make([]string, 0, len(dirs))
	for d := range dirs {
		ordered = append(ordered, d)
	}
	sort.Slice(ordered, func(i, j int) bool {
		if di, dj := strings.Count(ordered[i], "/"), strings.Count(ordered[j], "/"); di != dj {
			return di > dj
		}
		return ordered[i] < ordered[j]
	})
	for _, rel := range ordered {
		dir := filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(rel))
		ents, err := os.ReadDir(dir)
		if errors.Is(err, fs.ErrNotExist) || (err == nil && len(ents) > 0) {
			continue
		}
		if err == nil {
			err = os.Remove(dir)
		}
		if err != nil && !errors.Is(err, fs.ErrNotExist) {
			s.Log.Warn("video archive directory not removed", "dir", rel, "error", err)
		}
	}
	return nil
}
