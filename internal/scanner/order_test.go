package scanner

import (
	"slices"
	"sort"
	"strings"
	"testing"
)

func TestCompareFollowsSegmentsNotStrings(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"a/b", "a-b/x", -1}, // string order says "a-b/x" < "a/b"
		{"a/b", "a.b", -1},
		{"a", "a/b", -1}, // a directory precedes its contents
		{"a/b", "a", 1},
		{"a/b", "a/b", 0},
		{"", "a", -1},
		{"B", "a", -1}, // byte order: upper case first
		{"z", "é", -1},
		{"x/y/z", "x/y0", -1},
	}
	for _, c := range cases {
		if got := Compare(KeyOf(c.a), KeyOf(c.b)); got != c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
		if got := Compare(KeyOf(c.b), KeyOf(c.a)); got != -c.want {
			t.Errorf("Compare(%q, %q) = %d, want %d", c.b, c.a, got, -c.want)
		}
	}
	paths := []string{"a-b/x", "a/b", "a.b", "a", "a/b/c", "a0"}
	byKey := slices.Clone(paths)
	sort.Slice(byKey, func(i, j int) bool { return Compare(KeyOf(byKey[i]), KeyOf(byKey[j])) < 0 })
	byString := slices.Clone(paths)
	sort.Strings(byString)
	if want := []string{"a", "a/b", "a/b/c", "a-b/x", "a.b", "a0"}; !slices.Equal(byKey, want) {
		t.Errorf("key order = %q, want %q", byKey, want)
	}
	if slices.Equal(byKey, byString) {
		t.Errorf("fixture does not distinguish key order from string order")
	}
}

func TestKeyOfRootAndString(t *testing.T) {
	if k := KeyOf("."); len(k) != 0 || k == nil {
		t.Errorf("KeyOf(.) = %#v, want empty non-nil key", k)
	}
	if got := KeyOf("x/y z/é").String(); got != "x/y z/é" {
		t.Errorf("String round trip = %q", got)
	}
	if !IsPrefix(KeyOf("a/b"), KeyOf("a/b/c")) || !IsPrefix(KeyOf("a/b"), KeyOf("a/b")) ||
		IsPrefix(KeyOf("a/b"), KeyOf("a/bc")) || IsPrefix(KeyOf("a/b/c"), KeyOf("a/b")) {
		t.Errorf("IsPrefix disagrees with segment prefixes")
	}
}

func TestCursorActions(t *testing.T) {
	cursor := KeyOf("m/n/o.txt")
	cases := []struct {
		key  string
		dir  bool
		want cursorAction
	}{
		{"a", true, cursorSkip},      // wholly before: pruned
		{"m", true, cursorDescend},   // on the cursor's path
		{"m/n", true, cursorDescend}, // on the cursor's path
		{"m/a", true, cursorSkip},
		{"m/n/o.txt", false, cursorSkip}, // the cursor itself was delivered
		{"m/n/o.txt", true, cursorDescend},
		{"m/n/a.txt", false, cursorSkip},
		{"m/n/p", true, cursorVisit},
		{"m/n-1", true, cursorVisit},
		{"z", false, cursorVisit},
	}
	for _, c := range cases {
		if got := against(KeyOf(c.key), cursor, c.dir); got != c.want {
			t.Errorf("against(%q, dir=%v) = %d, want %d", c.key, c.dir, got, c.want)
		}
	}
	if got := against(KeyOf("a"), nil, true); got != cursorVisit {
		t.Errorf("nil cursor: got %d, want visit", got)
	}
}

func TestGlobMatch(t *testing.T) {
	cases := []struct {
		pattern string
		match   []string
		noMatch []string
	}{
		{"*.tmp", []string{"x.tmp", ".tmp"}, []string{"a/x.tmp", "x.tmpl"}},
		{"**/*.tmp", []string{"x.tmp", "a/x.tmp", "a/b/c/x.tmp"}, []string{"a/x.tmp/y", "x.txt"}},
		{"cache/**", []string{"cache", "cache/a", "cache/a/b"}, []string{"cache2", "x/cache"}},
		{"**/node_modules", []string{"node_modules", "a/b/node_modules"}, []string{"node_modules/x", "a/node_modules2"}},
		{"a/**/b/*.iso", []string{"a/b/x.iso", "a/x/y/b/z.iso", "a/b/b/x.iso"}, []string{"a/b.iso", "a/x/b/y/z.iso"}},
		{"[a-z]?.bak", []string{"ab.bak"}, []string{"Ab.bak", "abc.bak"}},
		{"**", []string{"x", "x/y"}, nil},
		{"**/**/x", []string{"x", "a/x", "a/b/x"}, []string{"a/y"}},
		{"Thumbs.db", []string{"Thumbs.db"}, []string{"a/Thumbs.db", "thumbs.db"}},
		{"a b/é*", []string{"a b/été"}, []string{"a b/e"}},
	}
	for _, c := range cases {
		g, err := CompileGlob(c.pattern)
		if err != nil {
			t.Fatalf("CompileGlob(%q): %v", c.pattern, err)
		}
		for _, p := range c.match {
			if !g.Match(KeyOf(p)) {
				t.Errorf("%q should match %q", c.pattern, p)
			}
		}
		for _, p := range c.noMatch {
			if g.Match(KeyOf(p)) {
				t.Errorf("%q should not match %q", c.pattern, p)
			}
		}
	}
}

func TestGlobManyDoubleStarsStaysLinear(t *testing.T) {
	g, err := CompileGlob(strings.Repeat("**/", 30) + "never")
	if err != nil {
		t.Fatal(err)
	}
	if g.Match(KeyOf(strings.Repeat("d/", 200) + "x")) {
		t.Fatal("unexpected match")
	}
}

func TestCompileGlobRejectsInvalid(t *testing.T) {
	for g, want := range map[string]string{
		"":      "non-empty",
		"/abs":  "relative",
		"a//b":  "empty path segment",
		"a/":    "empty path segment",
		"../x":  "'..'",
		"a/[b":  "malformed",
		"a/\\":  "malformed",
		"x/../": "'..'",
	} {
		if _, err := CompileGlob(g); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("CompileGlob(%q) = %v, want error containing %q", g, err, want)
		}
	}
}
