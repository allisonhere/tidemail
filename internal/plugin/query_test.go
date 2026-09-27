package plugin

import (
	"encoding/json"
	"errors"
	"strings"
	"testing"
	"time"
)

func TestParseManifestQueryPermissions(t *testing.T) {
	m, err := ParseManifest([]byte(`id = "a"
name = "A"
api = 1
command = "run"
capabilities = ["report.run"]

[permissions]
messages_query = true
threads_query = true
annotations_query = true
analytics_read = true
`))
	if err != nil {
		t.Fatal(err)
	}
	p := m.Permissions
	if !p.MessagesQuery || !p.ThreadsQuery || !p.AnnotationsQuery || !p.AnalyticsRead || p.MessageMetadata || p.Annotations {
		t.Fatalf("permissions = %+v", p)
	}
	if !m.HasCapability(CapabilityReport) {
		t.Fatal("report.run capability not parsed")
	}
	if got := strings.Join(p.Names(), ", "); got != "messages query, threads query, annotations query, analytics" {
		t.Fatalf("names = %q", got)
	}
}

func TestQueryPermissionsAreIndependent(t *testing.T) {
	for _, method := range QueryMethods {
		if ok, _ := QueryPermission(Permissions{}, method); ok {
			t.Fatalf("%s allowed with no permissions", method)
		}
		all := Permissions{MessagesQuery: true, ThreadsQuery: true, AnnotationsQuery: true, AnalyticsRead: true}
		if ok, _ := QueryPermission(all, method); !ok {
			t.Fatalf("%s denied with every permission", method)
		}
	}
	if ok, _ := QueryPermission(Permissions{AnalyticsRead: true}, QueryMessages); ok {
		t.Fatal("analytics_read must not grant query.messages")
	}
	if ok, _ := QueryPermission(Permissions{MessagesQuery: true}, QueryAnnotations); ok {
		t.Fatal("messages_query must not grant query.annotations")
	}
	if ok, missing := QueryPermission(Permissions{AnnotationsQuery: true}, QueryClassification); ok || missing != "analytics_read" {
		t.Fatal("query.classification needs analytics_read as well")
	}
}

func parse(t *testing.T, raw string) (Query, error) {
	t.Helper()
	return ParseQuery("q", json.RawMessage(raw))
}

func TestParseQueryDefaultsAndLimits(t *testing.T) {
	q, err := parse(t, `{"method":"query.messages","scope":"inbox","fields":["sender","subject"]}`)
	if err != nil {
		t.Fatal(err)
	}
	if q.Limit != DefaultQueryLimit || q.Sort != SortDateDesc || len(q.Fields) != 2 {
		t.Fatalf("defaults = %+v", q)
	}
	if q, _ := parse(t, `{"method":"query.messages","scope":"sent","fields":["id"],"limit":500,"sort":"date_asc"}`); q.Limit != 500 || q.Sort != SortDateAsc {
		t.Fatalf("explicit = %+v", q)
	}
	if q, _ := parse(t, `{"method":"analytics.volume"}`); q.Range != Range30d || q.GroupBy != GroupDay {
		t.Fatalf("volume defaults = %+v", q)
	}
	if q, _ := parse(t, `{"method":"analytics.contacts"}`); q.Limit != DefaultContactsLimit {
		t.Fatalf("contacts defaults = %+v", q)
	}
	q, err = parse(t, `{"method":"query.messages","scope":"inbox","fields":["id"],"filters":{"read":false,"starred":true,"has_attachment":true,"account":"Work","mailbox":"INBOX","date_from":"2026-01-01T00:00:00Z","date_to":"2026-02-01T00:00:00Z"}}`)
	if err != nil || *q.Filters.Read || !*q.Filters.Starred || !*q.Filters.HasAttachment || q.Filters.Account != "Work" || q.Filters.DateTo.Month() != time.February {
		t.Fatalf("filters = %+v, %v", q.Filters, err)
	}
}

func TestParseQueryRejects(t *testing.T) {
	cases := map[string]string{
		`{"method":"query.sql","sql":"SELECT 1"}`: "unknown method",
		`[]`: "JSON object",
		`{"method":"query.messages","scope":"everything","fields":["id"]}`:                                "scope",
		`{"method":"query.messages","scope":"inbox"}`:                                                     "fields is required",
		`{"method":"query.messages","scope":"inbox","fields":["body"]}`:                                   `unknown field "body"`,
		`{"method":"query.messages","scope":"inbox","fields":["html"]}`:                                   `unknown field "html"`,
		`{"method":"query.messages","scope":"inbox","fields":["id","id"]}`:                                "twice",
		`{"method":"query.messages","scope":"inbox","fields":["id"],"limit":1000000}`:                     "limit",
		`{"method":"query.messages","scope":"inbox","fields":["id"],"limit":0}`:                           "limit",
		`{"method":"query.messages","scope":"inbox","fields":["id"],"sort":"subject"}`:                    "sort",
		`{"method":"query.messages","scope":"inbox","fields":["id"],"order_by":"x"}`:                      `does not accept "order_by"`,
		`{"method":"query.messages","scope":"inbox","fields":["id"],"filters":{"subject":"x"}}`:           `filter "subject"`,
		`{"method":"query.messages","scope":"inbox","fields":["id"],"filters":{"date_from":"yesterday"}}`: "RFC 3339",
		`{"method":"query.messages","scope":"inbox","fields":["id"],"cursor":"a b"}`:                      "cursor",
		`{"method":"query.threads","scope":"inbox","fields":["subject"]}`:                                 `unknown field "subject"`,
		`{"method":"query.threads","scope":"inbox","fields":["thread_id"],"filters":{"read":true}}`:       `filter "read"`,
		`{"method":"query.annotations"}`:                                                                  "message_ids",
		`{"method":"query.annotations","message_ids":[0]}`:                                                "not valid",
		`{"method":"query.annotations","message_ids":[1],"keys":["Bad Key"]}`:                             "annotation key",
		`{"method":"query.annotations","message_ids":[1],"plugin_ids":["../x"]}`:                          "plugin id",
		`{"method":"analytics.volume","range":"forever"}`:                                                 "range",
		`{"method":"analytics.volume","range":"last week"}`:                                               "range",
		`{"method":"analytics.volume","range":"7d","from":"2026-01-01T00:00:00Z"}`:                        "either range",
		`{"method":"analytics.volume","from":"2026-02-01T00:00:00Z","to":"2026-01-01T00:00:00Z"}`:         "before",
		`{"method":"analytics.volume","group_by":"hour"}`:                                                 "group_by",
		`{"method":"analytics.volume","fields":["id"]}`:                                                   `does not accept "fields"`,
		`{"method":"analytics.attention","range":"7d"}`:                                                   `does not accept "range"`,
		`{"method":"analytics.contacts","limit":51}`:                                                      "limit",
	}
	for raw, want := range cases {
		_, err := parse(t, raw)
		var qe *QueryError
		if err == nil || !errors.As(err, &qe) || qe.Code != QueryErrInvalid || !strings.Contains(err.Error(), want) {
			t.Errorf("%s: err = %v, want %q", raw, err, want)
		}
	}
	if _, err := ParseQuery("Bad Name", json.RawMessage(`{"method":"analytics.attention"}`)); err == nil {
		t.Error("query names are validated")
	}
}

func TestQueryWindow(t *testing.T) {
	now := time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)
	q, _ := parse(t, `{"method":"analytics.volume","range":"7d"}`)
	from, to := q.Window(now)
	if !from.Equal(now.AddDate(0, 0, -7)) || !to.After(now) {
		t.Fatalf("7d window = %v..%v", from, to)
	}
	q, _ = parse(t, `{"method":"analytics.volume","range":"all"}`)
	if from, _ := q.Window(now); !from.IsZero() {
		t.Fatalf("all window starts at %v", from)
	}
}
