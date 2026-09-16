package state

import (
	"maps"
	"sort"
)

// CountBytes is a number of registry rows and the bytes they describe.
type CountBytes struct {
	Count int64 `json:"count"`
	Bytes int64 `json:"bytes"`
}

func (c *CountBytes) add(size int64) {
	c.Count++
	c.Bytes += size
}

// ScanStats are the cumulative statistics of a scan. The checkpoint stores them together with the
// scan cursor and the output offsets they describe, so a resumed scan continues them exactly.
type ScanStats struct {
	// Complete is set once the registry has been renamed into place.
	Complete bool  `json:"complete,omitempty"`
	Files    int64 `json:"files"`    // regular files with a registry row
	Dirs     int64 `json:"dirs"`     // directories, including unreadable ones
	Symlinks int64 `json:"symlinks"` // symlinks with a registry row
	Bytes    int64 `json:"bytes"`    // bytes of regular files with a row

	Binary  CountBytes `json:"binary"`
	Media   CountBytes `json:"media"`
	Picture CountBytes `json:"picture"`
	Video   CountBytes `json:"video"`
	Catia   CountBytes `json:"catia"`
	Large   CountBytes `json:"large"`

	// LargestVideo is the size of the largest video row, for split preflight.
	LargestVideo int64 `json:"largest_video,omitempty"`

	// MIME holds rows and bytes per detected MIME type of regular files.
	MIME map[string]CountBytes `json:"mime,omitempty"`
	// Skipped counts entries without a row by reason (unreadable, special).
	Skipped map[string]int64 `json:"skipped,omitempty"`
}

// FileFlags are the registry flags of one regular file, as counted by AddFile.
type FileFlags struct {
	Binary, Media, Picture, Video, Catia, Large bool
}

// AddFile counts a regular file row.
func (s *ScanStats) AddFile(size int64, mime string, f FileFlags) {
	s.Files++
	s.Bytes += size
	for _, c := range []struct {
		on  bool
		acc *CountBytes
	}{{f.Binary, &s.Binary}, {f.Media, &s.Media}, {f.Picture, &s.Picture}, {f.Video, &s.Video}, {f.Catia, &s.Catia}, {f.Large, &s.Large}} {
		if c.on {
			c.acc.add(size)
		}
	}
	if f.Video {
		s.LargestVideo = max(s.LargestVideo, size)
	}
	if s.MIME == nil {
		s.MIME = map[string]CountBytes{}
	}
	m := s.MIME[mime]
	m.add(size)
	s.MIME[mime] = m
}

// AddSkipped counts an entry that got no row.
func (s *ScanStats) AddSkipped(reason string) {
	if s.Skipped == nil {
		s.Skipped = map[string]int64{}
	}
	s.Skipped[reason]++
}

// SkippedTotal returns the number of entries without a row; a non-zero value makes the run partial.
func (s *ScanStats) SkippedTotal() int64 {
	var n int64
	for _, c := range s.Skipped {
		n += c
	}
	return n
}

// Rows returns the number of registry rows.
func (s *ScanStats) Rows() int64 { return s.Files + s.Symlinks }

// Clone returns a deep copy.
func (s *ScanStats) Clone() *ScanStats {
	c := *s
	c.MIME = maps.Clone(s.MIME)
	c.Skipped = maps.Clone(s.Skipped)
	return &c
}

// MIMEBytes is one entry of the top MIME types table.
type MIMEBytes struct {
	MIME string `json:"mime"`
	CountBytes
}

// TopMIMEs returns up to n MIME types ordered by bytes, then rows, then name.
func (s *ScanStats) TopMIMEs(n int) []MIMEBytes {
	out := make([]MIMEBytes, 0, len(s.MIME))
	for m, c := range s.MIME {
		out = append(out, MIMEBytes{MIME: m, CountBytes: c})
	}
	sort.Slice(out, func(i, j int) bool {
		a, b := out[i], out[j]
		if a.Bytes != b.Bytes {
			return a.Bytes > b.Bytes
		}
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.MIME < b.MIME
	})
	return out[:min(n, len(out))]
}

// ScanSummary is the scan section of report.json.
type ScanSummary struct {
	Files    int64            `json:"files"`
	Dirs     int64            `json:"dirs"`
	Symlinks int64            `json:"symlinks"`
	Bytes    int64            `json:"bytes"`
	Binary   CountBytes       `json:"binary"`
	Media    CountBytes       `json:"media"`
	Picture  CountBytes       `json:"picture"`
	Video    CountBytes       `json:"video"`
	Catia    CountBytes       `json:"catia,omitzero"`
	Large    CountBytes       `json:"large"`
	TopMIME  []MIMEBytes      `json:"top_mime"`
	Skipped  map[string]int64 `json:"skipped,omitempty"`
	ElapsedS float64          `json:"elapsed_s"`
}

// TopMIMECount is the number of MIME types kept in the summary.
const TopMIMECount = 10

// Summary returns the report section for s; elapsed is the cumulative scan time in seconds.
func (s *ScanStats) Summary(elapsedS float64) *ScanSummary {
	return &ScanSummary{
		Files: s.Files, Dirs: s.Dirs, Symlinks: s.Symlinks, Bytes: s.Bytes,
		Binary: s.Binary, Media: s.Media, Picture: s.Picture, Video: s.Video, Catia: s.Catia, Large: s.Large,
		TopMIME: s.TopMIMEs(TopMIMECount), Skipped: maps.Clone(s.Skipped), ElapsedS: elapsedS,
	}
}
