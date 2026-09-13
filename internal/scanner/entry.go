package scanner

import "io/fs"

// Reserved names owned by arxgo; see docs/openspec/spec.md#reserved-paths.
const (
	StateDirName      = ".arxgo"
	RegistryName      = "arxgo-registry.csv"
	VideoRegistryName = "arxgo-videos.csv"
	VideoSummaryName  = "arxgo-videos.md"
	PartSuffix        = ".arxgo-part" // same value as fsops.PartSuffix
)

// Kind classifies a delivered entry.
type Kind uint8

const (
	KindDir     Kind = iota + 1 // directory below the root
	KindFile                    // regular file
	KindSymlink                 // symbolic link; recorded, never followed or read
	KindSpecial                 // device, socket, pipe, junction or other irregular entry; skipped
)

func (k Kind) String() string {
	switch k {
	case KindDir:
		return "dir"
	case KindFile:
		return "file"
	case KindSymlink:
		return "symlink"
	case KindSpecial:
		return "special"
	}
	return "unknown"
}

// Skip reasons reported by Entry.SkipReason and counted in Stats.Skipped.
const (
	ReasonUnreadable = "unreadable"
	ReasonSpecial    = "special"
)

// Entry is one archive entry delivered in walk order. Every path appears at most once.
type Entry struct {
	Path string      // OS path: the root as given joined with Rel
	Rel  string      // relative slash path
	Key  Key         // walk order key of Rel
	Kind Kind        //
	Info fs.FileInfo // Lstat information; nil when Err prevented reading it
	// Err is set when the entry could not be read: a file or symlink whose Lstat failed, or a
	// directory that could not be listed completely (its readable children are still walked).
	Err error
}

// SkipReason returns the reason the entry counts as skipped, or "" when it does not.
func (e Entry) SkipReason() string {
	switch {
	case e.Err != nil:
		return ReasonUnreadable
	case e.Kind == KindSpecial:
		return ReasonSpecial
	}
	return ""
}

// Stats counts delivered entries. The scan operation keeps one per run and persists it in the
// checkpoint, so a resumed walk continues the counts instead of restarting them.
type Stats struct {
	Dirs     int64
	Files    int64
	Symlinks int64
	Special  int64
	Skipped  map[string]int64 // by reason, see Entry.SkipReason
}

// Count adds e. An unreadable directory counts both as a directory and as skipped.
func (s *Stats) Count(e Entry) {
	switch e.Kind {
	case KindDir:
		s.Dirs++
	case KindFile:
		s.Files++
	case KindSymlink:
		s.Symlinks++
	case KindSpecial:
		s.Special++
	}
	if reason := e.SkipReason(); reason != "" {
		if s.Skipped == nil {
			s.Skipped = map[string]int64{}
		}
		s.Skipped[reason]++
	}
}

// SkippedTotal returns the number of skipped entries; a non-zero value makes the run exit 6.
func (s *Stats) SkippedTotal() int64 {
	var n int64
	for _, c := range s.Skipped {
		n += c
	}
	return n
}
