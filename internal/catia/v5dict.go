package catia

import (
	"bytes"
	"encoding/binary"
	"strings"
	"unicode"
	"unicode/utf8"
)

// A V5 dictionary string is a length byte n (1..49: n-1 bytes of text) or a zero byte and a
// little-endian uint32 byte count, followed by UTF-8 text.
const dictShortMax = 49

type dictStatus int

const (
	dictOK dictStatus = iota
	dictShort
	dictInvalid
)

// dictString reads the dictionary string at the start of b. It is dictShort when b ends inside the
// string and dictInvalid when the length is not a dictionary length, exceeds max, or the text is
// not valid UTF-8 without control characters other than tab, line feed and carriage return.
func dictString(b []byte, max int) (string, int, dictStatus) {
	if len(b) == 0 {
		return "", 0, dictShort
	}
	var start, size int
	switch n := int(b[0]); {
	case n == 0:
		if len(b) < 5 {
			return "", 0, dictShort
		}
		u := binary.LittleEndian.Uint32(b[1:5])
		if u > uint32(max) {
			return "", 0, dictInvalid
		}
		start, size = 5, int(u)
	case n <= dictShortMax:
		start, size = 1, n-1
	default:
		return "", 0, dictInvalid
	}
	if size > max {
		return "", 0, dictInvalid
	}
	if len(b) < start+size {
		return "", 0, dictShort
	}
	text := b[start : start+size]
	if !dictText(text) {
		return "", 0, dictInvalid
	}
	return string(text), start + size, dictOK
}

func dictText(b []byte) bool {
	if !utf8.Valid(b) {
		return false
	}
	for _, r := range string(b) {
		if unicode.IsControl(r) && r != '\t' && r != '\n' && r != '\r' {
			return false
		}
	}
	return true
}

// markerWindow finds the first occurrence of a marker in a stream and keeps at most cap bytes
// from its start.
type markerWindow struct {
	marker  []byte
	cap     int
	partial []byte
	buf     []byte
	found   bool
}

// write feeds a chunk and reports whether the window gained bytes.
func (m *markerWindow) write(p []byte) bool {
	if !m.found {
		m.partial = append(m.partial, p...)
		i := bytes.Index(m.partial, m.marker)
		if i < 0 {
			if keep := len(m.marker) - 1; len(m.partial) > keep {
				m.partial = append(m.partial[:0], m.partial[len(m.partial)-keep:]...)
			}
			return false
		}
		m.found, p, m.partial = true, m.partial[i:], nil
	}
	room := m.cap - len(m.buf)
	if room <= 0 || len(p) == 0 {
		return false
	}
	m.buf = append(m.buf, p[:min(len(p), room)]...)
	return true
}

func (m *markerWindow) full() bool { return m.found && len(m.buf) >= m.cap }

// productAttrs maps a root product attribute to its property, the CATIA parameter name that stands
// in for the value when a parameter drives the property, and the value CATIA writes when unset.
var productAttrs = map[string]struct{ prop, param, unset string }{
	"_Revision":       {"revision", "Revision", ""},
	"_Definition":     {"definition", "Definition", ""},
	"_Nomenclature":   {"nomenclature", "Nomenclature", ""},
	"_Source":         {"source", "Source", "Unknown"},
	"_DescriptionRef": {"description", "Product Description", ""},
}

const productEnd = "_BagRepsList"

// productReader reads the dictionary run of the root product: ASMPRODUCT, the part number, then
// attribute names each followed by its value when one is stored.
type productReader struct {
	win   markerWindow
	pos   int
	strs  []string
	done  bool
	props map[string]string
}

func newProductReader() *productReader {
	return &productReader{win: markerWindow{marker: []byte("\x0bASMPRODUCT"), cap: dictWindow}}
}

func (r *productReader) write(p []byte) {
	if r.done || !r.win.write(p) {
		return
	}
	r.parse(false)
}

func (r *productReader) flush() {
	if !r.done && r.win.found {
		r.parse(true)
	}
}

func (r *productReader) parse(final bool) {
	for !r.done {
		s, n, st := dictString(r.win.buf[r.pos:], dictWindow)
		switch {
		case st == dictShort && !final && !r.win.full():
			return
		case st != dictOK:
			r.finish()
			return
		}
		r.pos += n
		if s == productEnd {
			r.finish()
			return
		}
		r.strs = append(r.strs, s)
		if len(r.strs) >= dictMaxStrings {
			r.finish()
		}
	}
}

func (r *productReader) finish() {
	r.done = true
	r.props = productProperties(r.strs)
	r.win.buf = nil
	r.strs = nil
}

// productProperties maps the strings of a root product run, ASMPRODUCT first, to properties.
func productProperties(strs []string) map[string]string {
	props := map[string]string{}
	if len(strs) > 1 && !strings.HasPrefix(strs[1], "_") {
		props["part_number"] = strs[1]
	}
	for i := 1; i+1 < len(strs); i++ {
		attr, ok := productAttrs[strs[i]]
		if !ok {
			continue
		}
		v := strs[i+1]
		if v == "" || v == attr.unset || v == attr.param || strings.HasPrefix(v, "_") {
			continue
		}
		if _, seen := props[attr.prop]; !seen {
			props[attr.prop] = v
		}
	}
	return props
}

// materialReader reads the string after the first String, Material dictionary pair.
type materialReader struct {
	win   markerWindow
	done  bool
	value string
}

var materialMarker = []byte("\x07String\x09Material")

func newMaterialReader() *materialReader {
	return &materialReader{win: markerWindow{marker: materialMarker, cap: len(materialMarker) + dictWindow}}
}

func (r *materialReader) write(p []byte) {
	if r.done || !r.win.write(p) {
		return
	}
	r.parse(false)
}

func (r *materialReader) flush() {
	if !r.done && r.win.found {
		r.parse(true)
	}
}

func (r *materialReader) parse(final bool) {
	s, _, st := dictString(r.win.buf[len(materialMarker):], dictWindow)
	if st == dictShort && !final && !r.win.full() {
		return
	}
	r.done = true
	r.win.buf = nil
	if st == dictOK && s != "" && s != "None" && !strings.HasPrefix(s, "_") {
		r.value = s
	}
}
