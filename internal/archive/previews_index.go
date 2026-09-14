package archive

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// previewIndex is reconstructed from durable WAL events, never from a guessed file name.
// Paths are relative to the main archive and sizes are the bytes published by the runner.
type previewIndex struct {
	owned   map[string]map[string]int64
	pending map[string]map[string]bool
}

func readPreviewIndex(root string) (*previewIndex, error) {
	idx := &previewIndex{owned: map[string]map[string]int64{}, pending: map[string]map[string]bool{}}
	runs, err := runsInStartOrder(root)
	if err != nil {
		return nil, err
	}
	for _, run := range runs {
		recs, err := state.ReadWALRecords(run.File(state.WALFile))
		if err != nil {
			return nil, err
		}
		begins := map[string]state.Record{}
		for _, rec := range recs {
			if rec.Step != state.StepPreviewBegin && rec.Step != state.StepPreviewDone &&
				rec.Step != state.StepPreviewFailed && rec.Step != state.StepPreviewDelete &&
				rec.Step != state.StepPreviewDeleted {
				continue
			}
			path, ok := previewRel(root, rec.Dst)
			if !ok || !scanner.LocalRelPath(rec.RelPath) {
				return nil, fmt.Errorf("%w: preview WAL path outside archive or reserved: %q", state.ErrStateCorrupt, rec.Dst)
			}
			if idx.owned[rec.RelPath] == nil {
				idx.owned[rec.RelPath] = map[string]int64{}
			}
			if idx.pending[rec.RelPath] == nil {
				idx.pending[rec.RelPath] = map[string]bool{}
			}
			switch rec.Step {
			case state.StepPreviewBegin, state.StepPreviewDelete:
				begins[rec.TxID] = rec
				idx.pending[rec.RelPath][path] = rec.Step == state.StepPreviewBegin
			case state.StepPreviewDone:
				if begin, ok := begins[rec.TxID]; ok {
					if old, valid := previewRel(root, begin.Dst); valid {
						delete(idx.pending[rec.RelPath], old)
					}
				}
				delete(begins, rec.TxID)
				if rec.Size > 0 {
					idx.owned[rec.RelPath][path] = rec.Size
				}
			case state.StepPreviewFailed:
				if begin, ok := begins[rec.TxID]; ok {
					if old, valid := previewRel(root, begin.Dst); valid {
						delete(idx.pending[rec.RelPath], old)
					}
				}
				delete(begins, rec.TxID)
			case state.StepPreviewDeleted:
				delete(begins, rec.TxID)
				delete(idx.pending[rec.RelPath], path)
				delete(idx.owned[rec.RelPath], path)
			}
		}
	}
	return idx, nil
}

func previewRel(root, abs string) (string, bool) {
	if !filepath.IsAbs(abs) {
		return "", false
	}
	rel, err := filepath.Rel(root, abs)
	if err != nil {
		return "", false
	}
	rel = filepath.ToSlash(rel)
	return rel, scanner.LocalRelPath(rel)
}

func (idx *previewIndex) skipPaths(root string) []string {
	var paths []string
	for _, byPath := range idx.owned {
		for rel := range byPath {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	for _, byPath := range idx.pending {
		for rel := range byPath {
			paths = append(paths, filepath.Join(root, filepath.FromSlash(rel)))
		}
	}
	return paths
}

func (idx *previewIndex) list(rel string) []string {
	var out []string
	for p := range idx.owned[rel] {
		out = append(out, p)
	}
	sort.Strings(out)
	return out
}

func (idx *previewIndex) encoded(rel string) string { return strings.Join(idx.list(rel), ";") }

func regularPreviewSize(path string) (int64, bool) {
	fi, err := os.Lstat(path)
	returnSize := int64(0)
	if err == nil && fi.Mode().IsRegular() && fi.Size() > 0 {
		returnSize = fi.Size()
	}
	return returnSize, returnSize > 0
}

func cleanupPreviewParts(root string, idx *previewIndex) error {
	for _, byPath := range idx.pending {
		for rel, generating := range byPath {
			if !generating {
				continue
			}
			part := media.PreviewPartPath(filepath.Join(root, filepath.FromSlash(rel)))
			if err := os.Remove(part); err != nil && !os.IsNotExist(err) {
				return err
			}
		}
	}
	return nil
}
