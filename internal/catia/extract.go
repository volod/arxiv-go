package catia

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
)

// Error kinds logged by callers at warn. Harvested names are never included.
const (
	ErrorKindRead   = "read"
	ErrorKindZip    = "zip"
	ErrorKindXML    = "xml"
	ErrorKindCancel = "canceled"
)

// Info is accessible metadata and harvested text from one CATIA file.
type Info struct {
	Kind          string
	Format        string
	Release       string
	BuildLevel    string
	SchemaVersion string
	Title         string
	Author        string
	Generator     string
	Created       string
	Components    []string
	Strings       []string
	Truncated     bool
	TextFailed    bool
	ErrorKind     string
	Err           error
}

// Extract reads one CATIA file from r. name is the file name used for kind and V5 self-name
// exclusion (the base name if name is a path).
func Extract(ctx context.Context, r io.Reader, name string) Info {
	info := emptyInfo(name)
	if err := ctx.Err(); err != nil {
		return fail(info, ErrorKindCancel, err)
	}
	if r == nil {
		return fail(info, ErrorKindRead, errors.New("nil reader"))
	}

	head := make([]byte, detectPeek)
	n, err := io.ReadFull(r, head)
	switch {
	case err == io.EOF || err == io.ErrUnexpectedEOF:
		head = head[:n]
	case err != nil:
		return fail(info, ErrorKindRead, err)
	default:
		head = head[:n]
	}
	info.Format = DetectFormat(head)

	if info.Format == FormatZIP {
		if ra, size, ok := zipRandomAccess(r); ok {
			return extractZIP(ctx, info, ra, size)
		}
		return fail(info, ErrorKindZip, errors.New("zip 3dxml needs random access"))
	}

	rest := io.MultiReader(bytes.NewReader(head), r)
	self := selfName(name)
	switch info.Format {
	case FormatXML:
		return extractXMLFile(ctx, info, rest)
	default:
		return extractStream(ctx, info, rest, self)
	}
}

// ExtractPath opens path and runs Extract. The kind and self-name come from the base name.
func ExtractPath(ctx context.Context, path string) Info {
	f, err := os.Open(path)
	if err != nil {
		info := emptyInfo(path)
		return fail(info, ErrorKindRead, err)
	}
	defer f.Close()
	return Extract(ctx, f, filepath.Base(path))
}

func emptyInfo(name string) Info {
	return Info{
		Kind:    kindFromName(name),
		Format:  FormatUnknown,
		Release: ReleaseUnknown,
	}
}

func kindFromName(name string) string {
	k, _ := KindOf(filepath.Ext(filepath.Base(name)))
	return k
}

func fail(info Info, kind string, err error) Info {
	info.ErrorKind = kind
	info.Err = err
	if info.Release == "" {
		info.Release = ReleaseUnknown
	}
	if info.Format == "" {
		info.Format = FormatUnknown
	}
	return info
}

func zipRandomAccess(r io.Reader) (io.ReaderAt, int64, bool) {
	s, ok := r.(io.Seeker)
	if !ok {
		return nil, 0, false
	}
	ra, ok := r.(io.ReaderAt)
	if !ok {
		return nil, 0, false
	}
	if _, err := s.Seek(0, io.SeekStart); err != nil {
		return nil, 0, false
	}
	size, ok := readerSize(r)
	if !ok {
		return nil, 0, false
	}
	return ra, size, true
}

func readerSize(r io.Reader) (int64, bool) {
	if f, ok := r.(*os.File); ok {
		st, err := f.Stat()
		if err != nil {
			return 0, false
		}
		return st.Size(), true
	}
	s, ok := r.(io.Seeker)
	if !ok {
		return 0, false
	}
	cur, err := s.Seek(0, io.SeekCurrent)
	if err != nil {
		return 0, false
	}
	end, err := s.Seek(0, io.SeekEnd)
	if err != nil {
		return 0, false
	}
	if _, err := s.Seek(cur, io.SeekStart); err != nil {
		return 0, false
	}
	return end, true
}

func selfName(name string) string {
	return filepath.Base(name)
}
