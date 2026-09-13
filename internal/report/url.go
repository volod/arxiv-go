package report

import (
	"net/url"
	"path"
	"path/filepath"
	"strings"
)

// EscapePath percent-encodes each slash-separated segment. "." and ".." are left intact.
func EscapePath(slashPath string) string {
	if slashPath == "" {
		return ""
	}
	parts := strings.Split(slashPath, "/")
	for i, p := range parts {
		if p == "" || p == "." || p == ".." {
			continue
		}
		parts[i] = url.PathEscape(p)
	}
	return strings.Join(parts, "/")
}

// ComposeURL joins a validated base URL (no trailing slash) with escaped rel_path segments.
func ComposeURL(base, relPath string) string {
	if base == "" || relPath == "" {
		return ""
	}
	return strings.TrimRight(base, "/") + "/" + EscapePath(relPath)
}

// RelativeLink is the URL-escaped relative path from fromDir to toAbs. It is empty when the
// two paths are not in the same filesystem namespace (filepath.Rel fails or is absolute).
func RelativeLink(fromDir, toAbs string) string {
	rel, err := filepath.Rel(fromDir, toAbs)
	if err != nil || rel == "" || filepath.IsAbs(rel) {
		return ""
	}
	slash := filepath.ToSlash(rel)
	if path.IsAbs(slash) {
		return ""
	}
	return EscapePath(slash)
}
