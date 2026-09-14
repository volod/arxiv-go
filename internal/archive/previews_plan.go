package archive

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"

	"github.com/volod/arxiv-go/internal/media"
	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
	"github.com/volod/arxiv-go/internal/state"
)

// videoPreviews is the preview plan of one video. A planning error is reported by the executor,
// after the move, as a failed preview.
type videoPreviews struct {
	video string // relative path, the same in both roots
	moved bool   // moved by an earlier run; new candidates are submitted after their commit
	plan  media.PreviewPlan
	err   error
}

// planSplitPreviews plans previews for new candidates and for videos earlier runs moved, so a
// rerun catches up missing previews. It returns the plans in path order and the estimated bytes
// of the previews that are not yet published, for the preflight.
func planSplitPreviews(ctx context.Context, s *Session, c SplitConfig, idx *previewIndex, videoRows []report.VideoRow, runner *media.Runner) ([]*videoPreviews, int64, error) {
	if !c.Preview.Enabled() {
		return nil, 0, nil
	}
	fileRows, err := report.LoadRegistry(c.Scan.Registry)
	if err != nil {
		return nil, 0, err
	}
	known := map[string]*media.MediaInfo{}
	sources := map[string]string{} // video -> current file to probe
	moved := map[string]bool{}
	for _, row := range videoRows {
		if row.Status == report.StatusMoved && scanner.LocalRelPath(row.RelPath) {
			sources[row.RelPath] = filepath.Join(s.cfg.VideoArchive, filepath.FromSlash(row.RelPath))
			moved[row.RelPath] = true
			known[row.RelPath] = row.Metadata.Media
		}
	}
	mimes := map[string]string{}
	for _, row := range fileRows {
		mimes[row.RelPath] = row.FileMIME
		if row.Metadata.Media != nil {
			known[row.RelPath] = row.Metadata.Media
		}
	}
	err = ReadCandidates(s.Run.File(state.CandidatesFile), func(v Candidate) error {
		if _, ok := sources[v.RelPath]; !ok && scanner.LocalRelPath(v.RelPath) {
			sources[v.RelPath] = filepath.Join(s.cfg.Archive, filepath.FromSlash(v.RelPath))
		}
		return nil
	})
	if err != nil {
		return nil, 0, err
	}
	p := previewPlanner{s: s, opts: c.Preview, idx: idx, reserved: map[string]bool{}}
	var plans []*videoPreviews
	var estimate int64
	for _, video := range sortedKeys(sources) {
		if err := ctx.Err(); err != nil {
			return nil, 0, err
		}
		if _, err := os.Lstat(sources[video]); err != nil {
			continue // neither a candidate nor a moved video any more
		}
		info := known[video]
		if !usableMedia(info) {
			info = runner.Probe(ctx, sources[video], mimes[video])
		}
		item := p.plan(video, info)
		item.moved = moved[video]
		estimate = addSat(estimate, p.missingBytes(item))
		plans = append(plans, item)
	}
	return plans, estimate, nil
}

type previewPlanner struct {
	s        *Session
	opts     media.PreviewOptions
	idx      *previewIndex
	reserved map[string]bool // planned preview paths of this run
}

func (p *previewPlanner) plan(video string, info *media.MediaInfo) *videoPreviews {
	item := &videoPreviews{video: video}
	if info.Error != "" {
		item.err = errors.New(info.Error)
		return item
	}
	item.plan, item.err = media.PlanPreviews(*info, video, p.opts, func(name string) bool {
		return p.occupied(video, previewPathFor(video, name))
	})
	if item.err != nil {
		return item
	}
	for _, job := range item.plan.Jobs {
		p.reserved[previewPathFor(video, job.Output)] = true
	}
	for _, warning := range item.plan.Warnings {
		p.s.Log.Warn(warning, "rel_path", video)
	}
	return item
}

// occupied protects files of other videos and of the operator; a preview this video already owns
// or started keeps its name.
func (p *previewPlanner) occupied(video, preview string) bool {
	switch {
	case p.reserved[preview]:
		return true
	case p.idx.owned.has(video, preview), p.idx.generating.has(video, preview):
		return false
	}
	_, err := os.Lstat(p.idx.abs(preview))
	return !os.IsNotExist(err)
}

func (p *previewPlanner) missingBytes(item *videoPreviews) int64 {
	var n int64
	for _, job := range item.plan.Jobs {
		if !p.idx.published(item.video, previewPathFor(item.video, job.Output)) {
			n = addSat(n, job.EstimateBytes)
		}
	}
	return n
}

// usableMedia reports metadata complete enough to plan without probing again.
func usableMedia(m *media.MediaInfo) bool {
	return m != nil && m.Error == "" && m.VideoStreams > 0 && m.Width >= 2 && m.Height >= 2 && m.DurationS > 0
}

func sortedKeys[V any](m map[string]V) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	slices.Sort(keys)
	return keys
}
