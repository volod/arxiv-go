package catia

import (
	"bytes"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"
)

var (
	octetStart = []byte("CATOctetArray")
	octetEnd   = []byte{0x08, 'F', 'I', 'N', 'J', 'P', 'L'}
	chunkSep   = "\x01\x04File"
	sohSemi    = ";\u0001"
)

const (
	winSearch = iota
	winCollect
	winDone
)

type windowFinder struct {
	state   int
	partial []byte
	buf     []byte
}

func (w *windowFinder) write(b []byte) {
	switch w.state {
	case winSearch:
		w.partial = append(w.partial, b...)
		if i := bytes.Index(w.partial, octetStart); i >= 0 {
			w.state = winCollect
			w.buf = append([]byte(nil), w.partial[i:]...)
			w.partial = nil
			w.finishIfEnded()
			return
		}
		if keep := len(octetStart) - 1; len(w.partial) > keep {
			w.partial = append([]byte(nil), w.partial[len(w.partial)-keep:]...)
		}
	case winCollect:
		w.buf = append(w.buf, b...)
		w.finishIfEnded()
	}
}

func (w *windowFinder) finishIfEnded() {
	if w.state != winCollect {
		return
	}
	if i := bytes.Index(w.buf, octetEnd); i >= 0 {
		if i > windowCap {
			w.buf = nil
		} else {
			w.buf = w.buf[:i]
		}
		w.state = winDone
		return
	}
	if len(w.buf) > windowCap+len(octetEnd)-1 {
		w.buf = nil
		w.state = winDone
	}
}

func (w *windowFinder) window() []byte {
	if w.state != winDone {
		return nil
	}
	return w.buf
}

func parseComponents(window []byte, self string) []string {
	if len(window) == 0 {
		return nil
	}
	text := strings.ToValidUTF8(string(window), "\uFFFD")
	text = strings.ReplaceAll(text, sohSemi, "")
	chunks := strings.Split(text, chunkSep)
	seen := make(map[string]struct{})
	var out []string
	for _, raw := range chunks {
		if name, ok := componentName(raw, self); ok {
			if _, dup := seen[name]; dup {
				continue
			}
			seen[name] = struct{}{}
			out = append(out, name)
		}
	}
	sortByte(out)
	return out
}

func sortByte(s []string) {
	sort.Strings(s)
}

func componentName(raw, self string) (string, bool) {
	if strings.HasPrefix(raw, "CATOctetArray") {
		return "", false
	}
	if len(raw) > 1 && strings.HasPrefix(raw[1:], "feat") {
		return "", false
	}
	nul := strings.IndexByte(raw, 0)
	quote := strings.IndexByte(raw, '"')
	if nul < 0 || quote < 0 || quote <= nul {
		return "", false
	}
	clean := strings.ReplaceAll(raw[nul:quote], "\x00", "")
	if i := strings.LastIndexByte(clean, '\\'); i >= 0 {
		clean = clean[i+1:]
	}
	if clean == "" {
		return "", false
	}
	_, n := utf8.DecodeLastRuneInString(clean)
	clean = clean[:len(clean)-n]
	if clean == "" || strings.Contains(clean, "/") || strings.ContainsRune(clean, '\uFFFD') {
		return "", false
	}
	if strings.ContainsFunc(clean, unicode.IsControl) {
		return "", false
	}
	if self != "" && strings.EqualFold(clean, self) {
		return "", false
	}
	return clean, true
}
