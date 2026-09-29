package plugin

import (
	"context"
	"encoding/json"
	"strings"
	"testing"
)

const testViewJSON = `{"title":"Mail","subtitle":"last 30 days","blocks":[
 {"type":"stats","items":[{"label":"Received","value":"428"},{"label":"Needs you","value":"12","tone":"attention"}]},
 {"type":"sparkline","title":"Volume","note":"peak 31","series":[{"label":"in","values":[1,2,3]},{"label":"out","values":[0,1,0]}],"start":"Aug 28","end":"Sep 27"},
 {"type":"bars","title":"Categories","kind":"category","items":[{"label":"github","count":298,"note":"30%"}]},
 {"type":"heatmap","title":"Rhythm","columns":["Mon","Tue"],"rows":[{"label":"wk 1","values":[3,0]}]},
 {"type":"table","title":"Busiest","columns":["Msgs","Subject"],"cells":[["3","Plan"]]},
 {"type":"text","text":"line one\nline two"}
]}`

func TestParseViewAcceptsEveryBlock(t *testing.T) {
	v, err := ParseView(json.RawMessage(testViewJSON))
	if err != nil {
		t.Fatal(err)
	}
	if len(v.Blocks) != len(BlockTypes) || v.Blocks[1].Series[1].Label != "out" || v.Blocks[2].Items[0].Count != 298 {
		t.Fatalf("view = %+v", v)
	}
}

func TestParseViewRejects(t *testing.T) {
	cases := map[string]string{
		`{"title":"x","blocks":[]}`:                                                                   "1 to 32 blocks",
		`{"blocks":[{"type":"text","text":"a"}]}`:                                                     "title is required",
		`{"title":"x","blocks":[{"type":"chart3d"}]}`:                                                 "unknown block type",
		`{"title":"x","blocks":[{"type":"bars","color":"red","items":[]}]}`:                           `do not accept "color"`,
		`{"title":"x","style":"bold","blocks":[{"type":"text","text":"a"}]}`:                          "unknown field",
		`{"title":"x","blocks":[{"type":"stats","items":[{"label":"a","value":"1","tone":"red"}]}]}`:  "tone",
		`{"title":"x","blocks":[{"type":"stats","items":[{"label":"a\u001b[31m","value":"1"}]}]}`:     "plain text",
		`{"title":"x","blocks":[{"type":"text","text":"\u001b]8;;http://x\u0007hi"}]}`:                "plain text",
		`{"title":"x","blocks":[{"type":"sparkline","series":[{"label":"a","values":[-1]}]}]}`:        "not negative",
		`{"title":"x","blocks":[{"type":"table","columns":["a","b"],"cells":[["1"]]}]}`:               "2 cells",
		`{"title":"x","blocks":[{"type":"bars","kind":"rainbow","items":[{"label":"a","count":1}]}]}`: "kind",
	}
	for raw, want := range cases {
		if _, err := ParseView(json.RawMessage(raw)); err == nil || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
}

func TestReportReturnsView(t *testing.T) {
	m := reportManager(t, "tidemail-plugin-report-view")
	res, err := m.Report(context.Background(), "p", testRC, &fakeExec{})
	if err != nil {
		t.Fatal(err)
	}
	if res.View == nil || res.Report != nil || res.View.Title != "Mail" {
		t.Fatalf("result = %+v", res)
	}
	m = reportManager(t, "tidemail-plugin-report-badview")
	if _, err := m.Report(context.Background(), "p", testRC, &fakeExec{}); err == nil || !strings.Contains(err.Error(), `"color"`) {
		t.Fatalf("styled view accepted: %v", err)
	}
}
