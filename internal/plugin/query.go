package plugin

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Read-only queries a plugin can ask TideMail to run during a report (see
// report.go). A query is structured data validated against fixed allowlists;
// TideMail executes it and returns sanitized results. There is no SQL, no
// database path or handle, no message body, and no way to change mail.

// Query methods. Each needs its own permission (QueryPermission); none
// implies another.
const (
	QueryMessages       = "query.messages"
	QueryThreads        = "query.threads"
	QueryAnnotations    = "query.annotations"
	QueryClassification = "query.classification"

	AnalyticsVolume        = "analytics.volume"
	AnalyticsCategories    = "analytics.categories"
	AnalyticsAttention     = "analytics.attention"
	AnalyticsResponseTimes = "analytics.response_times"
	AnalyticsContacts      = "analytics.contacts"
)

// Query limits.
const (
	DefaultQueryLimit    = 100
	MaxQueryLimit        = 500
	MaxQueryMessageIDs   = 500
	MaxQueryFilterValues = 32
	DefaultContactsLimit = 10
	MaxContactsLimit     = 50
	MaxQueryCursorLen    = 512
	MaxVolumeBuckets     = 2000
)

// Scopes select which cached messages a message or thread query covers.
const (
	ScopeInbox          = "inbox"
	ScopeSent           = "sent"
	ScopeAllCached      = "all_cached"
	ScopeCurrentAccount = "current_account"
	ScopeCurrentMailbox = "current_mailbox"
)

// Sort orders.
const (
	SortDateDesc = "date_desc"
	SortDateAsc  = "date_asc"
)

// Analytics ranges and groupings.
const (
	Range7d   = "7d"
	Range30d  = "30d"
	Range90d  = "90d"
	Range365d = "365d"
	RangeAll  = "all"

	GroupDay   = "day"
	GroupWeek  = "week"
	GroupMonth = "month"
)

// Query error codes. TideMail shows them as "code: message".
const (
	QueryErrInvalid          = "invalid_query"
	QueryErrPermission       = "permission_denied"
	QueryErrScopeUnavailable = "scope_unavailable"
	QueryErrCursor           = "invalid_cursor"
	QueryErrFailed           = "query_failed"
)

// QueryError is a rejected or failed query.
type QueryError struct {
	Name    string // the plugin's name for the query, when known
	Code    string
	Message string
}

func (e *QueryError) Error() string {
	if e.Name == "" {
		return e.Code + ": " + e.Message
	}
	return fmt.Sprintf("query %q: %s: %s", e.Name, e.Code, e.Message)
}

// QueryMethods lists every query method in documentation order.
var QueryMethods = []string{
	QueryMessages, QueryThreads, QueryAnnotations, QueryClassification,
	AnalyticsVolume, AnalyticsCategories, AnalyticsAttention, AnalyticsResponseTimes, AnalyticsContacts,
}

// MessageQueryFields are the fields query.messages can return. They are the
// metadata TideMail already sends in message.metadata, never bodies.
var MessageQueryFields = []string{
	"id", "message_id", "sender", "recipients", "cc", "reply_to", "subject", "date",
	"read", "starred", "has_attachment", "account_name", "mailbox_name", "flags",
}

// ThreadQueryFields are the fields query.threads can return.
var ThreadQueryFields = []string{
	"thread_id", "participants", "message_count", "latest_sender", "latest_subject",
	"latest_date", "latest_from_me", "needs_you", "waiting_on_them",
}

var (
	queryScopes = set(ScopeInbox, ScopeSent, ScopeAllCached, ScopeCurrentAccount, ScopeCurrentMailbox)
	querySorts  = set(SortDateDesc, SortDateAsc)
	queryRanges = map[string]time.Duration{
		Range7d: 7 * 24 * time.Hour, Range30d: 30 * 24 * time.Hour, Range90d: 90 * 24 * time.Hour,
		Range365d: 365 * 24 * time.Hour, RangeAll: 0,
	}
	queryGroups        = set(GroupDay, GroupWeek, GroupMonth)
	messageFieldSet    = set(MessageQueryFields...)
	threadFieldSet     = set(ThreadQueryFields...)
	queryNamePattern   = regexp.MustCompile(`^[a-z0-9][a-z0-9_.-]{0,31}$`)
	queryCursorPattern = regexp.MustCompile(`^[A-Za-z0-9_-]*$`)
)

// queryParams lists the parameters each method accepts besides "method".
// Anything else is an error, so a typo never silently widens a query.
var queryParams = map[string]map[string]bool{
	QueryMessages:          set("scope", "fields", "limit", "cursor", "sort", "filters"),
	QueryThreads:           set("scope", "fields", "limit", "cursor", "filters"),
	QueryAnnotations:       set("message_ids", "plugin_ids", "keys"),
	QueryClassification:    set("message_ids"),
	AnalyticsVolume:        set("range", "from", "to", "group_by"),
	AnalyticsCategories:    set("range", "from", "to"),
	AnalyticsAttention:     set(),
	AnalyticsResponseTimes: set("range", "from", "to"),
	AnalyticsContacts:      set("range", "from", "to", "limit"),
}

// queryFilters lists the filters each method accepts.
var queryFilters = map[string]map[string]bool{
	QueryMessages: set("read", "starred", "has_attachment", "account", "mailbox", "date_from", "date_to"),
	QueryThreads:  set("account", "mailbox", "date_from", "date_to"),
}

// Query is one validated query. Only the fields its method accepts are set.
type Query struct {
	// Name is the plugin's label for the query; results come back under it.
	Name   string
	Method string

	Scope   string
	Fields  []string
	Limit   int
	Cursor  string
	Sort    string
	Filters QueryFilters

	MessageIDs []int64
	PluginIDs  []string
	Keys       []string

	Range   string
	From    time.Time // explicit from/to; zero when Range is used
	To      time.Time
	GroupBy string
}

// QueryFilters narrows a message or thread query. Nil and empty mean "any".
type QueryFilters struct {
	Read          *bool     `json:"read,omitempty"`
	Starred       *bool     `json:"starred,omitempty"`
	HasAttachment *bool     `json:"has_attachment,omitempty"`
	Account       string    `json:"account,omitempty"`
	Mailbox       string    `json:"mailbox,omitempty"`
	DateFrom      time.Time `json:"-"`
	DateTo        time.Time `json:"-"`
}

type rawQuery struct {
	Method     string          `json:"method"`
	Scope      string          `json:"scope"`
	Fields     []string        `json:"fields"`
	Limit      *int            `json:"limit"`
	Cursor     string          `json:"cursor"`
	Sort       string          `json:"sort"`
	Filters    json.RawMessage `json:"filters"`
	MessageIDs []int64         `json:"message_ids"`
	PluginIDs  []string        `json:"plugin_ids"`
	Keys       []string        `json:"keys"`
	Range      string          `json:"range"`
	From       string          `json:"from"`
	To         string          `json:"to"`
	GroupBy    string          `json:"group_by"`
}

type rawFilters struct {
	Read          *bool  `json:"read"`
	Starred       *bool  `json:"starred"`
	HasAttachment *bool  `json:"has_attachment"`
	Account       string `json:"account"`
	Mailbox       string `json:"mailbox"`
	DateFrom      string `json:"date_from"`
	DateTo        string `json:"date_to"`
}

// ParseQuery validates one query from a plugin's report response. It checks
// only the query's shape; QueryPermission decides whether the plugin may run
// it.
func ParseQuery(name string, raw json.RawMessage) (Query, error) {
	invalid := func(format string, args ...any) (Query, error) {
		return Query{}, &QueryError{Name: name, Code: QueryErrInvalid, Message: fmt.Sprintf(format, args...)}
	}
	if !queryNamePattern.MatchString(name) {
		return invalid("query names must be 1-32 characters of a-z, 0-9, '_', '.' or '-'")
	}
	var keys map[string]json.RawMessage
	if err := strictDecode(raw, &keys); err != nil || keys == nil {
		return invalid("a query must be a JSON object")
	}
	var method string
	_ = json.Unmarshal(keys["method"], &method)
	allowed, ok := queryParams[method]
	if !ok {
		return invalid("unknown method %q (supported: %s)", method, strings.Join(QueryMethods, ", "))
	}
	for _, key := range sortedKeys(keysOf(keys)) {
		if key != "method" && !allowed[key] {
			return invalid("%s does not accept %q", method, key)
		}
	}
	var r rawQuery
	if err := strictDecode(raw, &r); err != nil {
		return invalid("%s", err.Error())
	}

	q := Query{Name: name, Method: r.Method}
	var err error
	switch r.Method {
	case QueryMessages, QueryThreads:
		err = q.parseListing(r)
	case QueryAnnotations, QueryClassification:
		err = q.parseIDs(r)
	default:
		err = q.parseAnalytics(r)
	}
	if err != nil {
		return invalid("%s", err.Error())
	}
	return q, nil
}

func (q *Query) parseListing(r rawQuery) error {
	q.Scope = r.Scope
	if !queryScopes[q.Scope] {
		return fmt.Errorf("scope %q is not one of %s", r.Scope, strings.Join(sortedKeys(queryScopes), ", "))
	}
	fieldSet, all := messageFieldSet, MessageQueryFields
	if q.Method == QueryThreads {
		fieldSet, all = threadFieldSet, ThreadQueryFields
	}
	if len(r.Fields) == 0 {
		return errors.New("fields is required; list only the fields you need")
	}
	seen := map[string]bool{}
	for _, f := range r.Fields {
		switch {
		case !fieldSet[f]:
			return fmt.Errorf("unknown field %q (supported: %s)", f, strings.Join(all, ", "))
		case seen[f]:
			return fmt.Errorf("field %q is listed twice", f)
		}
		seen[f] = true
	}
	q.Fields = r.Fields
	q.Limit = DefaultQueryLimit
	if r.Limit != nil {
		if *r.Limit < 1 || *r.Limit > MaxQueryLimit {
			return fmt.Errorf("limit must be between 1 and %d", MaxQueryLimit)
		}
		q.Limit = *r.Limit
	}
	if len(r.Cursor) > MaxQueryCursorLen || !queryCursorPattern.MatchString(r.Cursor) {
		return errors.New("cursor must be a next_cursor value from an earlier result")
	}
	q.Cursor = r.Cursor
	q.Sort = SortDateDesc
	if q.Method == QueryMessages && r.Sort != "" {
		if !querySorts[r.Sort] {
			return fmt.Errorf("sort %q is not one of %s, %s", r.Sort, SortDateDesc, SortDateAsc)
		}
		q.Sort = r.Sort
	}
	return q.parseFilters(r.Filters)
}

func (q *Query) parseFilters(raw json.RawMessage) error {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return nil
	}
	var keys map[string]json.RawMessage
	if err := strictDecode(raw, &keys); err != nil || keys == nil {
		return errors.New("filters must be a JSON object")
	}
	allowed := queryFilters[q.Method]
	for _, key := range sortedKeys(keysOf(keys)) {
		if !allowed[key] {
			return fmt.Errorf("%s does not accept filter %q (supported: %s)", q.Method, key, strings.Join(sortedKeys(allowed), ", "))
		}
	}
	var f rawFilters
	if err := strictDecode(raw, &f); err != nil {
		return fmt.Errorf("filters: %w", err)
	}
	if err := checkText("filters.account", f.Account); err != nil {
		return err
	}
	if err := checkText("filters.mailbox", f.Mailbox); err != nil {
		return err
	}
	q.Filters = QueryFilters{Read: f.Read, Starred: f.Starred, HasAttachment: f.HasAttachment, Account: f.Account, Mailbox: f.Mailbox}
	var err error
	if q.Filters.DateFrom, err = parseQueryTime("filters.date_from", f.DateFrom); err != nil {
		return err
	}
	if q.Filters.DateTo, err = parseQueryTime("filters.date_to", f.DateTo); err != nil {
		return err
	}
	if !q.Filters.DateFrom.IsZero() && !q.Filters.DateTo.IsZero() && !q.Filters.DateFrom.Before(q.Filters.DateTo) {
		return errors.New("filters.date_from must be before filters.date_to")
	}
	return nil
}

func (q *Query) parseIDs(r rawQuery) error {
	if len(r.MessageIDs) == 0 || len(r.MessageIDs) > MaxQueryMessageIDs {
		return fmt.Errorf("message_ids must list 1 to %d message ids", MaxQueryMessageIDs)
	}
	seen := map[int64]bool{}
	for _, id := range r.MessageIDs {
		if id <= 0 {
			return fmt.Errorf("message id %d is not valid", id)
		}
		if !seen[id] {
			seen[id] = true
			q.MessageIDs = append(q.MessageIDs, id)
		}
	}
	if len(r.PluginIDs) > MaxQueryFilterValues || len(r.Keys) > MaxQueryFilterValues {
		return fmt.Errorf("plugin_ids and keys may list at most %d values", MaxQueryFilterValues)
	}
	for _, id := range r.PluginIDs {
		if err := ValidateID(id); err != nil {
			return err
		}
	}
	for _, k := range r.Keys {
		if len(k) > MaxAnnotationKeyLen || !annotationKeyPattern.MatchString(k) {
			return fmt.Errorf("key %q is not a valid annotation key", k)
		}
	}
	q.PluginIDs, q.Keys = r.PluginIDs, r.Keys
	return nil
}

func (q *Query) parseAnalytics(r rawQuery) error {
	if r.Range != "" && (r.From != "" || r.To != "") {
		return errors.New("use either range or from/to, not both")
	}
	switch {
	case r.From != "" || r.To != "":
		var err error
		if q.From, err = parseQueryTime("from", r.From); err != nil {
			return err
		}
		if q.To, err = parseQueryTime("to", r.To); err != nil {
			return err
		}
		if q.From.IsZero() || q.To.IsZero() || !q.From.Before(q.To) {
			return errors.New("from and to are both required, and from must be before to")
		}
	case q.Method != AnalyticsAttention:
		q.Range = Range30d
		if r.Range != "" {
			if _, ok := queryRanges[r.Range]; !ok {
				return fmt.Errorf("range %q is not one of %s", r.Range, strings.Join([]string{Range7d, Range30d, Range90d, Range365d, RangeAll}, ", "))
			}
			q.Range = r.Range
		}
	}
	switch q.Method {
	case AnalyticsVolume:
		q.GroupBy = GroupDay
		if r.GroupBy != "" {
			if !queryGroups[r.GroupBy] {
				return fmt.Errorf("group_by %q is not one of %s, %s, %s", r.GroupBy, GroupDay, GroupWeek, GroupMonth)
			}
			q.GroupBy = r.GroupBy
		}
	case AnalyticsContacts:
		q.Limit = DefaultContactsLimit
		if r.Limit != nil {
			if *r.Limit < 1 || *r.Limit > MaxContactsLimit {
				return fmt.Errorf("limit must be between 1 and %d", MaxContactsLimit)
			}
			q.Limit = *r.Limit
		}
	}
	return nil
}

// Window resolves an analytics query's time range against now: messages
// dated in [from, to). A zero from means no lower bound ("all").
func (q Query) Window(now time.Time) (from, to time.Time) {
	if !q.From.IsZero() {
		return q.From, q.To
	}
	to = now.Add(time.Nanosecond) // include messages dated exactly now
	if d := queryRanges[q.Range]; d > 0 {
		from = now.Add(-d)
	}
	return from, to
}

// QueryPermission reports whether perms allow method, and if not, which
// permission is missing.
func QueryPermission(perms Permissions, method string) (ok bool, missing string) {
	switch method {
	case QueryMessages:
		return perms.MessagesQuery, "messages_query"
	case QueryThreads:
		return perms.ThreadsQuery, "threads_query"
	case QueryAnnotations:
		return perms.AnnotationsQuery, "annotations_query"
	case QueryClassification:
		if !perms.AnnotationsQuery {
			return false, "annotations_query"
		}
		return perms.AnalyticsRead, "analytics_read"
	case AnalyticsVolume, AnalyticsCategories, AnalyticsAttention, AnalyticsResponseTimes, AnalyticsContacts:
		return perms.AnalyticsRead, "analytics_read"
	}
	return false, "an unknown method"
}

// QueryExecutor runs validated, permission-checked queries. TideMail's
// implementation lives outside this package (internal/pluginquery) so the
// plugin runtime never touches the database. The result is encoded as JSON
// for the plugin; a *QueryError is reported to the user as is.
type QueryExecutor interface {
	ExecuteQuery(ctx context.Context, q Query, rc ReportContext) (any, error)
}

func parseQueryTime(field, s string) (time.Time, error) {
	if s == "" {
		return time.Time{}, nil
	}
	t, err := time.Parse(time.RFC3339, s)
	if err != nil {
		return time.Time{}, fmt.Errorf("%s must be an RFC 3339 time", field)
	}
	return t, nil
}

func checkText(field, s string) error {
	if len(s) > 256 || hasHiddenRunes(s) {
		return fmt.Errorf("%s must be at most 256 bytes of plain text", field)
	}
	return nil
}

// strictDecode decodes exactly one JSON value, rejecting unknown fields.
func strictDecode(raw json.RawMessage, v any) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(v); err != nil {
		return err
	}
	if dec.More() {
		return errors.New("unexpected data after the value")
	}
	return nil
}

func set(values ...string) map[string]bool {
	m := make(map[string]bool, len(values))
	for _, v := range values {
		m[v] = true
	}
	return m
}

func keysOf(m map[string]json.RawMessage) map[string]bool {
	out := make(map[string]bool, len(m))
	for k := range m {
		out[k] = true
	}
	return out
}

func sortedKeys(m map[string]bool) []string {
	keys := make([]string, 0, len(m))
	for k := range m {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}
