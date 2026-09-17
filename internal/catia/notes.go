package catia

import (
	"bytes"
	"encoding/binary"
	"unicode"
)

// RTF text starts; a dictionary string that starts with one of them is a note.
var noteMarkers = [][]byte{[]byte("{{\\fonttbl"), []byte("{\\rtf")}

// noteTail is the bytes a chunk keeps for the next one: a length prefix and a marker minus a byte.
const noteTail = 5 + 10 - 1

// noteReader collects the notes of a V5 stream: dictionary strings holding RTF, converted to plain
// text, filtered, unique, in stream order.
type noteReader struct {
	tail      []byte
	pending   []byte // RTF string being collected
	need      int    // bytes still missing from pending
	seen      map[string]struct{}
	notes     []string
	size      int
	truncated bool
}

func newNoteReader() *noteReader {
	return &noteReader{seen: map[string]struct{}{}}
}

func (r *noteReader) write(p []byte) {
	for len(p) > 0 && !r.truncated {
		if r.need == 0 {
			r.scan(p)
			return
		}
		take := min(r.need, len(p))
		bad := controlByte(p[:take])
		if bad >= 0 {
			take = bad
		}
		r.pending = append(r.pending, p[:take]...)
		r.need -= take
		p = p[take:]
		if bad < 0 && r.need > 0 {
			return
		}
		raw := r.pending
		r.pending, r.need = nil, 0
		if bad >= 0 || !r.add(raw) {
			r.scan(raw[1:])
		}
	}
}

// scan searches the kept tail and p for note strings. A string that ends beyond p is collected by
// later writes; the tail is empty then, because the next string starts after that one. A candidate
// that turns out not to be a text string is searched again from its second byte, so a false length
// prefix cannot hide the strings after it.
func (r *noteReader) scan(p []byte) {
	data := append(r.tail, p...)
	from := 0
	found := []int{markerUnknown, markerUnknown}
	for {
		at, size := nextNote(data, from, len(r.tail), found)
		if at < 0 {
			break
		}
		end := min(at+size, len(data))
		if controlByte(data[at:end]) >= 0 {
			from = at + 1
			continue
		}
		if at+size > len(data) {
			r.pending = append([]byte(nil), data[at:]...)
			r.need = at + size - len(data)
			r.tail = r.tail[:0]
			return
		}
		if !r.add(data[at : at+size]) {
			from = at + 1
			continue
		}
		if r.truncated {
			return
		}
		from = at + size
	}
	r.tail = append(make([]byte, 0, noteTail), data[max(from, len(data)-noteTail):]...)
}

// controlByte returns the index of the first ASCII control byte other than tab, line feed and
// carriage return, or -1. Such a byte ends a candidate early, before it is fully collected.
func controlByte(b []byte) int {
	for i, c := range b {
		if c < 0x20 && c != '\t' && c != '\n' && c != '\r' || c == 0x7F {
			return i
		}
	}
	return -1
}

const (
	markerNone    = -1 // no further occurrence in the data
	markerUnknown = -2 // not searched yet
)

// nextNote finds the first note string at or after from whose marker was not complete within the
// old tail, and returns its start and byte count, or -1. found caches the next occurrence of each
// marker, which stays valid because from only grows within one scan.
func nextNote(data []byte, from, old int, found []int) (int, int) {
	best, bestSize := -1, 0
	for k, m := range noteMarkers {
		start := max(from, old-len(m)+1)
		for found[k] != markerNone {
			if found[k] < start {
				i := -1
				if start <= len(data)-len(m) {
					i = bytes.Index(data[start:], m)
				}
				if i < 0 {
					found[k] = markerNone
					break
				}
				found[k] = start + i
			}
			at := found[k]
			if best >= 0 && at >= best {
				break
			}
			if size, ok := notePrefix(data, at, from); ok {
				best, bestSize = at, size
				break
			}
			start = at + 1
		}
	}
	return best, bestSize
}

// notePrefix reads the dictionary length in front of a marker at at, without reaching before
// from, where the previous string ended.
func notePrefix(data []byte, at, from int) (int, bool) {
	if at-5 >= from && data[at-5] == 0 {
		if u := binary.LittleEndian.Uint32(data[at-4 : at]); u <= noteValueMax && int(u) >= len("{\\rtf") {
			return int(u), true
		}
	}
	if at-1 >= from {
		if n := int(data[at-1]); n > len("{\\rtf") && n <= dictShortMax {
			return n - 1, true
		}
	}
	return 0, false
}

// add converts one collected RTF string and keeps it when it is a new note that fits the cap. It
// reports whether raw is a text string at all.
func (r *noteReader) add(raw []byte) bool {
	if len(raw) == 0 || raw[len(raw)-1] != '}' || !dictText(raw) {
		return false
	}
	note := rtfPlain(string(raw))
	if _, dup := r.seen[note]; dup || !noteText(note) {
		return true
	}
	if r.size+len(note) > sidecarCap {
		r.truncated = true
		return true
	}
	r.seen[note] = struct{}{}
	r.notes = append(r.notes, note)
	r.size += len(note)
	return true
}

// noteText reports whether text has at least three letters, two of them distinct ignoring case,
// outside CATIA symbol tags such as <DEGREE>.
func noteText(text string) bool {
	letters := 0
	var first rune = -1
	distinct := false
	rs := []rune(text)
	for i := 0; i < len(rs); i++ {
		if end := symbolTagEnd(rs, i); end > i {
			i = end
			continue
		}
		if !unicode.IsLetter(rs[i]) {
			continue
		}
		letters++
		l := unicode.ToLower(rs[i])
		if first < 0 {
			first = l
		} else if l != first {
			distinct = true
		}
	}
	return letters >= 3 && distinct
}

// symbolTagEnd returns the index of the closing '>' of a symbol tag starting at i, or i.
func symbolTagEnd(rs []rune, i int) int {
	if rs[i] != '<' {
		return i
	}
	j := i + 1
	for j < len(rs) && (rs[j] >= 'A' && rs[j] <= 'Z' || rs[j] >= '0' && rs[j] <= '9' || rs[j] == '_') {
		j++
	}
	if j == i+1 || j >= len(rs) || rs[j] != '>' {
		return i
	}
	return j
}
