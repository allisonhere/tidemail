package plugin

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

// Report helpers answer report.run like a plugin would.
func init() {
	for name, h := range map[string]func(){
		"tidemail-plugin-report":      helperReport,
		"tidemail-plugin-report-loop": reportHelper(func(ReportRequest) any { return queriesOf(`{"v":{"method":"analytics.attention"}}`, nil) }),
		"tidemail-plugin-report-both": reportHelper(func(ReportRequest) any {
			return map[string]any{"queries": rawJSON(`{"v":{"method":"analytics.attention"}}`), "report": "x"}
		}),
		"tidemail-plugin-report-empty": reportHelper(func(ReportRequest) any { return map[string]any{} }),
		"tidemail-plugin-report-state": reportHelper(func(ReportRequest) any {
			return queriesOf(`{"v":{"method":"analytics.attention"}}`, strings.Repeat("x", MaxReportStateBytes))
		}),
		"tidemail-plugin-report-many": reportHelper(func(ReportRequest) any { return queriesOf(manyQueries(MaxReportQueriesPerRound+1), nil) }),
		"tidemail-plugin-report-ask":  helperReportAsk,
		"tidemail-plugin-report-invalid": reportHelper(func(ReportRequest) any {
			return queriesOf(`{"m":{"method":"query.messages","scope":"inbox","fields":["body"]}}`, nil)
		}),
	} {
		helpers[name] = h
	}
}

func rawJSON(s string) json.RawMessage { return json.RawMessage(s) }

func queriesOf(queries string, state any) map[string]any {
	out := map[string]any{"queries": rawJSON(queries)}
	if state != nil {
		out["state"] = state
	}
	return out
}

func manyQueries(n int) string {
	parts := make([]string, n)
	for i := range parts {
		parts[i] = fmt.Sprintf(`"q%d":{"method":"analytics.attention"}`, i)
	}
	return "{" + strings.Join(parts, ",") + "}"
}

func reportHelper(answer func(ReportRequest) any) func() {
	return func() {
		req := readRequest()
		var rr ReportRequest
		_ = json.Unmarshal(req.Data, &rr)
		respond(req, answer(rr))
	}
}

// helperReport asks two queries, then reports what came back, echoing the
// state and settings it was given.
func helperReport() {
	req := readRequest()
	var rr ReportRequest
	_ = json.Unmarshal(req.Data, &rr)
	if req.Method != MethodReportRun {
		respond(req, map[string]any{"report": "wrong method " + req.Method})
		return
	}
	if rr.Round == 1 {
		respond(req, queriesOf(`{"vol":{"method":"analytics.volume","range":"7d"},"att":{"method":"analytics.attention"}}`, map[string]any{"step": 1}))
		return
	}
	keys := make([]string, 0, len(rr.Results))
	for k := range rr.Results {
		keys = append(keys, k)
	}
	respond(req, map[string]any{"report": map[string]any{
		"round": rr.Round, "state": rr.State, "results": rr.Results, "keys": len(keys),
		"context": rr.Context, "settings": req.Settings,
	}})
}

// helperReportAsk asks for the query in query.json in its plugin directory
// (its working directory).
func helperReportAsk() {
	req := readRequest()
	var rr ReportRequest
	_ = json.Unmarshal(req.Data, &rr)
	if rr.Round > 1 {
		respond(req, map[string]any{"report": "done"})
		return
	}
	q, _ := os.ReadFile("query.json")
	respond(req, queriesOf(`{"q":`+string(q)+`}`, nil))
}

// fakeExec records queries and answers with canned values.
type fakeExec struct {
	mu     sync.Mutex
	calls  []Query
	answer func(Query) (any, error)
}

func (f *fakeExec) ExecuteQuery(_ context.Context, q Query, _ ReportContext) (any, error) {
	f.mu.Lock()
	f.calls = append(f.calls, q)
	f.mu.Unlock()
	if f.answer != nil {
		return f.answer(q)
	}
	return map[string]any{"method": q.Method}, nil
}

func reportManager(t *testing.T, command string, extraTOML ...string) *Manager {
	t.Helper()
	root := t.TempDir()
	extra := append([]string{`capabilities = ["report.run"]`}, extraTOML...)
	installPlugin(t, root, "p", "p", command, extra...)
	m, err := Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := m.Plugin("p"); !ok {
		t.Fatalf("plugin not discovered: %v", m.Errors())
	}
	return m
}

const analyticsPerm = "[permissions]\nanalytics_read = true"

var testRC = ReportContext{AccountName: "Work", MailboxName: "INBOX", Now: "2026-09-27T12:00:00Z", Timezone: "UTC"}

func TestReportRoundsEchoStateAndResults(t *testing.T) {
	m := reportManager(t, "tidemail-plugin-report", analyticsPerm)
	exec := &fakeExec{}
	res, err := m.Report(context.Background(), "p", testRC, exec)
	if err != nil {
		t.Fatal(err)
	}
	if res.Rounds != 2 || res.Queries != 2 || len(exec.calls) != 2 {
		t.Fatalf("result %+v, calls %d", res, len(exec.calls))
	}
	var report struct {
		Round   int                        `json:"round"`
		State   map[string]any             `json:"state"`
		Results map[string]json.RawMessage `json:"results"`
		Context ReportContext              `json:"context"`
	}
	if err := json.Unmarshal(res.Report, &report); err != nil {
		t.Fatal(err)
	}
	if report.Round != 2 || report.State["step"] != float64(1) || report.Context != testRC {
		t.Fatalf("report = %s", res.Report)
	}
	if string(report.Results["vol"]) != `{"method":"analytics.volume"}` || string(report.Results["att"]) != `{"method":"analytics.attention"}` {
		t.Fatalf("results = %v", report.Results)
	}
	// Queries are parsed and normalized before execution.
	for _, q := range exec.calls {
		if q.Method == AnalyticsVolume && (q.Range != Range7d || q.GroupBy != GroupDay) {
			t.Fatalf("volume query = %+v", q)
		}
	}
}

func TestReportRequiresCapability(t *testing.T) {
	m := managerWith(t, "tidemail-plugin-report")
	if _, err := m.Report(context.Background(), "p", testRC, &fakeExec{}); !errors.Is(err, ErrPermissionDenied) {
		t.Fatalf("report without capability: %v", err)
	}
	if _, err := m.Call(context.Background(), "p", MethodReportRun, nil); err == nil {
		t.Fatal("Call must not bypass the report driver")
	}
}

func TestReportPermissionsEnforcedBeforeExecution(t *testing.T) {
	cases := []struct {
		name, perms, query string
		ok                 bool
	}{
		{"undeclared messages", "", `{"method":"query.messages","scope":"inbox","fields":["id"]}`, false},
		{"declared messages", "[permissions]\nmessages_query = true", `{"method":"query.messages","scope":"inbox","fields":["id"]}`, true},
		{"analytics does not imply messages", analyticsPerm, `{"method":"query.messages","scope":"inbox","fields":["id"]}`, false},
		{"messages does not imply annotations", "[permissions]\nmessages_query = true", `{"method":"query.annotations","message_ids":[1]}`, false},
		{"messages does not imply analytics", "[permissions]\nmessages_query = true", `{"method":"analytics.volume"}`, false},
		{"threads", "[permissions]\nthreads_query = true", `{"method":"query.threads","scope":"inbox","fields":["thread_id"]}`, true},
		{"annotations", "[permissions]\nannotations_query = true", `{"method":"query.annotations","message_ids":[1]}`, true},
		{"classification needs analytics too", "[permissions]\nannotations_query = true", `{"method":"query.classification","message_ids":[1]}`, false},
		{"classification needs annotations too", analyticsPerm, `{"method":"query.classification","message_ids":[1]}`, false},
		{"classification", "[permissions]\nannotations_query = true\nanalytics_read = true", `{"method":"query.classification","message_ids":[1]}`, true},
		{"metadata grants nothing", "[permissions]\nmessage_metadata = true\nannotations = true", `{"method":"analytics.attention"}`, false},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			m := reportManager(t, "tidemail-plugin-report-ask", c.perms)
			p, _ := m.Plugin("p")
			if err := os.WriteFile(filepath.Join(p.Dir, "query.json"), []byte(c.query), 0o644); err != nil {
				t.Fatal(err)
			}
			exec := &fakeExec{}
			_, err := m.Report(context.Background(), "p", testRC, exec)
			if c.ok != (err == nil) {
				t.Fatalf("err = %v, want ok=%v", err, c.ok)
			}
			if !c.ok {
				if !errors.Is(err, ErrPermissionDenied) || len(exec.calls) != 0 {
					t.Fatalf("denied query must not run: err %v, calls %d", err, len(exec.calls))
				}
			}
		})
	}
}

func TestReportBounds(t *testing.T) {
	cases := map[string]string{
		"tidemail-plugin-report-loop":    fmt.Sprintf("within %d rounds", MaxReportRounds),
		"tidemail-plugin-report-both":    "not both",
		"tidemail-plugin-report-empty":   "needs queries or a report",
		"tidemail-plugin-report-state":   "state exceeds",
		"tidemail-plugin-report-many":    "at most 8 queries",
		"tidemail-plugin-report-invalid": `unknown field "body"`,
	}
	for command, want := range cases {
		t.Run(command, func(t *testing.T) {
			m := reportManager(t, command, "[permissions]\nanalytics_read = true\nmessages_query = true")
			exec := &fakeExec{}
			res, err := m.Report(context.Background(), "p", testRC, exec)
			if err == nil || !strings.Contains(err.Error(), want) {
				t.Fatalf("err = %v, want %q", err, want)
			}
			if command == "tidemail-plugin-report-loop" && res.Rounds != MaxReportRounds {
				t.Fatalf("rounds = %d", res.Rounds)
			}
			if command == "tidemail-plugin-report-invalid" && len(exec.calls) != 0 {
				t.Fatal("an invalid query must not run")
			}
		})
	}
}

func TestReportResultSizeCapped(t *testing.T) {
	m := reportManager(t, "tidemail-plugin-report", analyticsPerm)
	big := strings.Repeat("x", MaxReportResultsBytes/2+1)
	_, err := m.Report(context.Background(), "p", testRC, &fakeExec{answer: func(Query) (any, error) { return big, nil }})
	if err == nil || !strings.Contains(err.Error(), "exceed") {
		t.Fatalf("oversized results: %v", err)
	}
}

func TestReportExecutorErrorsNameTheQuery(t *testing.T) {
	m := reportManager(t, "tidemail-plugin-report", analyticsPerm)
	_, err := m.Report(context.Background(), "p", testRC, &fakeExec{answer: func(q Query) (any, error) {
		if q.Method == AnalyticsAttention {
			return nil, &QueryError{Code: QueryErrScopeUnavailable, Message: "nope"}
		}
		return 1, nil
	}})
	var qe *QueryError
	if !errors.As(err, &qe) || qe.Name != "att" || qe.Code != QueryErrScopeUnavailable {
		t.Fatalf("err = %v", err)
	}
}

func TestReportTimeoutPerRound(t *testing.T) {
	m := reportManager(t, "tidemail-plugin-hang")
	m.Timeout = 100 * time.Millisecond
	if _, err := m.Report(context.Background(), "p", testRC, &fakeExec{}); err == nil || !strings.Contains(err.Error(), "timed out") {
		t.Fatalf("hung report: %v", err)
	}
}
