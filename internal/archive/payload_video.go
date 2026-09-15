package archive

import (
	"context"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// RoleVideoArchive is the preflight role of the video payload's mirror root.
const RoleVideoArchive Role = "video_archive"

// videoPayload moves videos into the video archive, writes arxgo-videos.csv and generates
// previews after each commit.
var videoPayload = &payloadSpec{
	kind: PayloadVideo, noun: "video", plural: "videos", role: RoleVideoArchive, registry: scanner.VideoRegistryName,
	candidate: func(ft scanner.FileType) bool { return ft.IsVideo },
	counters: func(st *Stats) payloadCounters {
		return payloadCounters{done: &st.VideosDone, skipped: &st.VideosSkipped, failed: &st.VideosFailed,
			bytes: &st.VideoBytes, mirrorWritten: &st.VideoArchiveWritten, mirrorFreed: &st.VideoArchiveFreed}
	},
	totals: func(c state.Counters) payloadTotals {
		return payloadTotals{done: c.VideosDone, skipped: c.VideosSkipped, failed: c.VideosFailed,
			bytes: c.VideoBytes, mirrorWritten: c.VideoWritten, mirrorFreed: c.VideoFreed}
	},
	loadRows: func(path string) ([]report.PayloadRow, error) {
		rows, err := report.LoadVideoFile(path)
		if rows == nil || err != nil {
			return nil, err
		}
		out := make([]report.PayloadRow, len(rows))
		for i, r := range rows {
			out[i] = r.Payload()
		}
		return out, nil
	},
	newSplit:   func(s *Session) splitHooks { return &videoSplit{s: s} },
	newRestore: newVideoRestore,
}

// videoSplit is the video part of split: the preview index and plans, and arxgo-videos.csv.
type videoSplit struct {
	s       *Session
	rows    []report.VideoRow
	history []runHistory
	idx     *previewIndex
	runner  *media.Runner
	plans   []*videoPreviews
}

func (v *videoSplit) prepare(_ context.Context, c *SplitConfig) error {
	s := v.s
	// A corrupt operator registry must stop the run before the first archive move.
	rows, err := loadVideoRegistry(s)
	if err != nil {
		return err
	}
	if v.history, err = readHistory(s.cfg.Archive, PayloadVideo); err != nil {
		return err
	}
	if v.idx, err = newPreviewIndex(s.cfg.Archive, v.history); err != nil {
		return err
	}
	if !s.cfg.DryRun {
		if err := v.idx.removeUnfinishedParts(); err != nil {
			return err
		}
	}
	v.rows = rows
	c.Scan.SkipPaths = append(c.Scan.SkipPaths, v.idx.skipPaths()...)
	return nil
}

func (v *videoSplit) plan(ctx context.Context, c *SplitConfig, remaining *Candidates) error {
	v.runner = &media.Runner{Tools: c.Tools, Log: v.s.Log}
	if c.Preview.Enabled() {
		v.rows = replayVideoRows(v.rows, v.history, v.idx, nil)
	}
	plans, previewBytes, err := planSplitPreviews(ctx, v.s, *c, v.idx, v.rows, v.runner)
	if err != nil {
		return err
	}
	v.plans, remaining.PreviewBytes = plans, previewBytes
	return nil
}

func (v *videoSplit) start(ctx context.Context, w *state.WAL) (postCommit, error) {
	previews := newSplitPreviews(ctx, previewExecutor{s: v.s, w: w, idx: v.idx, runner: v.runner}, v.plans)
	if err := previews.catchUp(w.Committed()); err != nil {
		return nil, previews.stop(err)
	}
	return previews, nil
}

func (v *videoSplit) writeRegistry(c SplitConfig) error { return writeVideoOutputs(v.s, c) }

// videoRestore is the video part of restore: arxgo-videos.csv rows and preview cleanup.
type videoRestore struct {
	s        *Session
	idx      *previewIndex
	videos   []report.VideoRow
	byRel    map[string]report.PayloadRow
	deletion bool // --previews delete
}

func newVideoRestore(_ context.Context, s *Session, c *RestoreConfig) (restoreHooks, error) {
	history, err := readHistory(s.cfg.Archive, PayloadVideo)
	if err != nil {
		return nil, err
	}
	idx, err := newPreviewIndex(s.cfg.Archive, history)
	if err != nil {
		return nil, err
	}
	videos, err := loadVideoRegistry(s)
	if err != nil {
		return nil, err
	}
	rows := make([]report.PayloadRow, len(videos))
	for i, r := range videos {
		rows[i] = r.Payload()
	}
	return &videoRestore{s: s, idx: idx, videos: videos, byRel: payloadRowsByPath(s, rows), deletion: c.DeletePreviews}, nil
}

func (v *videoRestore) rows() map[string]report.PayloadRow { return v.byRel }

// include keeps a video that split moved a candidate even when detection alone would not call it
// a video, for example one selected by --video-extensions, which restore does not take.
func (v *videoRestore) include(rel string) bool {
	row, ok := v.byRel[rel]
	return ok && (row.Status == report.StatusMoved || row.Status == report.StatusRestored)
}

func (v *videoRestore) beforeExecute(w *state.WAL) error {
	return restorePreviewsBeforeExecute(v.s, w, v.idx, v.deletion)
}

func (v *videoRestore) committed(w *state.WAL, rel string) error {
	if !v.deletion {
		return nil
	}
	return deletePreviews(v.s, w, v.idx, rel)
}

func (v *videoRestore) updateRegistry(c RestoreConfig) error {
	return writeRestoreOutputs(v.s, c, v.videos)
}

// loadVideoRegistry reads arxgo-videos.csv from the archive, or from the video archive when the
// archive has none.
func loadVideoRegistry(s *Session) ([]report.VideoRow, error) {
	for _, path := range registryPaths(s, scanner.VideoRegistryName) {
		rows, err := report.LoadVideoFile(path)
		if err != nil {
			return nil, corruptRegistry(path, err)
		}
		if rows != nil {
			return rows, nil
		}
	}
	return nil, nil
}
