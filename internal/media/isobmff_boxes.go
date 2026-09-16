package media

import (
	"encoding/binary"
	"errors"
	"io"
	"math"
	"strings"

	mp4 "github.com/abema/go-mp4"
)

// Probe expands several sample tables into slices. Check their declared counts first so a
// malformed but small box cannot turn a metadata scan into a large allocation.
func safeProbeTable(h *mp4.ReadHandle, name string) (bool, error) {
	if h.BoxInfo.Size-h.BoxInfo.HeaderSize > isoBoxLimit {
		return false, nil
	}
	var b strings.Builder
	if _, err := h.ReadData(&b); err != nil {
		return false, err
	}
	data := []byte(b.String())
	offset := 4
	if name == "stsz" {
		offset = 8
	}
	if len(data) < offset+4 {
		return false, io.ErrUnexpectedEOF
	}
	count := binary.BigEndian.Uint32(data[offset:])
	entrySize := 0
	switch name {
	case "stco":
		entrySize = 4
	case "co64", "ctts":
		entrySize = 8
	case "stsc":
		entrySize = 12
	case "elst":
		entrySize = 12
		if data[0] == 1 {
			entrySize = 20
		}
	case "stsz":
		if binary.BigEndian.Uint32(data[4:8]) == 0 {
			entrySize = 4
		}
	}
	if uint64(count)*uint64(entrySize) > uint64(len(data)-offset-4) {
		return false, errors.New("invalid " + name + " entry count")
	}
	return count <= 50000, nil
}

func readSTTS(h *mp4.ReadHandle) (uint64, uint64, error) {
	if h.BoxInfo.Size-h.BoxInfo.HeaderSize > isoBoxLimit {
		return 0, 0, errors.New("stts box too large")
	}
	var raw [8]byte
	// ReadData is bounded by the caller's box-size check, but reading directly avoids an
	// untrusted entry count becoming an allocation in the library's typed Stts decoder.
	var b strings.Builder
	_, err := h.ReadData(&b)
	if err != nil {
		return 0, 0, err
	}
	data := []byte(b.String())
	if len(data) < 8 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	copy(raw[:], data[:8])
	count := binary.BigEndian.Uint32(raw[4:8])
	if uint64(count)*8 > uint64(len(data)-8) {
		return 0, 0, errors.New("invalid stts entry count")
	}
	var samples, ticks uint64
	for i := uint32(0); i < count; i++ {
		off := 8 + int(i)*8
		n := uint64(binary.BigEndian.Uint32(data[off:]))
		delta := uint64(binary.BigEndian.Uint32(data[off+4:]))
		if math.MaxUint64-samples < n || (delta > 0 && n > (math.MaxUint64-ticks)/delta) {
			return 0, 0, errors.New("stts duration overflow")
		}
		samples += n
		ticks += n * delta
	}
	return samples, ticks, nil
}

func readTRUN(h *mp4.ReadHandle, defaultDuration uint32) (uint64, uint64, error) {
	if h.BoxInfo.Size-h.BoxInfo.HeaderSize > isoBoxLimit {
		return 0, 0, errors.New("trun box too large")
	}
	var b strings.Builder
	if _, err := h.ReadData(&b); err != nil {
		return 0, 0, err
	}
	data := []byte(b.String())
	if len(data) < 8 {
		return 0, 0, io.ErrUnexpectedEOF
	}
	flags := uint32(data[1])<<16 | uint32(data[2])<<8 | uint32(data[3])
	count := binary.BigEndian.Uint32(data[4:8])
	off := 8
	if flags&1 != 0 {
		off += 4
	}
	if flags&4 != 0 {
		off += 4
	}
	stride := 0
	for _, f := range []uint32{0x100, 0x200, 0x400, 0x800} {
		if flags&f != 0 {
			stride += 4
		}
	}
	if off > len(data) || uint64(count)*uint64(stride) > uint64(len(data)-off) {
		return 0, 0, errors.New("invalid trun sample count")
	}
	if flags&0x100 == 0 {
		if defaultDuration == 0 {
			return 0, uint64(count), nil
		}
		return uint64(count) * uint64(defaultDuration), 0, nil
	}
	var ticks uint64
	for i := uint32(0); i < count; i++ {
		v := uint64(binary.BigEndian.Uint32(data[off:]))
		if math.MaxUint64-ticks < v {
			return 0, 0, errors.New("trun duration overflow")
		}
		ticks += v
		off += stride
	}
	return ticks, 0, nil
}
