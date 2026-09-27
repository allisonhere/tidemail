// Package pluginquery executes plugin report queries (plugin.Query) against
// TideMail's cache. It is the only code that turns a plugin's structured,
// already validated and permission-checked request into database reads.
//
// Guarantees: header columns only (never bodies, HTML, raw headers,
// summaries, or attachments); no writes; bounded pages; every string handed
// back is stripped of control and format characters.
package pluginquery

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"strings"
	"time"
	"unicode"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// Executor runs plugin queries. It implements plugin.QueryExecutor.
type Executor struct {
	DB *db.DB
	// Me are the user's own lowercase addresses (conversation.MyAddresses),
	// used for sent/received direction, Waiting on Them, and response times.
	Me map[string]bool
	// Location buckets analytics by local day/week/month. Nil uses the
	// report context's timezone, then time.Local.
	Location *time.Location
}

var _ plugin.QueryExecutor = (*Executor)(nil)

// ExecuteQuery implements plugin.QueryExecutor.
func (e *Executor) ExecuteQuery(ctx context.Context, q plugin.Query, rc plugin.ReportContext) (any, error) {
	if e == nil || e.DB == nil {
		return nil, fmt.Errorf("no mail cache")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	env := e.env(rc)
	switch q.Method {
	case plugin.QueryMessages:
		return e.messages(q, rc, env)
	case plugin.QueryThreads:
		return e.threads(q, rc, env)
	case plugin.QueryAnnotations:
		return e.annotations(q)
	case plugin.QueryClassification:
		return e.classification(q)
	case plugin.AnalyticsVolume:
		return e.volume(q, env)
	case plugin.AnalyticsCategories:
		return e.categories(q, env)
	case plugin.AnalyticsAttention:
		return e.attention(env)
	case plugin.AnalyticsResponseTimes:
		return e.responseTimes(q, env)
	case plugin.AnalyticsContacts:
		return e.contacts(q, env)
	}
	return nil, &plugin.QueryError{Code: plugin.QueryErrInvalid, Message: "unknown method " + q.Method}
}

// env is one query's view of time.
type env struct {
	now time.Time
	loc *time.Location
}

func (e *Executor) env(rc plugin.ReportContext) env {
	loc := e.Location
	if loc == nil {
		if l, err := time.LoadLocation(rc.Timezone); err == nil && rc.Timezone != "" {
			loc = l
		} else {
			loc = time.Local
		}
	}
	now := time.Now()
	if t, err := time.Parse(time.RFC3339, rc.Now); err == nil {
		now = t
	}
	return env{now: now.In(loc), loc: loc}
}

// mailboxSet is the mailboxes a query covers, with names for output.
type mailboxSet struct {
	ids  []int64 // non-nil, possibly empty
	info map[int64]db.MailboxInfo
}

// scopeMailboxes resolves a listing query's scope and account/mailbox filters.
func (e *Executor) scopeMailboxes(q plugin.Query, rc plugin.ReportContext) (mailboxSet, error) {
	unavailable := func(what string) (mailboxSet, error) {
		return mailboxSet{}, &plugin.QueryError{Code: plugin.QueryErrScopeUnavailable, Message: q.Scope + " needs " + what + ", and the report was not started from one"}
	}
	switch q.Scope {
	case plugin.ScopeCurrentAccount:
		if rc.AccountName == "" {
			return unavailable("a current account")
		}
	case plugin.ScopeCurrentMailbox:
		if rc.AccountName == "" || rc.MailboxName == "" {
			return unavailable("a current folder")
		}
	}
	all, err := e.DB.ListAllMailboxes()
	if err != nil {
		return mailboxSet{}, err
	}
	set := mailboxSet{ids: []int64{}, info: map[int64]db.MailboxInfo{}}
	for _, mb := range all {
		var in bool
		switch q.Scope {
		case plugin.ScopeInbox:
			in = db.IsInboxMailbox(mb.Mailbox)
		case plugin.ScopeSent:
			in = db.IsSentMailbox(mb.Mailbox)
		case plugin.ScopeAllCached:
			in = true
		case plugin.ScopeCurrentAccount:
			in = strings.EqualFold(mb.AccountName, rc.AccountName)
		case plugin.ScopeCurrentMailbox:
			in = strings.EqualFold(mb.AccountName, rc.AccountName) && mailboxNamed(mb, rc.MailboxName)
		}
		if f := q.Filters.Account; in && f != "" {
			in = strings.EqualFold(mb.AccountName, f)
		}
		if f := q.Filters.Mailbox; in && f != "" {
			in = mailboxNamed(mb, f)
		}
		if in {
			set.ids = append(set.ids, mb.ID)
			set.info[mb.ID] = mb
		}
	}
	return set, nil
}

func mailboxNamed(mb db.MailboxInfo, name string) bool {
	return strings.EqualFold(mb.Name, name) || strings.EqualFold(mb.DisplayName, name)
}

// analyticsMailboxes are the folders analytics count: everything except
// Trash, Junk, and Drafts, as for Waiting on Them.
func (e *Executor) analyticsMailboxes() (mailboxSet, error) {
	all, err := e.DB.ListAllMailboxes()
	if err != nil {
		return mailboxSet{}, err
	}
	set := mailboxSet{ids: []int64{}, info: map[int64]db.MailboxInfo{}}
	for _, mb := range all {
		if !db.ExcludedFromWaiting(mb.Mailbox) {
			set.ids = append(set.ids, mb.ID)
			set.info[mb.ID] = mb
		}
	}
	return set, nil
}

// cursor is an opaque keyset position. Hash binds it to the query that
// produced it, so a cursor cannot be replayed against a different scope,
// filter, or order.
type cursor struct {
	Date int64  `json:"d"`
	ID   int64  `json:"i"`
	Hash string `json:"h"`
}

func queryHash(q plugin.Query, rc plugin.ReportContext) string {
	f := q.Filters
	key := fmt.Sprintf("%s|%s|%s|%v|%v|%v|%q|%q|%d|%d|%q|%q",
		q.Method, q.Scope, q.Sort, boolKey(f.Read), boolKey(f.Starred), boolKey(f.HasAttachment),
		f.Account, f.Mailbox, f.DateFrom.Unix(), f.DateTo.Unix(), rc.AccountName, rc.MailboxName)
	sum := sha256.Sum256([]byte(key))
	return hex.EncodeToString(sum[:8])
}

func boolKey(b *bool) string {
	if b == nil {
		return "-"
	}
	return fmt.Sprint(*b)
}

func encodeCursor(date, id int64, hash string) string {
	raw, _ := json.Marshal(cursor{Date: date, ID: id, Hash: hash})
	return base64.RawURLEncoding.EncodeToString(raw)
}

func decodeCursor(s, hash string) (cursor, error) {
	bad := &plugin.QueryError{Code: plugin.QueryErrCursor, Message: "cursor does not belong to this query; start again without a cursor"}
	raw, err := base64.RawURLEncoding.DecodeString(s)
	if err != nil {
		return cursor{}, bad
	}
	var c cursor
	if err := json.Unmarshal(raw, &c); err != nil || c.Hash != hash || c.ID <= 0 {
		return cursor{}, bad
	}
	return c, nil
}

// clean strips control, format, and separator characters from text headed
// to a plugin, and trims it.
func clean(s string) string {
	s = strings.ToValidUTF8(s, "")
	s = strings.Map(func(r rune) rune {
		switch {
		case r == '\t' || r == '\n' || r == '\r':
			return ' '
		case unicode.In(r, unicode.Cc, unicode.Cf, unicode.Zl, unicode.Zp):
			return -1
		}
		return r
	}, s)
	return strings.TrimSpace(s)
}

func cleanList(in []string) []string {
	out := make([]string, 0, len(in))
	for _, s := range in {
		if s = clean(s); s != "" {
			out = append(out, s)
		}
	}
	return out
}

// dedupe keeps one row per message across folder copies (db.MessageKey),
// the first seen, and reports each key's rows.
func dedupe(rows []db.Message) (unique []db.Message, copies map[string][]db.Message) {
	copies = map[string][]db.Message{}
	for _, m := range rows {
		key := db.MessageKey(m)
		if _, seen := copies[key]; !seen {
			unique = append(unique, m)
		}
		copies[key] = append(copies[key], m)
	}
	return unique, copies
}

func rfc3339(t time.Time, loc *time.Location) string {
	if t.IsZero() {
		return ""
	}
	return t.In(loc).Format(time.RFC3339)
}
