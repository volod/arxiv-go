package catia

import (
	"bytes"
	"context"
	"testing"
)

func FuzzExtract(f *testing.F) {
	f.Add([]byte("V5_CFV2\x00"))
	f.Add([]byte{'P', 'K', 0x03, 0x04})
	f.Add([]byte("<Model"))
	f.Add([]byte{0x00, 0x05, 0x16, 0x07})
	f.Add(v5File(v5LastSave(30, 5), v5Window(componentChunk("fixture-part.CATPart"))))
	f.Add([]byte(sample3DXML))
	f.Fuzz(func(t *testing.T, data []byte) {
		names := []string{"fixture-part.CATPart", "fixture-product.CATProduct", "fixture-drawing.CATDrawing", "fixture.cgr", "fixture.3dxml"}
		for _, name := range names {
			info := Extract(context.Background(), bytes.NewReader(data), name)
			if info.Kind == "" && name != "fixture.cgr" && name != "fixture.3dxml" {
				t.Fatalf("kind empty for %s", name)
			}
			_ = info.Format
			_ = info.Release
			_ = info.Components
			_ = info.Strings
		}
	})
}
