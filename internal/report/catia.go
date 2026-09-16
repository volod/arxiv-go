package report

import (
	"path"
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
	Archive     string // absolute archive root
	Identity    TextIdentity
	ExtractedAt time.Time
	Info        catia.Info
}

// TextIdentity is the identity block a sidecar repeats from its description: the values exactly as
// the description holds them, and the description's rel_path. Empty fields are omitted.
type TextIdentity struct {
	FileSize, FileMIME, SHA256, Modified, Catia, MovedTo, URL string
	Description                                               string
}

// TextIdentityOf takes the identity block of a parsed description written at descriptionRel.
func TextIdentityOf(d Description, descriptionRel string) TextIdentity {
	return TextIdentity{
		FileSize: d["file_size"], FileMIME: d["file_mime"], SHA256: d["sha256"], Modified: d["modified"],
		Catia: d["catia"], MovedTo: d["moved_to"], URL: d["url"], Description: descriptionRel,
	}
}

// RenderCatiaText returns the sidecar body, UTF-8 without BOM, capped at 1 MiB. The marker,
// archive, extracted_at and truncated lines are always written; identity fields follow in order
// while they fit, and blocks are filled only after the whole identity block fits.
func RenderCatiaText(in CatiaTextInput) []byte {
	head := fieldLine(TextSidecarMarker, in.RelPath) + optionalFieldLine("archive", in.Archive)
	tail := func(truncated bool) string {
		return fieldLine("extracted_at", formatTime(in.ExtractedAt)) + "truncated: " + strconv.FormatBool(truncated) + "\n"
	}
	if len(head)+len(tail(false)) > catiaTextCap {
		h := []byte(head + tail(true))
		return h[:min(len(h), catiaTextCap)]
	}
	truncated := in.Info.Truncated
	remain := catiaTextCap - len(head) - len(tail(false))
	var identity, body strings.Builder
	complete := true
	for _, ln := range identityLines(in) {
		if len(ln) > remain {
			truncated, complete = true, false
			break
		}
		writeBuilder(&identity, ln)
		remain -= len(ln)
	}
	writeBlock := func(title string, lines []string) {
		if !complete || len(lines) == 0 {
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
	writeBuilder(&out, head)
	writeBuilder(&out, identity.String())
	writeBuilder(&out, tail(truncated))
	writeBuilder(&out, body.String())
	return []byte(out.String())
}

// identityLines are the non-empty identity lines in field order: file_name, the description's
// fields, then the description path.
func identityLines(in CatiaTextInput) []string {
	id := in.Identity
	pairs := [][2]string{
		{"file_name", path.Base(in.RelPath)}, {"file_size", id.FileSize}, {"file_mime", id.FileMIME},
		{"sha256", id.SHA256}, {"modified", id.Modified}, {"catia", id.Catia}, {"moved_to", id.MovedTo},
		{"url", id.URL}, {"description", id.Description},
	}
	var out []string
	for _, p := range pairs {
		if p[1] != "" {
			out = append(out, fieldLine(p[0], p[1]))
		}
	}
	return out
}

// fieldLine is one "key: value" line with the description escaping.
func fieldLine(key, value string) string {
	if value == "" {
		return key + ": \n"
	}
	return key + ": " + quoteValue(value) + "\n"
}

func optionalFieldLine(key, value string) string {
	if value == "" {
		return ""
	}
	return fieldLine(key, value)
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
