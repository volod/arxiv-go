package catia

import (
	"bytes"
	"encoding/binary"
	"strings"
)

var v5Keys = [][]byte{
	[]byte("LastSaveVersion"),
	[]byte("MinimalVersionToRead"),
	[]byte("CATBuildLevel"),
}

const maxV5Key = 20 // len("MinimalVersionToRead")

const v5TypeString uint32 = 0x0000000e

type v5props struct {
	lastSave  string
	minVer    string
	build     string
	hold      []byte
	pending   *v5pending
	holdLimit int
}

type v5pending struct {
	which int
	need  int
	got   []byte
}

func newV5props() *v5props {
	return &v5props{holdLimit: maxV5Key + 2 + 8 + propValueMax}
}

func (p *v5props) done() bool {
	return p.lastSave != "" && p.minVer != "" && p.build != ""
}

func (p *v5props) write(b []byte) {
	if p.done() && p.pending == nil {
		return
	}
	if p.pending != nil {
		take := p.pending.need
		if take > len(b) {
			take = len(b)
		}
		p.pending.got = append(p.pending.got, b[:take]...)
		p.pending.need -= take
		b = b[take:]
		if p.pending.need == 0 {
			p.accept(p.pending.which, p.pending.got)
			p.pending = nil
		}
		if len(b) == 0 {
			return
		}
	}
	p.hold = append(p.hold, b...)
	p.scan()
	if p.pending == nil && len(p.hold) > p.holdLimit {
		p.hold = append([]byte(nil), p.hold[len(p.hold)-maxV5Key-2:]...)
	}
}

func (p *v5props) scan() {
	for p.pending == nil {
		i, which := p.findKey()
		if i < 0 {
			return
		}
		key := v5Keys[which]
		if i < 2 {
			p.hold = p.hold[i+1:]
			continue
		}
		klen := binary.LittleEndian.Uint16(p.hold[i-2 : i])
		if int(klen) != len(key) {
			p.hold = p.hold[i+1:]
			continue
		}
		rec := i + len(key)
		if rec+8 > len(p.hold) {
			return
		}
		typ := binary.LittleEndian.Uint32(p.hold[rec : rec+4])
		vlen := binary.LittleEndian.Uint32(p.hold[rec+4 : rec+8])
		if typ != v5TypeString || vlen > propValueMax {
			p.hold = p.hold[i+1:]
			continue
		}
		valStart := rec + 8
		need := int(vlen)
		if valStart+need <= len(p.hold) {
			p.accept(which, p.hold[valStart:valStart+need])
			p.hold = p.hold[valStart+need:]
			continue
		}
		p.pending = &v5pending{which: which, need: valStart + need - len(p.hold), got: append([]byte(nil), p.hold[valStart:]...)}
		p.hold = p.hold[:0]
		return
	}
}

func (p *v5props) findKey() (int, int) {
	best, which := -1, -1
	for wi, key := range v5Keys {
		if p.have(wi) {
			continue
		}
		if i := indexFrom(p.hold, key, 0); i >= 0 && (best < 0 || i < best) {
			best, which = i, wi
		}
	}
	return best, which
}

func (p *v5props) have(which int) bool {
	switch which {
	case 0:
		return p.lastSave != ""
	case 1:
		return p.minVer != ""
	default:
		return p.build != ""
	}
}

func (p *v5props) accept(which int, val []byte) {
	s := strings.ToValidUTF8(string(val), "\uFFFD")
	switch which {
	case 0:
		if p.lastSave == "" {
			p.lastSave = s
		}
	case 1:
		if p.minVer == "" {
			p.minVer = s
		}
	default:
		if p.build == "" {
			p.build = s
		}
	}
}

func (p *v5props) release() string {
	if tok := parseLastSave(p.lastSave); tok != "" {
		return tok
	}
	if tok := parseMinimalVersion(p.minVer); tok != "" {
		return tok
	}
	return ""
}

func parseLastSave(s string) string {
	rel := xmlish(s, "Release")
	if rel == "" {
		return ""
	}
	tok := "V5R" + rel
	if sp := xmlish(s, "ServicePack"); sp != "" {
		tok += " SP" + sp
	}
	return tok
}

func parseMinimalVersion(s string) string {
	i := strings.Index(s, "V5R")
	if i < 0 {
		return ""
	}
	j := i + 3
	for j < len(s) && s[j] >= '0' && s[j] <= '9' {
		j++
	}
	if j == i+3 {
		return ""
	}
	return s[i:j]
}

func xmlish(s, tag string) string {
	start := "<" + tag + ">"
	i := strings.Index(s, start)
	if i < 0 {
		return ""
	}
	i += len(start)
	for _, end := range []string{"</" + tag + ">", "/<" + tag + ">"} {
		if j := strings.Index(s[i:], end); j >= 0 {
			return s[i : i+j]
		}
	}
	return ""
}

func indexFrom(b, sep []byte, from int) int {
	if from >= len(b) {
		return -1
	}
	i := bytes.Index(b[from:], sep)
	if i < 0 {
		return -1
	}
	return from + i
}
