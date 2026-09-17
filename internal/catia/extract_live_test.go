//go:build catialive

package catia

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// Optional aggregate run against the gitignored experimental tree, or the directory named by
// ARXGO_CATIA_LIVE_DIR. Records may keep counts only.
func TestExtractExperimentalAggregates(t *testing.T) {
	_, file, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("caller")
	}
	root := filepath.Join(filepath.Dir(file), "..", "..", "bin", "catia")
	if dir := os.Getenv("ARXGO_CATIA_LIVE_DIR"); dir != "" {
		root = dir
	}
	if st, err := filepath.Glob(filepath.Join(root, "*")); err != nil || len(st) == 0 {
		t.Skip("experimental CATIA tree not present")
	}

	var files, v5, withComps, parts, products, drawings, errors, textFailed, unknownRelease int
	compSum := 0
	releases := map[string]int{}
	var withProps, withDescription, withDefinition, withMaterial, withNotes, noteItems, noteBytes, truncated int
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
		if info.Product != (Product{}) {
			withProps++
		}
		for n, has := range map[*int]bool{&withDescription: info.Product.Description != "",
			&withDefinition: info.Product.Definition != "", &withMaterial: info.Product.Material != ""} {
			if has {
				*n++
			}
		}
		if len(info.Notes) > 0 {
			withNotes++
		}
		noteItems += len(info.Notes)
		for _, note := range info.Notes {
			noteBytes += len(note)
		}
		if info.Truncated {
			truncated++
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("files=%d parts=%d products=%d drawings=%d v5=%d with_components=%d component_names=%d unknown_release=%d distinct_releases=%d errors=%d text_failed=%d",
		files, parts, products, drawings, v5, withComps, compSum, unknownRelease, len(releases), errors, textFailed)
	t.Logf("with_properties=%d with_description=%d with_definition=%d with_material=%d with_notes=%d note_items=%d note_bytes=%d truncated=%d",
		withProps, withDescription, withDefinition, withMaterial, withNotes, noteItems, noteBytes, truncated)
	if files == 0 {
		t.Fatal("no CATIA files")
	}
	if errors != 0 {
		t.Fatalf("extract errors: %d", errors)
	}
}
