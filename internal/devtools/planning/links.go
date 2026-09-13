package planning

import (
	"fmt"
	"io/fs"
	"net/url"
	"path"
	"regexp"
	"strings"
	"unicode"
)

var (
	linkRe    = regexp.MustCompile(`\[[^\]]*\]\(([^)\s]+)\)`)
	headingRe = regexp.MustCompile(`^#{1,6}\s+(.+?)\s*#*\s*$`)
)

// skippedDirs are never scanned for Markdown files. testdata holds golden
// product output whose links are archive paths, not repository documentation.
var skippedDirs = map[string]bool{
	".git": true, "dist": true, "bin": true, "vendor": true, "node_modules": true, "testdata": true,
}

// CheckLinks verifies that relative Markdown links in documentation .md files
// of fsys resolve to existing files and, when an anchor is given, to a heading.
func CheckLinks(fsys fs.FS) ([]string, error) {
	docs := map[string]string{}
	err := fs.WalkDir(fsys, ".", func(p string, d fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		if d.IsDir() {
			if p != "." && (skippedDirs[d.Name()] || strings.HasPrefix(d.Name(), ".")) && d.Name() != ".github" {
				return fs.SkipDir
			}
			return nil
		}
		if strings.HasSuffix(p, ".md") {
			b, err := fs.ReadFile(fsys, p)
			if err != nil {
				return err
			}
			docs[p] = string(b)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}
	anchors := map[string]map[string]bool{}
	for p, text := range docs {
		anchors[p] = headingAnchors(text)
	}
	var errs []string
	for p, text := range docs {
		for n, line := range proseLines(text) {
			for _, m := range linkRe.FindAllStringSubmatch(line, -1) {
				if msg := checkLink(fsys, p, m[1], anchors); msg != "" {
					errs = append(errs, fmt.Sprintf("%s:%d: %s", p, n+1, msg))
				}
			}
		}
	}
	return errs, nil
}

func checkLink(fsys fs.FS, from, target string, anchors map[string]map[string]bool) string {
	if strings.Contains(target, "://") || strings.HasPrefix(target, "mailto:") {
		return ""
	}
	file, anchor, _ := strings.Cut(target, "#")
	resolved := from
	if file != "" {
		unescaped, err := url.PathUnescape(file)
		if err != nil {
			return fmt.Sprintf("bad link %q", target)
		}
		resolved = path.Clean(path.Join(path.Dir(from), unescaped))
		if strings.HasPrefix(resolved, "..") {
			return fmt.Sprintf("link %q leaves the repository", target)
		}
		if _, err := fs.Stat(fsys, resolved); err != nil {
			return fmt.Sprintf("broken link %q", target)
		}
	}
	if anchor == "" {
		return ""
	}
	set, ok := anchors[resolved]
	if !ok {
		return "" // anchors into non-Markdown files are not checked
	}
	if !set[anchor] {
		return fmt.Sprintf("missing anchor %q", target)
	}
	return ""
}

// proseLines returns the lines of text with fenced code blocks blanked, so
// line numbers stay aligned.
func proseLines(text string) []string {
	lines := strings.Split(text, "\n")
	inFence := false
	for i, l := range lines {
		if strings.HasPrefix(strings.TrimSpace(l), "```") {
			inFence = !inFence
			lines[i] = ""
			continue
		}
		if inFence {
			lines[i] = ""
		}
	}
	return lines
}

// headingAnchors returns GitHub-style heading slugs, with -1, -2 suffixes for duplicates.
func headingAnchors(text string) map[string]bool {
	set := map[string]bool{}
	seen := map[string]int{}
	for _, line := range proseLines(text) {
		m := headingRe.FindStringSubmatch(line)
		if m == nil {
			continue
		}
		slug := Slug(m[1])
		if n := seen[slug]; n > 0 {
			set[fmt.Sprintf("%s-%d", slug, n)] = true
		} else {
			set[slug] = true
		}
		seen[slug]++
	}
	return set
}

// Slug converts a heading to its GitHub anchor.
func Slug(heading string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(heading) {
		switch {
		case r == ' ':
			b.WriteRune('-')
		case r == '-' || r == '_' || unicode.IsLetter(r) || unicode.IsDigit(r):
			b.WriteRune(r)
		}
	}
	return b.String()
}
