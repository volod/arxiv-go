package report

import (
	"fmt"
	"path/filepath"
	"strings"
	"unicode/utf8"
)

const maxStubIndex = 10000

// ChooseStubPath picks an absolute stub path next to the original video at srcAbs.
// Preference: <name>.<ext>.md, then <name>.<ext>.arxgo.md, then <prefix>-<idx>.<ext>.md
// with truncation so the filename and full path stay within OS limits. An existing
// stub whose front matter names relPath is reused and overwritten.
func ChooseStubPath(srcAbs, relPath string) (string, error) {
	dir := filepath.Dir(srcAbs)
	videoName := filepath.Base(srcAbs)
	if p, ok, err := tryStubName(dir, videoName+".md", relPath); ok || err != nil {
		return p, err
	}
	if p, ok, err := tryStubName(dir, videoName+".arxgo.md", relPath); ok || err != nil {
		return p, err
	}
	for i := 1; i <= maxStubIndex; i++ {
		name, ok := indexedStubName(videoName, i, dir)
		if !ok {
			continue
		}
		if p, done, err := tryStubName(dir, name, relPath); done || err != nil {
			return p, err
		}
	}
	return "", fmt.Errorf("stub conflict near %s: no available filename", srcAbs)
}

func tryStubName(dir, name, relPath string) (string, bool, error) {
	if name == "" || !fitsName(dir, name) {
		return "", false, nil
	}
	p := filepath.Join(dir, name)
	occ, err := InspectStub(p, relPath)
	if err != nil {
		return "", false, err
	}
	switch occ {
	case StubAbsent, StubOwned:
		return p, true, nil
	default:
		return "", false, nil
	}
}

func fitsName(dir, name string) bool {
	if pathUnitLen(name) < 1 || pathUnitLen(name) > nameMax {
		return false
	}
	return pathUnitLen(filepath.Join(dir, name)) <= pathMax
}

// indexedStubName builds <prefix>-<idx>.<ext>.md, shortening prefix until the name
// and the joined path fit the platform limits.
func indexedStubName(videoName string, idx int, dir string) (string, bool) {
	stem, ext := splitVideoName(videoName)
	suffix := fmt.Sprintf("-%d%s.md", idx, ext)
	prefix := truncateUnits(stem, nameMax-pathUnitLen(suffix))
	for {
		name := prefix + suffix
		if prefix == "" {
			name = strings.TrimPrefix(suffix, "-")
		}
		if fitsName(dir, name) {
			return name, true
		}
		if prefix == "" {
			return "", false
		}
		prefix = dropLastRune(prefix)
	}
}

func splitVideoName(name string) (stem, ext string) {
	i := strings.LastIndex(name, ".")
	if i <= 0 || i == len(name)-1 {
		return name, ""
	}
	return name[:i], name[i:]
}

func truncateUnits(s string, max int) string {
	if max < 1 {
		return ""
	}
	for pathUnitLen(s) > max && s != "" {
		s = dropLastRune(s)
	}
	return s
}

func dropLastRune(s string) string {
	_, n := utf8.DecodeLastRuneInString(s)
	if n <= 0 {
		return ""
	}
	return s[:len(s)-n]
}
