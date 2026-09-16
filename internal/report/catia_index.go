package report

import (
	"bufio"
	"errors"
	"io"
	"os"
	"strconv"
	"strings"
	"time"
)

// catiaTextLineLimit bounds one sidecar line. A sidecar arxgo wrote is at most 1 MiB in total, so a
// longer line means the file was replaced or edited and is not read.
const catiaTextLineLimit = catiaTextCap + 1

// CatiaText is the part of an owned text sidecar the index repeats. Block items keep the escaped
// text after "- " exactly as the sidecar holds it.
type CatiaText struct {
	Fields                          Description // header fields, the marker included
	Properties, Components, Strings []string
}

// ReadCatiaText parses the owned sidecar of relPath at path. A file whose first line is not
// "arxgo-text: <relPath>" is ErrNotDescription. Strings are kept only when withStrings is set. Header
// fields end at the first block; a line that is neither a block title nor an item ends the blocks,
// so text an operator appended is ignored.
func ReadCatiaText(path, relPath string, withStrings bool) (CatiaText, error) {
	f, err := os.Open(path)
	if err != nil {
		return CatiaText{}, err
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 0, 64<<10), catiaTextLineLimit)
	text := CatiaText{Fields: Description{}}
	var block *[]string
	inBlocks := false
	for first := true; sc.Scan(); first = false {
		line := sc.Text()
		if first {
			line = strings.TrimPrefix(line, "\uFEFF")
			key, value, found := strings.Cut(line, ": ")
			if v, err := unquoteValue(value); !found || key != TextSidecarMarker || err != nil || v != relPath {
				return CatiaText{}, ErrNotDescription
			}
			text.Fields[TextSidecarMarker] = relPath
			continue
		}
		switch {
		case line == "properties:":
			block, inBlocks = &text.Properties, true
		case line == "components:":
			block, inBlocks = &text.Components, true
		case line == "strings:":
			block, inBlocks = nil, true
			if withStrings {
				block = &text.Strings
			}
		case strings.HasPrefix(line, "- ") && inBlocks:
			if block != nil {
				*block = append(*block, line[2:])
			}
		case !inBlocks:
			key, value, ok := parseField(line, false)
			if !ok {
				return text, nil
			}
			text.Fields[key] = value
		default:
			return text, nil
		}
	}
	if err := sc.Err(); err != nil {
		return CatiaText{}, err
	}
	if len(text.Fields) == 0 {
		return CatiaText{}, ErrNotDescription // empty file
	}
	return text, nil
}

// TextIdentityOfSidecar is the identity block a sidecar repeated from its description, without the
// description path: a reader uses it only when that description is gone.
func TextIdentityOfSidecar(t CatiaText) TextIdentity { return TextIdentityOf(t.Fields, "") }

// CatiaIndexHeader is the header of the CATIA text index.
type CatiaIndexHeader struct {
	Archive, CatiaArchive string
	HistoryAt             time.Time // newest record of the CATIA run history
	Files, Components     int
	MissingText           int
}

// CatiaIndexSection is one moved CATIA file of the index.
type CatiaIndexSection struct {
	RelPath  string
	Identity TextIdentity
	// Text is the archive-relative path of the owned sidecar; empty when the file has none, and
	// then the blocks are empty too.
	Text      string
	Truncated string // the sidecar's truncated value
	CatiaText
}

// MissingText is a moved CATIA file of the index without a readable owned sidecar.
type MissingText struct {
	RelPath, Reason string
}

// Reasons a moved CATIA file is listed under Missing text.
const (
	MissingNotRecorded = "not_recorded" // no completed text sidecar in the run history
	MissingAbsent      = "missing"      // the recorded sidecar no longer exists
	MissingForeign     = "foreign"      // the file at the recorded path is not the owned sidecar
	MissingUnreadable  = "unreadable"   // the sidecar could not be read
)

// WriteCatiaIndexHeader writes the title and the header fields.
func WriteCatiaIndexHeader(w io.Writer, h CatiaIndexHeader) error {
	var b strings.Builder
	writeBuilder(&b, "# arxgo CATIA text index\n\n")
	writeBuilder(&b, optionalFieldLine("archive", h.Archive))
	writeBuilder(&b, optionalFieldLine("catia_archive", h.CatiaArchive))
	writeBuilder(&b, optionalFieldLine("history_at", formatTime(h.HistoryAt)))
	writeBuilder(&b, "files: "+strconv.Itoa(h.Files)+"\n")
	writeBuilder(&b, "components: "+strconv.Itoa(h.Components)+"\n")
	writeBuilder(&b, "missing_text: "+strconv.Itoa(h.MissingText)+"\n")
	_, err := io.WriteString(w, b.String())
	return err
}

// WriteCatiaIndexSection writes the section of one moved CATIA file: its identity fields, the
// sidecar path and truncated flag, then the properties, components and, when present, strings
// blocks.
func WriteCatiaIndexSection(w io.Writer, s CatiaIndexSection) error {
	var b strings.Builder
	writeBuilder(&b, "\n## "+quoteValue(s.RelPath)+"\n\n")
	for _, ln := range identityLines(CatiaTextInput{RelPath: s.RelPath, Identity: s.Identity}) {
		writeBuilder(&b, ln)
	}
	if s.Text != "" {
		writeBuilder(&b, fieldLine("text", s.Text))
		writeBuilder(&b, optionalFieldLine("truncated", s.Truncated))
	}
	if _, err := io.WriteString(w, b.String()); err != nil {
		return err
	}
	for _, block := range []struct {
		title string
		items []string
	}{{"properties:", s.Properties}, {"components:", s.Components}, {"strings:", s.Strings}} {
		if err := writeItems(w, block.title, block.items); err != nil {
			return err
		}
	}
	return nil
}

func writeItems(w io.Writer, title string, items []string) error {
	if len(items) == 0 {
		return nil
	}
	if _, err := io.WriteString(w, title+"\n"); err != nil {
		return err
	}
	for _, item := range items {
		if _, err := io.WriteString(w, "- "+item+"\n"); err != nil {
			return err
		}
	}
	return nil
}

// WriteMissingText writes the Missing text section; nothing when every file has its sidecar.
func WriteMissingText(w io.Writer, missing []MissingText) error {
	if len(missing) == 0 {
		return nil
	}
	var b strings.Builder
	writeBuilder(&b, "\n## Missing text\n\n")
	for _, m := range missing {
		writeBuilder(&b, "- "+m.Reason+": "+quoteValue(m.RelPath)+"\n")
	}
	_, err := io.WriteString(w, b.String())
	return err
}

// HasArxgoMarker reports whether the file at path starts with the marker line of a description or
// a text sidecar, whatever rel_path it names. A missing file is not an error.
func HasArxgoMarker(path string) (bool, error) {
	f, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer f.Close()
	line, _ := bufio.NewReaderSize(io.LimitReader(f, descriptionHeaderLimit), 4096).ReadString('\n')
	line = strings.TrimPrefix(line, "\uFEFF")
	return strings.HasPrefix(line, DescriptionMarker+": ") || strings.HasPrefix(line, TextSidecarMarker+": "), nil
}
