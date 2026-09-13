package report

import (
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"strings"
	"unicode"
)

// StubMarker is the first front-matter key of every stub arxgo writes.
const StubMarker = "arxgo_stub"

// StubMarkerValue is the current stub format version written next to StubMarker.
const StubMarkerValue = "1"

// ErrNotFrontMatter reports a file that is not an arxgo stub header.
var ErrNotFrontMatter = errors.New("not YAML front matter")

// FrontMatter is a flat key/value map from a Markdown stub header.
type FrontMatter map[string]string

// ParseFrontMatter reads a minimal line-based header: a --- opener, "key: value"
// lines (flat keys only), and a --- closer. No YAML library is used.
func ParseFrontMatter(r io.Reader) (FrontMatter, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, err
	}
	return parseFrontMatter(string(data))
}

func parseFrontMatter(s string) (FrontMatter, error) {
	s = strings.TrimPrefix(s, "\uFEFF")
	if !strings.HasPrefix(s, "---\n") && s != "---" && !strings.HasPrefix(s, "---\r\n") {
		return nil, ErrNotFrontMatter
	}
	rest := s
	if strings.HasPrefix(rest, "---\r\n") {
		rest = rest[len("---\r\n"):]
	} else {
		rest = rest[len("---\n"):]
	}
	fm := FrontMatter{}
	for {
		line, next, ok := strings.Cut(rest, "\n")
		if !ok {
			return nil, ErrNotFrontMatter
		}
		line = strings.TrimSuffix(line, "\r")
		rest = next
		if line == "---" {
			return fm, nil
		}
		if line == "" {
			continue
		}
		key, val, found := strings.Cut(line, ":")
		if !found {
			return nil, ErrNotFrontMatter
		}
		key = strings.TrimSpace(key)
		if key == "" {
			return nil, ErrNotFrontMatter
		}
		val = strings.TrimSpace(val)
		if unquoted, err := unquoteYAML(val); err != nil {
			return nil, err
		} else {
			val = unquoted
		}
		fm[key] = val
	}
}

func unquoteYAML(v string) (string, error) {
	if v == "" {
		return "", nil
	}
	if v[0] != '"' {
		return v, nil
	}
	if len(v) < 2 || v[len(v)-1] != '"' {
		return "", ErrNotFrontMatter
	}
	var b strings.Builder
	for i := 1; i < len(v)-1; i++ {
		if v[i] != '\\' {
			b.WriteByte(v[i])
			continue
		}
		i++
		if i >= len(v)-1 {
			return "", ErrNotFrontMatter
		}
		switch v[i] {
		case '\\', '"':
			b.WriteByte(v[i])
		case 'n':
			b.WriteByte('\n')
		default:
			return "", ErrNotFrontMatter
		}
	}
	return b.String(), nil
}

func quoteYAML(v string) string {
	if v == "" {
		return `""`
	}
	need := strings.ContainsAny(v, "#\"\\\n") ||
		strings.HasPrefix(v, "{") || strings.HasPrefix(v, "[") ||
		unicode.IsSpace(rune(v[0])) || unicode.IsSpace(rune(v[len(v)-1]))
	if !need {
		return v
	}
	var b strings.Builder
	b.WriteByte('"')
	for i := 0; i < len(v); i++ {
		switch v[i] {
		case '\\', '"':
			b.WriteByte('\\')
			b.WriteByte(v[i])
		case '\n':
			b.WriteString(`\n`)
		default:
			b.WriteByte(v[i])
		}
	}
	b.WriteByte('"')
	return b.String()
}

func formatYAMLLine(key, value string) string {
	return key + ": " + quoteYAML(value)
}

// ReadFrontMatterFile parses the header of path. A missing file wraps fs.ErrNotExist.
func ReadFrontMatterFile(path string) (FrontMatter, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseFrontMatter(f)
}

// StubOccupancy is how ChooseStubPath classifies a candidate filename.
type StubOccupancy int

// Occupancy of a candidate stub path.
const (
	StubAbsent StubOccupancy = iota
	StubOwned
	StubForeign
)

// stubHeaderLimit bounds the bytes read from a candidate stub path; arxgo front matter is far
// smaller, so a longer header is not an arxgo stub.
const stubHeaderLimit = 64 << 10

// InspectStub reports whether path is missing, an arxgo stub for relPath, or a foreign file.
// A matching rel_path in front matter is enough to treat the file as ours (and overwrite it).
// Anything that is not a readable regular file (a directory, a symlink, a file without read
// permission) is foreign: arxgo never overwrites or deletes it. Only a failure to look up the path
// itself is returned as an error.
func InspectStub(path, relPath string) (StubOccupancy, error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return StubAbsent, nil
	case err != nil:
		return StubForeign, err
	case !fi.Mode().IsRegular():
		return StubForeign, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return StubForeign, nil
	}
	defer f.Close()
	fm, err := ParseFrontMatter(io.LimitReader(f, stubHeaderLimit))
	if err != nil || fm["rel_path"] != relPath {
		return StubForeign, nil
	}
	return StubOwned, nil
}

// FormatFrontMatter writes the stub header in contract key order, omitting empty optional fields.
func FormatFrontMatter(keys [][2]string) string {
	var b strings.Builder
	b.WriteString("---\n")
	fmt.Fprintf(&b, "%s: %s\n", StubMarker, StubMarkerValue)
	for _, kv := range keys {
		if kv[1] == "" {
			continue
		}
		b.WriteString(formatYAMLLine(kv[0], kv[1]))
		b.WriteByte('\n')
	}
	b.WriteString("---\n")
	return b.String()
}
