package pluginquery

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

func TestMain(m *testing.M) {
	os.Setenv("TIDEMAIL_DISABLE_KEYRING", "1")
	os.Exit(m.Run())
}

var testNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

type harness struct {
	t    *testing.T
	db   *db.DB
	exec *Executor
	rc   plugin.ReportContext
}

func newHarness(t *testing.T, f Fixture) *harness {
	t.Helper()
	database, err := db.OpenPath(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	if err := f.Load(database); err != nil {
		t.Fatal(err)
	}
	return &harness{t: t, db: database,
		exec: &Executor{DB: database, Me: conversation.MyAddresses(f.Me...), Location: time.UTC},
		rc:   plugin.ReportContext{AccountName: f.Accounts[0].Name, MailboxName: "INBOX", Now: testNow.Format(time.RFC3339), Timezone: "UTC"},
	}
}

func defaultHarness(t *testing.T) *harness { return newHarness(t, DefaultFixture(testNow)) }

// run parses raw like a plugin's query, then executes it.
func (h *harness) run(raw string) (any, error) {
	h.t.Helper()
	q, err := plugin.ParseQuery("q", json.RawMessage(raw))
	if err != nil {
		h.t.Fatalf("ParseQuery(%s): %v", raw, err)
	}
	return h.exec.ExecuteQuery(context.Background(), q, h.rc)
}

// must runs raw and decodes its JSON result into out; it returns the JSON.
func (h *harness) must(raw string, out any) string {
	h.t.Helper()
	v, err := h.run(raw)
	if err != nil {
		h.t.Fatalf("%s: %v", raw, err)
	}
	data, err := json.Marshal(v)
	if err != nil {
		h.t.Fatal(err)
	}
	if out != nil {
		if err := json.Unmarshal(data, out); err != nil {
			h.t.Fatal(err)
		}
	}
	return string(data)
}

func queryErrCode(err error) string {
	var qe *plugin.QueryError
	if errors.As(err, &qe) {
		return qe.Code
	}
	return ""
}

type msgPage struct {
	Messages   []map[string]any `json:"messages"`
	NextCursor string           `json:"next_cursor"`
}

func subjects(p msgPage) []string {
	var out []string
	for _, m := range p.Messages {
		out = append(out, m["subject"].(string))
	}
	return out
}

func TestMessagesScopesAndFieldSelection(t *testing.T) {
	h := defaultHarness(t)
	var inbox msgPage
	h.must(`{"method":"query.messages","scope":"inbox","fields":["subject","date"]}`, &inbox)
	if len(inbox.Messages) != 8 || inbox.NextCursor != "" {
		t.Fatalf("inbox = %d messages, cursor %q", len(inbox.Messages), inbox.NextCursor)
	}
	for _, m := range inbox.Messages {
		if len(m) != 2 || m["subject"] == nil || m["date"] == nil {
			t.Fatalf("row has fields beyond those requested: %v", m)
		}
	}
	if got := subjects(inbox); got[0] != "CI failed on main" || got[7] != "Last week" {
		t.Fatalf("not newest first: %v", got)
	}
	var sent msgPage
	h.must(`{"method":"query.messages","scope":"sent","fields":["subject"]}`, &sent)
	if len(sent.Messages) != 2 {
		t.Fatalf("sent = %v", subjects(sent))
	}
	var all msgPage
	h.must(`{"method":"query.messages","scope":"all_cached","fields":["id"]}`, &all)
	if len(all.Messages) != 11 {
		t.Fatalf("all_cached = %d", len(all.Messages))
	}
	var current msgPage
	h.must(`{"method":"query.messages","scope":"current_mailbox","fields":["mailbox_name","account_name"]}`, &current)
	if len(current.Messages) != 8 || current.Messages[0]["mailbox_name"] != "INBOX" || current.Messages[0]["account_name"] != "Personal" {
		t.Fatalf("current_mailbox = %v", current.Messages)
	}
	h.rc = plugin.ReportContext{Now: h.rc.Now}
	if _, err := h.run(`{"method":"query.messages","scope":"current_account","fields":["id"]}`); queryErrCode(err) != plugin.QueryErrScopeUnavailable {
		t.Fatalf("current_account without context: %v", err)
	}
}

func TestMessagesPaginationAndSort(t *testing.T) {
	h := defaultHarness(t)
	for _, sort := range []string{"date_desc", "date_asc"} {
		var got []string
		cursor := ""
		pages := 0
		for {
			raw := `{"method":"query.messages","scope":"inbox","fields":["subject","date"],"limit":3,"sort":"` + sort + `","cursor":"` + cursor + `"}`
			var p msgPage
			h.must(raw, &p)
			pages++
			if len(p.Messages) > 3 {
				t.Fatalf("page larger than limit: %d", len(p.Messages))
			}
			got = append(got, subjects(p)...)
			if p.NextCursor == "" {
				break
			}
			cursor = p.NextCursor
		}
		if pages != 3 || len(got) != 8 {
			t.Fatalf("%s: %d pages, %d messages", sort, pages, len(got))
		}
		seen := map[string]bool{}
		for _, s := range got {
			if seen[s] {
				t.Fatalf("%s: %q repeated across pages", sort, s)
			}
			seen[s] = true
		}
		first := "CI failed on main"
		if sort == "date_asc" {
			first = "Last week"
		}
		if got[0] != first {
			t.Fatalf("%s starts with %q", sort, got[0])
		}
	}
}

func TestMessagesCursorBoundToQuery(t *testing.T) {
	h := defaultHarness(t)
	var p msgPage
	h.must(`{"method":"query.messages","scope":"inbox","fields":["id"],"limit":2}`, &p)
	if _, err := h.run(`{"method":"query.messages","scope":"all_cached","fields":["id"],"limit":2,"cursor":"` + p.NextCursor + `"}`); queryErrCode(err) != plugin.QueryErrCursor {
		t.Fatalf("cursor replayed on another scope: %v", err)
	}
	if _, err := h.run(`{"method":"query.messages","scope":"inbox","fields":["id"],"cursor":"AAAA"}`); queryErrCode(err) != plugin.QueryErrCursor {
		t.Fatalf("forged cursor: %v", err)
	}
}

func TestMessagesFilters(t *testing.T) {
	h := defaultHarness(t)
	cases := map[string]int{
		`{"read":false}`:                       5, // 4 inbox + 1 trash
		`{"read":true}`:                        6, // 4 inbox + 2 sent
		`{"starred":true}`:                     1,
		`{"has_attachment":true}`:              1,
		`{"date_from":"2026-09-22T00:00:00Z"}`: 5, // 4 inbox + 1 sent
		`{"date_from":"2026-09-20T00:00:00Z","date_to":"2026-09-25T00:00:00Z"}`: 5, // 3 inbox, 1 sent, 1 trash
		`{"account":"personal","mailbox":"inbox"}`:                              8,
		`{"account":"Nobody"}`:                                                  0,
	}
	for filters, want := range cases {
		var p msgPage
		h.must(`{"method":"query.messages","scope":"all_cached","fields":["id"],"filters":`+filters+`}`, &p)
		if len(p.Messages) != want {
			t.Errorf("filters %s: %d messages, want %d", filters, len(p.Messages), want)
		}
	}
}

func TestMessagesNeverExposeBodiesOrSecrets(t *testing.T) {
	h := defaultHarness(t)
	var mb int64
	if err := h.db.QueryRow(`SELECT id FROM mailboxes WHERE name = 'INBOX'`).Scan(&mb); err != nil {
		t.Fatal(err)
	}
	if err := h.db.UpsertMessage(db.Message{MailboxID: mb, UID: 99, Subject: "has body", Date: testNow,
		BodyText: "BODY-TEXT-SECRET", BodyHTML: "<p>BODY-HTML-SECRET</p>", Summary: "SUMMARY-SECRET", Headers: "X-Secret: HEADER-SECRET"}); err != nil {
		t.Fatal(err)
	}
	all := `["` + strings.Join(plugin.MessageQueryFields, `","`) + `"]`
	out := h.must(`{"method":"query.messages","scope":"all_cached","fields":`+all+`,"limit":500}`, nil)
	threads := h.must(`{"method":"query.threads","scope":"all_cached","fields":["`+strings.Join(plugin.ThreadQueryFields, `","`)+`"]}`, nil)
	for _, secret := range []string{"BODY-TEXT-SECRET", "BODY-HTML-SECRET", "SUMMARY-SECRET", "HEADER-SECRET"} {
		if strings.Contains(out, secret) || strings.Contains(threads, secret) {
			t.Fatalf("%s leaked to a plugin", secret)
		}
	}
	if !strings.Contains(out, "has body") {
		t.Fatal("the message itself should be listed")
	}
}

func TestMessagesSanitizeHeaders(t *testing.T) {
	f := DefaultFixture(testNow)
	f.Accounts[0].Mailboxes[0].Messages = append(f.Accounts[0].Mailboxes[0].Messages, FixtureMessage{
		From: "\"Evil\x1b[31m\u202e\" <e@x.example>", Subject: "Hi\x1b]8;;http://x\x07there\u200b", Date: testNow.Format(time.RFC3339),
	})
	h := newHarness(t, f)
	var p msgPage
	h.must(`{"method":"query.messages","scope":"inbox","fields":["sender","subject"],"limit":1}`, &p)
	for _, v := range p.Messages[0] {
		s := v.(string)
		if strings.ContainsAny(s, "\x1b\x07\u202e\u200b") {
			t.Fatalf("unsanitized value %q", s)
		}
	}
}

type threadPage struct {
	Threads    []map[string]any `json:"threads"`
	NextCursor string           `json:"next_cursor"`
}

func TestThreadsReuseThreadingAndState(t *testing.T) {
	h := defaultHarness(t)
	var p threadPage
	h.must(`{"method":"query.threads","scope":"all_cached","fields":["latest_subject","message_count","latest_sender","latest_from_me","needs_you","waiting_on_them","participants","thread_id","latest_date"]}`, &p)
	bySubject := map[string]map[string]any{}
	for _, th := range p.Threads {
		bySubject[th["latest_subject"].(string)] = th
	}
	plan := bySubject["Re: Quarterly plan"]
	if plan == nil || plan["message_count"].(float64) != 3 || plan["latest_from_me"] != false || plan["needs_you"] != true {
		t.Fatalf("quarterly plan thread = %v", plan)
	}
	if !strings.Contains(plan["latest_sender"].(string), "ann@ann.example") || plan["latest_date"] != "2026-09-17T16:00:00Z" {
		t.Fatalf("latest = %v", plan)
	}
	parts, _ := json.Marshal(plan["participants"])
	if !strings.Contains(string(parts), `"address":"ann@ann.example"`) || !strings.Contains(string(parts), `"address":"me@example.com"`) {
		t.Fatalf("participants = %s", parts)
	}
	contract := bySubject["Contract draft"]
	if contract == nil || contract["waiting_on_them"] != true || contract["latest_from_me"] != true {
		t.Fatalf("contract thread = %v", contract)
	}
	if bySubject["Dinner?"]["needs_you"] != false {
		t.Fatal("a dismissed message is not in Needs You")
	}
	if len(p.Threads) != 9 {
		t.Fatalf("threads = %d", len(p.Threads))
	}
}

func TestThreadsSameSubjectStaySeparateAndPagesAreBounded(t *testing.T) {
	f := Fixture{Me: []string{"me@example.com"}, Accounts: []FixtureAccount{{Name: "A", Mailboxes: []FixtureMailbox{{Name: "INBOX"}}}}}
	for i := 0; i < 7; i++ {
		f.Accounts[0].Mailboxes[0].Messages = append(f.Accounts[0].Mailboxes[0].Messages, FixtureMessage{
			MessageID: "<h" + string(rune('a'+i)) + "@x>", From: "x@x.example", Subject: "Hello",
			Date: testNow.Add(-time.Duration(i) * time.Hour).Format(time.RFC3339),
		})
	}
	h := newHarness(t, f)
	ids := map[string]bool{}
	cursor := ""
	for pages := 0; ; pages++ {
		var p threadPage
		h.must(`{"method":"query.threads","scope":"inbox","fields":["thread_id"],"limit":3,"cursor":"`+cursor+`"}`, &p)
		if len(p.Threads) > 3 || pages > 3 {
			t.Fatal("unbounded thread page")
		}
		for _, th := range p.Threads {
			ids[th["thread_id"].(string)] = true
		}
		if cursor = p.NextCursor; cursor == "" {
			break
		}
	}
	if len(ids) != 7 {
		t.Fatalf("same-subject unrelated messages formed %d threads, want 7", len(ids))
	}
}

func TestAnnotationsAreRawPluginJudgments(t *testing.T) {
	h := defaultHarness(t)
	var ids []int64
	var p msgPage
	h.must(`{"method":"query.messages","scope":"inbox","fields":["id","subject"]}`, &p)
	byID := map[string]int64{}
	for _, m := range p.Messages {
		id := int64(m["id"].(float64))
		ids = append(ids, id)
		byID[m["subject"].(string)] = id
	}
	idsJSON, _ := json.Marshal(append(ids, 999999))
	var anns struct {
		Annotations []AnnotationRow `json:"annotations"`
	}
	h.must(`{"method":"query.annotations","message_ids":`+string(idsJSON)+`,"keys":["category"]}`, &anns)
	cats := map[int64]string{}
	for _, a := range anns.Annotations {
		if a.Key != "category" || a.PluginID != "fixture" {
			t.Fatalf("filter ignored: %+v", a)
		}
		cats[a.MessageID] = a.Value
	}
	if _, ok := cats[byID["Last week"]]; ok {
		t.Fatal("a user correction must not appear as a plugin annotation")
	}
	if cats[byID["CI failed on main"]] != "github" || len(cats) != 6 {
		t.Fatalf("categories = %v", cats)
	}
	var none struct {
		Annotations []AnnotationRow `json:"annotations"`
	}
	h.must(`{"method":"query.annotations","message_ids":`+string(idsJSON)+`,"plugin_ids":["other"]}`, &none)
	if len(none.Annotations) != 0 {
		t.Fatalf("plugin filter ignored: %v", none.Annotations)
	}

	var cls struct {
		Classifications []ClassificationRow `json:"classifications"`
	}
	h.must(`{"method":"query.classification","message_ids":[`+itoa(byID["Last week"])+`,`+itoa(byID["Quarterly plan"])+`,999999]}`, &cls)
	if len(cls.Classifications) != 2 {
		t.Fatalf("unknown ids should be skipped: %+v", cls)
	}
	last, plan := cls.Classifications[0], cls.Classifications[1]
	if last.Category.Value != "personal" || last.Category.Source != "user" {
		t.Fatalf("override provenance = %+v", last.Category)
	}
	if plan.Category.Value != "work" || plan.Category.Source != "plugin" || plan.NeedsReply.Value != true || plan.Urgent.Source != "none" {
		t.Fatalf("plugin provenance = %+v", plan)
	}
}

func itoa(n int64) string { b, _ := json.Marshal(n); return string(b) }

func TestVolume(t *testing.T) {
	h := defaultHarness(t)
	var v VolumeResult
	out := h.must(`{"method":"analytics.volume","range":"30d","group_by":"day"}`, &v)
	if v.Received != 8 || v.Sent != 2 || len(v.Buckets) != 31 {
		t.Fatalf("30d day volume: received %d sent %d buckets %d", v.Received, v.Sent, len(v.Buckets))
	}
	if b := v.Buckets[len(v.Buckets)-2]; b.Start != "2026-09-26" || b.Received != 1 {
		t.Fatalf("yesterday bucket = %+v", b)
	}
	if strings.Contains(out, "Quarterly") || strings.Contains(out, "ann@") {
		t.Fatal("volume must not contain subjects or addresses")
	}
	var week, month VolumeResult
	h.must(`{"method":"analytics.volume","range":"30d","group_by":"week"}`, &week)
	h.must(`{"method":"analytics.volume","range":"30d","group_by":"month"}`, &month)
	if week.Received != 8 || month.Received != 8 || len(month.Buckets) != 2 || month.Buckets[0].Start != "2026-08-01" {
		t.Fatalf("week %+v month %+v", week, month)
	}
	for _, b := range week.Buckets {
		if d, _ := time.Parse(time.DateOnly, b.Start); d.Weekday() != time.Monday {
			t.Fatalf("week bucket %s is not a Monday", b.Start)
		}
	}
	var recent VolumeResult
	h.must(`{"method":"analytics.volume","range":"7d"}`, &recent)
	if recent.Received != 4 || recent.Sent != 1 {
		t.Fatalf("7d volume: received %d sent %d", recent.Received, recent.Sent)
	}
	var explicit VolumeResult
	h.must(`{"method":"analytics.volume","from":"2026-09-15T00:00:00Z","to":"2026-09-18T00:00:00Z"}`, &explicit)
	if explicit.Received != 2 || explicit.Sent != 1 || len(explicit.Buckets) != 3 {
		t.Fatalf("explicit window: %+v", explicit)
	}
}

func TestVolumeCountsCopiesOnceAndUsesSender(t *testing.T) {
	f := DefaultFixture(testNow)
	// A Gmail-style All Mail folder holding copies of inbox and sent mail.
	var copies []FixtureMessage
	copies = append(copies, f.Accounts[0].Mailboxes[0].Messages...)
	copies = append(copies, f.Accounts[0].Mailboxes[1].Messages...)
	f.Accounts[0].Mailboxes = append(f.Accounts[0].Mailboxes, FixtureMailbox{Name: "[Gmail]/All Mail", Flags: []string{`\All`}, Messages: copies})
	h := newHarness(t, f)
	var v VolumeResult
	h.must(`{"method":"analytics.volume","range":"30d"}`, &v)
	if v.Received != 8 || v.Sent != 2 {
		t.Fatalf("copies double-counted: received %d sent %d", v.Received, v.Sent)
	}
}

func TestCategories(t *testing.T) {
	h := defaultHarness(t)
	var c CategoriesResult
	out := h.must(`{"method":"analytics.categories","range":"30d"}`, &c)
	got := map[string]int{}
	for _, cc := range c.Categories {
		got[cc.Category] = cc.Count
	}
	want := map[string]int{"work": 2, "github": 2, "newsletter": 1, "personal": 1, "billing": 1, "none": 1}
	if c.Total != 8 || len(got) != len(want) {
		t.Fatalf("categories = %v (total %d)", got, c.Total)
	}
	for k, n := range want {
		if got[k] != n {
			t.Fatalf("categories = %v, want %v", got, want)
		}
	}
	if strings.Contains(out, "Quarterly") {
		t.Fatal("categories must not contain subjects")
	}

	// A removed plugin's annotations are cleaned up; the user's correction
	// still decides.
	rows, _ := h.db.ListHeaders(db.HeaderQuery{})
	for _, m := range rows {
		if m.Subject == "Last week" {
			if err := h.db.SetClassificationOverride(m.ID, "category", "family"); err != nil {
				t.Fatal(err)
			}
		}
	}
	if err := h.db.DeletePluginAnnotationsForPlugin("fixture"); err != nil {
		t.Fatal(err)
	}
	h.must(`{"method":"analytics.categories","range":"30d"}`, &c)
	if len(c.Categories) != 2 || c.Categories[0].Category != "none" || c.Categories[0].Count != 7 || c.Categories[1].Category != "family" {
		t.Fatalf("after plugin removal = %+v", c.Categories)
	}
}

func TestAttention(t *testing.T) {
	h := defaultHarness(t)
	var a AttentionResult
	out := h.must(`{"method":"analytics.attention"}`, &a)
	want := AttentionResult{NeedsYou: 3, NeedsYouDismissed: 1, Waiting: 1, SnoozedMessages: 1, Corrections: 1}
	if a != want {
		t.Fatalf("attention = %+v, want %+v", a, want)
	}
	var generic map[string]any
	_ = json.Unmarshal([]byte(out), &generic)
	for _, v := range generic {
		if _, ok := v.(float64); !ok {
			t.Fatalf("attention returned a non-count: %s", out)
		}
	}
}

func TestResponseTimes(t *testing.T) {
	h := defaultHarness(t)
	var r ResponseTimesResult
	h.must(`{"method":"analytics.response_times","range":"30d"}`, &r)
	// Ann 12d 09:00 -> me 11d 10:00 (25h); me -> Ann 10d 16:00 (30h).
	if r.UserSamples != 1 || r.MedianUserResponse != 25*3600 || r.OtherSamples != 1 || r.MedianOther != 30*3600 {
		t.Fatalf("response times = %+v", r)
	}
	var none ResponseTimesResult
	h.must(`{"method":"analytics.response_times","range":"7d"}`, &none)
	if none.UserSamples != 0 || none.OtherSamples != 0 {
		t.Fatalf("7d should have no replies: %+v", none)
	}
}

func TestResponseTimesMultiMessageAndDirection(t *testing.T) {
	at := func(h int) string { return testNow.Add(-time.Duration(48-h) * time.Hour).Format(time.RFC3339) }
	f := Fixture{Me: []string{"me@example.com"}, Accounts: []FixtureAccount{{Name: "A", Mailboxes: []FixtureMailbox{
		{Name: "INBOX", Messages: []FixtureMessage{
			{MessageID: "<1@x>", From: "a@a.example", To: "me@example.com", Date: at(0)},
			{MessageID: "<2@x>", References: "<1@x>", From: "a@a.example", To: "me@example.com", Date: at(2)},
			{MessageID: "<4@x>", References: "<1@x> <3@x>", From: "a@a.example", To: "me@example.com", Date: at(10)},
			// No Message-ID and no references: its own thread, no sample.
			{From: "b@b.example", To: "me@example.com", Date: at(1)},
		}},
		{Name: "Sent", Flags: []string{`\Sent`}, Messages: []FixtureMessage{
			{MessageID: "<3@x>", References: "<1@x> <2@x>", From: "me@example.com", To: "a@a.example", Date: at(6)},
			{From: "me@example.com", To: "b@b.example", Date: at(3)},
		}},
	}}}}
	h := newHarness(t, f)
	var r ResponseTimesResult
	h.must(`{"method":"analytics.response_times","range":"7d"}`, &r)
	// The wait is measured from the first unanswered message (0h -> 6h), then
	// the other side answers 4h later.
	if r.UserSamples != 1 || r.MedianUserResponse != 6*3600 || r.OtherSamples != 1 || r.MedianOther != 4*3600 {
		t.Fatalf("response times = %+v", r)
	}
	h.exec.Me = nil
	h.must(`{"method":"analytics.response_times","range":"7d"}`, &r)
	if r != (ResponseTimesResult{}) {
		t.Fatalf("without the user's addresses there is no direction: %+v", r)
	}
}

func TestContacts(t *testing.T) {
	h := defaultHarness(t)
	var c ContactsResult
	h.must(`{"method":"analytics.contacts","range":"30d","limit":3}`, &c)
	if len(c.Correspondents) != 3 || len(c.Domains) != 3 {
		t.Fatalf("limit ignored: %+v", c)
	}
	top := c.Correspondents[0]
	if top.Address != "ann@ann.example" || top.Name != "Ann Lee" || top.Received != 2 || top.Sent != 1 || top.Total != 3 {
		t.Fatalf("top correspondent = %+v", top)
	}
	if c.Domains[0].Domain != "ann.example" || c.Domains[0].Total != 3 {
		t.Fatalf("top domain = %+v", c.Domains[0])
	}
	for _, p := range c.Correspondents {
		if p.Address == "me@example.com" {
			t.Fatal("the user is not their own correspondent")
		}
	}
}

func TestDefaultFixtureDocumentedCounts(t *testing.T) {
	// The CLI and docs rely on the default fixture loading cleanly.
	h := defaultHarness(t)
	rows, err := h.db.ListHeaders(db.HeaderQuery{})
	if err != nil || len(rows) != 11 {
		t.Fatalf("default fixture rows = %d, %v", len(rows), err)
	}
	if !slices.ContainsFunc(rows, func(m db.Message) bool { return m.Subject == "Deleted" }) {
		t.Fatal("trash message missing")
	}
}
