package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/state"
)

func TestMarkdownStubChoosesFallbackForForeignFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	tx := state.Tx{Begin: state.Record{Src: src, RelPath: "clip.mp4"}}
	p := NewMarkdownStub(StubConfig{})
	if p.Path(tx) != src+".md" {
		t.Fatalf("missing primary: %s", p.Path(tx))
	}
	if err := os.WriteFile(src+".md", []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p.Path(tx) != src+".arxgo.md" {
		t.Fatalf("foreign primary: %s", p.Path(tx))
	}
	owned := []byte("---\narxgo_stub: 1\nrel_path: clip.mp4\n---\n")
	if err := os.WriteFile(src+".md", owned, 0o644); err != nil {
		t.Fatal(err)
	}
	if p.Path(tx) != src+".md" {
		t.Fatalf("owned primary: %s", p.Path(tx))
	}
}

func TestMarkdownStubIndexedNameWhenFallbackIsForeign(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	if err := os.WriteFile(src+".md", []byte("user"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(src+".arxgo.md", []byte("also user"), 0o644); err != nil {
		t.Fatal(err)
	}
	tx := state.Tx{Begin: state.Record{Src: src, RelPath: "clip.mp4"}}
	p := NewMarkdownStub(StubConfig{})
	if p.Path(tx) != filepath.Join(dir, "clip-1.mp4.md") {
		t.Fatalf("indexed: %s", p.Path(tx))
	}
}
