package scanner

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

// benchTree builds dirs x dirs directories with files files each.
func benchTree(b *testing.B, dirs, files int) (string, int) {
	b.Helper()
	root := b.TempDir()
	for i := range dirs {
		for j := range dirs {
			dir := filepath.Join(root, fmt.Sprintf("d%03d", i), fmt.Sprintf("s-%03d", j))
			if err := os.MkdirAll(dir, 0o755); err != nil {
				b.Fatal(err)
			}
			for k := range files {
				if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("f%04d.bin", k)), nil, 0o644); err != nil {
					b.Fatal(err)
				}
			}
		}
	}
	return root, dirs + dirs*dirs + dirs*dirs*files
}

func BenchmarkWalk(b *testing.B) {
	root, want := benchTree(b, 20, 50)
	b.Run("full", func(b *testing.B) {
		for b.Loop() {
			n := 0
			if err := Walk(context.Background(), root, Options{Exclude: []string{"**/*.tmp"}}, func(Entry) error { n++; return nil }); err != nil || n != want {
				b.Fatalf("n=%d want %d err=%v", n, want, err)
			}
		}
	})
	b.Run("resume-near-end", func(b *testing.B) {
		cursor := KeyOf("d019/s-019/f0000.bin")
		for b.Loop() {
			n := 0
			if err := Walk(context.Background(), root, Options{Cursor: cursor}, func(Entry) error { n++; return nil }); err != nil || n != 49 {
				b.Fatalf("n=%d err=%v", n, err)
			}
		}
	})
}
