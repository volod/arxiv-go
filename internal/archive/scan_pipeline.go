package archive

import (
	"context"
	"errors"
	"os"
	"path"
	"sync"
	"time"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// scanItem is one walked entry travelling from the walker through detection to the writer.
type scanItem struct {
	e     scanner.Entry
	ft    scanner.FileType
	media *media.MediaInfo
	link  string
	err   error         // detection or readlink error
	done  chan struct{} // closed when detection finished; nil when the entry needs none
}

// pipeline walks the root in order, detects file types on a bounded worker pool and writes rows in
// walk order: the walker queues every entry on order (bounded by Window) and hands files and
// symlinks to the workers; the writer takes entries from order and waits for each one's detection.
func (r *scanRun) pipeline(ctx context.Context) error {
	wctx, cancel := context.WithCancelCause(ctx)
	defer cancel(nil)
	jobs := make(chan *scanItem, r.cfg.Workers)
	order := make(chan *scanItem, r.cfg.Window)

	var workers sync.WaitGroup
	for range r.cfg.Workers {
		workers.Go(func() {
			for it := range jobs {
				r.inspect(wctx, it)
				close(it.done)
			}
		})
	}
	writeErr := make(chan error, 1)
	go func() {
		err := r.writeAll(wctx, order)
		if err != nil {
			cancel(err)
		}
		writeErr <- err
	}()

	skip := append([]string{r.cfg.Registry}, r.cfg.SkipPaths...)
	walkErr := scanner.Walk(wctx, r.cfg.Root, scanner.Options{
		Exclude: r.cfg.Exclude, SkipPaths: skip, Cursor: r.start, Log: r.s.Log,
	}, func(e scanner.Entry) error {
		it := &scanItem{e: e}
		if e.Err == nil && (e.Kind == scanner.KindFile || e.Kind == scanner.KindSymlink) {
			it.done = make(chan struct{})
			select {
			case jobs <- it:
			case <-wctx.Done():
				return context.Cause(wctx)
			}
		}
		select {
		case order <- it:
			return nil
		case <-wctx.Done():
			return context.Cause(wctx)
		}
	})
	close(jobs)
	close(order)
	workers.Wait()
	werr := <-writeErr
	switch {
	case werr != nil && !isCancel(werr):
		return werr // the walker saw the writer's error as a cancellation
	case walkErr != nil:
		return walkErr
	case werr != nil:
		return werr
	}
	return ctx.Err()
}

func isCancel(err error) bool {
	return errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded)
}

// inspect runs on a worker: type detection for files, the link target for symlinks.
func (r *scanRun) inspect(ctx context.Context, it *scanItem) {
	if err := ctx.Err(); err != nil {
		it.err = err
		return
	}
	switch it.e.Kind {
	case scanner.KindFile:
		it.ft, it.err = scanner.Detect(it.e.Path, it.e.Info.Size(), r.detect)
		if it.err == nil && it.ft.IsMedia && !it.ft.IsPicture {
			it.media = r.readMedia(ctx, it.e.Path, it.ft.MIME)
		}
	case scanner.KindSymlink:
		it.link, it.err = os.Readlink(it.e.Path)
	}
}

// writeAll writes entries in walk order until order is closed or the context ends.
func (r *scanRun) writeAll(ctx context.Context, order <-chan *scanItem) error {
	var written int64
	for it := range order {
		if it.done != nil {
			<-it.done // workers finish every queued job, also after cancellation
		}
		if err := ctx.Err(); err != nil {
			return context.Cause(ctx)
		}
		if err := r.write(it); err != nil {
			r.healthy = false
			return err
		}
		if len(r.cursor) == 0 || scanner.Compare(it.e.Key, r.cursor) > 0 {
			// A directory on the resume cursor's path that became unreadable is reported again
			// although it precedes the cursor; the cursor never moves backwards.
			r.cursor = it.e.Key
		}
		if err := r.s.AdvanceSync(1, r.periodicSync); err != nil {
			return err
		}
		if written++; r.cfg.afterEntry != nil {
			if err := r.cfg.afterEntry(written); err != nil {
				r.healthy = false
				return err
			}
		}
	}
	return nil
}

// write accounts one entry and writes its registry row and candidate record.
func (r *scanRun) write(it *scanItem) error {
	e, st := it.e, r.stats
	switch {
	case e.Kind == scanner.KindDir:
		st.Dirs++
		if e.Err != nil {
			r.skip(e.Rel, scanner.ReasonUnreadable, e.Err.Error())
		}
		return nil
	case e.Kind == scanner.KindSpecial:
		r.skip(e.Rel, scanner.ReasonSpecial, "not a regular file, directory or symlink")
		return nil
	case e.Err != nil:
		r.skip(e.Rel, scanner.ReasonUnreadable, e.Err.Error())
		return nil
	case e.Kind == scanner.KindSymlink:
		if it.err != nil {
			r.s.Log.Warn("cannot read symlink target", "rel_path", e.Rel, "error", it.err)
		}
		meta := report.FileMetadata(e.Info.ModTime())
		meta.LinkTarget = it.link
		st.Symlinks++
		r.s.Stats.Files.Add(1)
		return r.reg.Write(report.RegistryRow{RelPath: e.Rel, FileName: path.Base(e.Rel), FileType: "symlink", Metadata: meta})
	case it.err != nil:
		r.s.Log.Warn("skipped entry", "path", e.Rel, "kind", e.Kind.String(), "reason", scanner.ReasonUnreadable, "error", it.err.Error())
		r.skip(e.Rel, scanner.ReasonUnreadable, it.err.Error())
		return nil
	}
	size, ft := e.Info.Size(), it.ft
	if it.media != nil {
		if it.media.Error != "" {
			r.s.Log.Warn("media metadata unavailable", "rel_path", e.Rel, "error", it.media.Error)
		} else if it.media.VideoStreams == 0 && it.media.AudioStreams > 0 {
			// Only an audio-only result refines the flag. A parse with neither stream (for example
			// ffprobe guessing a damaged .avi as LRC lyrics) says nothing about the file being a
			// video, so the MIME and extension decision stands.
			ft.IsVideo = false
		}
	}
	st.AddFile(size, ft.MIME, state.FileFlags{Binary: ft.IsBinary, Media: ft.IsMedia, Picture: ft.IsPicture, Video: ft.IsVideo, Catia: ft.IsCatia, Large: ft.IsLarge})
	r.s.Stats.Files.Add(1)
	r.s.Stats.Bytes.Add(size)
	meta := report.FileMetadata(e.Info.ModTime())
	meta.Media = it.media
	row := report.RegistryRow{
		RelPath: e.Rel, FileName: path.Base(e.Rel), FileSize: size, FileType: ft.Type, FileMIME: ft.MIME,
		IsBinary: ft.IsBinary, IsMedia: ft.IsMedia, IsPicture: ft.IsPicture, IsVideo: ft.IsVideo, IsCatia: ft.IsCatia, IsLarge: ft.IsLarge,
		Metadata: meta,
	}
	if err := r.reg.Write(row); err != nil {
		return err
	}
	if r.cfg.Candidate == nil || !r.cfg.Candidate(e.Rel, ft) {
		return nil
	}
	return r.cand.write(Candidate{RelPath: e.Rel, Size: size, MTime: e.Info.ModTime().UTC(), MIME: ft.MIME, FileType: ft.Type})
}

// readMedia collects container metadata. File mode uses the pure-Go ISO parser only; media mode
// also runs ffprobe for other formats and ISO failures.
func (r *scanRun) readMedia(ctx context.Context, path, mime string) *media.MediaInfo {
	if r.cfg.Metadata == "media" {
		return media.ReadMetadata(ctx, path, mime, media.FFprobeReader{Path: r.cfg.FFprobePath, Log: r.s.Log})
	}
	if media.IsISOBMFF(mime) {
		return media.ReadISO(ctx, path, mime)
	}
	return nil
}

// skip counts an entry without a row; the walker or write already logged its warning.
func (r *scanRun) skip(rel, reason, detail string) {
	r.stats.AddSkipped(reason)
	r.s.RecordIssue(state.IssueSkipped, rel, reason+": "+detail)
}

func secondsDuration(s float64) time.Duration { return time.Duration(s * float64(time.Second)) }
