package report

import (
	"strconv"
	"strings"
	"time"

	"github.com/volod/arxiv-go/internal/catia"
)

// TextSidecarMarker is the first field of a CATIA text sidecar: "arxgo-text: <rel_path>".
const TextSidecarMarker = "arxgo-text"

const catiaTextCap = 1 << 20

// CatiaLine is the catia field of a description: kind, format, release and component count.
func CatiaLine(info catia.Info) string {
	kind := info.Kind
	format := info.Format
	if format == "" {
		format = catia.FormatUnknown
	}
	release := info.Release
	if release == "" {
		release = catia.ReleaseUnknown
	}
	n := len(info.Components)
	noun := "components"
	if n == 1 {
		noun = "component"
	}
	return kind + " | " + format + " | " + release + " | " + strconv.Itoa(n) + " " + noun
}

// CatiaTextInput is the data for one CATIA text sidecar.
type CatiaTextInput struct {
	RelPath     string
	ExtractedAt time.Time
	Info        catia.Info
}

// RenderCatiaText returns the sidecar body, UTF-8 without BOM, capped at 1 MiB.
func RenderCatiaText(in CatiaTextInput) []byte {
	headerFalse := catiaTextHeader(in.RelPath, in.ExtractedAt, false)
	reserve := len(headerFalse)
	if reserve >= catiaTextCap {
		h := []byte(catiaTextHeader(in.RelPath, in.ExtractedAt, true))
		if len(h) > catiaTextCap {
			h = h[:catiaTextCap]
		}
		return h
	}
	truncated := in.Info.Truncated
	var body strings.Builder
	remain := catiaTextCap - reserve
	writeBlock := func(title string, lines []string) {
		if len(lines) == 0 {
			return
		}
		head := title + "\n"
		if remain < len(head)+len(lines[0]) {
			truncated = true
			return
		}
		var kept []string
		need := len(head)
		for _, ln := range lines {
			if need+len(ln) > remain {
				truncated = true
				break
			}
			kept = append(kept, ln)
			need += len(ln)
		}
		if len(kept) == 0 {
			return
		}
		writeBuilder(&body, head)
		for _, ln := range kept {
			writeBuilder(&body, ln)
		}
		remain -= need
	}
	writeBlock("properties:", catiaPropertyLines(in.Info))
	writeBlock("components:", listLines(in.Info.Components))
	writeBlock("strings:", listLines(in.Info.Strings))
	var out strings.Builder
	writeBuilder(&out, catiaTextHeader(in.RelPath, in.ExtractedAt, truncated))
	writeBuilder(&out, body.String())
	return []byte(out.String())
}

func catiaTextHeader(rel string, at time.Time, truncated bool) string {
	var b strings.Builder
	writeBuilder(&b, TextSidecarMarker)
	writeBuilder(&b, ": ")
	writeBuilder(&b, quoteValue(rel))
	writeBuilder(&b, "\nextracted_at: ")
	writeBuilder(&b, quoteValue(formatTime(at)))
	writeBuilder(&b, "\ntruncated: ")
	writeBuilder(&b, strconv.FormatBool(truncated))
	writeBuilder(&b, "\n")
	return b.String()
}

func catiaPropertyLines(info catia.Info) []string {
	pairs := [][2]string{
		{"release", v5ReleaseProp(info)},
		{"build_level", info.BuildLevel},
		{"schema_version", info.SchemaVersion},
		{"title", info.Title},
		{"author", info.Author},
		{"generator", info.Generator},
		{"created", info.Created},
	}
	var out []string
	for _, p := range pairs {
		if p[1] == "" {
			continue
		}
		out = append(out, "- "+p[0]+": "+quoteValue(p[1])+"\n")
	}
	return out
}

func v5ReleaseProp(info catia.Info) string {
	if info.Format != catia.FormatV5 {
		return ""
	}
	if info.Release == "" || info.Release == catia.ReleaseUnknown {
		return ""
	}
	return info.Release
}

func listLines(items []string) []string {
	out := make([]string, 0, len(items))
	for _, s := range items {
		if s == "" {
			continue
		}
		out = append(out, "- "+quoteValue(s)+"\n")
	}
	return out
}
