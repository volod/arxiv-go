package scanner

import (
	"path/filepath"
	"strings"
)

// reservedName reports the names arxgo owns directly under a walked root.
func reservedName(name string) bool {
	switch name {
	case StateDirName, RegistryName, VideoRegistryName, CatiaRegistryName:
		return true
	}
	return false
}

// Reserved reports whether the relative slash path rel is owned by arxgo: the state directory or a
// registry name directly under the root (with everything below them), or a part file at any depth.
func Reserved(rel string) bool {
	first, _, _ := strings.Cut(rel, "/")
	return reservedName(first) || IsPartFile(rel)
}

// IsPartFile reports an arxgo temporary file: a transfer part "<name>.arxgo-part" or a preview
// part "<stem>.arxgo-part.<ext>", which keeps its extension for ffmpeg's muxer selection.
func IsPartFile(rel string) bool {
	name := rel[strings.LastIndex(rel, "/")+1:]
	if strings.HasSuffix(name, PartSuffix) {
		return true
	}
	i := strings.LastIndex(name, PartSuffix+".")
	return i >= 0 && !strings.Contains(name[i+len(PartSuffix)+1:], ".")
}

// LocalRelPath reports whether rel, read from a file such as a registry, is a relative slash path
// that stays below the root it is joined to and is not reserved: no empty, "." or ".." segment, no
// volume name or OS separator other than "/", and nothing that Reserved matches.
func LocalRelPath(rel string) bool {
	if rel == "" || (filepath.Separator != '/' && strings.ContainsRune(rel, filepath.Separator)) {
		return false
	}
	for _, seg := range strings.Split(rel, "/") {
		if seg == "" || seg == "." || seg == ".." {
			return false
		}
	}
	return filepath.IsLocal(filepath.FromSlash(rel)) && !Reserved(rel)
}
