package scanner

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// makeTree creates files (and directories for names ending in "/") below root.
func makeTree(t *testing.T, root string, paths ...string) {
	t.Helper()
	for _, p := range paths {
		full := filepath.Join(root, filepath.FromSlash(strings.TrimSuffix(p, "/")))
		if strings.HasSuffix(p, "/") {
			if err := os.MkdirAll(full, 0o755); err != nil {
				t.Fatal(err)
			}
			continue
		}
		if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(full, []byte(p), 0o644); err != nil {
			t.Fatal(err)
		}
	}
}

// walkAll collects every delivered entry.
func walkAll(t *testing.T, root string, opts Options) []Entry {
	t.Helper()
	var got []Entry
	if err := Walk(context.Background(), root, opts, func(e Entry) error {
		got = append(got, e)
		return nil
	}); err != nil {
		t.Fatalf("Walk: %v", err)
	}
	return got
}

func rels(entries []Entry) []string {
	out := make([]string, len(entries))
	for i, e := range entries {
		out[i] = e.Rel
	}
	return out
}

// rawWalkDir lists filepath.WalkDir order below root as relative slash paths.
func rawWalkDir(t *testing.T, root string) []string {
	t.Helper()
	var out []string
	err := filepath.WalkDir(root, func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if p != root {
			rel, _ := filepath.Rel(root, p)
			out = append(out, filepath.ToSlash(rel))
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return out
}

func canSymlink(t *testing.T, dir string) {
	t.Helper()
	if err := os.Symlink("target", filepath.Join(dir, "probe-link")); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	_ = os.Remove(filepath.Join(dir, "probe-link"))
}

// Stats counts delivered entries for walker tests; the scan operation counts in state.ScanStats.
type Stats struct {
	Dirs     int64
	Files    int64
	Symlinks int64
	Special  int64
	Skipped  map[string]int64 // by reason, see Entry.SkipReason
}

// Count adds e. An unreadable directory counts both as a directory and as skipped.
func (s *Stats) Count(e Entry) {
	switch e.Kind {
	case KindDir:
		s.Dirs++
	case KindFile:
		s.Files++
	case KindSymlink:
		s.Symlinks++
	case KindSpecial:
		s.Special++
	}
	if reason := e.SkipReason(); reason != "" {
		if s.Skipped == nil {
			s.Skipped = map[string]int64{}
		}
		s.Skipped[reason]++
	}
}

// SkippedTotal returns the number of skipped entries.
func (s *Stats) SkippedTotal() int64 {
	var n int64
	for _, c := range s.Skipped {
		n += c
	}
	return n
}
