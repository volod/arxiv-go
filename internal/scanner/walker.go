package scanner

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
)

// Options control a walk.
type Options struct {
	// Exclude holds --exclude globs. A matching directory is pruned with its whole subtree.
	Exclude []string
	// SkipPaths are OS paths excluded like reserved paths when they lie inside the root, for
	// example an explicit --registry file or a video archive root nested in the archive.
	SkipPaths []string
	// Cursor, when non-nil, resumes the walk: entries whose key is less than or equal to it are
	// not delivered, and directories wholly before it are not read.
	Cursor Key
	// Log receives one warning per skipped entry; nil discards.
	Log *slog.Logger
}

// ErrStop may be returned by the callback to end the walk early without an error.
var ErrStop = errors.New("scanner: stop walk")

// Walk traverses root with filepath.WalkDir and calls fn for every directory, regular file,
// symlink and special entry below it, in walk order. Reserved paths, excluded globs and
// SkipPaths are never delivered, and symlinks are never followed. Unreadable entries are delivered
// with Err set and do not abort the walk. Walk returns the first error from fn (other than
// ErrStop), a context error, or an error when root itself cannot be walked.
func Walk(ctx context.Context, root string, opts Options, fn func(Entry) error) error {
	w, err := newWalker(root, opts, fn)
	if err != nil {
		return err
	}
	w.ctx = ctx
	start := root
	if fi, err := os.Lstat(root); err != nil {
		return fmt.Errorf("scanner: walk %s: %w", root, err)
	} else if fi.Mode()&fs.ModeSymlink != 0 {
		// A root given through a symlink is walked through its target; entries keep root's name.
		w.linked = true
		if start, err = filepath.EvalSymlinks(root); err != nil {
			return fmt.Errorf("scanner: walk %s: %w", root, err)
		}
	} else if !fi.IsDir() {
		return fmt.Errorf("scanner: walk %s: not a directory", root)
	}
	// WalkDir joins names onto the root with filepath.Join, which cleans; "." yields bare names.
	w.start = filepath.Clean(start)
	switch {
	case w.start == ".":
		w.prefix = ""
	case strings.HasSuffix(w.start, string(filepath.Separator)):
		w.prefix = w.start
	default:
		w.prefix = w.start + string(filepath.Separator)
	}
	err = filepath.WalkDir(w.start, w.visit)
	if err == nil {
		err = w.flush(nil)
	}
	if errors.Is(err, ErrStop) {
		return nil
	}
	return err
}

type walker struct {
	ctx     context.Context
	root    string
	start   string
	linked  bool   // root is a symlink walked through its target
	prefix  string // start with a trailing separator; WalkDir paths below the root begin with it
	opts    Options
	globs   []Glob
	skip    map[string]bool // relative slash paths from SkipPaths
	fn      func(Entry) error
	log     *slog.Logger
	pending *Entry // directory held back until its listing result is known
}

func newWalker(root string, opts Options, fn func(Entry) error) (*walker, error) {
	w := &walker{root: root, opts: opts, fn: fn, log: opts.Log, skip: map[string]bool{}}
	if w.log == nil {
		w.log = slog.New(slog.DiscardHandler)
	}
	for _, pattern := range opts.Exclude {
		g, err := CompileGlob(pattern)
		if err != nil {
			return nil, fmt.Errorf("scanner: exclude %q: %w", pattern, err)
		}
		w.globs = append(w.globs, g)
	}
	absRoot, err := filepath.Abs(root)
	if err != nil {
		return nil, fmt.Errorf("scanner: walk %s: %w", root, err)
	}
	for _, p := range opts.SkipPaths {
		abs, err := filepath.Abs(p)
		if err != nil {
			continue
		}
		rel, err := filepath.Rel(absRoot, abs)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			continue
		}
		w.skip[filepath.ToSlash(rel)] = true
	}
	return w, nil
}

func (w *walker) visit(p string, d fs.DirEntry, walkErr error) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if p == w.start {
		if walkErr != nil {
			if d == nil {
				return fmt.Errorf("scanner: walk %s: %w", w.root, walkErr)
			}
			// The root exists but cannot be listed: nothing below it is visible.
			return fmt.Errorf("scanner: list %s: %w", w.root, walkErr)
		}
		return nil
	}
	if !strings.HasPrefix(p, w.prefix) {
		return fmt.Errorf("scanner: walk %s: path outside root %s", p, w.start)
	}
	rel := filepath.ToSlash(p[len(w.prefix):])
	if walkErr != nil {
		// WalkDir reports a listing failure in a second call for a directory already visited.
		if w.pending != nil && w.pending.Rel == rel {
			return w.flush(walkErr)
		}
		// A directory on the resume cursor's path was not held back. When it is the cursor itself,
		// it was delivered with this error before. Otherwise it was delivered as readable and its
		// contents after the cursor are now lost, so it is reported although its key precedes the
		// cursor.
		if err := w.flush(nil); err != nil {
			return err
		}
		key := KeyOf(rel)
		if Compare(key, w.opts.Cursor) == 0 {
			return nil
		}
		return w.deliver(Entry{Path: w.path(p, rel), Rel: rel, Key: key, Kind: kindOf(d), Err: walkErr})
	}
	if err := w.flush(nil); err != nil {
		return err
	}
	key := KeyOf(rel)
	isDir := d.IsDir()
	if w.excluded(rel, key) {
		return skipDir(isDir)
	}
	switch against(key, w.opts.Cursor, isDir) {
	case cursorSkip:
		return skipDir(isDir)
	case cursorDescend:
		return nil
	}
	e := Entry{Path: w.path(p, rel), Rel: rel, Key: key, Kind: kindOf(d)}
	if e.Kind != KindSpecial {
		if e.Info, e.Err = d.Info(); e.Err != nil {
			e.Info = nil
		}
	}
	if isDir {
		w.pending = &e
		return nil
	}
	return w.deliver(e)
}

// flush delivers the held-back directory, marking it unreadable when listErr is set.
func (w *walker) flush(listErr error) error {
	if w.pending == nil {
		return nil
	}
	e := *w.pending
	w.pending = nil
	if e.Err == nil {
		e.Err = listErr
	}
	return w.deliver(e)
}

func (w *walker) deliver(e Entry) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	if reason := e.SkipReason(); reason != "" {
		attrs := []any{"path", e.Rel, "kind", e.Kind.String(), "reason", reason}
		if e.Err != nil {
			attrs = append(attrs, "error", e.Err.Error())
		}
		w.log.Warn("skipped entry", attrs...)
	}
	err := w.fn(e)
	if err == fs.SkipDir || err == fs.SkipAll {
		// WalkDir would apply these to whatever entry it is visiting, which may not be e.
		return fmt.Errorf("scanner: callback for %s returned %w; use ErrStop", e.Rel, err)
	}
	return err
}

// path returns the OS path of an entry below the root as given, from WalkDir's path p.
func (w *walker) path(p, rel string) string {
	if !w.linked {
		return p // filepath.Join(root, rel), as WalkDir already joined it
	}
	return filepath.Join(w.root, filepath.FromSlash(rel))
}

// excluded reports reserved paths, SkipPaths and --exclude matches.
func (w *walker) excluded(rel string, key Key) bool {
	if len(key) == 1 && reservedName(key[0]) {
		return true
	}
	if IsPartFile(rel) || w.skip[rel] {
		return true
	}
	for _, g := range w.globs {
		if g.Match(key) {
			return true
		}
	}
	return false
}

func kindOf(d fs.DirEntry) Kind {
	if d == nil {
		return KindSpecial
	}
	t := d.Type()
	switch {
	case t.IsDir():
		return KindDir
	case t.IsRegular():
		return KindFile
	case t&fs.ModeSymlink != 0:
		return KindSymlink
	}
	return KindSpecial
}

func skipDir(isDir bool) error {
	if isDir {
		return filepath.SkipDir
	}
	return nil
}
