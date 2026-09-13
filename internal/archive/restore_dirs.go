package archive

import (
	"errors"
	"io/fs"
	"os"
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

func pruneEmptyVideoDirs(root string) error {
	if root == "" {
		return nil
	}
	var dirs []string
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if !d.IsDir() {
			return nil
		}
		if path != root && d.Name() == state.DirName {
			return fs.SkipDir
		}
		dirs = append(dirs, path)
		return nil
	})
	if err != nil {
		return err
	}
	sort.Slice(dirs, func(i, j int) bool {
		return strings.Count(dirs[i], string(os.PathSeparator)) > strings.Count(dirs[j], string(os.PathSeparator))
	})
	for _, dir := range dirs {
		if filepath.Clean(dir) == filepath.Clean(root) {
			continue
		}
		ents, err := os.ReadDir(dir)
		if err != nil {
			return err
		}
		if len(ents) > 0 {
			continue
		}
		if err := os.Remove(dir); err != nil && !errors.Is(err, fs.ErrNotExist) {
			return err
		}
	}
	return nil
}
