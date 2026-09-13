package scanner

import (
	"bytes"
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sort"
	"strings"
	"testing"
)

// orderFixture has names whose walk order differs from string order of the full path.
var orderFixture = []string{
	"a/b", "a/b-c/d", "a-b/x", "a.txt", "a0/y", "B.txt", ".hidden", "empty/",
	"deep/1/2/3/4/leaf.bin", "deep/1/2/3/4a", "deep/1/2-x", "deep/1/2/z",
	"sp ace/été.mp4", "comma,quote\"/f",
}

func TestWalkOrderEqualsWalkDirOrder(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, orderFixture...)
	got := walkAll(t, root, Options{})
	want := rawWalkDir(t, root)
	if !slices.Equal(rels(got), want) {
		t.Fatalf("walk order\n got %q\nwant %q", rels(got), want)
	}
	for i := 1; i < len(got); i++ {
		if Compare(got[i-1].Key, got[i].Key) >= 0 {
			t.Errorf("keys not strictly increasing at %q, %q", got[i-1].Rel, got[i].Rel)
		}
	}
	byString := slices.Clone(want)
	sort.Strings(byString)
	if slices.Equal(byString, want) {
		t.Fatal("fixture does not distinguish string order from walk order")
	}
	for _, e := range got {
		if e.Path != filepath.Join(root, filepath.FromSlash(e.Rel)) || e.Info == nil || e.Err != nil {
			t.Errorf("entry %+v: bad path, info or error", e)
		}
		if (e.Kind == KindDir) != e.Info.IsDir() {
			t.Errorf("entry %q kind %s disagrees with info", e.Rel, e.Kind)
		}
	}
}

func TestWalkResumeAfterEveryCursorYieldsSuffix(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, orderFixture...)
	full := rels(walkAll(t, root, Options{}))
	for i, rel := range full {
		got := rels(walkAll(t, root, Options{Cursor: KeyOf(rel)}))
		if want := full[i+1:]; !slices.Equal(got, want) {
			t.Errorf("cursor %q\n got %q\nwant %q", rel, got, want)
		}
	}
	// Cursors naming entries that no longer exist resume at the next existing key.
	for _, cursor := range []string{"a/a", "a/b-c/zz", "a-", "deep/1/2/3/4/leaf.bin/x", "zzz", "."} {
		var want []string
		for _, rel := range full {
			if Compare(KeyOf(rel), KeyOf(cursor)) > 0 {
				want = append(want, rel)
			}
		}
		got := rels(walkAll(t, root, Options{Cursor: KeyOf(cursor)}))
		if !slices.Equal(got, want) {
			t.Errorf("missing cursor %q\n got %q\nwant %q", cursor, got, want)
		}
	}
}

func TestWalkResumeMidDeepDirectoryPrunesEarlierSubtrees(t *testing.T) {
	skipUnlessPermissionsEnforced(t)
	root := t.TempDir()
	makeTree(t, root, "a/locked/f", "m/n/o/p1", "m/n/o/p2", "m/n/o/p3", "m/n/q", "z")
	lockDir(t, filepath.Join(root, "a", "locked"))
	var logs bytes.Buffer
	got := walkAll(t, root, Options{Cursor: KeyOf("m/n/o/p2"), Log: slog.New(slog.NewTextHandler(&logs, nil))})
	if want := []string{"m/n/o/p3", "m/n/q", "z"}; !slices.Equal(rels(got), want) {
		t.Fatalf("got %q, want %q", rels(got), want)
	}
	// The unreadable directory lies wholly before the cursor, so it was never listed.
	if logs.Len() != 0 {
		t.Errorf("pruned subtree was read: %s", logs.String())
	}
}

func TestWalkExcludesReservedPathsSkipPathsAndGlobs(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root,
		".arxgo/lock", ".arxgo/runs/r/wal.jsonl", "arxgo-registry.csv", "arxgo-videos.csv", "arxgo-videos.md",
		"video.mp4.arxgo-part", "sub/clip.mp4.arxgo-part", "sub/clip.mp4",
		"sub/.arxgo/kept", "sub/arxgo-registry.csv", // reserved names only at the root
		"custom/reg.csv", "videos/v.mp4", "videos-2/v.mp4",
		"x.tmp", "sub/x.tmp", "cache/a/b", "cache2/c", "deep/node_modules/m.js", "keep.txt",
	)
	got := walkAll(t, root, Options{
		Exclude:   []string{"*.tmp", "cache/**", "**/node_modules"},
		SkipPaths: []string{filepath.Join(root, "custom", "reg.csv"), filepath.Join(root, "videos"), filepath.Dir(root), root},
	})
	want := []string{"cache2", "cache2/c", "custom", "deep", "keep.txt",
		"sub", "sub/.arxgo", "sub/.arxgo/kept", "sub/arxgo-registry.csv", "sub/clip.mp4", "sub/x.tmp",
		"videos-2", "videos-2/v.mp4"}
	if !slices.Equal(rels(got), want) {
		t.Fatalf("got %q\nwant %q", rels(got), want)
	}
}

func TestWalkRejectsInvalidExclude(t *testing.T) {
	err := Walk(context.Background(), t.TempDir(), Options{Exclude: []string{"a/[b"}}, func(Entry) error { return nil })
	if err == nil || !strings.Contains(err.Error(), "malformed") {
		t.Fatalf("Walk = %v, want malformed glob error", err)
	}
}

func TestWalkReportsSymlinksWithoutFollowing(t *testing.T) {
	root := t.TempDir()
	canSymlink(t, root)
	outside := t.TempDir()
	makeTree(t, outside, "secret/file.txt")
	makeTree(t, root, "real/f.txt")
	links := map[string]string{
		"to-dir":      filepath.Join(outside, "secret"),
		"to-file":     filepath.Join(root, "real", "f.txt"),
		"dangling":    filepath.Join(root, "missing"),
		"real/loop":   "..",
		"real/.arxgo": "..", // reserved only at the root, so this is a plain symlink
	}
	for name, target := range links {
		if err := os.Symlink(target, filepath.Join(root, filepath.FromSlash(name))); err != nil {
			t.Fatal(err)
		}
	}
	got := walkAll(t, root, Options{})
	want := []string{"dangling", "real", "real/.arxgo", "real/f.txt", "real/loop", "to-dir", "to-file"}
	if !slices.Equal(rels(got), want) {
		t.Fatalf("got %q, want %q", rels(got), want)
	}
	var stats Stats
	for _, e := range got {
		stats.Count(e)
		if _, isLink := links[e.Rel]; isLink && (e.Kind != KindSymlink || e.Info.Mode()&fs.ModeSymlink == 0 || e.SkipReason() != "") {
			t.Errorf("%q: kind %s mode %v reason %q, want unskipped symlink", e.Rel, e.Kind, e.Info.Mode(), e.SkipReason())
		}
	}
	if stats.Symlinks != 5 || stats.Files != 1 || stats.Dirs != 1 || stats.SkippedTotal() != 0 {
		t.Errorf("stats = %+v", stats)
	}
}

func TestWalkThroughSymlinkedRoot(t *testing.T) {
	base := t.TempDir()
	canSymlink(t, base)
	makeTree(t, base, "real/a/b.txt")
	link := filepath.Join(base, "link")
	if err := os.Symlink(filepath.Join(base, "real"), link); err != nil {
		t.Fatal(err)
	}
	got := walkAll(t, link, Options{SkipPaths: []string{filepath.Join(link, "a", "b.txt")}})
	if len(got) != 1 || got[0].Rel != "a" || got[0].Path != filepath.Join(link, "a") {
		t.Fatalf("got %+v", got)
	}
}

func TestWalkRootErrors(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, "file")
	noop := func(Entry) error { return nil }
	for _, p := range []string{filepath.Join(root, "missing"), filepath.Join(root, "file")} {
		if err := Walk(context.Background(), p, Options{}, noop); err == nil {
			t.Errorf("Walk(%q) = nil, want error", p)
		}
	}
}

func TestWalkCallbackControl(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, "a/1", "a/2", "b/3")
	boom := errors.New("boom")
	var n int
	err := Walk(context.Background(), root, Options{}, func(e Entry) error {
		n++
		if e.Rel == "a/2" {
			return boom
		}
		return nil
	})
	if !errors.Is(err, boom) || n != 3 {
		t.Errorf("callback error: err=%v after %d entries", err, n)
	}

	n = 0
	err = Walk(context.Background(), root, Options{}, func(e Entry) error {
		n++
		if e.Rel == "a" {
			return ErrStop
		}
		return nil
	})
	if err != nil || n != 1 {
		t.Errorf("ErrStop: err=%v after %d entries", err, n)
	}

	// A held-back directory is delivered while WalkDir visits its first child, so SkipDir from
	// the callback would prune the wrong entry; it is rejected.
	err = Walk(context.Background(), root, Options{}, func(e Entry) error { return fs.SkipDir })
	if err == nil || !errors.Is(err, fs.SkipDir) || !strings.Contains(err.Error(), "ErrStop") {
		t.Errorf("SkipDir: err=%v", err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	n = 0
	err = Walk(ctx, root, Options{}, func(e Entry) error {
		n++
		cancel()
		return nil
	})
	if !errors.Is(err, context.Canceled) || n != 1 {
		t.Errorf("cancel: err=%v after %d entries", err, n)
	}
}

func TestStatsCount(t *testing.T) {
	var s Stats
	for _, e := range []Entry{
		{Kind: KindDir}, {Kind: KindDir, Err: fs.ErrPermission}, {Kind: KindFile}, {Kind: KindFile, Err: fs.ErrNotExist},
		{Kind: KindSymlink}, {Kind: KindSpecial},
	} {
		s.Count(e)
	}
	if s.Dirs != 2 || s.Files != 2 || s.Symlinks != 1 || s.Special != 1 ||
		s.Skipped[ReasonUnreadable] != 2 || s.Skipped[ReasonSpecial] != 1 || s.SkippedTotal() != 3 {
		t.Errorf("stats = %+v", s)
	}
}

func TestWalkRootSpellings(t *testing.T) {
	root := t.TempDir()
	makeTree(t, root, "a/b.txt")
	sep := string(filepath.Separator)
	for _, spelling := range []string{root + sep, root + sep + "." + sep, root + sep + sep} {
		got := walkAll(t, spelling, Options{})
		if !slices.Equal(rels(got), []string{"a", "a/b.txt"}) || got[1].Path != filepath.Join(spelling, "a", "b.txt") {
			t.Errorf("root %q: got %+v", spelling, got)
		}
	}
	t.Chdir(root)
	got := walkAll(t, ".", Options{SkipPaths: []string{filepath.Join(root, "a", "b.txt")}})
	if !slices.Equal(rels(got), []string{"a"}) || got[0].Path != "a" {
		t.Errorf("root \".\": got %+v", got)
	}
}
