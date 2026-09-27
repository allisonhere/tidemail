package pluginquery

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

// Fixture is a synthetic mail cache for developer tooling and tests. It is
// loaded into a throwaway database, never the user's. Messages carry headers
// only; there is no way to give them bodies.
type Fixture struct {
	// Me are the user's own addresses.
	Me []string `json:"me"`
	// Now pins the report's clock (RFC 3339). Empty means the current time.
	Now      string           `json:"now,omitempty"`
	Accounts []FixtureAccount `json:"accounts"`
}

// FixtureAccount is one account.
type FixtureAccount struct {
	Name      string           `json:"name"`
	Mailboxes []FixtureMailbox `json:"mailboxes"`
}

// FixtureMailbox is one folder. Flags are IMAP special-use flags such as
// "\\Sent" or "\\Trash".
type FixtureMailbox struct {
	Name     string           `json:"name"`
	Flags    []string         `json:"flags,omitempty"`
	Messages []FixtureMessage `json:"messages"`
}

// FixtureMessage is one cached message.
type FixtureMessage struct {
	MessageID     string              `json:"message_id,omitempty"`
	InReplyTo     string              `json:"in_reply_to,omitempty"`
	References    string              `json:"references,omitempty"`
	From          string              `json:"from,omitempty"`
	To            string              `json:"to,omitempty"`
	CC            string              `json:"cc,omitempty"`
	ReplyTo       string              `json:"reply_to,omitempty"`
	Subject       string              `json:"subject,omitempty"`
	Date          string              `json:"date"`
	Read          bool                `json:"read,omitempty"`
	Starred       bool                `json:"starred,omitempty"`
	HasAttachment bool                `json:"has_attachment,omitempty"`
	Flags         []string            `json:"flags,omitempty"`
	Annotations   []FixtureAnnotation `json:"annotations,omitempty"`
	// Overrides are user classification corrections, by key.
	Overrides map[string]string `json:"overrides,omitempty"`
	// Dismissed hides the message from Needs You.
	Dismissed bool `json:"dismissed,omitempty"`
	// SnoozedUntil snoozes the message (RFC 3339).
	SnoozedUntil string `json:"snoozed_until,omitempty"`
}

// FixtureAnnotation is one stored plugin annotation.
type FixtureAnnotation struct {
	Plugin     string   `json:"plugin"`
	Key        string   `json:"key"`
	Value      string   `json:"value"`
	Confidence *float64 `json:"confidence,omitempty"`
}

// ReadFixture reads a fixture file, rejecting unknown fields.
func ReadFixture(path string) (Fixture, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return Fixture{}, err
	}
	var f Fixture
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&f); err != nil {
		return Fixture{}, fmt.Errorf("fixture %s: %w", path, err)
	}
	return f, nil
}

// Load stores the fixture in database, which should be empty.
func (f Fixture) Load(database *db.DB) error {
	if len(f.Accounts) == 0 {
		return errors.New("fixture has no accounts")
	}
	for _, acc := range f.Accounts {
		accID, err := database.AddAccount("", acc.Name, "")
		if err != nil {
			return err
		}
		for _, mb := range acc.Mailboxes {
			mbID, err := database.UpsertMailbox(db.Mailbox{AccountID: accID, Name: mb.Name, Delimiter: "/", Flags: mb.Flags})
			if err != nil {
				return err
			}
			for i, m := range mb.Messages {
				if err := loadFixtureMessage(database, mbID, uint32(i+1), m); err != nil {
					return fmt.Errorf("%s/%s message %d: %w", acc.Name, mb.Name, i+1, err)
				}
			}
		}
	}
	return nil
}

func loadFixtureMessage(database *db.DB, mailboxID int64, uid uint32, m FixtureMessage) error {
	date, err := time.Parse(time.RFC3339, m.Date)
	if err != nil {
		return fmt.Errorf("date must be RFC 3339: %w", err)
	}
	msg := db.Message{
		MailboxID: mailboxID, UID: uid, MessageID: m.MessageID, InReplyTo: m.InReplyTo, References: m.References,
		From: m.From, To: m.To, CC: m.CC, ReplyTo: m.ReplyTo, Subject: m.Subject, Date: date,
		Read: m.Read, Starred: m.Starred, HasAttachment: m.HasAttachment, Flags: m.Flags,
	}
	if err := database.UpsertMessage(msg); err != nil {
		return err
	}
	id, err := database.MessageIDByUID(mailboxID, uid)
	if err != nil {
		return err
	}
	msg.ID = id
	byPlugin := map[string][]db.PluginAnnotation{}
	var order []string
	for _, a := range m.Annotations {
		if _, ok := byPlugin[a.Plugin]; !ok {
			order = append(order, a.Plugin)
		}
		byPlugin[a.Plugin] = append(byPlugin[a.Plugin], db.PluginAnnotation{Key: a.Key, Value: a.Value, Confidence: a.Confidence})
	}
	for _, p := range order {
		if err := database.ReplacePluginAnnotations(p, id, byPlugin[p]); err != nil {
			return err
		}
	}
	for key, value := range m.Overrides {
		if err := database.SetClassificationOverride(id, key, value); err != nil {
			return err
		}
	}
	if m.Dismissed {
		if err := database.SetNeedsYouDismissed(id, true); err != nil {
			return err
		}
	}
	if m.SnoozedUntil != "" {
		until, err := time.Parse(time.RFC3339, m.SnoozedUntil)
		if err != nil {
			return fmt.Errorf("snoozed_until must be RFC 3339: %w", err)
		}
		if err := database.SetSnooze(db.SnoozeMessage, db.MessageKey(msg), until); err != nil {
			return err
		}
	}
	return nil
}

// DefaultFixture is a small, varied mailbox used when no fixture is given: a
// few conversations, categories, a correction, a snooze, and sent mail,
// spread over the 30 days before now.
func DefaultFixture(now time.Time) Fixture {
	day := func(daysAgo, hour int) string {
		d := now.AddDate(0, 0, -daysAgo)
		return time.Date(d.Year(), d.Month(), d.Day(), hour, 0, 0, 0, time.UTC).Format(time.RFC3339)
	}
	ann := func(kv ...string) []FixtureAnnotation {
		var out []FixtureAnnotation
		for i := 0; i+1 < len(kv); i += 2 {
			out = append(out, FixtureAnnotation{Plugin: "fixture", Key: kv[i], Value: kv[i+1]})
		}
		return out
	}
	inbox := []FixtureMessage{
		{MessageID: "<q1@ann.example>", From: "Ann Lee <ann@ann.example>", To: "me@example.com", Subject: "Quarterly plan", Date: day(12, 9), Read: true, Annotations: ann("category", "work", "needs_reply", "true")},
		{MessageID: "<q3@ann.example>", InReplyTo: "<q2@example.com>", References: "<q1@ann.example> <q2@example.com>", From: "Ann Lee <ann@ann.example>", To: "me@example.com", Subject: "Re: Quarterly plan", Date: day(10, 16), Annotations: ann("category", "work", "urgency", "high")},
		{MessageID: "<ci1@github.example>", From: "GitHub <notifications@github.example>", To: "me@example.com", Subject: "CI failed on main", Date: day(1, 20), Annotations: ann("category", "github")},
		{MessageID: "<ci2@github.example>", From: "GitHub <notifications@github.example>", To: "me@example.com", Subject: "PR review requested", Date: day(3, 11), Read: true, Annotations: ann("category", "github", "needs_reply", "true")},
		{MessageID: "<n1@news.example>", From: "Weekly News <news@news.example>", To: "me@example.com", Subject: "This week", Date: day(7, 7), Read: true, Annotations: ann("category", "newsletter")},
		{MessageID: "<n2@news.example>", From: "Weekly News <news@news.example>", To: "me@example.com", Subject: "Last week", Date: day(14, 7), Read: true, Overrides: map[string]string{"category": "personal"}},
		{MessageID: "<b1@shop.example>", From: "Shop <billing@shop.example>", To: "me@example.com", Subject: "Your receipt", Date: day(5, 13), HasAttachment: true, Starred: true, Annotations: ann("category", "billing", "importance", "high"), SnoozedUntil: day(-2, 9)},
		{MessageID: "<p1@bob.example>", From: "Bob <bob@bob.example>", To: "me@example.com", Subject: "Dinner?", Date: day(2, 18), Annotations: ann("needs_reply", "true"), Dismissed: true},
	}
	sent := []FixtureMessage{
		{MessageID: "<q2@example.com>", InReplyTo: "<q1@ann.example>", References: "<q1@ann.example>", From: "Me <me@example.com>", To: "Ann Lee <ann@ann.example>", Subject: "Re: Quarterly plan", Date: day(11, 10), Read: true},
		{MessageID: "<c1@example.com>", From: "Me <me@example.com>", To: "Carol <carol@carol.example>", Subject: "Contract draft", Date: day(4, 15), Read: true},
	}
	return Fixture{
		Me:  []string{"me@example.com"},
		Now: now.UTC().Format(time.RFC3339),
		Accounts: []FixtureAccount{{Name: "Personal", Mailboxes: []FixtureMailbox{
			{Name: "INBOX", Messages: inbox},
			{Name: "Sent", Flags: []string{`\Sent`}, Messages: sent},
			{Name: "Trash", Flags: []string{`\Trash`}, Messages: []FixtureMessage{
				{MessageID: "<t1@spam.example>", From: "Spam <x@spam.example>", To: "me@example.com", Subject: "Deleted", Date: day(6, 6)},
			}},
		}}},
	}
}
