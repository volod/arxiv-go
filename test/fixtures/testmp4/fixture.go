// Package testmp4 builds small ISO BMFF fixtures without an external encoder.
package testmp4

import (
	"bytes"
	"encoding/binary"
)

type Track struct {
	Kind          string // vide or soun
	Codec         string
	Width, Height uint16
	Rotation      int
}

type Options struct {
	Brand string
	// QuickTime writes the media handler as component type mhlr and adds the data handler hdlr
	// (dhlr, "url ") that QuickTime movies carry inside minf.
	QuickTime  bool
	Tracks     []Track
	MoovAtEnd  bool
	Fragmented bool
	NoMehd     bool
	Fragments  int
	UseTrex    bool
	Title      string
	MdatBytes  int
}

func File(o Options) []byte {
	brand := o.Brand
	if brand == "" {
		brand = "isom"
	}
	ftyp := Box("ftyp", append(append([]byte(brand), 0, 0, 0, 0), []byte("isom")...))
	var children [][]byte
	children = append(children, mvhd(o.Fragmented))
	for i, t := range o.Tracks {
		children = append(children, track(uint32(i+1), t, o.Fragmented, o.QuickTime))
	}
	if o.Fragmented {
		var mvex []byte
		if !o.NoMehd {
			mvex = append(mvex, Box("mehd", append(fullBox(), u32(5000)...))...)
		}
		if o.UseTrex {
			trex := append(fullBox(), u32(1)...)
			trex = append(trex, u32(1)...)
			trex = append(trex, u32(40)...)
			trex = append(trex, u32(0)...)
			trex = append(trex, u32(0)...)
			mvex = append(mvex, Box("trex", trex)...)
		}
		children = append(children, Box("mvex", mvex))
	}
	if o.Title != "" {
		data := Box("data", append(append(u32(1), u32(0)...), []byte(o.Title)...))
		item := Box(string([]byte{0xa9, 'n', 'a', 'm'}), data)
		meta := Box("meta", append(fullBox(), Box("ilst", item)...))
		children = append(children, Box("udta", meta))
	}
	moov := Box("moov", bytes.Join(children, nil))
	var fragments []byte
	for i := 0; i < o.Fragments; i++ {
		flags := byte(8)
		if o.UseTrex {
			flags = 0
		}
		tfhd := append([]byte{0, 0, 0, flags}, u32(1)...)
		if !o.UseTrex {
			tfhd = append(tfhd, u32(40)...)
		}
		trun := append(fullBox(), u32(125)...)
		traf := Box("traf", bytes.Join([][]byte{Box("tfhd", tfhd), Box("trun", trun)}, nil))
		fragments = append(fragments, Box("moof", traf)...)
	}
	mdat := Box("mdat", make([]byte, o.MdatBytes))
	if o.MoovAtEnd {
		return bytes.Join([][]byte{ftyp, fragments, mdat, moov}, nil)
	}
	return bytes.Join([][]byte{ftyp, moov, fragments, mdat}, nil)
}

func Box(name string, payload []byte) []byte {
	b := make([]byte, 8+len(payload))
	binary.BigEndian.PutUint32(b, uint32(len(b)))
	copy(b[4:8], []byte(name))
	copy(b[8:], payload)
	return b
}

func mvhd(fragmented bool) []byte {
	b := make([]byte, 100)
	binary.BigEndian.PutUint32(b[12:16], 1000) // timescale
	if !fragmented {
		binary.BigEndian.PutUint32(b[16:20], 5000)
	}
	binary.BigEndian.PutUint32(b[20:24], 0x10000) // rate
	binary.BigEndian.PutUint16(b[24:26], 0x100)   // volume
	putMatrix(b[36:72], 0)
	binary.BigEndian.PutUint32(b[96:100], 3)
	return Box("mvhd", b)
}

func track(id uint32, t Track, fragmented, quickTime bool) []byte {
	var children [][]byte
	tk := make([]byte, 84)
	binary.BigEndian.PutUint32(tk[12:16], id)
	if !fragmented {
		binary.BigEndian.PutUint32(tk[20:24], 5000)
	}
	if t.Kind == "soun" {
		binary.BigEndian.PutUint16(tk[36:38], 0x100)
	}
	putMatrix(tk[40:76], t.Rotation)
	binary.BigEndian.PutUint32(tk[76:80], uint32(t.Width)<<16)
	binary.BigEndian.PutUint32(tk[80:84], uint32(t.Height)<<16)
	children = append(children, Box("tkhd", tk))
	md := make([]byte, 24)
	binary.BigEndian.PutUint32(md[12:16], 1000)
	if !fragmented {
		binary.BigEndian.PutUint32(md[16:20], 5000)
	}
	hd := make([]byte, 24)
	copy(hd[8:12], []byte(t.Kind))
	stsd := Box("stsd", append(append(fullBox(), u32(1)...), Box(t.Codec, nil)...))
	stts := Box("stts", append(append(append(fullBox(), u32(1)...), u32(125)...), u32(40)...))
	minf := [][]byte{Box("stbl", bytes.Join([][]byte{stsd, stts}, nil))}
	if quickTime {
		copy(hd[4:8], []byte("mhlr"))
		data := make([]byte, 24)
		copy(data[4:8], []byte("dhlr"))
		copy(data[8:12], []byte("url "))
		minf = append([][]byte{Box("hdlr", data)}, minf...)
	}
	mdia := Box("mdia", bytes.Join([][]byte{Box("mdhd", md), Box("hdlr", hd), Box("minf", bytes.Join(minf, nil))}, nil))
	children = append(children, mdia)
	return Box("trak", bytes.Join(children, nil))
}

func putMatrix(b []byte, rotation int) {
	var a, bb, c, d int32 = 65536, 0, 0, 65536
	switch rotation {
	case 90:
		a, bb, c, d = 0, 65536, -65536, 0
	case 180:
		a, bb, c, d = -65536, 0, 0, -65536
	case 270:
		a, bb, c, d = 0, -65536, 65536, 0
	}
	for i, v := range []int32{a, bb, 0, c, d, 0, 0, 0, 0x40000000} {
		binary.BigEndian.PutUint32(b[i*4:], uint32(v))
	}
}

func fullBox() []byte     { return []byte{0, 0, 0, 0} }
func u32(v uint32) []byte { b := make([]byte, 4); binary.BigEndian.PutUint32(b, v); return b }
