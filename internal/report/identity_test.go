package report

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/volod/arxiv-go/internal/catia"
)

func TestRenderCatiaTextIdentity(t *testing.T) {
	at := time.Date(2026, 9, 15, 12, 5, 1, 0, time.UTC)
	info := catia.Info{Format: catia.FormatV5, Release: "V5R30", Components: []string{"fixture-part.CATPart"}}
	full := TextIdentity{
		FileSize: "4096 (4.0 KiB)", FileMIME: "application/octet-stream", SHA256: strings.Repeat("ab", 32),
		Modified: "2026-09-15T12:00:00Z", Catia: "CATProduct | V5_CFV2 | V5R30 | 1 component",
		MovedTo: "[fixture.CATProduct](file:///mnt/nas/catia/cad/fixture.CATProduct)",
		URL:     "https://storage.example.com/catia/cad/fixture.CATProduct", Description: "cad/fixture.CATProduct.md",
	}
	cases := []struct {
		name string
		in   CatiaTextInput
		want string
	}{
		{
			name: "full identity",
			in:   CatiaTextInput{RelPath: "cad/fixture.CATProduct", Archive: "/mnt/nas/archive", Identity: full, ExtractedAt: at, Info: info},
			want: "arxgo-text: cad/fixture.CATProduct\narchive: /mnt/nas/archive\nfile_name: fixture.CATProduct\n" +
				"file_size: 4096 (4.0 KiB)\nfile_mime: application/octet-stream\nsha256: " + strings.Repeat("ab", 32) + "\n" +
				"modified: 2026-09-15T12:00:00Z\ncatia: CATProduct | V5_CFV2 | V5R30 | 1 component\n" +
				"moved_to: [fixture.CATProduct](file:///mnt/nas/catia/cad/fixture.CATProduct)\n" +
				"url: https://storage.example.com/catia/cad/fixture.CATProduct\ndescription: cad/fixture.CATProduct.md\n" +
				"extracted_at: 2026-09-15T12:05:01Z\ntruncated: false\nproperties:\n- release: V5R30\ncomponents:\n- fixture-part.CATPart\n",
		},
		{
			name: "no description",
			in:   CatiaTextInput{RelPath: "fixture.CATPart", Archive: "/mnt/nas/archive", ExtractedAt: at},
			want: "arxgo-text: fixture.CATPart\narchive: /mnt/nas/archive\nfile_name: fixture.CATPart\n" +
				"extracted_at: 2026-09-15T12:05:01Z\ntruncated: false\n",
		},
		{
			name: "escaped values",
			in: CatiaTextInput{RelPath: " cad/a \"b\".CATPart", Archive: "/mnt/nas/ar\\chive ", ExtractedAt: at,
				Identity: TextIdentity{Description: " cad/a \"b\".CATPart.md"}},
			want: "arxgo-text: \" cad/a \\\"b\\\".CATPart\"\narchive: \"/mnt/nas/ar\\\\chive \"\nfile_name: \"a \\\"b\\\".CATPart\"\n" +
				"description: \" cad/a \\\"b\\\".CATPart.md\"\nextracted_at: 2026-09-15T12:05:01Z\ntruncated: false\n",
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			got := string(RenderCatiaText(tc.in))
			if got != tc.want {
				t.Fatalf("got\n%s\nwant\n%s", got, tc.want)
			}
			if occ, err := inspectSidecarBytes(t, []byte(got), tc.in.RelPath); err != nil || occ != DescriptionOwned {
				t.Fatalf("rendered sidecar not owned: %v, %v", occ, err)
			}
		})
	}
}

func TestRenderCatiaTextIdentityMatchesDescription(t *testing.T) {
	description := RenderDescription(DescriptionInput{
		RelPath: "cad/fixture.CATPart", Archive: "/mnt/nas/archive", FileSize: 4096, FileMIME: "application/octet-stream",
		SHA256: strings.Repeat("cd", 32), Modified: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		MovedAt: time.Date(2026, 9, 15, 12, 5, 0, 0, time.UTC), MovedTo: "/mnt/nas/catia/cad/[x] fixture.CATPart",
		URL: "https://storage.example.com/catia/cad/fixture.CATPart", Catia: &catia.Info{Kind: catia.KindCATPart, Format: catia.FormatV5},
	})
	d, err := ParseDescription(bytes.NewReader(description))
	if err != nil {
		t.Fatal(err)
	}
	sidecar := RenderCatiaText(CatiaTextInput{RelPath: "cad/fixture.CATPart", Archive: "/mnt/nas/archive",
		Identity: TextIdentityOf(d, "cad/fixture.CATPart.md"), ExtractedAt: time.Date(2026, 9, 15, 12, 5, 1, 0, time.UTC)})
	s := string(sidecar)
	for _, key := range []string{"archive", "file_size", "file_mime", "sha256", "modified", "catia", "moved_to", "url"} {
		line := key + ": " + quoteValue(d[key]) + "\n"
		if !strings.Contains(string(description), line) || !strings.Contains(s, "\n"+line) {
			t.Errorf("%s: line %q not in both:\n%s\n%s", key, line, description, s)
		}
	}
	if strings.Contains(s, "moved_at:") {
		t.Errorf("sidecar repeats moved_at:\n%s", s)
	}
}

func TestRenderCatiaTextOversizedIdentity(t *testing.T) {
	info := catia.Info{Format: catia.FormatV5, Release: "V5R30", BuildLevel: "b", Components: []string{"a.CATPart"}, Notes: []string{"note"}}
	at := time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC)
	for name, id := range map[string]TextIdentity{
		"first field":  {FileSize: strings.Repeat("9", catiaTextCap)},
		"late field":   {FileSize: "1 (1 B)", URL: "https://example.com/" + strings.Repeat("u", catiaTextCap-200)},
		"whole block":  {MovedTo: strings.Repeat("m", catiaTextCap/2), URL: strings.Repeat("u", catiaTextCap/2)},
		"exact margin": {URL: strings.Repeat("u", catiaTextCap-120)},
	} {
		t.Run(name, func(t *testing.T) {
			got := string(RenderCatiaText(CatiaTextInput{RelPath: "cad/fixture.CATProduct", Archive: "/mnt/nas/archive",
				Identity: id, ExtractedAt: at, Info: info}))
			if len(got) > catiaTextCap {
				t.Fatalf("len %d exceeds the cap", len(got))
			}
			if !strings.HasPrefix(got, "arxgo-text: cad/fixture.CATProduct\narchive: /mnt/nas/archive\nfile_name: fixture.CATProduct\n") ||
				!strings.Contains(got, "\nextracted_at: 2026-09-15T12:00:00Z\ntruncated: true\n") || !strings.HasSuffix(got, "truncated: true\n") {
				t.Fatalf("header:\n%.400s\n...\n%s", got, got[max(0, len(got)-200):])
			}
			for _, block := range []string{"properties:", "components:", "notes:"} {
				if strings.Contains(got, "\n"+block+"\n") {
					t.Errorf("block %s written after the identity block overflowed", block)
				}
			}
		})
	}
}

func TestRenderCatiaTextIdentityCountsAgainstCap(t *testing.T) {
	var comps []string
	for i := 0; i < 3000; i++ {
		comps = append(comps, strings.Repeat("c", 400)+".CATPart")
	}
	in := CatiaTextInput{RelPath: "a.CATProduct", ExtractedAt: time.Date(2026, 9, 15, 12, 0, 0, 0, time.UTC),
		Info: catia.Info{Format: catia.FormatV5, Components: comps}}
	bare := RenderCatiaText(in)
	in.Archive, in.Identity = "/mnt/nas/archive", TextIdentity{URL: strings.Repeat("u", 64<<10)}
	with := RenderCatiaText(in)
	if len(bare) > catiaTextCap || len(with) > catiaTextCap {
		t.Fatalf("lengths %d, %d exceed the cap", len(bare), len(with))
	}
	if strings.Count(string(with), "\n- ") >= strings.Count(string(bare), "\n- ") {
		t.Fatal("identity block did not reduce the components that fit")
	}
	if !strings.Contains(string(with), "\ntruncated: true\ncomponents:\n") {
		t.Fatalf("header:\n%.300s", with[len(with)-(64<<10)-300:])
	}
}

func TestFindDescriptionPath(t *testing.T) {
	dir := t.TempDir()
	src := filepath.Join(dir, "fixture.CATPart")
	rel := "cad/fixture.CATPart"
	write := func(name, body string) {
		t.Helper()
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	find := func(want string) {
		t.Helper()
		got, err := FindDescriptionPath(src, rel)
		if err != nil || filepath.Base(got) != want && !(got == "" && want == "") {
			t.Fatalf("found %q, %v; want %q", got, err, want)
		}
	}
	find("")
	write("fixture.CATPart.md", "operator notes\n")
	find("")
	write("fixture.CATPart.arxgo.md", "arxgo: "+rel+"\n")
	find("fixture.CATPart.arxgo.md")
	write("fixture.CATPart.arxgo.md", "arxgo: other.CATPart\n")
	write("fixture-1.CATPart.md", "operator notes\n")
	write("fixture-2.CATPart.md", "arxgo: "+rel+"\n")
	find("fixture-2.CATPart.md")
	if err := os.Remove(filepath.Join(dir, "fixture-1.CATPart.md")); err != nil {
		t.Fatal(err)
	}
	find("") // the naming rule would take fixture-1 now, so a later name is not ours to report
	write("fixture.CATPart.md", "arxgo: "+rel+"\n")
	find("fixture.CATPart.md")
}

func inspectSidecarBytes(t *testing.T, data []byte, rel string) (DescriptionOccupancy, error) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sidecar.text.md")
	if err := os.WriteFile(p, data, 0o644); err != nil {
		t.Fatal(err)
	}
	return InspectTextSidecar(p, rel)
}
