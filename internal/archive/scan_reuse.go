package archive

import (
	"io"
	"path"
	"time"

	"github.com/volod/arxiv-go/internal/report"
	"github.com/volod/arxiv-go/internal/scanner"
)

// baseStream follows the rows of a reusable base registry in walk order alongside the walk, so a
// present entry finds its base row without holding the whole registry in memory. See
// docs/openspec/stage-1-core/registry.md#incremental-update.
type baseStream struct {
	r       *scanRun
	rr      *report.RegistryReader
	row     report.RegistryRow // next unconsumed row; valid while key != nil
	key     scanner.Key
	started time.Time // base scan start, truncated to the second
	// restored maps the files the run history records as restored last to that restore's commit
	// time (archiveView.restored).
	restored map[string]time.Time
}

// newBaseStream returns nil when base has no reusable rows.
func newBaseStream(r *scanRun, base *registryBase, view *archiveView) *baseStream {
	if base == nil || !base.reusable {
		return nil
	}
	rr, err := base.reader()
	if err != nil {
		r.s.Log.Warn("existing file registry cannot be parsed; not reusing its rows", "registry", base.path, "error", err)
		return nil
	}
	b := &baseStream{r: r, rr: rr, started: base.scanStarted}
	if view != nil {
		b.restored = view.restored
	}
	b.advance()
	return b
}

// advance reads the next row. A parse error or a row out of walk order ends reuse for the rest of
// the scan: every later entry is detected.
func (b *baseStream) advance() {
	prev := b.key
	row, err := b.rr.Next()
	if err != nil {
		if err != io.EOF {
			b.r.s.Log.Warn("existing file registry cannot be parsed; detecting the remaining files", "error", err)
		}
		b.key = nil
		return
	}
	key := scanner.KeyOf(row.RelPath)
	if prev != nil && scanner.Compare(key, prev) <= 0 {
		b.r.s.Log.Warn("existing file registry is not in walk order; detecting the remaining files", "rel_path", row.RelPath)
		b.key = nil
		return
	}
	b.row, b.key = row, key
}

// at returns the base row of a present entry with key k when one can be reused so far: the same
// entry kind, location archive, not returned by a restore since the base scan started, and a
// modification time equal to the row's and earlier than the base scan start, both to the second. A
// regular file also needs the same size; a symlink still needs its link text compared. Entries are
// asked in walk order.
func (b *baseStream) at(e scanner.Entry) (*report.RegistryRow, bool) {
	if b == nil {
		return nil, false
	}
	for b.key != nil && scanner.Compare(b.key, e.Key) < 0 {
		b.advance()
	}
	if b.key == nil || scanner.Compare(b.key, e.Key) != 0 {
		return nil, false
	}
	row := b.row
	b.advance()
	mtime := e.Info.ModTime().UTC().Truncate(time.Second)
	switch {
	case row.Location != report.LocationArchive, // a preserved row may have been reconstructed
		b.restoredSinceBase(e.Rel), // restore set the location back without detection
		row.Metadata.MTime != report.FileMetadata(e.Info.ModTime()).MTime,
		!mtime.Before(b.started),
		isSymlinkRow(row) != (e.Kind == scanner.KindSymlink),
		e.Kind == scanner.KindFile && row.FileSize != e.Info.Size():
		return nil, false
	}
	return &row, true
}

// restoredSinceBase reports whether a restore that committed not earlier than the base scan start,
// to the second, returned rel. Restore sets the location of its row back to archive without
// detection, and that row may have been kept or reconstructed while the mirror was unreadable, so
// the first scan after the restore detects the file once.
func (b *baseStream) restoredSinceBase(rel string) bool {
	t, ok := b.restored[rel]
	return ok && !t.UTC().Truncate(time.Second).Before(b.started)
}

// isSymlinkRow reports whether a registry row describes a symlink: file_type symlink with no MIME. A
// regular file always has a MIME, also one whose extension is ".symlink".
func isSymlinkRow(row report.RegistryRow) bool {
	return row.FileType == "symlink" && row.FileMIME == ""
}

// baseRow is a row taken from a base registry for a file at location: is_large and is_catia are
// recomputed from --large-threshold and the file name, as detection would set them.
func (r *scanRun) baseRow(row report.RegistryRow, location string) report.RegistryRow {
	row.FileName = path.Base(row.RelPath)
	row.IsLarge = r.detect.LargeThreshold > 0 && row.FileSize >= r.detect.LargeThreshold
	row.IsCatia = row.FileMIME != scanner.MIMEAppleDouble && scanner.IsCatiaName(row.FileName)
	row.Location = location
	return row
}

// rowFileType is the classification a reused row carries, for the candidate selection.
func rowFileType(row report.RegistryRow) scanner.FileType {
	return scanner.FileType{MIME: row.FileMIME, Type: row.FileType, IsBinary: row.IsBinary, IsMedia: row.IsMedia,
		IsPicture: row.IsPicture, IsVideo: row.IsVideo, IsCatia: row.IsCatia, IsLarge: row.IsLarge}
}
