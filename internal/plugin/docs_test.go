package plugin

// Drift tests: the Plugin API v1 docs (docs/plugins/) and the example plugin
// (examples/plugins/example/) are checked against this package, so the
// documentation cannot silently fall behind the implementation. Checked
// blocks are the fenced code blocks right after a <!-- drift:NAME --> marker.

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
	"testing"
)

const docsDir = "../../docs/plugins"

func readDoc(t *testing.T, name string) string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(docsDir, name))
	if err != nil {
		t.Fatal(err)
	}
	return string(data)
}

var driftBlock = regexp.MustCompile("(?s)<!-- drift:([a-z-]+) -->\\s*```[a-z]*\\n(.*?)```")

// driftBlocks returns the marked blocks named name in doc, in order.
func driftBlocks(t *testing.T, doc, name string) []string {
	t.Helper()
	var out []string
	for _, m := range driftBlock.FindAllStringSubmatch(doc, -1) {
		if m[1] == name {
			out = append(out, m[2])
		}
	}
	if len(out) == 0 {
		t.Fatalf("no <!-- drift:%s --> block", name)
	}
	return out
}

// lines returns the non-empty trimmed lines of a block.
func lines(block string) []string {
	var out []string
	for _, l := range strings.Split(block, "\n") {
		if l = strings.TrimSpace(l); l != "" {
			out = append(out, l)
		}
	}
	return out
}

func TestDocsDocumentedAPIVersion(t *testing.T) {
	if APIVersion != 1 {
		t.Fatalf("APIVersion is %d: docs/plugins/ describe API v1; write the new version's docs", APIVersion)
	}
	for _, name := range []string{"README.md", "compatibility.md", "protocol.md"} {
		if !strings.Contains(readDoc(t, name), "API v1") {
			t.Errorf("%s does not name API v1", name)
		}
	}
}

func TestDocsManifestExampleParses(t *testing.T) {
	for _, block := range driftBlocks(t, readDoc(t, "manifest.md"), "manifest") {
		m, err := ParseManifest([]byte(block))
		if err != nil {
			t.Fatalf("documented manifest: %v", err)
		}
		if m.API != APIVersion {
			t.Fatalf("documented api = %d", m.API)
		}
		// Every documented permission is a real one.
		if !m.Permissions.MessageMetadata || !m.Permissions.Annotations {
			t.Fatal("the documented manifest should show the common permissions")
		}
	}
}

func TestDocsEventsAndCapabilities(t *testing.T) {
	events := lines(driftBlocks(t, readDoc(t, "manifest.md"), "events")[0])
	if want := sortedKeys(knownEvents); !slices.Equal(events, want) {
		t.Fatalf("documented events %v, code supports %v", events, want)
	}
	caps := lines(driftBlocks(t, readDoc(t, "manifest.md"), "capabilities")[0])
	if want := sortedKeys(knownCapabilities); !slices.Equal(caps, want) {
		t.Fatalf("documented capabilities %v, code supports %v", caps, want)
	}
	if !strings.Contains(readDoc(t, "events.md"), EventMessageReceived) {
		t.Fatal("events.md does not name the event")
	}
}

func TestDocsMethods(t *testing.T) {
	got := lines(driftBlocks(t, readDoc(t, "protocol.md"), "methods")[0])
	want := []string{MethodPing, MethodMessageMetadata, EventMessageReceived, MethodTest, MethodReportRun}
	if !slices.Equal(got, want) {
		t.Fatalf("documented methods %v, want %v", got, want)
	}
}

func TestDocsSettingTypes(t *testing.T) {
	got := lines(driftBlocks(t, readDoc(t, "settings.md"), "setting-types")[0])
	want := []string{SettingBool, SettingSelect, SettingSecret}
	if !slices.Equal(got, want) {
		t.Fatalf("documented setting types %v, want %v", got, want)
	}
	// Each type is accepted by validation, and nothing else is.
	for _, typ := range got {
		spec := SettingSpec{Key: "k", Label: "L", Type: typ}
		if typ == SettingSelect {
			spec.Options = []string{"a"}
		}
		if errs := validateSettings([]SettingSpec{spec}); len(errs) > 0 {
			t.Errorf("documented type %q rejected: %v", typ, errs)
		}
	}
	doc := readDoc(t, "settings.md")
	for _, n := range []int{MaxSettings, MaxSettingLabelLen, MaxSettingHelpLen, MaxSelectOptions, MaxSelectOptionLen} {
		if !strings.Contains(doc, "**"+strconv.Itoa(n)+"**") {
			t.Errorf("settings.md does not state the limit %d", n)
		}
	}
}

func TestDocsAnnotationLimits(t *testing.T) {
	want := map[string]int{
		"max_annotations": MaxAnnotations,
		"max_key_length":  MaxAnnotationKeyLen,
		"max_value_bytes": MaxAnnotationValueLen,
	}
	got := map[string]int{}
	for _, l := range lines(driftBlocks(t, readDoc(t, "annotations.md"), "limits")[0]) {
		k, v, ok := strings.Cut(l, "=")
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if !ok || err != nil {
			t.Fatalf("bad limit line %q", l)
		}
		got[strings.TrimSpace(k)] = n
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("documented %s = %d, code has %d", k, got[k], v)
		}
	}
	if len(got) != len(want) {
		t.Errorf("documented limits %v, want %v", got, want)
	}
	if !strings.Contains(readDoc(t, "annotations.md"), "`"+annotationKeyPattern.String()+"`") {
		t.Error("annotations.md does not quote the key pattern")
	}
}

// decodeStrict unmarshals v, refusing fields the struct doesn't have.
func decodeStrict(t *testing.T, block string, v any) {
	t.Helper()
	dec := json.NewDecoder(bytes.NewReader([]byte(block)))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		t.Fatalf("documented JSON does not match %T: %v\n%s", v, err, block)
	}
}

func TestDocsProtocolExamples(t *testing.T) {
	doc := readDoc(t, "protocol.md")
	var req Request
	decodeStrict(t, driftBlocks(t, doc, "request")[0], &req)
	if err := req.validate(); err != nil {
		t.Fatalf("documented request: %v", err)
	}
	var meta MessageMetadata
	decodeStrict(t, string(req.Data), &meta)

	responses := driftBlocks(t, doc, "response")
	if len(responses) != 2 {
		t.Fatalf("want a success and an error example, got %d", len(responses))
	}
	for i, block := range responses {
		var resp Response
		decodeStrict(t, block, &resp)
		// The documented response must pass the real response checks.
		got, err := decodeResponse([]byte(block), Request{RequestID: req.RequestID})
		if err != nil {
			t.Fatalf("documented response %d rejected: %v", i, err)
		}
		if i == 0 {
			if _, err := ParseAnnotations(got.Data); err != nil || !got.OK {
				t.Fatalf("documented success response: ok=%v, %v", got.OK, err)
			}
		} else if got.OK || got.Error == nil {
			t.Fatal("the second example should be an error response")
		}
	}

	var full MessageMetadata
	decodeStrict(t, driftBlocks(t, doc, "metadata")[0], &full)
	// Every metadata field is shown in the example, so none is undocumented.
	encoded, _ := json.Marshal(full)
	var fields map[string]any
	_ = json.Unmarshal(encoded, &fields)
	for _, name := range []string{"id", "message_id", "from", "to", "cc", "reply_to", "subject", "date", "read", "starred", "has_attachment", "flags", "account_name", "mailbox_name"} {
		if _, ok := fields[name]; !ok {
			t.Errorf("metadata example lacks %q", name)
		}
	}
	if len(fields) != 14 {
		t.Errorf("MessageMetadata has %d fields; document new ones", len(fields))
	}
}

func TestExamplePluginManifestParses(t *testing.T) {
	m, err := LoadManifest("../../examples/plugins/example/plugin.toml")
	if err != nil {
		t.Fatal(err)
	}
	if m.ID != "example" || m.API != APIVersion || !m.Permissions.MessageMetadata || !m.Permissions.Annotations {
		t.Fatalf("example manifest = %+v", m)
	}
	if _, err := os.Stat(filepath.Join("../../examples/plugins/example", "main.go")); err != nil {
		t.Fatal("example source missing")
	}
}

// The view reference lists exactly the implemented blocks, tones, and limits.
func TestDocsViews(t *testing.T) {
	doc := readDoc(t, "views.md")
	if got := lines(driftBlocks(t, doc, "block-types")[0]); !slices.Equal(got, BlockTypes) {
		t.Fatalf("documented blocks %v, want %v", got, BlockTypes)
	}
	if got := lines(driftBlocks(t, doc, "tones")[0]); !slices.Equal(got, Tones) {
		t.Fatalf("documented tones %v, want %v", got, Tones)
	}
	want := map[string]int{
		"max_blocks": MaxViewBlocks, "max_title": MaxViewTitleLen, "max_label": MaxViewLabelLen,
		"max_value": MaxViewValueLen, "max_stats": MaxViewStats, "max_series": MaxViewSeries,
		"max_points": MaxViewPoints, "max_bars": MaxViewBars, "max_heat_rows": MaxViewHeatRows,
		"max_heat_columns": MaxViewHeatColumns, "max_table_columns": MaxViewTableCols,
		"max_table_rows": MaxViewTableRows, "max_cell": MaxViewCellLen, "max_text_bytes": MaxViewTextLen,
	}
	got := map[string]int{}
	for _, l := range lines(driftBlocks(t, doc, "view-limits")[0]) {
		k, v, _ := strings.Cut(l, "=")
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if err != nil {
			t.Fatalf("bad limit line %q", l)
		}
		got[strings.TrimSpace(k)] = n
	}
	if len(got) != len(want) {
		t.Fatalf("documented view limits %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("documented %s = %d, code has %d", k, got[k], v)
		}
	}
	// The documented example is a valid view.
	if _, err := ParseView(json.RawMessage(`{"title": "Mail Analytics", "subtitle": "last 30 days", "blocks": [
  {"type": "stats", "items": [{"label": "Received", "value": "428"}, {"label": "Needs you", "value": "12", "tone": "attention"}]},
  {"type": "sparkline", "title": "Volume per day", "series": [{"label": "in", "values": [12, 18, 9, 22]}], "start": "Sep 24", "end": "Sep 27"}
]}`)); err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(doc, `"view": {"title": "Mail Analytics"`) {
		t.Fatal("views.md example changed; update the copy in this test")
	}
}

// The query reference lists exactly the implemented methods, fields, scopes,
// and limits.
func TestDocsQueries(t *testing.T) {
	doc := readDoc(t, "queries.md")
	if got := lines(driftBlocks(t, doc, "query-methods")[0]); !slices.Equal(got, QueryMethods) {
		t.Fatalf("documented query methods %v, want %v", got, QueryMethods)
	}
	if got := lines(driftBlocks(t, doc, "message-fields")[0]); !slices.Equal(got, MessageQueryFields) {
		t.Fatalf("documented message fields %v, want %v", got, MessageQueryFields)
	}
	if got := lines(driftBlocks(t, doc, "thread-fields")[0]); !slices.Equal(got, ThreadQueryFields) {
		t.Fatalf("documented thread fields %v, want %v", got, ThreadQueryFields)
	}
	if got, want := lines(driftBlocks(t, doc, "scopes")[0]), sortedKeys(queryScopes); !slices.Equal(got, want) {
		t.Fatalf("documented scopes %v, want %v", got, want)
	}
	want := map[string]int{
		"default_limit":      DefaultQueryLimit,
		"max_limit":          MaxQueryLimit,
		"max_message_ids":    MaxQueryMessageIDs,
		"max_contacts":       MaxContactsLimit,
		"max_rounds":         MaxReportRounds,
		"max_queries":        MaxReportQueriesPerRound,
		"max_state_bytes":    MaxReportStateBytes,
		"max_results_bytes":  MaxReportResultsBytes,
		"report_timeout_s":   int(ReportTimeout.Seconds()),
		"max_volume_buckets": MaxVolumeBuckets,
	}
	got := map[string]int{}
	for _, l := range lines(driftBlocks(t, doc, "query-limits")[0]) {
		k, v, ok := strings.Cut(l, "=")
		n, err := strconv.Atoi(strings.TrimSpace(v))
		if !ok || err != nil {
			t.Fatalf("bad limit line %q", l)
		}
		got[strings.TrimSpace(k)] = n
	}
	if len(got) != len(want) {
		t.Fatalf("documented limits %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Fatalf("documented %s = %d, code has %d", k, got[k], v)
		}
	}
	for _, method := range QueryMethods {
		if strings.HasPrefix(method, "analytics.") && !strings.Contains(readDoc(t, "analytics.md"), method) {
			t.Fatalf("analytics.md does not document %s", method)
		}
	}
}
