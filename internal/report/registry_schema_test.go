package report

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// Every writer emits its full contract header even when every optional column is empty.
func TestRegistryWritersWriteFullHeader(t *testing.T) {
	path := t.TempDir() + "/reg.csv"
	for _, tc := range []struct {
		name   string
		write  func(b *strings.Builder) error
		header []string
	}{
		{"file registry", func(b *strings.Builder) error {
			w, err := CreateRegistry(path)
			if err != nil {
				return err
			}
			if err := w.Write(RegistryRow{RelPath: "a.txt", FileName: "a.txt", FileType: "txt"}); err != nil {
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}
			b.WriteString(string(mustReadFile(t, path)))
			return nil
		}, RegistryHeader},
		{"file registry empty", func(b *strings.Builder) error {
			w, err := CreateRegistry(path)
			if err != nil {
				return err
			}
			if err := w.Close(); err != nil {
				return err
			}
			b.WriteString(string(mustReadFile(t, path)))
			return nil
		}, RegistryHeader},
		{"video registry", func(b *strings.Builder) error {
			return WriteVideoCSV(b, []VideoRow{{RelPath: "a.mp4", FileName: "a.mp4", Status: StatusSkipped}})
		}, VideoHeader},
		{"video registry empty", func(b *strings.Builder) error { return WriteVideoCSV(b, nil) }, VideoHeader},
		{"catia registry", func(b *strings.Builder) error {
			return WriteCatiaCSV(b, []CatiaRow{{PayloadRow: PayloadRow{RelPath: "a.cgr", FileName: "a.cgr", Status: StatusConflict}}})
		}, CatiaHeader},
		{"catia registry empty", func(b *strings.Builder) error { return WriteCatiaCSV(b, nil) }, CatiaHeader},
	} {
		t.Run(tc.name, func(t *testing.T) {
			var b strings.Builder
			if err := tc.write(&b); err != nil {
				t.Fatal(err)
			}
			lines := strings.Split(strings.TrimSuffix(b.String(), "\n"), "\n")
			if lines[0] != strings.Join(tc.header, ",") {
				t.Fatalf("header = %s", lines[0])
			}
			for _, line := range lines[1:] {
				if got := strings.Count(line, ",") + 1; got != len(tc.header) {
					t.Fatalf("row %q has %d cells, want %d", line, got, len(tc.header))
				}
			}
		})
	}
}

// Registries written by earlier builds omitted optional columns empty in every row; they are still
// read, with every missing column empty, and the next write has the full header.
func TestRegistryReadersAcceptCompactedFiles(t *testing.T) {
	t.Run("file registry", func(t *testing.T) {
		compacted := strings.Join(RegistryHeader[:FileRegistryRequired], ",") + ",mtime,media_container\n" +
			"a.mp4,a.mp4,mp4,7,false,video/mp4,true,true,false,true,false,2024-05-01T10:22:03Z,mp4\n"
		rows, err := ReadRegistry(strings.NewReader(compacted))
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows = %+v (%v)", rows, err)
		}
		m := rows[0].Metadata
		if m.MTime != "2024-05-01T10:22:03Z" || m.LinkTarget != "" || m.Media == nil || m.Media.Container != "mp4" ||
			m.Media.Source != "" || m.Media.Width != 0 || !rows[0].IsVideo {
			t.Fatalf("row = %+v media %+v", rows[0], m.Media)
		}
		bare := strings.Join(RegistryHeader[:FileRegistryRequired], ",") + "\n" +
			"b.txt,b.txt,txt,1,false,text/plain,false,false,false,false,false\n"
		if rows, err := ReadRegistry(strings.NewReader(bare)); err != nil || len(rows) != 1 || HasMetadata(rows[0].Metadata) {
			t.Fatalf("required-only registry = %+v (%v)", rows, err)
		}
	})
	t.Run("video registry", func(t *testing.T) {
		compacted := strings.Join(VideoHeader[:VideoRegistryRequired], ",") + ",mtime\n" +
			"a.mp4,a.mp4,moved,file:///v/a.mp4,a.mp4.md,7,,rename,r1,video/mp4,,2024-05-01T10:22:03Z\n"
		rows, err := LoadVideoCSV(strings.NewReader(compacted))
		if err != nil || len(rows) != 1 || rows[0].Metadata.MTime != "2024-05-01T10:22:03Z" || rows[0].Metadata.Media != nil ||
			rows[0].SHA256 != "" || rows[0].Previews != "" {
			t.Fatalf("rows = %+v (%v)", rows, err)
		}
		var b strings.Builder
		if err := WriteVideoCSV(&b, rows); err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(b.String(), "\n"); first != strings.Join(VideoHeader, ",") {
			t.Fatalf("rewritten header = %s", first)
		}
		again, err := LoadVideoCSV(strings.NewReader(b.String()))
		if err != nil || !reflect.DeepEqual(again, rows) {
			t.Fatalf("rewrite changed rows: %+v (%v)", again, err)
		}
	})
	t.Run("catia registry", func(t *testing.T) {
		compacted := strings.Join(PayloadHeader, ",") + ",catia_kind,catia_format,catia_components,mtime\n" +
			"a.CATPart,a.CATPart,moved,file:///c/a.CATPart,a.CATPart.md,9,,rename,r1,application/octet-stream,CATPart,V5_CFV2,0,2026-09-15T12:00:00Z\n"
		rows, err := LoadCatiaCSV(strings.NewReader(compacted))
		if err != nil || len(rows) != 1 {
			t.Fatalf("rows = %+v (%v)", rows, err)
		}
		if r := rows[0]; r.TextRelPath != "" || r.Release != "" || r.Kind != "CATPart" || r.Format != "V5_CFV2" || r.MTime == "" {
			t.Fatalf("row = %+v", r)
		}
		var b strings.Builder
		if err := WriteCatiaCSV(&b, rows); err != nil {
			t.Fatal(err)
		}
		if first, _, _ := strings.Cut(b.String(), "\n"); first != strings.Join(CatiaHeader, ",") {
			t.Fatalf("rewritten header = %s", first)
		}
		again, err := LoadCatiaCSV(strings.NewReader(b.String()))
		if err != nil || !reflect.DeepEqual(again, rows) {
			t.Fatalf("rewrite changed rows: %+v (%v)", again, err)
		}
	})
}

func mustReadFile(t *testing.T, path string) []byte {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	return data
}
