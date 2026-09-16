package state

import (
	"slices"
	"testing"
)

func TestCommittedSet(t *testing.T) {
	s := NewCommittedSet()
	for _, p := range []string{"b/clip.mp4", "", "a.mp4", "b/clip.mp4"} {
		s.Add(p)
	}
	if s.Len() != 2 || s.Has("") || !s.Has("a.mp4") || !slices.Equal(s.Paths(), []string{"a.mp4", "b/clip.mp4"}) {
		t.Fatalf("set = %v", s.Paths())
	}
	var none *CommittedSet
	none.Add("x")
	if none.Has("x") || none.Len() != 0 || none.Paths() != nil {
		t.Fatal("nil set")
	}
}
