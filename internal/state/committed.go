package state

import (
	"slices"
	"sync"
)

// CommittedSet is the set of rel_path values whose WAL transactions committed. Resume skips
// these candidates. Lookups are O(1).
type CommittedSet struct {
	mu sync.RWMutex
	m  map[string]struct{}
}

// NewCommittedSet returns an empty set.
func NewCommittedSet() *CommittedSet {
	return &CommittedSet{m: make(map[string]struct{})}
}

// Add records relPath as committed. Empty paths are ignored.
func (s *CommittedSet) Add(relPath string) {
	if relPath == "" || s == nil {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.m == nil {
		s.m = make(map[string]struct{})
	}
	s.m[relPath] = struct{}{}
}

// Has reports whether relPath committed.
func (s *CommittedSet) Has(relPath string) bool {
	if s == nil {
		return false
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	_, ok := s.m[relPath]
	return ok
}

// Len returns the number of committed paths.
func (s *CommittedSet) Len() int {
	if s == nil {
		return 0
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	return len(s.m)
}

// Paths returns the committed rel_path values in sorted order.
func (s *CommittedSet) Paths() []string {
	if s == nil {
		return nil
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	out := make([]string, 0, len(s.m))
	for p := range s.m {
		out = append(out, p)
	}
	slices.Sort(out)
	return out
}
