package cli

import (
	"path/filepath"
	"strings"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

// CatiaIndexOptions configures the CATIA text index. It reads the archive and starts no run, so
// only the archive root and the console log settings of Common apply.
type CatiaIndexOptions struct {
	Common
	Out string // absolute output path
}

func buildCatiaIndexOptions(s *settings, fsys rootFS) (CatiaIndexOptions, error) {
	v := &validator{}
	o := CatiaIndexOptions{Common: Common{LogLevel: parseLevel(s.logLevel), LogFormat: s.logFormat}}
	archive, _, ok, _ := checkDir(fsys, "--archive", s.archive, false, v)
	o.Archive = archive
	if !ok && s.out == "" {
		return o, v.err()
	}
	out := s.out
	if out == "" {
		out = filepath.Join(archive, scanner.CatiaIndexName)
	}
	abs, err := fsys.abs(out)
	if err != nil {
		v.addf("--out %q: %v", out, err)
		return o, v.err()
	}
	o.Out = abs
	flagName := "--out"
	if s.out == "" {
		flagName = "default output"
	}
	if fi, err := fsys.stat(abs); err == nil && fi.IsDir() {
		v.addf("%s %q is a directory", flagName, abs)
	}
	if fi, err := fsys.stat(filepath.Dir(abs)); err != nil || !fi.IsDir() {
		v.addf("%s %q: parent directory does not exist", flagName, abs)
	}
	if ok && s.out != "" {
		checkIndexOut(fsys, archive, abs, v)
	}
	return o, v.err()
}

// checkIndexOut refuses an --out that would replace something arxgo owns: run state, a registry,
// a part file, or a description or text sidecar.
func checkIndexOut(fsys rootFS, archive, out string, v *validator) {
	a, o := archive, out
	if fsys.foldCase {
		a, o = strings.ToLower(a), strings.ToLower(o)
	}
	if rel, err := filepath.Rel(a, o); err == nil && filepath.IsLocal(rel) {
		rel = filepath.ToSlash(rel)
		if scanner.Reserved(rel) && rel != scanner.CatiaIndexName {
			v.addf("--out %q is a reserved arxgo path", out)
			return
		}
	}
	if scanner.IsPartFile(filepath.ToSlash(out)) {
		v.addf("--out %q is a reserved arxgo path", out)
		return
	}
	if owned, err := report.HasArxgoMarker(out); err != nil {
		v.addf("--out %q: %v", out, err)
	} else if owned {
		v.addf("--out %q is an arxgo description or text sidecar", out)
	}
}
