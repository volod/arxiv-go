package media

import (
	"bytes"
	"math"
	"strconv"
	"strings"
	"time"
)

const progressLineLimit = 64 << 10

// Progress is one ffmpeg -progress report.
type Progress struct {
	OutTime time.Duration
	Speed   float64
	End     bool
}

type progressCollector struct {
	callback func(Progress)
	line     []byte
	value    Progress
	overflow bool
	discard  bool
}

func (p *progressCollector) Write(data []byte) (int, error) {
	for _, c := range data {
		if c == '\n' {
			if !p.discard {
				p.parseLine(strings.TrimSuffix(string(p.line), "\r"))
			}
			p.line = p.line[:0]
			p.discard = false
			continue
		}
		if p.discard {
			continue
		}
		if len(p.line) == progressLineLimit {
			p.overflow = true
			p.discard = true
			p.line = p.line[:0]
			continue
		}
		p.line = append(p.line, c)
	}
	return len(data), nil
}

func (p *progressCollector) parseLine(line string) {
	key, value, ok := strings.Cut(line, "=")
	if !ok {
		return
	}
	switch key {
	case "out_time_us":
		us, err := strconv.ParseInt(value, 10, 64)
		if err == nil && us >= 0 && us <= math.MaxInt64/1000 {
			p.value.OutTime = time.Duration(us) * time.Microsecond
		}
	case "speed":
		n, err := strconv.ParseFloat(strings.TrimSuffix(value, "x"), 64)
		if err == nil && n >= 0 && !math.IsInf(n, 0) && !math.IsNaN(n) {
			p.value.Speed = n
		}
	case "progress":
		if value != "continue" && value != "end" {
			return
		}
		p.value.End = value == "end"
		if p.callback != nil {
			p.callback(p.value)
		}
		p.value = Progress{}
	}
}

// tailBuffer keeps the most recent bytes of stderr for an actionable failure.
type tailBuffer struct {
	buf   []byte
	limit int
}

func (b *tailBuffer) Write(data []byte) (int, error) {
	n := len(data)
	if n >= b.limit {
		b.buf = append(b.buf[:0], data[n-b.limit:]...)
		return n, nil
	}
	if excess := len(b.buf) + n - b.limit; excess > 0 {
		b.buf = bytes.Clone(b.buf[excess:])
	}
	b.buf = append(b.buf, data...)
	return n, nil
}

func (b *tailBuffer) String() string { return string(b.buf) }
