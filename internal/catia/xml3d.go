package catia

import (
	"bytes"
	"context"
	"encoding/xml"
	"io"
	"log/slog"
	"path"
	"strings"
)

func extractXMLFile(ctx context.Context, info Info, r io.Reader) Info {
	limited := &limitReader{r: r, n: xmlFileCap}
	info, err := parse3DXML(ctx, info, limited)
	if limited.hit {
		info.Truncated = true
	}
	if err != nil {
		return textFailed(info, err)
	}
	if err := ctx.Err(); err != nil {
		return fail(info, ErrorKindCancel, err)
	}
	return finishXML(info)
}

func parse3DXML(ctx context.Context, info Info, r io.Reader) (Info, error) {
	dec := xml.NewDecoder(r)
	dec.Strict = true
	var comps []string
	var capture *string
	var buf strings.Builder
	for {
		if err := ctx.Err(); err != nil {
			return info, err
		}
		tok, err := dec.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			return info, err
		}
		switch t := tok.(type) {
		case xml.StartElement:
			switch t.Name.Local {
			case "SchemaVersion":
				capture, buf = firstCapture(&info.SchemaVersion)
			case "Title":
				capture, buf = firstCapture(&info.Title)
			case "Author":
				capture, buf = firstCapture(&info.Author)
			case "Generator":
				capture, buf = firstCapture(&info.Generator)
			case "Created":
				capture, buf = firstCapture(&info.Created)
			}
			for _, a := range t.Attr {
				if a.Name.Local == "associatedFile" {
					if base, ok := fileRef(a.Value); ok {
						comps = append(comps, base)
					}
				}
				if base, ok := urnFile(a.Value); ok {
					comps = append(comps, base)
				}
			}
		case xml.CharData:
			s := string([]byte(t))
			if capture != nil {
				buf.WriteString(s)
			}
			if base, ok := urnFile(s); ok {
				comps = append(comps, base)
			}
		case xml.EndElement:
			if capture != nil {
				if *capture == "" {
					*capture = strings.TrimSpace(buf.String())
				}
				capture = nil
			}
		}
	}
	info.Components = append(info.Components, comps...)
	return info, nil
}

func firstCapture(dst *string) (*string, strings.Builder) {
	if *dst != "" {
		return nil, strings.Builder{}
	}
	return dst, strings.Builder{}
}

func finishXML(info Info) Info {
	if info.SchemaVersion != "" {
		info.Release = "3DXML " + info.SchemaVersion
	}
	info.Components = uniqueSorted(info.Components)
	return info
}

func textFailed(info Info, err error) Info {
	info.TextFailed = true
	info.Components = nil
	info.SchemaVersion = ""
	info.Title = ""
	info.Author = ""
	info.Generator = ""
	info.Created = ""
	info.BuildLevel = ""
	info.Release = ReleaseUnknown
	return fail(info, ErrorKindXML, err)
}

func fileRef(s string) (string, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return "", false
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "http:") || strings.HasPrefix(lower, "https:") {
		return "", false
	}
	if strings.Contains(lower, "://") && !strings.HasPrefix(lower, "urn:3dxml:") {
		return "", false
	}
	if rest, ok := cutURN(s); ok {
		s = rest
	}
	if i := strings.IndexByte(s, '#'); i >= 0 {
		s = s[:i]
	}
	s = strings.ReplaceAll(s, "\\", "/")
	if strings.Contains(s, "..") {
		return "", false
	}
	base := path.Base(s)
	if base == "" || base == "." || base == "/" {
		return "", false
	}
	return base, true
}

func urnFile(s string) (string, bool) {
	rest, ok := cutURN(s)
	if !ok {
		return "", false
	}
	return fileRef("urn:3DXML:" + rest)
}

func cutURN(s string) (string, bool) {
	const p = "urn:3dxml:"
	i := strings.Index(strings.ToLower(s), p)
	if i < 0 {
		return "", false
	}
	return s[i+len(p):], true
}

func uniqueSorted(in []string) []string {
	if len(in) == 0 {
		return nil
	}
	seen := make(map[string]struct{}, len(in))
	out := make([]string, 0, len(in))
	for _, v := range in {
		if v == "" {
			continue
		}
		if _, ok := seen[v]; ok {
			continue
		}
		seen[v] = struct{}{}
		out = append(out, v)
	}
	sortByte(out)
	return out
}

type limitReader struct {
	r   io.Reader
	n   int64
	hit bool
}

func (l *limitReader) Read(p []byte) (int, error) {
	if l.n <= 0 {
		l.hit = true
		return 0, io.EOF
	}
	if int64(len(p)) > l.n {
		p = p[:l.n]
	}
	n, err := l.r.Read(p)
	l.n -= int64(n)
	if l.n == 0 && err == nil {
		l.hit = true
	}
	return n, err
}

func parseManifestRoot(data []byte) string {
	dec := xml.NewDecoder(bytes.NewReader(data))
	var capture bool
	var buf strings.Builder
	for {
		tok, err := dec.Token()
		if err != nil {
			break
		}
		switch t := tok.(type) {
		case xml.StartElement:
			capture = t.Name.Local == "Root"
			buf.Reset()
		case xml.CharData:
			if capture {
				buf.Write([]byte(t))
			}
		case xml.EndElement:
			if capture && t.Name.Local == "Root" {
				return strings.TrimSpace(buf.String())
			}
		}
	}
	return ""
}

func logMemberSkip(err error) {
	slog.Debug("catia xml member skipped", "error_kind", ErrorKindXML, "err", err)
}
