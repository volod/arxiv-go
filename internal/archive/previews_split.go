package archive

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"sync"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

type plannedPreview struct {
	rel   string
	moved bool
	plan  media.PreviewPlan
	err   error
}

func previewsEnabled(o media.PreviewOptions) bool {
	return o.SampleMode != "" && o.SampleMode != "none" || o.ImageMode != "" && o.ImageMode != "none"
}

// planSplitPreviews includes both new candidates and previously moved videos. The ffprobe pass
// also supplies durations for --metadata file, before the free-space decision is made.
func planSplitPreviews(ctx context.Context, s *Session, c SplitConfig, idx *previewIndex, list string, runner *media.Runner) (map[string]plannedPreview, int64, error) {
	if !previewsEnabled(c.Preview) {
		return nil, 0, nil
	}
	fileRows, err := report.LoadRegistry(c.Scan.Registry)
	if err != nil {
		return nil, 0, err
	}
	byFile := report.RegistryByPath(fileRows)
	old, err := loadVideoRegistry(s.cfg.Archive, s.cfg.VideoArchive)
	if err != nil {
		return nil, 0, err
	}
	rows, err := replayVideoRows(old, s.cfg.Archive, nil)
	if err != nil {
		return nil, 0, err
	}
	paths := map[string]string{}
	moved := map[string]bool{}
	for _, row := range rows {
		if row.Status == report.StatusMoved && scanner.LocalRelPath(row.RelPath) {
			paths[row.RelPath] = filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(row.RelPath))
			moved[row.RelPath] = true
		}
	}
	if err := ReadCandidates(list, func(v Candidate) error {
		if !scanner.LocalRelPath(v.RelPath) {
			return nil
		}
		if _, ok := paths[v.RelPath]; !ok {
			paths[v.RelPath] = filepath.Join(s.cfg.Archive, filepath.FromSlash(v.RelPath))
		}
		return nil
	}); err != nil {
		return nil, 0, err
	}
	planned := make(map[string]plannedPreview, len(paths))
	reserved := map[string]bool{}
	var estimate int64
	ordered := make([]string, 0, len(paths))
	for rel := range paths {
		ordered = append(ordered, rel)
	}
	sort.Strings(ordered)
	for _, rel := range ordered {
		source := paths[rel]
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if _, err := os.Lstat(source); err != nil {
			continue
		}
		var info *media.MediaInfo
		if row, ok := byFile[rel]; ok && row.Metadata.Media != nil && row.Metadata.Media.Error == "" {
			info = row.Metadata.Media
		}
		if info == nil || info.Width < 2 || info.Height < 2 || info.DurationS <= 0 {
			mime := byFile[rel].FileMIME
			info = runner.Probe(ctx, source, mime)
		}
		entry := plannedPreview{rel: rel, moved: moved[rel]}
		if info.Error != "" {
			entry.err = errors.New(info.Error)
		} else {
			dir := filepath.Join(s.cfg.Archive, filepath.Dir(filepath.FromSlash(rel)))
			entry.plan, entry.err = media.PlanPreviews(*info, rel, c.Preview, func(name string) bool {
				p := filepath.Join(dir, name)
				if reserved[p] {
					return true
				}
				previewRel, ok := previewRel(s.cfg.Archive, p)
				if ok && (idx.owned[rel][previewRel] > 0 || idx.pending[rel][previewRel]) {
					return false
				}
				_, err := os.Lstat(p)
				return err == nil || !os.IsNotExist(err)
			})
		}
		if entry.err == nil {
			for _, job := range entry.plan.Jobs {
				path := filepath.Join(s.cfg.Archive, filepath.Dir(filepath.FromSlash(rel)), job.Output)
				reserved[path] = true
				previewRel, _ := previewRel(s.cfg.Archive, path)
				if size := idx.owned[rel][previewRel]; size > 0 {
					if actual, regular := regularPreviewSize(path); regular && actual == size {
						continue
					}
				}
				estimate = addSat(estimate, job.EstimateBytes)
			}
			for _, warning := range entry.plan.Warnings {
				s.Log.Warn(warning, "rel_path", rel)
			}
		}
		planned[rel] = entry
	}
	return planned, estimate, nil
}

type previewWorker struct {
	jobs chan plannedPreview
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func startPreviewWorker(ctx context.Context, s *Session, w *state.WAL, idx *previewIndex, runner *media.Runner) *previewWorker {
	p := &previewWorker{jobs: make(chan plannedPreview, 1), done: make(chan struct{})}
	go func() {
		defer close(p.done)
		for item := range p.jobs {
			if err := executePreviews(ctx, s, w, idx, runner, item); err != nil {
				p.mu.Lock()
				p.err = errors.Join(p.err, err)
				p.mu.Unlock()
			}
		}
	}()
	return p
}

func (p *previewWorker) close() error {
	close(p.jobs)
	<-p.done
	p.mu.Lock()
	defer p.mu.Unlock()
	return p.err
}

func executePreviews(ctx context.Context, s *Session, w *state.WAL, idx *previewIndex, runner *media.Runner, item plannedPreview) error {
	if item.err != nil {
		s.Issue(state.IssueFailed, item.rel, "preview planning: "+item.err.Error())
		s.Stats.VideosFailed.Add(1)
		return nil
	}
	source := filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(item.rel))
	if _, err := os.Lstat(source); err != nil {
		return fmt.Errorf("preview source %s: %w", item.rel, err)
	}
	for _, job := range item.plan.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		path := filepath.Join(s.cfg.Archive, filepath.Dir(filepath.FromSlash(item.rel)), job.Output)
		rel, ok := previewRel(s.cfg.Archive, path)
		if !ok {
			return fmt.Errorf("invalid preview path %q", path)
		}
		if size := idx.owned[item.rel][rel]; size > 0 {
			if actual, regular := regularPreviewSize(path); regular && actual == size {
				continue
			}
		}
		if idx.pending[item.rel][rel] {
			if actual, regular := regularPreviewSize(path); regular {
				if err := runner.ValidatePublishedPreview(ctx, path, job); err != nil {
					s.Issue(state.IssueFailed, item.rel, "preview output occupied by invalid file: "+rel+": "+err.Error())
					s.Stats.VideosFailed.Add(1)
					continue
				}
				if err := recordPreviewDone(s, w, idx, item.rel, path, actual); err != nil {
					return err
				}
				continue
			}
		}
		if _, err := os.Lstat(path); err == nil {
			s.Issue(state.IssueFailed, item.rel, "preview output occupied: "+rel)
			s.Stats.VideosFailed.Add(1)
			continue
		} else if !os.IsNotExist(err) {
			return err
		}
		begin, err := w.BeginPreview(item.rel, path, 0, false)
		if err != nil {
			return err
		}
		job.Output = path
		actual := path
		if job.Kind == "sample" {
			actual, err = runner.GenerateSample(ctx, source, job)
		} else {
			err = runner.GenerateFrame(ctx, source, job)
		}
		if err != nil {
			if _, appendErr := w.FinishPreview(begin.TxID, state.StepPreviewFailed, "", 0, err.Error()); appendErr != nil {
				return appendErr
			}
			s.Issue(state.IssueFailed, item.rel, "preview: "+err.Error())
			s.Stats.VideosFailed.Add(1)
			continue
		}
		size, valid := regularPreviewSize(actual)
		if !valid {
			return fmt.Errorf("published preview is missing: %s", actual)
		}
		if err := finishGeneratedPreview(s, w, idx, item.rel, begin.TxID, actual, size); err != nil {
			return err
		}
	}
	return nil
}

func recordPreviewDone(s *Session, w *state.WAL, idx *previewIndex, rel, path string, size int64) error {
	begin, err := w.BeginPreview(rel, path, 0, false)
	if err != nil {
		return err
	}
	return finishGeneratedPreview(s, w, idx, rel, begin.TxID, path, size)
}

func finishGeneratedPreview(s *Session, w *state.WAL, idx *previewIndex, rel, txid, path string, size int64) error {
	previewRel, _ := previewRel(s.cfg.Archive, path)
	if idx.owned[rel] == nil {
		idx.owned[rel] = map[string]int64{}
	}
	idx.owned[rel][previewRel] = size
	if err := refreshPreviewStub(s, rel, idx.list(rel)); err != nil {
		return err
	}
	if _, err := w.FinishPreview(txid, state.StepPreviewDone, path, size, ""); err != nil {
		return err
	}
	delete(idx.pending[rel], previewRel)
	s.Stats.ArchiveWritten.Add(size)
	s.Log.Info("preview generated", "rel_path", rel, "preview", previewRel, "bytes", size)
	return nil
}

func refreshPreviewStub(s *Session, rel string, previews []string) error {
	base := filepath.Join(s.cfg.Archive, filepath.FromSlash(rel))
	for _, p := range []string{base + ".md", base + ".arxgo.md"} {
		owner, err := report.InspectStub(p, rel)
		if err != nil {
			return err
		}
		if owner != report.StubOwned {
			continue
		}
		data, err := os.ReadFile(p)
		if err != nil {
			return err
		}
		links := make([]string, 0, len(previews))
		for _, prev := range previews {
			name := filepath.Base(prev)
			url := report.RelativeLink(filepath.Dir(p), filepath.Join(s.cfg.Archive, filepath.FromSlash(prev)))
			if filepath.Ext(name) == ".png" {
				links = append(links, "- !["+name+"]("+url+")")
			} else {
				links = append(links, "- ["+name+"]("+url+")")
			}
		}
		out := report.ReplacePreviewSection(data, links)
		if err := s.cfg.FS.AtomicWriteFile(p, out, 0o644); err != nil {
			return err
		}
		s.Stats.ArchiveWritten.Add(int64(len(out)))
		return nil
	}
	return nil
}
