package archive

import (
	"bytes"
	"context"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"sync"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/state"
)

// previewQueue runs the preview executor on one worker goroutine, reading committed videos from
// the video archive while the split loop moves the next ones. The first fatal error stops the
// worker and is returned by the next submit, so the loop stops at a transaction boundary.
type previewQueue struct {
	exec previewExecutor
	jobs chan *videoPreviews
	done chan struct{}
	mu   sync.Mutex
	err  error
}

func startPreviewQueue(ctx context.Context, exec previewExecutor) *previewQueue {
	q := &previewQueue{exec: exec, jobs: make(chan *videoPreviews, 1), done: make(chan struct{})}
	go func() {
		defer close(q.done)
		for item := range q.jobs {
			if q.failed() != nil {
				continue
			}
			if err := exec.run(ctx, item); err != nil {
				q.mu.Lock()
				q.err = err
				q.mu.Unlock()
			}
		}
	}()
	return q
}

func (q *previewQueue) failed() error {
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.err
}

func (q *previewQueue) submit(item *videoPreviews) error {
	if err := q.failed(); err != nil {
		return err
	}
	q.jobs <- item
	return nil
}

// close waits for the submitted videos and returns the fatal error, if any.
func (q *previewQueue) close() error {
	close(q.jobs)
	<-q.done
	return q.failed()
}

// previewExecutor generates the planned previews of committed videos. Each output is surrounded
// by preview WAL events; a failed preview is an issue, never a failure of the move.
type previewExecutor struct {
	s      *Session
	w      *state.WAL
	idx    *previewIndex
	runner *media.Runner
}

func (e previewExecutor) run(ctx context.Context, item *videoPreviews) error {
	if item.err != nil {
		e.fail(item.video, "preview planning: "+item.err.Error())
		return nil
	}
	source := filepath.Join(e.s.cfg.Payload.Root, filepath.FromSlash(item.video))
	if _, err := os.Lstat(source); err != nil {
		e.fail(item.video, "preview source: "+err.Error())
		return nil
	}
	for _, job := range item.plan.Jobs {
		if err := ctx.Err(); err != nil {
			return err
		}
		if err := e.generate(ctx, item.video, source, job); err != nil {
			return err
		}
	}
	// Refreshing after every run, not only after new outputs, repairs a description written by a later
	// move or left stale by a crash after the last preview_done.
	return refreshPreviewDescription(e.s, e.idx, item.video)
}

func (e previewExecutor) generate(ctx context.Context, video, source string, job media.PreviewJob) error {
	preview := previewPathFor(video, job.Output)
	job.Output = e.idx.abs(preview)
	if e.idx.published(video, preview) {
		return nil
	}
	if _, err := os.Lstat(job.Output); err == nil {
		return e.adopt(ctx, video, source, preview, job)
	} else if !os.IsNotExist(err) {
		return err
	}
	begin, err := e.w.BeginEvent(e.idx.family, video, job.Output, 0, false)
	if err != nil {
		return err
	}
	if err := e.runner.Generate(ctx, source, job); err != nil {
		if ctx.Err() != nil {
			return ctx.Err() // unfinished, not failed: the resumed run removes the part and retries
		}
		if _, werr := e.w.FinishEvent(e.idx.family, begin.TxID, e.idx.family.Failed, "", 0, err.Error()); werr != nil {
			return werr
		}
		e.fail(video, "preview: "+err.Error())
		return nil
	}
	return e.done(video, preview, begin.TxID)
}

// adopt handles an existing file at a planned path. Only a preview whose generation started but
// never logged its outcome is taken over, and only when it passes the normal content checks.
func (e previewExecutor) adopt(ctx context.Context, video, source, preview string, job media.PreviewJob) error {
	if _, ok := nonEmptyFileSize(job.Output); !ok || !e.idx.generating.has(video, preview) {
		e.fail(video, "preview output occupied: "+preview)
		return nil
	}
	if err := e.runner.ValidatePublishedPreview(ctx, source, job.Output, job); err != nil {
		e.fail(video, "preview output occupied by invalid file: "+preview+": "+err.Error())
		return nil
	}
	begin, err := e.w.BeginEvent(e.idx.family, video, job.Output, 0, false)
	if err != nil {
		return err
	}
	return e.done(video, preview, begin.TxID)
}

func (e previewExecutor) done(video, preview, txid string) error {
	size, ok := nonEmptyFileSize(e.idx.abs(preview))
	if !ok {
		return fmt.Errorf("published preview is missing: %s", preview)
	}
	if _, err := e.w.FinishEvent(e.idx.family, txid, e.idx.family.Done, "", size, ""); err != nil {
		return err
	}
	e.idx.owned.put(video, preview, size)
	e.idx.generating.remove(video, preview)
	e.s.Stats.PreviewsDone.Add(1)
	e.s.Stats.ArchiveWritten.Add(size)
	e.s.Log.Info("preview generated", "rel_path", video, "preview", preview, "bytes", size)
	return nil
}

func (e previewExecutor) fail(video, reason string) {
	e.s.Issue(state.IssueFailed, video, reason)
	e.s.Stats.PreviewsFailed.Add(1)
}

// refreshPreviewDescription rewrites the preview section of the owned description of video when it differs from
// the recorded previews. A foreign or missing description is left alone.
func refreshPreviewDescription(s *Session, idx *previewIndex, video string) error {
	base := filepath.Join(s.cfg.Archive, filepath.FromSlash(video))
	for _, description := range []string{base + ".md", base + ".arxgo.md"} {
		owner, err := report.InspectDescription(description, video)
		if err != nil {
			return err
		}
		if owner != report.DescriptionOwned {
			continue
		}
		data, err := os.ReadFile(description)
		if err != nil {
			return err
		}
		var links []report.PreviewLink
		for _, preview := range idx.owned.sorted(video) {
			links = append(links, report.PreviewLink{Name: path.Base(preview),
				URL: report.RelativeLink(filepath.Dir(description), idx.abs(preview))})
		}
		out := report.ReplacePreviewLinks(data, links)
		if bytes.Equal(out, data) {
			return nil
		}
		if err := s.cfg.FS.AtomicWriteFile(description, out, 0o644); err != nil {
			return err
		}
		s.Stats.ArchiveWritten.Add(int64(len(out)))
		return nil
	}
	return nil
}

// splitPreviews submits the preview plans of committed videos to the preview queue.
type splitPreviews struct {
	queue *previewQueue
	plans map[string]*videoPreviews
}

func newSplitPreviews(ctx context.Context, exec previewExecutor, plans []*videoPreviews) *splitPreviews {
	p := &splitPreviews{plans: make(map[string]*videoPreviews, len(plans))}
	if len(plans) == 0 {
		return p
	}
	for _, item := range plans {
		p.plans[item.video] = item
	}
	p.queue = startPreviewQueue(ctx, exec)
	return p
}

// catchUp submits, in path order, videos moved by earlier runs and by an earlier process of
// this run.
func (p *splitPreviews) catchUp(committed *state.CommittedSet) error {
	for _, video := range sortedKeys(p.plans) {
		if item := p.plans[video]; item.moved || committed.Has(video) {
			if err := p.queue.submit(item); err != nil {
				return err
			}
		}
	}
	return nil
}

func (p *splitPreviews) committed(video string) error {
	if item, ok := p.plans[video]; ok {
		return p.queue.submit(item)
	}
	return nil
}

// stop waits for the queued previews and joins their fatal error with err.
func (p *splitPreviews) stop(err error) error {
	if p.queue == nil {
		return err
	}
	if qerr := p.queue.close(); err == nil {
		return qerr
	}
	return err
}
