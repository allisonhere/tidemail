package pluginquery

import (
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/conversation"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// Analytics return computed counts and statistics, never message records.
// They cover every cached folder except Trash, Junk, and Drafts, count each
// message once however many folders hold a copy (db.MessageKey), and call a
// message "sent" when it is from one of the user's addresses or sits in a
// Sent folder, "received" otherwise.

// counted is one message for analytics.
type counted struct {
	msg  db.Message
	ids  []int64 // row IDs of every copy
	sent bool
}

// analyticsRows loads the de-duplicated messages dated in [from, to).
func (e *Executor) analyticsRows(from, to time.Time) ([]counted, error) {
	boxes, err := e.analyticsMailboxes()
	if err != nil {
		return nil, err
	}
	rows, err := e.DB.ListHeaders(db.HeaderQuery{MailboxIDs: boxes.ids, DateFrom: from, DateTo: to})
	if err != nil {
		return nil, err
	}
	unique, copies := dedupe(rows)
	out := make([]counted, 0, len(unique))
	for _, m := range unique {
		c := counted{msg: m, sent: e.Me[conversation.BareAddress(m.From)]}
		for _, cp := range copies[db.MessageKey(m)] {
			c.ids = append(c.ids, cp.ID)
			if db.IsSentMailbox(boxes.info[cp.MailboxID].Mailbox) {
				c.sent = true
			}
		}
		out = append(out, c)
	}
	return out, nil
}

// VolumeBucket is one period of analytics.volume.
type VolumeBucket struct {
	Start    string `json:"start"` // local date, YYYY-MM-DD
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
}

// VolumeResult is analytics.volume's result.
type VolumeResult struct {
	GroupBy  string         `json:"group_by"`
	From     string         `json:"from,omitempty"`
	To       string         `json:"to"`
	Received int            `json:"received"`
	Sent     int            `json:"sent"`
	Buckets  []VolumeBucket `json:"buckets"`
}

func (e *Executor) volume(q plugin.Query, env env) (VolumeResult, error) {
	from, to := q.Window(env.now)
	rows, err := e.analyticsRows(from, to)
	if err != nil {
		return VolumeResult{}, err
	}
	out := VolumeResult{GroupBy: q.GroupBy, From: rfc3339(from, env.loc), To: rfc3339(to, env.loc), Buckets: []VolumeBucket{}}
	first := from
	if first.IsZero() {
		for _, c := range rows {
			if !c.msg.Date.IsZero() && (first.IsZero() || c.msg.Date.Before(first)) {
				first = c.msg.Date
			}
		}
		if first.IsZero() {
			return out, nil
		}
	}
	last := to.Add(-time.Nanosecond)
	index := map[string]int{}
	for start := bucketStart(first, q.GroupBy, env.loc); !start.After(last); start = nextBucket(start, q.GroupBy) {
		if len(out.Buckets) == plugin.MaxVolumeBuckets {
			return VolumeResult{}, &plugin.QueryError{Code: plugin.QueryErrInvalid, Message: "the range has too many " + q.GroupBy + " buckets; group by week or month"}
		}
		key := start.Format(time.DateOnly)
		index[key] = len(out.Buckets)
		out.Buckets = append(out.Buckets, VolumeBucket{Start: key})
	}
	for _, c := range rows {
		if c.msg.Date.IsZero() {
			continue
		}
		i, ok := index[bucketStart(c.msg.Date, q.GroupBy, env.loc).Format(time.DateOnly)]
		if !ok {
			continue
		}
		if c.sent {
			out.Buckets[i].Sent++
			out.Sent++
		} else {
			out.Buckets[i].Received++
			out.Received++
		}
	}
	return out, nil
}

func bucketStart(t time.Time, group string, loc *time.Location) time.Time {
	t = t.In(loc)
	day := time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
	switch group {
	case plugin.GroupWeek: // ISO weeks start on Monday
		return day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	case plugin.GroupMonth:
		return time.Date(t.Year(), t.Month(), 1, 0, 0, 0, 0, loc)
	}
	return day
}

func nextBucket(start time.Time, group string) time.Time {
	switch group {
	case plugin.GroupWeek:
		return start.AddDate(0, 0, 7)
	case plugin.GroupMonth:
		return start.AddDate(0, 1, 0)
	}
	return start.AddDate(0, 0, 1)
}

// CategoryCount is one category of analytics.categories.
type CategoryCount struct {
	Category string `json:"category"` // "none" for uncategorized mail
	Count    int    `json:"count"`
}

// CategoriesResult is analytics.categories' result.
type CategoriesResult struct {
	Total      int             `json:"total"`
	Categories []CategoryCount `json:"categories"`
}

func (e *Executor) categories(q plugin.Query, env env) (CategoriesResult, error) {
	rows, err := e.analyticsRows(q.Window(env.now))
	if err != nil {
		return CategoriesResult{}, err
	}
	var ids []int64
	for _, c := range rows {
		if !c.sent {
			ids = append(ids, c.ids...)
		}
	}
	effective, err := e.effective(ids)
	if err != nil {
		return CategoriesResult{}, err
	}
	counts := map[string]int{}
	out := CategoriesResult{Categories: []CategoryCount{}}
	for _, c := range rows {
		if c.sent {
			continue
		}
		// Copies can carry different annotations; a user correction on any
		// copy wins, then any plugin category.
		category, best := db.ClassificationNone, 0
		for _, id := range c.ids {
			ec := effective[id]
			rank := 0
			switch ec.CategorySource {
			case "user":
				rank = 2
			case "plugin":
				rank = 1
			}
			if rank > best {
				best = rank
				category = db.ClassificationNone
				if v := clean(ec.Category); v != "" {
					category = v
				}
			}
		}
		counts[category]++
		out.Total++
	}
	for category, n := range counts {
		out.Categories = append(out.Categories, CategoryCount{Category: category, Count: n})
	}
	sort.Slice(out.Categories, func(i, j int) bool {
		a, b := out.Categories[i], out.Categories[j]
		if a.Count != b.Count {
			return a.Count > b.Count
		}
		return a.Category < b.Category
	})
	return out, nil
}

// AttentionResult is analytics.attention's result: current state only.
// TideMail keeps no history of these states (snoozes are deleted when they
// end; corrections keep only their latest value), so no counts over time
// are offered.
type AttentionResult struct {
	NeedsYou          int `json:"needs_you_current"`
	NeedsYouDismissed int `json:"needs_you_dismissed_current"`
	Waiting           int `json:"waiting_current"`
	SnoozedMessages   int `json:"snoozed_messages_current"`
	SnoozedThreads    int `json:"snoozed_threads_current"`
	Corrections       int `json:"corrections_current"`
}

func (e *Executor) attention(env env) (AttentionResult, error) {
	var out AttentionResult
	var err error
	if out.NeedsYou, err = e.DB.CountNeedsYou(); err != nil {
		return out, err
	}
	dismissed, err := e.DB.DismissedFromNeedsYou()
	if err != nil {
		return out, err
	}
	out.NeedsYouDismissed = len(dismissed)
	waiting, err := e.currentWaiting(env)
	if err != nil {
		return out, err
	}
	out.Waiting = len(waiting)
	snoozes, err := e.DB.ListSnoozes()
	if err != nil {
		return out, err
	}
	for _, sn := range snoozes {
		if !sn.Until.After(env.now) {
			continue
		}
		if sn.TargetType == db.SnoozeThread {
			out.SnoozedThreads++
		} else {
			out.SnoozedMessages++
		}
	}
	out.Corrections, err = e.DB.CountCorrectedMessages()
	return out, err
}

// ResponseTimesResult is analytics.response_times' result, in seconds.
// Samples come from TideMail's Message-ID threading: a reply is the first
// message in the other direction after one or more messages. It is an
// approximation: replies whose other half is not cached (for example an
// unsynced Sent folder) are missed, and threads broken by clients that drop
// References are split.
type ResponseTimesResult struct {
	UserSamples         int   `json:"user_sample_count"`
	MedianUserResponse  int64 `json:"median_user_response_s"`
	AverageUserResponse int64 `json:"average_user_response_s"`
	OtherSamples        int   `json:"other_sample_count"`
	MedianOther         int64 `json:"median_other_response_s"`
	AverageOther        int64 `json:"average_other_response_s"`
	Excluded            int   `json:"excluded_count"`
}

func (e *Executor) responseTimes(q plugin.Query, env env) (ResponseTimesResult, error) {
	var out ResponseTimesResult
	if len(e.Me) == 0 {
		return out, nil // direction is unknown without the user's addresses
	}
	from, to := q.Window(env.now)
	// Load all history so a reply in range can pair with an earlier message.
	rows, err := e.analyticsRows(time.Time{}, to)
	if err != nil {
		return out, err
	}
	msgs := make([]db.Message, 0, len(rows))
	sentByID := map[int64]bool{}
	for _, c := range rows {
		msgs = append(msgs, c.msg)
		sentByID[c.msg.ID] = c.sent
	}
	var user, other []int64
	for _, t := range conversation.BuildThreads(msgs) {
		var pending time.Time // first message of the current unanswered run
		pendingSent := false
		for _, m := range t.Messages {
			if !conversation.Meaningful(m) && !sentByID[m.ID] {
				continue
			}
			if m.Date.IsZero() {
				out.Excluded++
				continue
			}
			sent := sentByID[m.ID]
			switch {
			case pending.IsZero():
				pending, pendingSent = m.Date, sent
			case sent != pendingSent:
				gap := m.Date.Sub(pending)
				inRange := !m.Date.Before(from) && m.Date.Before(to)
				switch {
				case gap < 0:
					out.Excluded++
				case inRange && sent:
					user = append(user, int64(gap/time.Second))
				case inRange:
					other = append(other, int64(gap/time.Second))
				}
				pending, pendingSent = m.Date, sent
			}
		}
	}
	out.UserSamples, out.MedianUserResponse, out.AverageUserResponse = stats(user)
	out.OtherSamples, out.MedianOther, out.AverageOther = stats(other)
	return out, nil
}

func stats(samples []int64) (n int, median, average int64) {
	if len(samples) == 0 {
		return 0, 0, 0
	}
	sort.Slice(samples, func(i, j int) bool { return samples[i] < samples[j] })
	var sum int64
	for _, s := range samples {
		sum += s
	}
	mid := len(samples) / 2
	median = samples[mid]
	if len(samples)%2 == 0 {
		median = (samples[mid-1] + samples[mid]) / 2
	}
	return len(samples), median, sum / int64(len(samples))
}

// Contact is one correspondent of analytics.contacts.
type Contact struct {
	Address  string `json:"address"`
	Name     string `json:"name,omitempty"`
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
	Total    int    `json:"total"`
}

// Domain is one sender/recipient domain of analytics.contacts.
type Domain struct {
	Domain   string `json:"domain"`
	Received int    `json:"received"`
	Sent     int    `json:"sent"`
	Total    int    `json:"total"`
}

// ContactsResult is analytics.contacts' result.
type ContactsResult struct {
	Correspondents []Contact `json:"correspondents"`
	Domains        []Domain  `json:"domains"`
}

func (e *Executor) contacts(q plugin.Query, env env) (ContactsResult, error) {
	rows, err := e.analyticsRows(q.Window(env.now))
	if err != nil {
		return ContactsResult{}, err
	}
	people := map[string]*Contact{}
	domains := map[string]*Domain{}
	newest := map[string]time.Time{}
	add := func(p conversation.Participant, date time.Time, sent bool) {
		addr := clean(p.Addr)
		if addr == "" || e.Me[addr] {
			return
		}
		c := people[addr]
		if c == nil {
			c = &Contact{Address: addr}
			people[addr] = c
		}
		if name := clean(p.Name); name != "" && !date.Before(newest[addr]) {
			c.Name, newest[addr] = name, date
		}
		domain := addr[strings.LastIndex(addr, "@")+1:]
		d := domains[domain]
		if d == nil {
			d = &Domain{Domain: domain}
			domains[domain] = d
		}
		if sent {
			c.Sent++
			d.Sent++
		} else {
			c.Received++
			d.Received++
		}
		c.Total++
		d.Total++
	}
	for _, c := range rows {
		if c.sent {
			seen := map[string]bool{}
			for _, p := range conversation.ParseParticipants(c.msg.To, c.msg.CC) {
				if !seen[p.Addr] {
					seen[p.Addr] = true
					add(p, c.msg.Date, true)
				}
			}
			continue
		}
		if ps := conversation.ParseParticipants(c.msg.From); len(ps) > 0 {
			add(ps[0], c.msg.Date, false)
		}
	}
	out := ContactsResult{Correspondents: []Contact{}, Domains: []Domain{}}
	for _, c := range people {
		out.Correspondents = append(out.Correspondents, *c)
	}
	for _, d := range domains {
		out.Domains = append(out.Domains, *d)
	}
	sort.Slice(out.Correspondents, func(i, j int) bool {
		a, b := out.Correspondents[i], out.Correspondents[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Address < b.Address
	})
	sort.Slice(out.Domains, func(i, j int) bool {
		a, b := out.Domains[i], out.Domains[j]
		if a.Total != b.Total {
			return a.Total > b.Total
		}
		return a.Domain < b.Domain
	})
	out.Correspondents = out.Correspondents[:min(q.Limit, len(out.Correspondents))]
	out.Domains = out.Domains[:min(q.Limit, len(out.Domains))]
	return out, nil
}
