package scanner

import (
	"errors"
	"path"
	"strings"
)

// Key is the walk order key of an archive entry: its relative path split into slash-separated
// segments. Keys compare segment by segment, which matches filepath.WalkDir order; plain string
// comparison does not, because '/' sorts after '-' and '.'. A checkpoint cursor is a Key.
type Key []string

// KeyOf returns the key of a relative slash path. The root ("" or ".") has an empty key, which
// precedes every other key.
func KeyOf(rel string) Key {
	if rel == "" || rel == "." {
		return Key{}
	}
	return Key(strings.Split(rel, "/"))
}

// String returns the relative slash path of k.
func (k Key) String() string { return strings.Join(k, "/") }

// Compare returns -1, 0 or +1 as a sorts before, equal to or after b in walk order. A key sorts
// after each of its prefixes, so a directory precedes its contents.
func Compare(a, b Key) int {
	for i := 0; i < len(a) && i < len(b); i++ {
		if c := strings.Compare(a[i], b[i]); c != 0 {
			return c
		}
	}
	switch {
	case len(a) < len(b):
		return -1
	case len(a) > len(b):
		return 1
	}
	return 0
}

// IsPrefix reports whether every segment of p is the leading segment of k, i.e. whether k names
// p itself or an entry inside the directory p.
func IsPrefix(p, k Key) bool {
	if len(p) > len(k) {
		return false
	}
	for i := range p {
		if p[i] != k[i] {
			return false
		}
	}
	return true
}

// cursorAction is what the walker does with an entry relative to a resume cursor.
type cursorAction uint8

const (
	cursorVisit   cursorAction = iota // after the cursor: deliver it
	cursorDescend                     // directory on the cursor's path: skip it, walk its contents
	cursorSkip                        // at or before the cursor, and so is its whole subtree
)

// against classifies key k for a walk resuming after cursor. Entries with a key less than or
// equal to the cursor were already delivered. A directory whose key is a prefix of the cursor
// may still contain later entries; any other directory at or before the cursor is pruned, since
// every descendant then also precedes the cursor.
func against(k, cursor Key, dir bool) cursorAction {
	if cursor == nil || Compare(k, cursor) > 0 {
		return cursorVisit
	}
	if dir && IsPrefix(k, cursor) {
		return cursorDescend
	}
	return cursorSkip
}

// Glob is a compiled --exclude pattern: path.Match syntax per segment, and "**" matching any
// number of segments, including none. It is anchored at the walked root, so "*.tmp" matches only
// top-level names and "**/*.tmp" matches at any depth.
type Glob struct {
	src  string
	segs []string
}

// CompileGlob validates and compiles a relative slash-path glob.
func CompileGlob(pattern string) (Glob, error) {
	if pattern == "" || strings.HasPrefix(pattern, "/") {
		return Glob{}, errors.New("must be a non-empty path relative to the root")
	}
	segs := strings.Split(pattern, "/")
	for _, s := range segs {
		switch {
		case s == "":
			return Glob{}, errors.New("empty path segment")
		case s == "..":
			return Glob{}, errors.New("'..' segments are not allowed")
		case s == "**":
			continue
		}
		if _, err := path.Match(s, ""); err != nil {
			return Glob{}, errors.New("malformed glob pattern")
		}
	}
	return Glob{src: pattern, segs: segs}, nil
}

// String returns the source pattern.
func (g Glob) String() string { return g.src }

// Match reports whether the entry with key k matches the pattern. It runs in O(len(pattern) *
// len(k)) using the classic single-star backtracking scheme, where "**" plays the role of "*".
func (g Glob) Match(k Key) bool {
	p := g.segs
	pi, ki := 0, 0
	star, mark := -1, 0
	for ki < len(k) {
		switch {
		case pi < len(p) && p[pi] == "**":
			star, mark = pi, ki
			pi++
		case pi < len(p) && segmentMatch(p[pi], k[ki]):
			pi++
			ki++
		case star >= 0:
			mark++
			pi, ki = star+1, mark
		default:
			return false
		}
	}
	for pi < len(p) && p[pi] == "**" {
		pi++
	}
	return pi == len(p)
}

func segmentMatch(pattern, name string) bool {
	ok, _ := path.Match(pattern, name) // patterns are validated by CompileGlob
	return ok
}
