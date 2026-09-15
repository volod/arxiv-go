package catia

import (
	"sort"
	"strings"
)

type stringSet struct {
	m         map[string]struct{}
	n         int
	truncated bool
}

func newStringSet() *stringSet {
	return &stringSet{m: make(map[string]struct{})}
}

func (s *stringSet) add(v string) {
	if s == nil || s.n >= sidecarCap {
		if s != nil && s.n >= sidecarCap {
			s.truncated = true
		}
		return
	}
	v = strings.TrimSpace(v)
	if !qualifyingString(v) {
		return
	}
	if _, ok := s.m[v]; ok {
		return
	}
	if s.n+len(v) > sidecarCap {
		s.truncated = true
		return
	}
	s.m[v] = struct{}{}
	s.n += len(v)
}

func (s *stringSet) list(exclude []string) []string {
	drop := make(map[string]struct{}, len(exclude))
	for _, e := range exclude {
		drop[e] = struct{}{}
	}
	out := make([]string, 0, len(s.m))
	for v := range s.m {
		if _, skip := drop[v]; skip {
			continue
		}
		out = append(out, v)
	}
	sortByte(out)
	return out
}

func qualifyingString(s string) bool {
	if len(s) < minStringRun {
		return false
	}
	n := 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
			n++
			if n >= minStringAlpha {
				return true
			}
		}
	}
	return false
}

type asciiRun struct {
	b       []byte
	letters int
}

func (a *asciiRun) feed(c byte, out *stringSet) {
	if c >= 0x20 && c <= 0x7E {
		if len(a.b) < sidecarCap {
			a.b = append(a.b, c)
			if c >= 'A' && c <= 'Z' || c >= 'a' && c <= 'z' {
				a.letters++
			}
		} else if out != nil {
			out.truncated = true
		}
		return
	}
	a.flush(out)
}

func (a *asciiRun) flush(out *stringSet) {
	if out == nil {
		a.b = a.b[:0]
		a.letters = 0
		return
	}
	if len(a.b) >= minStringRun && a.letters >= minStringAlpha {
		out.add(string(a.b))
	}
	a.b = a.b[:0]
	a.letters = 0
}

type utf16Run struct {
	ascii asciiRun
}

func (u *utf16Run) feedPair(lo, hi byte, out *stringSet) {
	if hi == 0 && lo >= 0x20 && lo <= 0x7E {
		u.ascii.feed(lo, out)
		return
	}
	u.ascii.flush(out)
}

func (u *utf16Run) flush(out *stringSet) {
	u.ascii.flush(out)
}

func harvestASCIIValue(s string, out *stringSet) {
	var run asciiRun
	for i := 0; i < len(s); i++ {
		run.feed(s[i], out)
	}
	run.flush(out)
}

func sortByte(s []string) {
	sort.Strings(s)
}
