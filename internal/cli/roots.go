package cli

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// rootFS is the read-only filesystem view used by validation. Tests swap foldCase to exercise
// Windows comparison rules on any platform.
type rootFS struct {
	stat         func(string) (fs.FileInfo, error)
	abs          func(string) (string, error)
	evalSymlinks func(string) (string, error)
	foldCase     bool
}

func osRootFS() rootFS {
	return rootFS{
		stat:         os.Stat,
		abs:          filepath.Abs,
		evalSymlinks: filepath.EvalSymlinks,
		foldCase:     runtime.GOOS == "windows",
	}
}

// checkRoots validates --archive and, for split and restore, --video-archive. It returns the
// absolute root paths and whether the split video archive root still has to be created.
func checkRoots(op string, s *settings, fsys rootFS, v *validator) (archive, video string, videoMissing bool) {
	archive, archiveReal, ok, _ := checkDir(fsys, "--archive", s.archive, false, v)
	if op == OpScan {
		return archive, "", false
	}
	video, videoReal, videoOK, videoMissing := checkDir(fsys, "--video-archive", s.videoArchive, op == OpSplit, v)
	if ok && videoOK {
		a, b := archiveReal, videoReal
		if fsys.foldCase {
			a, b = strings.ToLower(a), strings.ToLower(b)
		}
		switch {
		case a == b:
			v.addf("--archive and --video-archive resolve to the same directory %q", archiveReal)
		case within(a, b):
			v.addf("--video-archive %q is inside --archive %q", video, archive)
		case within(b, a):
			v.addf("--archive %q is inside --video-archive %q", archive, video)
		}
	}
	return archive, video, videoMissing
}

// checkDir requires value to name an existing directory. With allowMissing, a missing final
// element is accepted when its parent directory exists. It returns the absolute path, the
// symlink-resolved path used for comparisons, whether both were determined, and whether the
// accepted directory is missing.
func checkDir(fsys rootFS, flagName, value string, allowMissing bool, v *validator) (abs, resolved string, ok, missing bool) {
	if value == "" {
		v.addf("%s is required", flagName)
		return "", "", false, false
	}
	abs, err := fsys.abs(value)
	if err != nil {
		v.addf("%s %q: %v", flagName, value, err)
		return "", "", false, false
	}
	fi, err := fsys.stat(abs)
	switch {
	case err == nil && !fi.IsDir():
		v.addf("%s %q is not a directory", flagName, abs)
		return abs, "", false, false
	case err == nil:
		if resolved, err = fsys.evalSymlinks(abs); err != nil {
			v.addf("%s %q: %v", flagName, abs, err)
			return abs, "", false, false
		}
		return abs, resolved, true, false
	case !errors.Is(err, fs.ErrNotExist):
		v.addf("%s %q: %v", flagName, abs, err)
		return abs, "", false, false
	case !allowMissing:
		v.addf("%s %q does not exist", flagName, abs)
		return abs, "", false, false
	}
	parent := filepath.Dir(abs)
	if pfi, err := fsys.stat(parent); err != nil || !pfi.IsDir() {
		v.addf("%s %q does not exist and neither does its parent directory", flagName, abs)
		return abs, "", false, false
	}
	realParent, err := fsys.evalSymlinks(parent)
	if err != nil {
		v.addf("%s %q: %v", flagName, abs, err)
		return abs, "", false, false
	}
	return abs, filepath.Join(realParent, filepath.Base(abs)), true, true
}

// within reports whether child is strictly below parent. Both are clean absolute paths.
func within(parent, child string) bool {
	if parent == child {
		return false
	}
	prefix := parent
	if !strings.HasSuffix(prefix, string(filepath.Separator)) {
		prefix += string(filepath.Separator)
	}
	return strings.HasPrefix(child, prefix)
}
