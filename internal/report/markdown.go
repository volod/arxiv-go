package report

import (
	"bytes"
	"path"
	"strconv"
	"strings"
	"text/template"
	"time"

	"github.com/volod/arxiv-go/internal/media"
)

// StubInput is the data for one Markdown stub. Tests pin RunID, MovedAt and paths.
type StubInput struct {
	RelPath          string
	VideoArchivePath string // absolute destination path, slash-separated in output
	URL              string
	FileSize         int64
	FileMIME         string
	SHA256           string
	RunID            string
	MovedAt          time.Time
	FileName         string // defaults to the base name of RelPath
	RelLink          string // relative link from the stub to the video; omitted when empty
	Media            *media.MediaInfo
}

const stubTemplate = `{{.FrontMatter}}

# {{.FileName}}

This video was moved to the video archive by arxgo.
{{if .RelLink}}
- Video archive: [{{.FileName}}]({{.RelLink}})
{{- end}}
- Absolute path: ` + "`{{.VideoArchivePath}}`" + `
{{- if .URL}}
- Cloud link: <{{.URL}}>
{{- end}}
- Size: {{.Size}}
{{- if .MediaLine}}
- Duration: {{.MediaLine}}
{{- end}}
`

var stubTmpl = template.Must(template.New("stub").Parse(stubTemplate))

type stubView struct {
	FrontMatter      string
	FileName         string
	RelLink          string
	VideoArchivePath string
	URL              string
	Size             string
	MediaLine        string
}

// RenderStub returns a complete Markdown stub. Front matter always starts with arxgo_stub: 1. The
// preview section is added by ReplacePreviewSection once previews exist.
func RenderStub(in StubInput) ([]byte, error) {
	name := in.FileName
	if name == "" {
		name = path.Base(in.RelPath)
	}
	moved, size := "", ""
	if !in.MovedAt.IsZero() {
		moved = in.MovedAt.UTC().Format(time.RFC3339)
	}
	if in.FileSize > 0 {
		size = strconv.FormatInt(in.FileSize, 10)
	}
	fm := FormatFrontMatter([][2]string{
		{"rel_path", in.RelPath},
		{"video_archive_path", in.VideoArchivePath},
		{"url", in.URL},
		{"file_size", size},
		{"file_mime", in.FileMIME},
		{"sha256", in.SHA256},
		{"run_id", in.RunID},
		{"moved_at", moved},
	})
	view := stubView{
		FrontMatter:      strings.TrimRight(fm, "\n"),
		FileName:         name,
		RelLink:          in.RelLink,
		VideoArchivePath: in.VideoArchivePath,
		URL:              in.URL,
		MediaLine:        MediaLine(in.Media),
	}
	if in.FileSize > 0 {
		view.Size = FormatSize(in.FileSize)
	}
	var buf bytes.Buffer
	if err := stubTmpl.Execute(&buf, view); err != nil {
		return nil, err
	}
	return buf.Bytes(), nil
}

// PreviewLink is one preview listed in a stub: PNG frames are embedded, samples linked.
type PreviewLink struct {
	Name string // file name shown
	URL  string // escaped link relative to the stub
}

const (
	previewHeading = "## Previews\n\n"
	previewBegin   = "<!-- arxgo-previews-begin -->\n"
	previewEnd     = "<!-- arxgo-previews-end -->\n"
)

// ReplacePreviewSection sets the generated preview section of an owned stub to links. The section
// is delimited by HTML comments, so front matter and operator notes before or after it are kept.
// Without previews the section is removed; a stub without a section gets one appended.
func ReplacePreviewSection(data []byte, links []PreviewLink) []byte {
	var section strings.Builder
	if len(links) > 0 {
		section.WriteString(previewHeading + previewBegin)
		for _, l := range links {
			if path.Ext(l.Name) == ".png" {
				section.WriteString("- ![" + l.Name + "](" + l.URL + ")\n")
			} else {
				section.WriteString("- [" + l.Name + "](" + l.URL + ")\n")
			}
		}
		section.WriteString(previewEnd)
	}
	s := string(data)
	start := strings.Index(s, previewBegin)
	stop := strings.Index(s, previewEnd)
	if start < 0 || stop < start {
		if section.Len() == 0 {
			return data
		}
		if !strings.HasSuffix(s, "\n") {
			s += "\n"
		}
		return []byte(s + "\n" + section.String())
	}
	if strings.HasSuffix(s[:start], previewHeading) {
		start -= len(previewHeading)
	}
	prefix, suffix := s[:start], s[stop+len(previewEnd):]
	if section.Len() == 0 {
		// Drop the blank line that separated the removed section from its neighbors.
		if suffix == "" {
			prefix = strings.TrimSuffix(prefix, "\n")
		} else {
			suffix = strings.TrimPrefix(suffix, "\n")
		}
	}
	return []byte(prefix + section.String() + suffix)
}
