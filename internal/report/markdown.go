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
	FileName         string
	RelLink          string
	SizeHuman        string
	MediaLine        string
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
- Size: {{.SizeHuman}}
{{- if .MediaLine}}
- Duration: {{.MediaLine}}
{{- end}}

## Previews

(stage 2: embedded PNG frames and sample clip links)
`

var stubTmpl = template.Must(template.New("stub").Parse(stubTemplate))

type stubView struct {
	FrontMatter      string
	FileName         string
	RelLink          string
	VideoArchivePath string
	URL              string
	SizeHuman        string
	MediaLine        string
}

// RenderStub returns a complete Markdown stub. Front matter always starts with arxgo_stub: 1.
func RenderStub(in StubInput) ([]byte, error) {
	name := in.FileName
	if name == "" {
		name = path.Base(in.RelPath)
	}
	moved := ""
	if !in.MovedAt.IsZero() {
		moved = in.MovedAt.UTC().Format(time.RFC3339)
	}
	size := ""
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
		SizeHuman:        in.SizeHuman,
		MediaLine:        in.MediaLine,
	}
	if view.SizeHuman == "" && in.FileSize > 0 {
		view.SizeHuman = FormatSize(in.FileSize)
	}
	if view.MediaLine == "" {
		view.MediaLine = MediaLine(in.Media)
	}
	var buf bytes.Buffer
	if err := stubTmpl.Execute(&buf, view); err != nil {
		return nil, err
	}
	out := buf.Bytes()
	if !bytes.HasSuffix(out, []byte("\n")) {
		out = append(out, '\n')
	}
	return out, nil
}

// ReplacePreviewSection updates only the generated preview section of an owned stub.
// The front matter and original move timestamp remain unchanged on catch-up runs.
func ReplacePreviewSection(data []byte, links []string) []byte {
	const (
		heading     = "## Previews\n"
		begin       = "<!-- arxgo-previews-begin -->\n"
		end         = "<!-- arxgo-previews-end -->\n"
		placeholder = "(stage 2: embedded PNG frames and sample clip links)\n"
	)
	s := string(data)
	i := strings.Index(s, heading)
	if i < 0 {
		return data
	}
	var content strings.Builder
	content.WriteString(begin)
	if len(links) == 0 {
		content.WriteString("(no previews)\n")
	} else {
		for _, link := range links {
			content.WriteString(link)
			content.WriteByte('\n')
		}
	}
	content.WriteString(end)
	tail := s[i+len(heading):]
	if start := strings.Index(tail, begin); start >= 0 {
		if stop := strings.Index(tail[start+len(begin):], end); stop >= 0 {
			stop += start + len(begin) + len(end)
			return []byte(s[:i+len(heading)] + tail[:start] + content.String() + tail[stop:])
		}
	}
	if start := strings.Index(tail, placeholder); start >= 0 {
		return []byte(s[:i+len(heading)] + tail[:start] + content.String() + tail[start+len(placeholder):])
	}
	return []byte(s[:i+len(heading)] + "\n" + content.String() + tail)
}
