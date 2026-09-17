package catia

import (
	"context"
	"io"
)

type streamExtract struct {
	props    *v5props
	win      windowFinder
	product  *productReader
	material *materialReader
	notes    *noteReader
}

// extractStream reads a V5 document, or skips a file of another format, which has no properties,
// components or notes.
func extractStream(ctx context.Context, info Info, r io.Reader, self string) Info {
	if info.Format != FormatV5 {
		return info
	}
	s := streamExtract{props: newV5props(), product: newProductReader(), material: newMaterialReader(), notes: newNoteReader()}
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
	s.product.flush()
	s.material.flush()
	return finishStream(info, &s, self)
}

func (s *streamExtract) write(p []byte) {
	s.props.write(p)
	s.win.write(p)
	s.product.write(p)
	s.material.write(p)
	s.notes.write(p)
}

func finishStream(info Info, s *streamExtract, self string) Info {
	if rel := s.props.release(); rel != "" {
		info.Release = rel
	}
	info.BuildLevel = s.props.build
	info.Components = parseComponents(s.win.window(), self)
	p := s.product.props
	info.Product = Product{
		PartNumber: p["part_number"], Revision: p["revision"], Definition: p["definition"],
		Nomenclature: p["nomenclature"], Source: p["source"], Description: p["description"],
		Material: s.material.value,
	}
	info.Notes = s.notes.notes
	info.Truncated = info.Truncated || s.notes.truncated
	return info
}
