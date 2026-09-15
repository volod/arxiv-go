package catia

import (
	"context"
	"io"
)

type streamExtract struct {
	props  *v5props
	win    windowFinder
	ascii  asciiRun
	u16e   utf16Run
	u16o   utf16Run
	str    *stringSet
	off    int64
	evenLo byte
	oddLo  byte
	hasOdd bool
}

func extractStream(ctx context.Context, info Info, r io.Reader, self string) Info {
	s := streamExtract{props: newV5props(), str: newStringSet()}
	buf := make([]byte, readChunk)
	for {
		if err := ctx.Err(); err != nil {
			return fail(info, ErrorKindCancel, err)
		}
		n, err := r.Read(buf)
		if n > 0 {
			s.write(buf[:n])
		}
		if err == io.EOF {
			break
		}
		if err != nil {
			return fail(info, ErrorKindRead, err)
		}
	}
	s.flush()
	return finishStream(info, &s, self)
}

func (s *streamExtract) write(p []byte) {
	s.props.write(p)
	s.win.write(p)
	if s.str.truncated {
		return
	}
	for _, c := range p {
		s.ascii.feed(c, s.str)
		if s.off%2 == 0 {
			s.evenLo = c
			if s.hasOdd {
				s.u16o.feedPair(s.oddLo, c, s.str)
			}
		} else {
			s.oddLo = c
			s.hasOdd = true
			s.u16e.feedPair(s.evenLo, c, s.str)
		}
		s.off++
		if s.str.truncated {
			return
		}
	}
}

func (s *streamExtract) flush() {
	s.ascii.flush(s.str)
	s.u16e.flush(s.str)
	s.u16o.flush(s.str)
}

func finishStream(info Info, s *streamExtract, self string) Info {
	if info.Format == FormatV5 {
		if rel := s.props.release(); rel != "" {
			info.Release = rel
		}
		info.BuildLevel = s.props.build
		info.Components = parseComponents(s.win.window(), self)
	}
	info.Strings = s.str.list(info.Components)
	info.Truncated = info.Truncated || s.str.truncated
	return info
}
