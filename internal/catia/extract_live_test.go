//go:build catialive

package catia

import (
	"context"
	"io/fs"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Optional aggregate run against the gitignored experimental tree. Records may keep counts only.
func TestExtractExperimentalAggregates(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "bin", "catia")
	if st, err := filepath.Glob(filepath.Join(root, "*")); err != nil || len(st) == 0 {
		t.Skip("experimental CATIA tree not present")
	}

	var files, v5, withComps, parts, products, drawings, errors, textFailed, unknownRelease int
	compSum := 0
	releases := map[string]int{}
	ctx := context.Background()
	err := filepath.WalkDir(root, func(path string, d fs.DirEntry, err error) error {
		if err != nil || d.IsDir() {
			return err
		}
		ext := strings.ToLower(filepath.Ext(d.Name()))
		if _, ok := KindOf(ext); !ok {
			return nil
		}
		files++
		switch ext {
		case ".catpart":
			parts++
		case ".catproduct":
			products++
		case ".catdrawing":
			drawings++
		}
		info := ExtractPath(ctx, path)
		if info.Err != nil {
			errors++
		}
		if info.TextFailed {
			textFailed++
		}
		if info.Format == FormatV5 {
			v5++
		}
		if info.Release == ReleaseUnknown {
			unknownRelease++
		} else {
			releases[info.Release]++
		}
		if n := len(info.Components); n > 0 {
			withComps++
			compSum += n
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("files=%d parts=%d products=%d drawings=%d v5=%d with_components=%d component_names=%d unknown_release=%d distinct_releases=%d errors=%d text_failed=%d",
		files, parts, products, drawings, v5, withComps, compSum, unknownRelease, len(releases), errors, textFailed)
	if files == 0 {
		t.Fatal("no CATIA files")
	}
	if errors != 0 {
		t.Fatalf("extract errors: %d", errors)
	}
}
