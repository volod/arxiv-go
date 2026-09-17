package report

import (
	"reflect"
	"strings"
	"testing"
)

func TestCatiaRegistryColumnsAndRoundTrip(t *testing.T) {
	want := "rel_path,file_name,status,url,description_rel_path,file_size,sha256,transfer,run_id,file_mime," +
		"text_rel_path,catia_kind,catia_format,catia_release,catia_components,mtime"
	if got := strings.Join(CatiaHeader, ","); got != want {
		t.Fatalf("header = %s", got)
	}
	rows := []CatiaRow{
		{PayloadRow: PayloadRow{RelPath: "cad/fixture-part.CATPart", FileName: "fixture-part.CATPart", Status: StatusMoved,
			URL: "file:///m/cad/fixture-part.CATPart", DescriptionRelPath: "cad/fixture-part.CATPart.md", FileSize: 4096,
			Transfer: "rename", RunID: "r1", FileMIME: "application/octet-stream"},
			Kind: "CATPart", Format: "V5_CFV2", Components: 0, MTime: "2026-09-15T12:00:00Z"},
		{PayloadRow: PayloadRow{RelPath: "cad/view.3dxml", FileName: "view.3dxml", Status: StatusConflict}},
	}
	var b strings.Builder
	if err := WriteCatiaCSV(&b, rows); err != nil {
		t.Fatal(err)
	}
	// text_rel_path and catia_release are empty in every row and still written; a zero component count is kept.
	wantCSV := want + "\n" +
		"cad/fixture-part.CATPart,fixture-part.CATPart,moved,file:///m/cad/fixture-part.CATPart,cad/fixture-part.CATPart.md,4096,,rename,r1,application/octet-stream,,CATPart,V5_CFV2,,0,2026-09-15T12:00:00Z\n" +
		"cad/view.3dxml,view.3dxml,conflict,,,0,,,,,,,,,,\n"
	if b.String() != wantCSV {
		t.Fatalf("csv:\n%s\nwant\n%s", b.String(), wantCSV)
	}
	got, err := LoadCatiaCSV(strings.NewReader(b.String()))
	if err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(got, rows) {
		t.Fatalf("round trip\n got %+v\nwant %+v", got, rows)
	}
	for _, bad := range []string{
		"rel_path,file_name\n",
		strings.Join(PayloadHeader, ",") + ",mtime,catia_kind\n",
		strings.Join(PayloadHeader, ",") + ",previews\n",
		strings.Join(PayloadHeader, ",") + ",catia_components\na,a,moved,,,1,,,,,x\n",
	} {
		if _, err := LoadCatiaCSV(strings.NewReader(bad)); err == nil {
			t.Errorf("accepted %q", bad)
		}
	}
}

func TestMergeCatiaRowsKeepsMovedAndSummary(t *testing.T) {
	moved := CatiaRow{PayloadRow: PayloadRow{RelPath: "b.CATPart", Status: StatusMoved, FileSize: 9, RunID: "r1"},
		Kind: "CATPart", Format: "V5_CFV2", Release: "V5R30", Components: 3}
	got := MergeCatiaRows([]CatiaRow{moved}, []CatiaRow{
		{PayloadRow: PayloadRow{RelPath: "b.CATPart", Status: StatusSkipped}},
		{PayloadRow: PayloadRow{RelPath: "a.cgr", Status: StatusConflict}},
	})
	if len(got) != 2 || got[0].RelPath != "a.cgr" || !reflect.DeepEqual(got[1], moved) {
		t.Fatalf("merge = %+v", got)
	}
	got = MarkCatiaRestored(MergeCatiaRows(got, []CatiaRow{{PayloadRow: PayloadRow{RelPath: "b.CATPart", Status: StatusMoved, RunID: "r2"}}}),
		map[string]struct{}{"b.CATPart": {}}, "r3")
	if b := got[1]; b.Status != StatusRestored || b.RunID != "r3" || b.Components != 3 || b.FileSize != 9 {
		t.Fatalf("restored row = %+v", b)
	}
}
