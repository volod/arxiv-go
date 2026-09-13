package state

import (
	"reflect"
	"testing"
)

func TestScanStatsCountsFlagsMIMEAndSkipped(t *testing.T) {
	var s ScanStats
	s.AddFile(100, "video/mp4", FileFlags{Binary: true, Media: true, Video: true, Large: true})
	s.AddFile(300, "video/mp4", FileFlags{Binary: true, Media: true, Video: true})
	s.AddFile(5, "text/plain", FileFlags{})
	s.AddFile(5, "text/csv", FileFlags{})
	s.AddFile(7, "image/png", FileFlags{Binary: true, Media: true, Picture: true})
	s.AddSkipped("unreadable")
	s.AddSkipped("unreadable")
	s.AddSkipped("special")
	s.Symlinks, s.Dirs = 2, 3

	if s.Files != 5 || s.Bytes != 417 || s.Rows() != 7 || s.SkippedTotal() != 3 || s.LargestVideo != 300 {
		t.Errorf("totals = %+v", s)
	}
	if s.Video != (CountBytes{2, 400}) || s.Large != (CountBytes{1, 100}) || s.Picture != (CountBytes{1, 7}) ||
		s.Media != (CountBytes{3, 407}) || s.Binary != (CountBytes{3, 407}) {
		t.Errorf("flags = %+v", s)
	}
	want := []MIMEBytes{{"video/mp4", CountBytes{2, 400}}, {"image/png", CountBytes{1, 7}}, {"text/csv", CountBytes{1, 5}}}
	if got := s.TopMIMEs(3); !reflect.DeepEqual(got, want) {
		t.Errorf("top = %+v, want %+v (ties by rows, then name)", got, want)
	}

	c := s.Clone()
	c.AddFile(1, "video/mp4", FileFlags{})
	c.AddSkipped("special")
	if s.MIME["video/mp4"].Count != 2 || s.Skipped["special"] != 1 {
		t.Error("Clone shares maps with the original")
	}
	sum := s.Summary(12.5)
	if sum.Files != 5 || len(sum.TopMIME) != 4 || sum.ElapsedS != 12.5 || sum.Skipped["unreadable"] != 2 {
		t.Errorf("summary = %+v", sum)
	}
}
