package scanner

import (
	"bytes"
	"encoding/binary"
	"math/rand/v2"
	"strings"
	"testing"
)

// ftypBox builds an ISO BMFF file start: an ftyp box with the major brand and compatible brands,
// followed by an empty mdat box.
func ftypBox(major string, compatible ...string) []byte {
	var b bytes.Buffer
	size := uint32(16 + 4*len(compatible))
	_ = binary.Write(&b, binary.BigEndian, size)
	b.WriteString("ftyp" + major)
	_ = binary.Write(&b, binary.BigEndian, uint32(0x200))
	for _, c := range compatible {
		b.WriteString(c)
	}
	_ = binary.Write(&b, binary.BigEndian, uint32(8))
	b.WriteString("mdat")
	return b.Bytes()
}

// ebmlHeader builds a Matroska/WebM EBML header with the given DocType.
func ebmlHeader(docType string) []byte {
	body := []byte{0x42, 0x86, 0x81, 0x01, 0x42, 0xF7, 0x81, 0x01, 0x42, 0xF2, 0x81, 0x04, 0x42, 0xF3, 0x81, 0x08}
	body = append(body, 0x42, 0x82, byte(0x80|len(docType)))
	body = append(body, docType...)
	body = append(body, 0x42, 0x87, 0x81, 0x04, 0x42, 0x85, 0x81, 0x02)
	out := []byte{0x1A, 0x45, 0xDF, 0xA3, byte(0x80 | len(body))}
	return append(out, body...)
}

func wavHeader() []byte {
	var b bytes.Buffer
	b.WriteString("RIFF")
	_ = binary.Write(&b, binary.LittleEndian, uint32(36))
	b.WriteString("WAVEfmt ")
	_ = binary.Write(&b, binary.LittleEndian, []any{uint32(16), uint16(1), uint16(1), uint32(8000), uint32(8000), uint16(1), uint16(8)})
	b.WriteString("data")
	_ = binary.Write(&b, binary.LittleEndian, uint32(0))
	return b.Bytes()
}

// randomBytes returns deterministic bytes that start with a byte no signature uses.
func randomBytes(n int) []byte {
	r := rand.New(rand.NewPCG(1, 2))
	out := make([]byte, n)
	for i := range out {
		out[i] = byte(r.UintN(256))
	}
	out[0], out[1] = 0x00, 0xFE
	return out
}

var (
	pngSignature  = []byte("\x89PNG\r\n\x1a\n\x00\x00\x00\x0dIHDR\x00\x00\x00\x01\x00\x00\x00\x01\x08\x02\x00\x00\x00")
	jpegSignature = []byte{0xFF, 0xD8, 0xFF, 0xE0, 0x00, 0x10, 'J', 'F', 'I', 'F', 0x00, 0x01}
)

func TestClassifyFixtures(t *testing.T) {
	opts := NewDetectOptions(1<<20, []string{".DAT"})
	tests := []struct {
		name, file string
		head       []byte
		want       FileType
	}{
		{"plain text", "notes.txt", []byte("hello archive\n"),
			FileType{MIME: "text/plain", Type: "txt"}},
		{"UTF-8 with BOM", "bom.txt", []byte("\xEF\xBB\xBFprivet \xD0\xBF\xD1\x80\xD0\xB8\n"),
			FileType{MIME: "text/plain", Type: "txt"}},
		{"UTF-16LE with BOM", "wide.txt", []byte("\xFF\xFEh\x00i\x00\n\x00"),
			FileType{MIME: "text/plain", Type: "txt"}},
		{"JSON", "data.bin", []byte(`{"a": [1, 2, "three"]}`),
			FileType{MIME: "application/json", Type: "json"}},
		{"SVG", "logo", []byte(`<svg xmlns="http://www.w3.org/2000/svg" width="1" height="1"></svg>`),
			FileType{MIME: "image/svg+xml", Type: "svg", IsPicture: true, IsMedia: true}},
		{"HTML is text by hierarchy", "page", []byte("<!DOCTYPE html><html><body>x</body></html>"),
			FileType{MIME: "text/html", Type: "html"}},
		{"PDF header", "doc.dat", []byte("%PDF-1.7\n%\xE2\xE3\xCF\xD3\n1 0 obj\n"),
			FileType{MIME: "application/pdf", Type: "pdf", IsBinary: true}},
		{"PNG", "image.jpg", pngSignature,
			FileType{MIME: "image/png", Type: "png", IsBinary: true, IsPicture: true, IsMedia: true}},
		{"JPEG", "photo", jpegSignature,
			FileType{MIME: "image/jpeg", Type: "jpg", IsBinary: true, IsPicture: true, IsMedia: true}},
		{"WAV", "sound.mp4", wavHeader(),
			FileType{MIME: "audio/wav", Type: "wav", IsBinary: true, IsMedia: true}},
		{"MP4 ftyp", "clip.txt", ftypBox("isom", "isom", "iso2", "avc1", "mp41"),
			FileType{MIME: "video/mp4", Type: "mp4", IsBinary: true, IsVideo: true, IsMedia: true}},
		{"M4A ftyp", "song.mp4", ftypBox("M4A ", "M4A ", "mp42", "isom"),
			FileType{MIME: "audio/x-m4a", Type: "m4a", IsBinary: true, IsMedia: true}},
		{"MOV ftyp", "movie", ftypBox("qt  ", "qt  "),
			FileType{MIME: "video/quicktime", Type: "mov", IsBinary: true, IsVideo: true, IsMedia: true}},
		{"Matroska EBML", "clip.webm", ebmlHeader("matroska"),
			FileType{MIME: "video/matroska", Type: "mkv", IsBinary: true, IsVideo: true, IsMedia: true}},
		{"WebM EBML", "clip.mkv", ebmlHeader("webm"),
			FileType{MIME: "video/webm", Type: "webm", IsBinary: true, IsVideo: true, IsMedia: true}},
		{"random bytes with video extension", "Capture.MTS", randomBytes(2048),
			FileType{MIME: MIMEOctetStream, Type: "mts", IsBinary: true, IsVideo: true, IsMedia: true}},
		{"random bytes with extra video extension", "tape.dat", randomBytes(2048),
			FileType{MIME: MIMEOctetStream, Type: "dat", IsBinary: true, IsVideo: true, IsMedia: true}},
		{"random bytes with other extension", "blob.bin", randomBytes(2048),
			FileType{MIME: MIMEOctetStream, Type: "bin", IsBinary: true}},
		{"random bytes without extension", "blob", randomBytes(2048),
			FileType{MIME: MIMEOctetStream, IsBinary: true}},
		{"dotfile has no extension", ".mp4", randomBytes(2048),
			FileType{MIME: MIMEOctetStream, IsBinary: true}},
		{"text with video extension", "fake.mp4", []byte("this is not a video\n"),
			FileType{MIME: "text/plain", Type: "txt"}},
		{"empty", "empty.mp4", nil,
			FileType{MIME: MIMEEmpty}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := Classify(tt.head, tt.file, opts); got != tt.want {
				t.Errorf("Classify(%s)\n got %+v\nwant %+v", tt.file, got, tt.want)
			}
		})
	}
}

func TestClassifyIgnoresBytesBeyondLimit(t *testing.T) {
	text := strings.Repeat("plain text line\n", DetectLimit/16)
	beyond := append([]byte(text), 0, 0, 0, 0)
	got := Classify(beyond, "log", DetectOptions{})
	if got.MIME != "text/plain" || got.IsBinary {
		t.Fatalf("text followed by NUL bytes past the limit: got %+v", got)
	}
	within := append([]byte(text[:DetectLimit-4]), 0, 0, 0, 0)
	if bin := Classify(within, "log", DetectOptions{}); !bin.IsBinary {
		t.Fatalf("NUL bytes inside the limit must be binary: got %+v", bin)
	}
}

func TestVideoExtensionOptions(t *testing.T) {
	blob := randomBytes(512)
	for _, ext := range BuiltinVideoExtensions {
		if ft := Classify(blob, "x."+ext, DetectOptions{}); !ft.IsVideo || ft.Type != ext {
			t.Errorf("zero options, built-in %q: got %+v", ext, ft)
		}
		if ft := Classify(blob, "x."+strings.ToUpper(ext), NewDetectOptions(0, nil)); !ft.IsVideo {
			t.Errorf("upper-case built-in %q not video", ext)
		}
	}
	opts := NewDetectOptions(0, []string{" .BRAW ", "r3d", ""})
	for _, name := range []string{"a.braw", "a.R3D", "a.mp4"} {
		if !Classify(blob, name, opts).IsVideo {
			t.Errorf("%s: extra extension not video", name)
		}
	}
	if Classify(blob, "a.mkv.txt", opts).IsVideo {
		t.Error("only the last extension counts")
	}
	// A signature always wins over the extension list.
	if Classify([]byte("text"), "a.braw", opts).IsVideo || Classify(pngSignature, "a.mkv", opts).IsVideo {
		t.Error("extension list applied to an unambiguous signature")
	}
}

func TestIsBinaryHierarchy(t *testing.T) {
	for _, head := range [][]byte{
		[]byte(`<?xml version="1.0"?><root/>`),
		[]byte("#!/bin/sh\necho hi\n"),
		[]byte("a,b,c\n1,2,3\n"),
		[]byte("{\"a\":1}\n{\"b\":2}\n"),
	} {
		if ft := Classify(head, "f", DetectOptions{}); ft.IsBinary {
			t.Errorf("%q detected as %s must be text", head, ft.MIME)
		}
	}
	for _, head := range [][]byte{
		[]byte("PK\x03\x04\x14\x00\x00\x00\x08\x00"),
		{0x1F, 0x8B, 0x08, 0x00, 0x00, 0x00, 0x00, 0x00},
		[]byte("text\x00with nul"),
	} {
		if ft := Classify(head, "f", DetectOptions{}); !ft.IsBinary {
			t.Errorf("%q detected as %s must be binary", head, ft.MIME)
		}
	}
}

func TestStripParams(t *testing.T) {
	for in, want := range map[string]string{
		"text/plain; charset=utf-8": "text/plain",
		" Text/HTML ;charset=x":     "text/html",
		"video/mp4":                 "video/mp4",
	} {
		if got := stripParams(in); got != want {
			t.Errorf("stripParams(%q) = %q, want %q", in, got, want)
		}
	}
}
