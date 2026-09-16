package archive

import (
	"context"
	"os"
	"path"
	"slices"
	"strings"
	"sync"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

// preservedRow is the registry row of a moved file, merged into the walk at its key.
type preservedRow struct {
	key scanner.Key
	row report.RegistryRow
}

// preservedQueue holds the preserved rows after the resume cursor in walk order.
type preservedQueue struct {
	rows []preservedRow
	next int
}

// Preserved row warnings; see docs/openspec/stage-1-core/registry.md#preserved-row-sources.
const (
	warnRowKept          = "registry-row-kept"
	warnRowReconstructed = "registry-row-reconstructed"
)

// preparePreserved resolves a row for every moved file after the resume cursor that --exclude does
// not cover, from the first source that has one: the stamped base registry, detection of the mirror
// copy, any base registry, then the replayed payload registry.
func (r *scanRun) preparePreserved(ctx context.Context, view *archiveView, base *registryBase) error {
	rels := make([]string, 0, len(view.moved))
	for rel := range view.moved {
		if scanner.Compare(scanner.KeyOf(rel), r.start) > 0 && !r.excludedRel(rel) {
			rels = append(rels, rel)
		}
	}
	if len(rels) == 0 {
		r.preserved = &preservedQueue{}
		return nil
	}
	slices.SortFunc(rels, func(a, b string) int { return scanner.Compare(scanner.KeyOf(a), scanner.KeyOf(b)) })
	var baseRows map[string]report.RegistryRow
	if base != nil {
		baseRows = base.rowsFor(r.s, func(rel string) bool { _, ok := view.moved[rel]; return ok })
	}
	rows := make([]preservedRow, len(rels))
	pending := make(chan int)
	var wg sync.WaitGroup
	for range min(r.cfg.Workers, len(rels)) {
		wg.Go(func() {
			for i := range pending {
				rows[i].key = scanner.KeyOf(rels[i])
				rows[i].row, _ = r.preservedFromBase(base, baseRows, rels[i], view.moved[rels[i]], true)
				if rows[i].row.RelPath == "" && ctx.Err() == nil {
					rows[i].row, _ = r.preservedFromMirror(ctx, rels[i], view.moved[rels[i]])
				}
			}
		})
	}
	for i := range rels {
		pending <- i
	}
	close(pending)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	var payload map[string]reconstructSource
	for i := range rows {
		if rows[i].row.RelPath != "" {
			continue
		}
		rel, m := rels[i], view.moved[rels[i]]
		if row, ok := r.preservedFromBase(base, baseRows, rel, m, false); ok {
			r.s.Log.Warn("moved file keeps its row from the existing registry; its mirror copy could not be read",
				"warning", warnRowKept, "rel_path", rel, "mirror", m.mirror)
			rows[i].row = row
			continue
		}
		if payload == nil {
			payload = payloadRowsForReconstruction(view)
		}
		rows[i].row = r.reconstructRow(rel, m, payload[rel])
		r.s.Log.Warn("moved file row reconstructed from the run history; its mirror copy could not be read",
			"warning", warnRowReconstructed, "rel_path", rel, "mirror", m.mirror)
	}
	r.preserved = &preservedQueue{rows: rows}
	return nil
}

// excludedRel applies --exclude to a moved file as the walker applies it to a present one: a
// matching ancestor directory excludes it too.
func (r *scanRun) excludedRel(rel string) bool {
	key := scanner.KeyOf(rel)
	for _, g := range r.globs {
		for n := 1; n <= len(key); n++ {
			if g.Match(key[:n]) {
				return true
			}
		}
	}
	return scanner.Reserved(rel)
}

// preservedFromBase takes the base row of rel; reusable limits it to a reusable base.
func (r *scanRun) preservedFromBase(base *registryBase, rows map[string]report.RegistryRow, rel string, m movedFile, reusable bool) (report.RegistryRow, bool) {
	if base == nil || (reusable && !base.reusable) {
		return report.RegistryRow{}, false
	}
	row, ok := rows[rel]
	if !ok {
		return report.RegistryRow{}, false
	}
	return r.baseRow(row, locationOf(m.kind)), true
}

// preservedFromMirror detects the mirror copy, which has the moved file's content, name, size and
// modification time. The mirror is only read.
func (r *scanRun) preservedFromMirror(ctx context.Context, rel string, m movedFile) (report.RegistryRow, bool) {
	if m.mirror == "" {
		return report.RegistryRow{}, false
	}
	p := osPath(m.mirror, rel)
	info, err := os.Lstat(p)
	if err != nil || !info.Mode().IsRegular() {
		return report.RegistryRow{}, false
	}
	it := &scanItem{e: scanner.Entry{Path: p, Rel: rel, Kind: scanner.KindFile, Info: info}}
	r.inspect(ctx, it)
	if it.err != nil {
		return report.RegistryRow{}, false
	}
	row := r.fileRow(it)
	row.Location = locationOf(m.kind)
	return row, true
}

// reconstructSource is what the payload registry and the WAL still know about a moved file.
type reconstructSource struct {
	fileName, mime, mtime string
	size                  int64
	metadata              report.Metadata
}

// payloadRowsForReconstruction replays each payload's history onto its registry in the archive
// root, or in the mirror root of the payload's last run when the archive has none.
func payloadRowsForReconstruction(view *archiveView) map[string]reconstructSource {
	out := map[string]reconstructSource{}
	for _, kind := range viewPayloads {
		history := view.history[kind]
		var roots []string
		roots = append(roots, view.archive)
		if n := len(history); n > 0 && history[n-1].mirror != "" {
			roots = append(roots, history[n-1].mirror)
		}
		if kind == PayloadVideo {
			var existing []report.VideoRow
			for _, root := range roots {
				if rows, err := report.LoadVideoFile(osPath(root, scanner.VideoRegistryName)); err == nil && rows != nil {
					existing = rows
					break
				}
			}
			idx, err := newPreviewIndex(view.archive, history)
			if err != nil {
				continue
			}
			for _, row := range replayVideoRows(existing, history, idx, nil) {
				out[row.RelPath] = reconstructSource{fileName: row.FileName, mime: row.FileMIME, mtime: row.Metadata.MTime,
					size: row.FileSize, metadata: row.Metadata}
			}
			continue
		}
		var existing []report.CatiaRow
		for _, root := range roots {
			if rows, err := report.LoadCatiaFile(osPath(root, scanner.CatiaRegistryName)); err == nil && rows != nil {
				existing = rows
				break
			}
		}
		for _, row := range replayCatiaRows(existing, history) {
			out[row.RelPath] = reconstructSource{fileName: row.FileName, mime: row.FileMIME, mtime: row.MTime, size: row.FileSize}
		}
	}
	return out
}

// reconstructRow builds the row of a moved file from the replayed payload registry row and the WAL.
func (r *scanRun) reconstructRow(rel string, m movedFile, src reconstructSource) report.RegistryRow {
	name := path.Base(rel)
	size := src.size
	if size == 0 {
		size = m.size
	}
	meta := src.metadata
	if m.kind != PayloadVideo {
		meta = report.Metadata{}
	}
	meta.LinkTarget = ""
	if meta.MTime = src.mtime; meta.MTime == "" {
		meta.MTime = report.FileMetadata(m.mtime).MTime
	}
	row := report.RegistryRow{
		RelPath: rel, FileName: name, FileSize: size, FileType: nameExtension(name), FileMIME: src.mime,
		IsBinary: true, IsVideo: m.kind == PayloadVideo, IsMedia: m.kind == PayloadVideo,
		IsCatia:  m.kind == PayloadCatia || scanner.IsCatiaName(name),
		IsLarge:  r.detect.LargeThreshold > 0 && size >= r.detect.LargeThreshold,
		Location: locationOf(m.kind), Metadata: meta,
	}
	if row.FileMIME == "" {
		row.FileMIME = scanner.MIMEOctetStream
	}
	return row
}

// nameExtension is the lower-cased part of name after its last dot; a name whose only dot is
// leading has none.
func nameExtension(name string) string {
	i := strings.LastIndexByte(name, '.')
	if i <= 0 {
		return ""
	}
	return strings.ToLower(name[i+1:])
}

// writePreservedBefore writes the preserved rows that precede key k in walk order. A walked entry
// other than a directory at a preserved row's key is present: presence wins and the preserved row
// is dropped.
func (r *scanRun) writePreservedBefore(k scanner.Key, dir bool) error {
	q := r.preserved
	for q != nil && q.next < len(q.rows) {
		c := scanner.Compare(q.rows[q.next].key, k)
		if c > 0 || (c == 0 && dir) {
			return nil
		}
		if c == 0 {
			q.next++
			return nil
		}
		if err := r.writePreserved(q.rows[q.next]); err != nil {
			return err
		}
		q.next++
	}
	return nil
}

// writeRemainingPreserved writes the preserved rows after the last walked entry.
func (r *scanRun) writeRemainingPreserved() error {
	q := r.preserved
	for q != nil && q.next < len(q.rows) {
		if err := r.writePreserved(q.rows[q.next]); err != nil {
			return err
		}
		q.next++
	}
	return nil
}

func (r *scanRun) writePreserved(p preservedRow) error {
	if err := r.reg.Write(p.row); err != nil {
		r.healthy = false
		return err
	}
	r.stats.AddPreserved(p.row.FileSize)
	r.s.Stats.Files.Add(1)
	r.s.Stats.Bytes.Add(p.row.FileSize)
	r.cursor = p.key
	return nil
}
