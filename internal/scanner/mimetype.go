package scanner

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"syscall"

	"github.com/gabriel-vasile/mimetype"
)

// DetectLimit is the number of leading bytes read for detection: the mimetype default read limit.
const DetectLimit = 4096

// Detection results that do not come from a signature.
const (
	MIMEEmpty       = "inode/x-empty"
	MIMEOctetStream = "application/octet-stream"
	mimeTextPlain   = "text/plain"
)

// BuiltinVideoExtensions are the extensions (lower case, no dot) that mark a file with an
// ambiguous signature as video. See docs/openspec/stage-1-core/registry.md#type-detection.
var BuiltinVideoExtensions = []string{
	"mp4", "m4v", "mov", "qt", "3gp", "3g2", "mkv", "webm", "avi", "wmv", "asf", "flv", "f4v",
	"mpg", "mpeg", "m2v", "ts", "m2ts", "mts", "vob", "ogv", "mxf", "dv", "rm", "rmvb",
}

// textMIMEs are non-text/plain descendants that still count as text.
var textMIMEs = map[string]bool{
	"application/json": true,
	"application/xml":  true,
	"image/svg+xml":    true,
}

// DetectOptions configures Detect.
type DetectOptions struct {
	// LargeThreshold is the --large-threshold size; files at least this large are large. A value
	// of zero or less marks no file as large.
	LargeThreshold int64
	videoExt       map[string]bool
}

// NewDetectOptions builds options from --large-threshold and --video-extensions. Extra extensions
// may carry a leading dot and any case; they extend BuiltinVideoExtensions.
func NewDetectOptions(largeThreshold int64, extraVideoExtensions []string) DetectOptions {
	exts := make(map[string]bool, len(BuiltinVideoExtensions)+len(extraVideoExtensions))
	for _, e := range BuiltinVideoExtensions {
		exts[e] = true
	}
	for _, e := range extraVideoExtensions {
		if e = normalizeExt(e); e != "" {
			exts[e] = true
		}
	}
	return DetectOptions{LargeThreshold: largeThreshold, videoExt: exts}
}

// FileType is the registry classification of one regular file.
type FileType struct {
	MIME      string // file_mime: detected MIME type without parameters
	Type      string // file_type: canonical extension without the dot, or the file's own extension
	IsBinary  bool
	IsMedia   bool
	IsPicture bool
	IsVideo   bool
	IsLarge   bool
}

// headPool recycles detection buffers; the classifier does not retain them.
var headPool = sync.Pool{New: func() any { return new([DetectLimit]byte) }}

// Detect classifies the regular file at path from its first DetectLimit bytes. size is the file
// size recorded for the registry row and decides IsLarge. An error means the file could not be
// opened or read (or is no longer a regular file); the scan counts it as unreadable. The ISO BMFF
// no-video-track refinement of IsVideo is applied later by the media metadata step.
func Detect(path string, size int64, opts DetectOptions) (FileType, error) {
	buf := headPool.Get().(*[DetectLimit]byte)
	defer headPool.Put(buf)
	head, err := readHead(path, buf[:])
	if err != nil {
		return FileType{}, err
	}
	ft := Classify(head, filepath.Base(path), opts)
	ft.IsLarge = opts.LargeThreshold > 0 && size >= opts.LargeThreshold
	return ft, nil
}

// Classify derives the type fields other than IsLarge from the leading bytes of a file and its
// base name. An empty head is an empty file.
func Classify(head []byte, name string, opts DetectOptions) FileType {
	if len(head) == 0 {
		return FileType{MIME: MIMEEmpty}
	}
	if len(head) > DetectLimit {
		head = head[:DetectLimit]
	}
	m := mimetype.Detect(head)
	ft := FileType{MIME: stripParams(m.String())}
	ext := fileExt(name)
	ambiguous := ft.MIME == MIMEOctetStream
	if ambiguous {
		ft.Type = ext
	} else {
		ft.Type = strings.TrimPrefix(m.Extension(), ".")
	}
	ft.IsBinary = !isText(m)
	ft.IsPicture = strings.HasPrefix(ft.MIME, "image/")
	ft.IsVideo = strings.HasPrefix(ft.MIME, "video/") || ambiguous && ext != "" && opts.isVideoExt(ext)
	ft.IsMedia = ft.IsVideo || ft.IsPicture || strings.HasPrefix(ft.MIME, "audio/")
	return ft
}

func (o DetectOptions) isVideoExt(ext string) bool {
	if o.videoExt == nil {
		for _, e := range BuiltinVideoExtensions {
			if e == ext {
				return true
			}
		}
		return false
	}
	return o.videoExt[ext]
}

// readHead reads up to len(buf) bytes into buf and returns the filled prefix. O_NONBLOCK keeps a file swapped for a FIFO after the
// walk from blocking the open; it has no effect on regular files and is ignored on Windows.
func readHead(path string, buf []byte) ([]byte, error) {
	f, err := os.OpenFile(path, os.O_RDONLY|syscall.O_NONBLOCK, 0)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, fmt.Errorf("detect %s: not a regular file (%s)", path, info.Mode().Type())
	}
	n, err := io.ReadFull(f, buf)
	if err != nil && err != io.EOF && err != io.ErrUnexpectedEOF {
		return nil, err
	}
	return buf[:n], nil
}

// isText reports whether m or an ancestor is text/plain, or m is in the text allow-list.
func isText(m *mimetype.MIME) bool {
	s := stripParams(m.String())
	if textMIMEs[s] || strings.HasPrefix(s, "text/") {
		return true
	}
	for p := m; p != nil; p = p.Parent() {
		if p.Is(mimeTextPlain) {
			return true
		}
	}
	return false
}

func stripParams(mime string) string {
	if i := strings.IndexByte(mime, ';'); i >= 0 {
		mime = mime[:i]
	}
	return strings.ToLower(strings.TrimSpace(mime))
}

// fileExt returns the lower-cased extension of a base name without the dot. A leading dot alone
// does not start an extension, so ".bashrc" and "Makefile" have none.
func fileExt(name string) string {
	return normalizeExt(filepath.Ext(strings.TrimLeft(name, ".")))
}

func normalizeExt(ext string) string {
	return strings.ToLower(strings.TrimPrefix(strings.TrimSpace(ext), "."))
}
