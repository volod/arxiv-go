package report

import (
	"bufio"
	"errors"
	"io"
	"io/fs"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
	"unicode"

	"github.com/volod/arxiv-go/internal/catia"
	"github.com/volod/arxiv-go/internal/media"
)

// DescriptionMarker names the first field of every description arxgo writes: "arxgo: <rel_path>". It marks the
// file as an arxgo description and names the video it stands for.
const DescriptionMarker = "arxgo"

// descriptionHeaderLimit bounds the bytes read to find the marker line; a longer first line is not ours.
const descriptionHeaderLimit = 64 << 10

// DescriptionInput is the data for one video description. Empty optional fields are omitted.
type DescriptionInput struct {
	RelPath  string
	FileSize int64
	FileMIME string
	SHA256   string
	Modified time.Time // source file modification time
	MovedAt  time.Time
	MovedTo  string // absolute path of the video in the video archive
	URL      string
	Media    *media.MediaInfo
	Catia    *catia.Info // when set, a CATIA description: no created: or video: fields
}

// RenderDescription returns a description: one "key: value" line per field, the marker first, no blank lines.
// Preview links are appended later by ReplacePreviewLinks.
func RenderDescription(in DescriptionInput) []byte {
	var b strings.Builder
	field := func(key, value string) {
		if value != "" {
			writeBuilder(&b, key)
			writeBuilder(&b, ": ")
			writeBuilder(&b, quoteValue(value))
			writeBuilder(&b, "\n")
		}
	}
	field(DescriptionMarker, in.RelPath)
	if in.FileSize > 0 {
		field("file_size", strconv.FormatInt(in.FileSize, 10)+" ("+FormatSize(in.FileSize)+")")
	}
	field("file_mime", in.FileMIME)
	field("sha256", in.SHA256)
	if in.Catia == nil && in.Media != nil && in.Media.Error == "" {
		field("created", in.Media.CreationTime)
	}
	field("modified", formatTime(in.Modified))
	if in.Catia != nil {
		field("catia", CatiaLine(*in.Catia))
	} else {
		field("video", MediaLine(in.Media))
	}
	field("moved_at", formatTime(in.MovedAt))
	if u := FileURL(filepath.ToSlash(in.MovedTo)); u != "" {
		field("moved_to", "["+markdownText(path.Base(filepath.ToSlash(in.MovedTo)))+"]("+u+")")
	}
	field("url", in.URL)
	return []byte(b.String())
}

// markdownText escapes the characters that would end or break a Markdown link text.
func markdownText(s string) string {
	return strings.NewReplacer(`\`, `\\`, "[", `\[`, "]", `\]`).Replace(s)
}

func formatTime(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.UTC().Format(time.RFC3339)
}

// Description is the field block of a description, keyed by field name.
type Description map[string]string

// ErrNotDescription reports a file that does not start with the arxgo marker line.
var ErrNotDescription = errors.New("not an arxgo description")

// ParseDescription reads the field block: the marker line, then "key: value" lines up to the first line
// that is empty, a list item or not a field.
func ParseDescription(r io.Reader) (Description, error) {
	sc := bufio.NewScanner(r)
	sc.Buffer(make([]byte, 0, 4096), descriptionHeaderLimit)
	description := Description{}
	for first := true; sc.Scan(); first = false {
		key, value, ok := parseField(strings.TrimSuffix(sc.Text(), "\r"), first)
		if !ok {
			if first {
				return nil, ErrNotDescription
			}
			break
		}
		description[key] = value
	}
	if err := sc.Err(); err != nil {
		return nil, ErrNotDescription
	}
	if _, ok := description[DescriptionMarker]; !ok {
		return nil, ErrNotDescription
	}
	return description, nil
}

// ReadDescriptionFile parses the description at path. A missing file wraps fs.ErrNotExist.
func ReadDescriptionFile(path string) (Description, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return ParseDescription(f)
}

func parseField(line string, first bool) (string, string, bool) {
	if first {
		line = strings.TrimPrefix(line, "\uFEFF")
	}
	key, value, found := strings.Cut(line, ": ")
	if !found || key == "" || strings.HasPrefix(line, "- ") || strings.ContainsFunc(key, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '_')
	}) || first && key != DescriptionMarker {
		return "", "", false
	}
	value, err := unquoteValue(value)
	return key, value, err == nil
}

// DescriptionOccupancy is how ChooseDescriptionPath classifies a candidate filename.
type DescriptionOccupancy int

// Occupancy of a candidate description path.
const (
	DescriptionAbsent DescriptionOccupancy = iota
	DescriptionOwned
	DescriptionForeign
)

// InspectDescription reports whether path is missing, an arxgo description for relPath, or a foreign file. A
// first line "arxgo: <relPath>" is enough to treat the file as ours (and overwrite it). Anything
// that is not a readable regular file (a directory, a symlink, a file without read permission) is
// foreign: arxgo never overwrites or deletes it. Only a failure to look up the path is an error.
func InspectDescription(path, relPath string) (DescriptionOccupancy, error) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return DescriptionAbsent, nil
	case err != nil:
		return DescriptionForeign, err
	case !fi.Mode().IsRegular():
		return DescriptionForeign, nil
	}
	f, err := os.Open(path)
	if err != nil {
		return DescriptionForeign, nil
	}
	defer f.Close()
	line, err := bufio.NewReaderSize(io.LimitReader(f, descriptionHeaderLimit), 4096).ReadString('\n')
	if err != nil && !(errors.Is(err, io.EOF) && line != "") {
		return DescriptionForeign, nil
	}
	key, value, ok := parseField(strings.TrimRight(line, "\r\n"), true)
	if !ok || key != DescriptionMarker || value != relPath {
		return DescriptionForeign, nil
	}
	return DescriptionOwned, nil
}

// PreviewLink is one preview listed in a description: PNG frames are embedded, samples linked.
type PreviewLink struct {
	Name string // file name shown
	URL  string // escaped link relative to the description
}

// ReplacePreviewLinks sets the preview list of a description: the "- " link lines that directly follow the
// field block. Other text, before or after, is kept.
func ReplacePreviewLinks(data []byte, links []PreviewLink) []byte {
	lines := strings.SplitAfter(string(data), "\n")
	if len(lines) > 0 && lines[len(lines)-1] == "" {
		lines = lines[:len(lines)-1]
	}
	fields := 0
	for fields < len(lines) {
		if _, _, ok := parseField(strings.TrimRight(lines[fields], "\r\n"), fields == 0); !ok {
			break
		}
		fields++
	}
	end := fields
	for end < len(lines) && (strings.HasPrefix(lines[end], "- [") || strings.HasPrefix(lines[end], "- ![")) {
		end++
	}
	var b strings.Builder
	for _, l := range lines[:fields] {
		writeBuilder(&b, l)
	}
	if fields > 0 && !strings.HasSuffix(lines[fields-1], "\n") {
		writeBuilder(&b, "\n")
	}
	for _, l := range links {
		if strings.HasSuffix(strings.ToLower(l.Name), ".png") {
			writeBuilder(&b, "- ![")
		} else {
			writeBuilder(&b, "- [")
		}
		writeBuilder(&b, l.Name)
		writeBuilder(&b, "](")
		writeBuilder(&b, l.URL)
		writeBuilder(&b, ")\n")
	}
	for _, l := range lines[end:] {
		writeBuilder(&b, l)
	}
	return []byte(b.String())
}

// quoteValue double-quotes a value that would not read back unchanged: leading or trailing space,
// a quote, a backslash or a line break.
func quoteValue(v string) string {
	need := strings.ContainsAny(v, "\"\\\n\r") || unicode.IsSpace(rune(v[0])) || unicode.IsSpace(rune(v[len(v)-1]))
	if !need {
		return v
	}
	r := strings.NewReplacer(`\`, `\\`, `"`, `\"`, "\n", `\n`, "\r", `\r`)
	return `"` + r.Replace(v) + `"`
}

func unquoteValue(v string) (string, error) {
	if v == "" || v[0] != '"' {
		return v, nil
	}
	if len(v) < 2 || v[len(v)-1] != '"' {
		return "", ErrNotDescription
	}
	var b strings.Builder
	for i := 1; i < len(v)-1; i++ {
		if v[i] != '\\' {
			_ = b.WriteByte(v[i])
			continue
		}
		if i++; i >= len(v)-1 {
			return "", ErrNotDescription
		}
		switch v[i] {
		case '\\', '"':
			_ = b.WriteByte(v[i])
		case 'n':
			_ = b.WriteByte('\n')
		case 'r':
			_ = b.WriteByte('\r')
		default:
			return "", ErrNotDescription
		}
	}
	return b.String(), nil
}

func writeBuilder(b *strings.Builder, s string) {
	_, _ = b.WriteString(s)
}
