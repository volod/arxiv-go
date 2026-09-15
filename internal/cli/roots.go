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

// selectPayload resolves --video and --catia (default video) and checks that the mirror root flags
// given on the command line match the payload. A root of the other payload that comes only from
// the environment is ignored.
func selectPayload(s *settings, v *validator) string {
	payload := PayloadVideo
	switch {
	case s.video && s.catia:
		v.addf("%s and %s are mutually exclusive; select one payload", s.explicit["video"], s.explicit["catia"])
	case s.catia:
		payload = PayloadCatia
	}
	switch {
	case payload == PayloadCatia && s.explicit["video-archive"] == "--video-archive":
		v.addf("--video-archive cannot be used with --catia; use --catia-archive for the CATIA archive")
	case payload != PayloadCatia && s.explicit["catia-archive"] == "--catia-archive":
		v.addf("--catia-archive requires --catia; use --video-archive for the video archive")
	}
	return payload
}

// root is one validated root flag: its absolute path and the symlink-resolved path compared for
// nesting ("" when it could not be determined).
type root struct {
	flag, abs, real string
}

// mirrorFlag is the command-line flag of a payload's mirror root.
func mirrorFlag(payload string) string {
	if payload == PayloadCatia {
		return "--catia-archive"
	}
	return "--video-archive"
}

// checkRoots validates --archive and, for split and restore, the mirror root of the selected
// payload (--video-archive or --catia-archive). It returns the absolute archive and selected mirror
// root, and whether a split mirror root still has to be created. The mirror root of the other
// payload is only compared for nesting when it is set; it need not exist.
func checkRoots(op, payload string, s *settings, fsys rootFS, v *validator) (archive, mirror string, missing bool) {
	archive, archiveReal, ok, _ := checkDir(fsys, "--archive", s.archive, false, v)
	if op == OpScan {
		return archive, "", false
	}
	values := map[string]string{"--video-archive": s.videoArchive, "--catia-archive": s.catiaArchive}
	selected := mirrorFlag(payload)
	mirror, mirrorReal, mirrorOK, missing := checkDir(fsys, selected, values[selected], op == OpSplit, v)
	roots := []root{}
	if ok {
		roots = append(roots, root{"--archive", archive, archiveReal})
	}
	if mirrorOK {
		roots = append(roots, root{selected, mirror, mirrorReal})
	}
	for _, other := range []string{"--video-archive", "--catia-archive"} {
		if other == selected || values[other] == "" {
			continue
		}
		if r, ok := pathOnly(fsys, other, values[other]); ok {
			roots = append(roots, r)
		}
	}
	for i := range roots {
		for j := i + 1; j < len(roots); j++ {
			checkApart(fsys, roots[i], roots[j], v)
		}
	}
	return archive, mirror, missing
}

// pathOnly makes a root that is not selected absolute and resolves its symlinks when it exists.
func pathOnly(fsys rootFS, flagName, value string) (root, bool) {
	abs, err := fsys.abs(value)
	if err != nil {
		return root{}, false
	}
	real := abs
	if resolved, err := fsys.evalSymlinks(abs); err == nil {
		real = resolved
	} else if parent, err := fsys.evalSymlinks(filepath.Dir(abs)); err == nil {
		real = filepath.Join(parent, filepath.Base(abs))
	}
	return root{flagName, abs, real}, true
}

// checkApart requires two roots to be neither equal nor nested in either direction.
func checkApart(fsys rootFS, a, b root, v *validator) {
	x, y := a.real, b.real
	if fsys.foldCase {
		x, y = strings.ToLower(x), strings.ToLower(y)
	}
	switch {
	case x == y:
		v.addf("%s and %s resolve to the same directory %q", a.flag, b.flag, a.real)
	case within(x, y):
		v.addf("%s %q is inside %s %q", b.flag, b.abs, a.flag, a.abs)
	case within(y, x):
		v.addf("%s %q is inside %s %q", a.flag, a.abs, b.flag, b.abs)
	}
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
