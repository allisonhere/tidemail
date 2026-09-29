package pluginquery

import (
	"crypto/sha256"
	"encoding/hex"
	"sort"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// MessagesResult is query.messages' result.
type MessagesResult struct {
	Messages   []map[string]any `json:"messages"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

func (e *Executor) messages(q plugin.Query, rc plugin.ReportContext, env env) (MessagesResult, error) {
	boxes, err := e.scopeMailboxes(q, rc)
	if err != nil {
		return MessagesResult{}, err
	}
	hash := queryHash(q, rc)
	hq := db.HeaderQuery{
		MailboxIDs: boxes.ids, Read: q.Filters.Read, Starred: q.Filters.Starred, HasAttachment: q.Filters.HasAttachment,
		DateFrom: q.Filters.DateFrom, DateTo: q.Filters.DateTo, Ascending: q.Sort == plugin.SortDateAsc,
		Limit: q.Limit + 1, // one extra row says whether there is a next page
	}
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor, hash)
		if err != nil {
			return MessagesResult{}, err
		}
		hq.AfterDate, hq.AfterID = c.Date, c.ID
	}
	rows, err := e.DB.ListHeaders(hq)
	if err != nil {
		return MessagesResult{}, err
	}
	out := MessagesResult{Messages: []map[string]any{}}
	if len(rows) > q.Limit {
		rows = rows[:q.Limit]
		last := rows[len(rows)-1]
		out.NextCursor = encodeCursor(last.Date.Unix(), last.ID, hash)
	}
	for _, m := range rows {
		mb := boxes.info[m.MailboxID]
		row := make(map[string]any, len(q.Fields))
		for _, f := range q.Fields {
			row[f] = messageField(f, m, mb, env)
		}
		out.Messages = append(out.Messages, row)
	}
	return out, nil
}

// messageField maps an allowlisted field name to its value. Keep it in step
// with plugin.MessageQueryFields.
func messageField(field string, m db.Message, mb db.MailboxInfo, env env) any {
	switch field {
	case "id":
		return m.ID
	case "message_id":
		return clean(m.MessageID)
	case "sender":
		return clean(m.From)
	case "recipients":
		return clean(m.To)
	case "cc":
		return clean(m.CC)
	case "reply_to":
		return clean(m.ReplyTo)
	case "subject":
		return clean(m.Subject)
	case "date":
		return rfc3339(m.Date, env.loc)
	case "read":
		return m.Read
	case "starred":
		return m.Starred
	case "has_attachment":
		return m.HasAttachment
	case "account_name":
		return clean(mb.AccountName)
	case "mailbox_name":
		return clean(mb.Name)
	case "flags":
		return cleanList(m.Flags)
	}
	return nil
}

// ThreadsResult is query.threads' result.
type ThreadsResult struct {
	Threads    []map[string]any `json:"threads"`
	NextCursor string           `json:"next_cursor,omitempty"`
}

// Participant is one conversation participant.
type Participant struct {
	Name    string `json:"name,omitempty"`
	Address string `json:"address"`
}

// maxThreadParticipants caps participants per thread in results.
const maxThreadParticipants = 50

func (e *Executor) threads(q plugin.Query, rc plugin.ReportContext, env env) (ThreadsResult, error) {
	boxes, err := e.scopeMailboxes(q, rc)
	if err != nil {
		return ThreadsResult{}, err
	}
	hash := queryHash(q, rc)
	var after *cursor
	if q.Cursor != "" {
		c, err := decodeCursor(q.Cursor, hash)
		if err != nil {
			return ThreadsResult{}, err
		}
		after = &c
	}
	rows, err := e.DB.ListHeaders(db.HeaderQuery{MailboxIDs: boxes.ids, DateFrom: q.Filters.DateFrom, DateTo: q.Filters.DateTo})
	if err != nil {
		return ThreadsResult{}, err
	}
	// One row per message across folder copies, then TideMail's own
	// threading, newest conversation first.
	unique, _ := dedupe(rows)
	threads := conversation.BuildThreads(unique)

	start := 0
	if after != nil {
		start = sort.Search(len(threads), func(i int) bool {
			r := threads[i].Representative
			d := r.Date.Unix()
			return d < after.Date || (d == after.Date && r.ID < after.ID)
		})
	}
	page := threads[start:]
	out := ThreadsResult{Threads: []map[string]any{}}
	if len(page) > q.Limit {
		page = page[:q.Limit]
		last := page[len(page)-1].Representative
		out.NextCursor = encodeCursor(last.Date.Unix(), last.ID, hash)
	}

	wants := map[string]bool{}
	for _, f := range q.Fields {
		wants[f] = true
	}
	var needsYou, waiting map[int64]bool
	if wants["needs_you"] {
		if needsYou, err = e.DB.NeedsYouIDs(); err != nil {
			return ThreadsResult{}, err
		}
	}
	if wants["waiting_on_them"] {
		if waiting, err = e.waitingIDs(env); err != nil {
			return ThreadsResult{}, err
		}
	}
	for _, t := range page {
		row := make(map[string]any, len(q.Fields))
		for _, f := range q.Fields {
			row[f] = e.threadField(f, t, env, needsYou, waiting)
		}
		out.Threads = append(out.Threads, row)
	}
	return out, nil
}

// threadField maps an allowlisted field name to its value. Keep it in step
// with plugin.ThreadQueryFields.
func (e *Executor) threadField(field string, t conversation.Thread, env env, needsYou, waiting map[int64]bool) any {
	rep := t.Representative
	switch field {
	case "thread_id":
		sum := sha256.Sum256([]byte(t.Key))
		return hex.EncodeToString(sum[:8])
	case "participants":
		return threadParticipants(t)
	case "message_count":
		return t.Count
	case "latest_sender":
		return clean(rep.From)
	case "latest_subject":
		return clean(rep.Subject)
	case "latest_date":
		return rfc3339(rep.Date, env.loc)
	case "latest_from_me":
		return e.Me[conversation.BareAddress(rep.From)]
	case "needs_you":
		return anyIn(t, needsYou)
	case "waiting_on_them":
		return anyIn(t, waiting)
	}
	return nil
}

func threadParticipants(t conversation.Thread) []Participant {
	out := []Participant{}
	seen := map[string]bool{}
	for _, m := range t.Messages {
		for _, p := range conversation.ParseParticipants(m.From, m.To, m.CC) {
			addr := clean(p.Addr)
			if addr == "" || seen[addr] {
				continue
			}
			seen[addr] = true
			out = append(out, Participant{Name: clean(p.Name), Address: addr})
			if len(out) == maxThreadParticipants {
				return out
			}
		}
	}
	return out
}

func anyIn(t conversation.Thread, ids map[int64]bool) bool {
	for _, m := range t.Messages {
		if ids[m.ID] {
			return true
		}
	}
	return false
}

// waitingIDs are the row IDs of messages currently Waiting on Them, computed
// exactly as the view does (stopped and snoozed waits excluded).
func (e *Executor) waitingIDs(env env) (map[int64]bool, error) {
	waiting, err := e.currentWaiting(env)
	if err != nil {
		return nil, err
	}
	ids := map[int64]bool{}
	for _, w := range waiting {
		ids[w.Message.ID] = true
	}
	return ids, nil
}

func (e *Executor) currentWaiting(env env) ([]conversation.WaitingThread, error) {
	candidates, err := e.DB.ListWaitingCandidates()
	if err != nil {
		return nil, err
	}
	stopped, err := e.DB.WaitingStopped()
	if err != nil {
		return nil, err
	}
	snoozes, err := e.DB.ListSnoozes()
	if err != nil {
		return nil, err
	}
	snoozed := map[string]bool{}
	for _, sn := range snoozes {
		if sn.TargetType == db.SnoozeThread && sn.Until.After(env.now) {
			snoozed[sn.TargetKey] = true
		}
	}
	waiting, _, _ := conversation.ComputeWaiting(candidates, e.Me, stopped, snoozed)
	return waiting, nil
}
