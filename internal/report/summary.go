package report

import (
	"bytes"
	"fmt"
	"io"
	"path"
	"sort"
	"strings"
	"text/template"
	"time"
)

// SummaryInput is the data for arxgo-videos.md.
type SummaryInput struct {
	Generated       time.Time
	Version         string
	RunIDs          []string
	Archive         string
	VideoArchive    string
	BaseURL         string
	Rows            []VideoRow
	PreviewFailures []string
}

type summaryView struct {
	Generated       string
	Version         string
	RunIDs          string
	Archive         string
	VideoArchive    string
	BaseURL         string
	Videos          int
	Bytes           string
	BytesRaw        int64
	Duration        string
	Containers      []countRow
	Codecs          []countRow
	Bands           []countRow
	Skipped         []VideoRow
	Conflicts       []VideoRow
	Largest         []largestRow
	PreviewFailures []string
}

type countRow struct {
	Name  string
	Count int
}

type largestRow struct {
	RelPath string
	Size    string
	Link    string
}

const summaryTemplate = `# Video archive summary

Generated: {{.Generated}}
arxgo: {{.Version}}
Run IDs: {{.RunIDs}}
Archive: {{.Archive}}
Video archive: {{.VideoArchive}}
{{- if .BaseURL}}
Base URL: {{.BaseURL}}
{{- end}}

## Totals

- Videos: {{.Videos}}
- Bytes: {{.Bytes}}
{{- if .Duration}}
- Duration: {{.Duration}}
{{- end}}

## Containers

| Container | Count |
| --- | ---: |
{{- range .Containers}}
| {{.Name}} | {{.Count}} |
{{- end}}

## Codecs

| Codec | Count |
| --- | ---: |
{{- range .Codecs}}
| {{.Name}} | {{.Count}} |
{{- end}}

## Resolution

| Band | Count |
| --- | ---: |
{{- range .Bands}}
| {{.Name}} | {{.Count}} |
{{- end}}

## Skipped and conflicts
{{- if and (eq (len .Skipped) 0) (eq (len .Conflicts) 0)}}

None.
{{- else}}
{{- range .Conflicts}}

- conflict: {{.RelPath}} ({{.FileName}})
{{- end}}
{{- range .Skipped}}

- skipped: {{.RelPath}} ({{.FileName}})
{{- end}}
{{- end}}

## Largest videos

| Path | Size | Link |
| --- | --- | --- |
{{- range .Largest}}
| {{.RelPath}} | {{.Size}} | {{.Link}} |
{{- end}}
{{- if .PreviewFailures}}

## Preview failures
{{- range .PreviewFailures}}

- {{.}}
{{- end}}
{{- end}}
`

var summaryTmpl = template.Must(template.New("summary").Parse(summaryTemplate))

// RenderSummary writes arxgo-videos.md for rows (already merged and sorted).
func RenderSummary(in SummaryInput) ([]byte, error) {
	view := buildSummaryView(in)
	var buf bytes.Buffer
	if err := summaryTmpl.Execute(&buf, view); err != nil {
		return nil, err
	}
	if buf.Len() == 0 || buf.Bytes()[buf.Len()-1] != '\n' {
		buf.WriteByte('\n')
	}
	return buf.Bytes(), nil
}

func buildSummaryView(in SummaryInput) summaryView {
	v := summaryView{
		Generated:       in.Generated.UTC().Format(time.RFC3339),
		Version:         in.Version,
		RunIDs:          strings.Join(in.RunIDs, ", "),
		Archive:         in.Archive,
		VideoArchive:    in.VideoArchive,
		BaseURL:         in.BaseURL,
		PreviewFailures: in.PreviewFailures,
	}
	if v.Version == "" {
		v.Version = "dev"
	}
	if v.RunIDs == "" {
		v.RunIDs = "(none)"
	}
	containers := map[string]int{}
	codecs := map[string]int{}
	bands := map[string]int{BandBelowSD: 0, BandSD: 0, BandHD: 0, Band4K: 0}
	var moved []VideoRow
	var duration float64
	var durKnown bool
	for _, r := range in.Rows {
		switch r.Status {
		case StatusConflict:
			v.Conflicts = append(v.Conflicts, r)
			continue
		case StatusSkipped:
			v.Skipped = append(v.Skipped, r)
			continue
		case StatusRestored:
			continue
		}
		moved = append(moved, r)
		v.Videos++
		v.BytesRaw += r.FileSize
		if m := r.Metadata.Media; m != nil && m.Error == "" {
			if m.Container != "" {
				containers[m.Container]++
			} else if r.FileMIME != "" {
				containers[r.FileMIME]++
			}
			if m.VideoCodec != "" {
				codecs[m.VideoCodec]++
			}
			if b := ResolutionBand(m.Width, m.Height); b != "" {
				bands[b]++
			}
			if m.DurationS > 0 {
				duration += m.DurationS
				durKnown = true
			}
		} else if r.FileMIME != "" {
			containers[r.FileMIME]++
		}
	}
	v.Bytes = FormatSize(v.BytesRaw)
	if durKnown {
		v.Duration = FormatClock(duration)
	}
	v.Containers = sortedCounts(containers)
	v.Codecs = sortedCounts(codecs)
	v.Bands = []countRow{
		{BandBelowSD, bands[BandBelowSD]},
		{BandSD, bands[BandSD]},
		{BandHD, bands[BandHD]},
		{Band4K, bands[Band4K]},
	}
	if len(v.Containers) == 0 {
		v.Containers = []countRow{{Name: "(none)", Count: 0}}
	}
	if len(v.Codecs) == 0 {
		v.Codecs = []countRow{{Name: "(none)", Count: 0}}
	}
	v.Largest = topLargest(moved, 100)
	return v
}

func sortedCounts(m map[string]int) []countRow {
	out := make([]countRow, 0, len(m))
	for k, n := range m {
		out = append(out, countRow{k, n})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return out[i].Name < out[j].Name
	})
	return out
}

func topLargest(rows []VideoRow, n int) []largestRow {
	cp := append([]VideoRow(nil), rows...)
	sort.SliceStable(cp, func(i, j int) bool { return cp[i].FileSize > cp[j].FileSize })
	if len(cp) > n {
		cp = cp[:n]
	}
	out := make([]largestRow, len(cp))
	for i, r := range cp {
		link := r.RelPath
		if r.URL != "" {
			link = fmt.Sprintf("[%s](%s)", path.Base(r.RelPath), r.URL)
		} else if r.StubRelPath != "" {
			link = fmt.Sprintf("[%s](%s)", path.Base(r.RelPath), EscapePath(r.StubRelPath))
		}
		out[i] = largestRow{RelPath: r.RelPath, Size: FormatSize(r.FileSize), Link: link}
	}
	return out
}

// WriteSummaryTo writes the rendered summary.
func WriteSummaryTo(w io.Writer, in SummaryInput) error {
	b, err := RenderSummary(in)
	if err != nil {
		return err
	}
	_, err = w.Write(b)
	return err
}
