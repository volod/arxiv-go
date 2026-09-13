package media

import (
	"context"
	"errors"
	"io"

	mp4 "github.com/abema/go-mp4"
)

const isoReadLimit = 32 << 20
const isoBoxLimit = 1 << 20

var errNotRegular = errors.New("not a regular file")

type isoReader struct {
	r    io.ReadSeeker
	size int64
	read int64
	ctx  context.Context
}

func (r *isoReader) Read(p []byte) (int, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	if r.read+int64(len(p)) > isoReadLimit {
		return 0, errors.New("metadata read limit exceeded")
	}
	n, err := r.r.Read(p)
	r.read += int64(n)
	return n, err
}

func (r *isoReader) Seek(offset int64, whence int) (int64, error) {
	if err := r.ctx.Err(); err != nil {
		return 0, err
	}
	var base int64
	switch whence {
	case io.SeekStart:
		base = 0
	case io.SeekCurrent:
		var err error
		base, err = r.r.Seek(0, io.SeekCurrent)
		if err != nil {
			return 0, err
		}
	case io.SeekEnd:
		base = r.size
	default:
		return 0, errors.New("invalid seek")
	}
	if offset < -base || offset > r.size-base {
		return 0, errors.New("box extends beyond file")
	}
	return r.r.Seek(base+offset, io.SeekStart)
}

type isoTrack struct {
	id                      uint32
	kind                    string
	codec                   string
	width, height, rotation int
	timescale               uint32
	duration                uint64
	samples, ticks          uint64
	fragmentTicks           uint64
}

type isoFragment struct {
	trackID         uint32
	defaultDuration uint32
	ticks           uint64
	pendingSamples  uint64
}

type isoScan struct {
	info            *MediaInfo
	tracks          []*isoTrack
	track           *isoTrack
	fragment        *isoFragment
	fragmentTicks   map[uint32]uint64
	fragmentPending map[uint32]uint64
	trexDefaults    map[uint32]uint32
	moov, ftyp      bool
	timescale       uint32
	duration        uint64
	mehd            uint64
	probeSafe       bool
}

func parseISO(ctx context.Context, f io.ReadSeeker, size int64, info *MediaInfo) error {
	r := &isoReader{r: f, size: size, ctx: ctx}
	s := &isoScan{info: info, probeSafe: true}
	_, err := mp4.ReadBoxStructure(r, s.box)
	if err != nil {
		return err
	}
	if !s.moov {
		return errors.New("moov box missing")
	}
	if len(s.tracks) == 0 {
		return errors.New("no tracks")
	}
	// Probe is useful on small, ordinary sample tables. The structural walk owns normalization,
	// because Probe materializes every sample and cannot handle all valid codec entries.
	if s.probeSafe && r.read < isoBoxLimit && len(s.tracks) <= 8 {
		_, _ = mp4.Probe(r)
	}
	s.finish(size)
	return nil
}

func (s *isoScan) box(h *mp4.ReadHandle) (any, error) {
	name := h.BoxInfo.Type.String()
	path := h.Path
	switch name {
	case "ftyp":
		s.ftyp = true
	case "moov":
		s.moov = true
	case "trak":
		if len(s.tracks) >= 64 {
			return nil, errors.New("too many tracks")
		}
		s.track = &isoTrack{}
		s.tracks = append(s.tracks, s.track)
	case "traf":
		s.fragment = &isoFragment{}
		s.probeSafe = false // Probe expands trun entries even when the box has no per-sample fields.
	case "mvhd":
		box, err := readSmall(h)
		if err != nil {
			return nil, err
		}
		v := box.(*mp4.Mvhd)
		s.timescale, s.duration = v.Timescale, v.GetDuration()
		s.info.CreationTime = mp4Time(v.GetCreationTime())
	case "mehd":
		box, err := readSmall(h)
		if err != nil {
			return nil, err
		}
		s.mehd = box.(*mp4.Mehd).GetFragmentDuration()
	case "trex":
		box, err := readSmall(h)
		if err != nil {
			return nil, err
		}
		v := box.(*mp4.Trex)
		if s.trexDefaults == nil {
			s.trexDefaults = make(map[uint32]uint32)
		}
		s.trexDefaults[v.TrackID] = v.DefaultSampleDuration
	case "tkhd":
		if s.track != nil {
			box, err := readSmall(h)
			if err != nil {
				return nil, err
			}
			v := box.(*mp4.Tkhd)
			s.track.id = v.TrackID
			s.track.width, s.track.height = int(v.Width>>16), int(v.Height>>16)
			s.track.rotation = matrixRotation(v.Matrix)
		}
	case "mdhd":
		if s.track != nil {
			box, err := readSmall(h)
			if err != nil {
				return nil, err
			}
			v := box.(*mp4.Mdhd)
			s.track.timescale, s.track.duration = v.Timescale, v.GetDuration()
		}
	case "hdlr":
		// Only the media handler directly in mdia names the track kind; QuickTime also puts a data
		// handler (dhlr) in minf, which must not overwrite it.
		if s.track != nil && parent(path) == "mdia" {
			box, err := readSmall(h)
			if err != nil {
				return nil, err
			}
			s.track.kind = string(box.(*mp4.Hdlr).HandlerType[:])
		}
	case "stts":
		if s.track != nil {
			samples, ticks, err := readSTTS(h)
			if err != nil {
				return nil, err
			}
			s.track.samples, s.track.ticks = samples, ticks
			if samples > 50000 {
				s.probeSafe = false
			}
		}
	case "stco", "co64", "stsc", "ctts", "stsz", "elst":
		if inPath(path, "trak") {
			safe, err := safeProbeTable(h, name)
			if err != nil {
				return nil, err
			}
			s.probeSafe = s.probeSafe && safe
		}
	case "tfhd":
		if s.fragment != nil {
			box, err := readSmall(h)
			if err != nil {
				return nil, err
			}
			v := box.(*mp4.Tfhd)
			s.fragment.trackID, s.fragment.defaultDuration = v.TrackID, v.DefaultSampleDuration
		}
	case "trun":
		if s.fragment != nil {
			ticks, pending, err := readTRUN(h, s.fragment.defaultDuration)
			if err != nil {
				return nil, err
			}
			s.fragment.ticks += ticks
			s.fragment.pendingSamples += pending
		}
	case "data":
		if inPath(path, "ilst") {
			if err := s.tag(h); err != nil {
				return nil, err
			}
		}
	default:
		if s.track != nil && parent(path) == "stsd" && s.track.codec == "" {
			s.track.codec = codecName(name)
		}
	}
	if name == "traf" {
		_, err := h.Expand()
		if err != nil {
			return nil, err
		}
		if s.fragmentTicks == nil {
			s.fragmentTicks = make(map[uint32]uint64)
		}
		s.fragmentTicks[s.fragment.trackID] += s.fragment.ticks
		if s.fragmentPending == nil {
			s.fragmentPending = make(map[uint32]uint64)
		}
		s.fragmentPending[s.fragment.trackID] += s.fragment.pendingSamples
		s.fragment = nil
		return nil, nil
	}
	if name == "trak" {
		_, err := h.Expand()
		s.track = nil
		return nil, err
	}
	if expandBox(name, path) {
		_, err := h.Expand()
		return nil, err
	}
	return nil, nil
}

func expandBox(name string, path mp4.BoxPath) bool {
	switch name {
	case "moov", "mdia", "minf", "stbl", "stsd", "edts", "mvex", "moof", "udta", "meta", "ilst":
		return true
	}
	return inPath(path, "ilst") && len(path) > 1 && name != "data" && len(path) <= 5
}

func parent(path mp4.BoxPath) string {
	if len(path) < 2 {
		return ""
	}
	return path[len(path)-2].String()
}
func inPath(path mp4.BoxPath, name string) bool {
	for _, p := range path {
		if p.String() == name {
			return true
		}
	}
	return false
}
