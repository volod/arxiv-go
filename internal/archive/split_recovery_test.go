package archive

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/volod/arxiv-go/internal/state"
)

func TestPlaceholderStubChoosesFallbackForForeignFile(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "clip.mp4")
	tx := state.Tx{Begin: state.Record{Src: src, RelPath: "clip.mp4"}}
	p := PlaceholderStub{}
	if p.Path(tx) != src+".md" {
		t.Fatalf("missing primary: %s", p.Path(tx))
	}
	if err := os.WriteFile(src+".md", []byte("notes"), 0o644); err != nil {
		t.Fatal(err)
	}
	if p.Path(tx) != src+".arxgo.md" {
		t.Fatalf("foreign primary: %s", p.Path(tx))
	}
	owned := []byte("---\nrel_path: clip.mp4\n---\n")
	if err := os.WriteFile(src+".md", owned, 0o644); err != nil {
		t.Fatal(err)
	}
	if p.Path(tx) != src+".md" {
		t.Fatalf("owned primary: %s", p.Path(tx))
	}
}
