package state

import (
	"fmt"
	"testing"
	"time"
)

func TestCommittedSetLookupScalesTo1e6(t *testing.T) {
	const n = 1_000_000
	s := &CommittedSet{m: make(map[string]struct{}, n)}
	for i := 0; i < n; i++ {
		s.Add(fmt.Sprintf("dir/file-%d.mp4", i))
	}
	if s.Len() != n {
		t.Fatalf("len = %d", s.Len())
	}
	start := time.Now()
	for i := 0; i < 10_000; i++ {
		p := fmt.Sprintf("dir/file-%d.mp4", (i*97)%n)
		if !s.Has(p) {
			t.Fatalf("missing %s", p)
		}
	}
	if s.Has("dir/file-nope.mp4") || !s.Has("dir/file-0.mp4") || !s.Has("dir/file-999999.mp4") {
		t.Fatal("boundary lookup")
	}
	elapsed := time.Since(start)
	if elapsed > 2*time.Second {
		t.Fatalf("10000 lookups over 1e6 entries took %s", elapsed)
	}
	t.Logf("1e6 entries, 10000 lookups in %s", elapsed)
}

func TestCommittedSetIgnoresEmpty(t *testing.T) {
	s := NewCommittedSet()
	s.Add("")
	if s.Len() != 0 || s.Has("") {
		t.Fatal("empty path recorded")
	}
	var none *CommittedSet
	none.Add("x")
	if none.Has("x") {
		t.Fatal("nil set")
	}
}
