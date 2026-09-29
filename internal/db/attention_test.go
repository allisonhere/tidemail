package db

import (
	"fmt"
	"reflect"
	"testing"
	"time"
)

// attentionDB has two accounts' inboxes plus Trash and Sent, and returns a
// helper that adds a message and gives its ID.
type attentionDB struct {
	*DB
	t                    *testing.T
	inbox, inbox2, trash int64
	sent                 int64
	uid                  uint32
}

func newAttentionDB(t *testing.T) *attentionDB {
	t.Helper()
	d := newTestDB(t)
	a1, _ := d.AddAccount("", "Work", "")
	a2, _ := d.AddAccount("", "Home", "")
	mk := func(acc int64, name string, flags ...string) int64 {
		id, err := d.UpsertMailbox(Mailbox{AccountID: acc, Name: name, Delimiter: "/", Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	return &attentionDB{DB: d, t: t,
		inbox: mk(a1, "INBOX"), inbox2: mk(a2, "Inbox"),
		trash: mk(a1, "Trash", "\\Trash"), sent: mk(a1, "Sent", "\\Sent"),
	}
}

// add stores a message at minutes-ago and annotates it with plugin:key=value
// triples.
func (a *attentionDB) add(mailbox int64, subject string, minutesAgo int, anns ...string) int64 {
	a.t.Helper()
	a.uid++
	date := time.Unix(1_800_000_000, 0).Add(-time.Duration(minutesAgo) * time.Minute)
	if err := a.UpsertMessage(Message{MailboxID: mailbox, UID: a.uid, Subject: subject, Date: date}); err != nil {
		a.t.Fatal(err)
	}
	id, err := a.MessageIDByUID(mailbox, a.uid)
	if err != nil {
		a.t.Fatal(err)
	}
	byPlugin := map[string][]PluginAnnotation{}
	for _, s := range anns {
		var plugin, key, value string
		if _, err := fmt.Sscanf(s, "%s %s %s", &plugin, &key, &value); err != nil {
			a.t.Fatalf("bad annotation spec %q", s)
		}
		byPlugin[plugin] = append(byPlugin[plugin], PluginAnnotation{Key: key, Value: value})
	}
	for plugin, list := range byPlugin {
		if err := a.ReplacePluginAnnotations(plugin, id, list); err != nil {
			a.t.Fatal(err)
		}
	}
	return id
}

func (a *attentionDB) subjects(unreadOnly bool) []string {
	a.t.Helper()
	msgs, err := a.ListNeedsYou(unreadOnly)
	if err != nil {
		a.t.Fatal(err)
	}
	out := []string{}
	for _, m := range msgs {
		out = append(out, m.Subject)
	}
	return out
}

func (a *attentionDB) count() int {
	a.t.Helper()
	n, err := a.CountNeedsYou()
	if err != nil {
		a.t.Fatal(err)
	}
	return n
}

func TestNeedsYouQualification(t *testing.T) {
	a := newAttentionDB(t)
	a.add(a.inbox, "reply", 1, "p needs_reply true")
	a.add(a.inbox, "reply-yes", 2, "p needs_reply YES")
	a.add(a.inbox, "urgent", 3, "p urgency high")
	a.add(a.inbox, "critical", 4, "p urgency critical")
	a.add(a.inbox, "important", 5, "p importance high")
	a.add(a.inbox, "category-only", 6, "p category security")
	a.add(a.inbox, "reply-false", 7, "p needs_reply false")
	a.add(a.inbox, "low-urgency", 8, "p urgency low")
	a.add(a.inbox, "unknown-key", 9, "p mood high")
	a.add(a.inbox, "no-annotations", 10)

	got := a.subjects(false)
	want := []string{"urgent", "critical", "reply", "reply-yes", "important"}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("needs you = %v, want %v", got, want)
	}
	if a.count() != len(want) {
		t.Fatalf("count = %d", a.count())
	}
}

func TestNeedsYouAnyPluginAndConflicts(t *testing.T) {
	a := newAttentionDB(t)
	// A signal from any plugin qualifies, including one that is no longer
	// installed (the database has no notion of installed).
	a.add(a.inbox, "other-plugin", 1, "removed importance high")
	// Positive wins over a conflicting negative.
	a.add(a.inbox, "conflict", 2, "a needs_reply true", "b needs_reply false")
	// Three plugins agreeing: still one row.
	a.add(a.inbox, "agree", 3, "a importance high", "b importance high", "c importance high")
	got := a.subjects(false)
	if !reflect.DeepEqual(got, []string{"conflict", "other-plugin", "agree"}) {
		t.Fatalf("got %v", got)
	}
	if a.count() != 3 {
		t.Fatalf("count = %d", a.count())
	}
}

func TestNeedsYouInboxOnly(t *testing.T) {
	a := newAttentionDB(t)
	a.add(a.inbox, "work-inbox", 1, "p needs_reply true")
	a.add(a.inbox2, "home-inbox", 2, "p needs_reply true")
	a.add(a.trash, "trash", 3, "p needs_reply true")
	a.add(a.sent, "sent", 4, "p needs_reply true")
	if got := a.subjects(false); !reflect.DeepEqual(got, []string{"work-inbox", "home-inbox"}) {
		t.Fatalf("got %v", got)
	}
}

func TestNeedsYouRanking(t *testing.T) {
	a := newAttentionDB(t)
	a.add(a.inbox, "important-new", 1, "p importance high")
	a.add(a.inbox, "reply-old", 50, "p needs_reply true")
	a.add(a.inbox, "urgent-oldest", 90, "p urgency high")
	a.add(a.inbox, "reply+important", 70, "p needs_reply true", "p importance high")
	a.add(a.inbox, "reply-new", 5, "p needs_reply true")
	// Scores: reply+important 5, urgent 4, reply 3 (newest first), important 2.
	want := []string{"reply+important", "urgent-oldest", "reply-new", "reply-old", "important-new"}
	if got := a.subjects(false); !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v, want %v", got, want)
	}
	// Duplicate plugins never add weight: three plugins saying importance
	// still rank below one plugin saying needs_reply.
	a.add(a.inbox, "triple-important", 0, "a importance high", "b importance high", "c importance high")
	got := a.subjects(false)
	if got[len(got)-2] != "triple-important" || got[len(got)-1] != "important-new" {
		t.Fatalf("got %v", got)
	}
}

func TestNeedsYouDismissal(t *testing.T) {
	a := newAttentionDB(t)
	keep := a.add(a.inbox, "keep", 1, "p needs_reply true")
	gone := a.add(a.inbox, "dismissed", 2, "p urgency high")
	if err := a.SetNeedsYouDismissed(gone, true); err != nil {
		t.Fatal(err)
	}
	if got := a.subjects(false); !reflect.DeepEqual(got, []string{"keep"}) || a.count() != 1 {
		t.Fatalf("got %v", got)
	}
	// Dismissing twice is fine, and it touches neither the annotations nor
	// the message.
	if err := a.SetNeedsYouDismissed(gone, true); err != nil {
		t.Fatal(err)
	}
	if anns, _ := a.ListPluginAnnotations(gone); len(anns) != 1 {
		t.Fatalf("annotations changed: %v", anns)
	}
	if msg, err := a.GetMessage(gone); err != nil || msg.Read || msg.Starred {
		t.Fatalf("message changed: %+v %v", msg, err)
	}
	dismissed, err := a.DismissedFromNeedsYou()
	if err != nil || !dismissed[gone] || dismissed[keep] {
		t.Fatalf("dismissed = %v %v", dismissed, err)
	}
	// Reclassification does not undo a dismissal.
	if err := a.ReplacePluginAnnotations("p", gone, []PluginAnnotation{{Key: "urgency", Value: "critical"}}); err != nil {
		t.Fatal(err)
	}
	if a.count() != 1 {
		t.Fatal("a dismissal must survive new annotations")
	}
	// Restore brings it back.
	if err := a.SetNeedsYouDismissed(gone, false); err != nil {
		t.Fatal(err)
	}
	if got := a.subjects(false); !reflect.DeepEqual(got, []string{"dismissed", "keep"}) {
		t.Fatalf("after restore: %v", got)
	}
	// Deleting the message removes its override too.
	if err := a.SetNeedsYouDismissed(keep, true); err != nil {
		t.Fatal(err)
	}
	if err := a.DeleteMessage(keep); err != nil {
		t.Fatal(err)
	}
	var n int
	if err := a.QueryRow(`SELECT COUNT(*) FROM message_attention_overrides`).Scan(&n); err != nil || n != 0 {
		t.Fatalf("orphaned overrides: %d %v", n, err)
	}
}

func TestNeedsYouUnreadOnlyKeepsCountSemantics(t *testing.T) {
	a := newAttentionDB(t)
	read := a.add(a.inbox, "read", 1, "p needs_reply true")
	a.add(a.inbox, "unread", 2, "p needs_reply true")
	if _, err := a.Exec(`UPDATE messages SET read = 1 WHERE id = ?`, read); err != nil {
		t.Fatal(err)
	}
	// Reading does not remove a message; the unread filter only narrows the list.
	if got := a.subjects(false); len(got) != 2 {
		t.Fatalf("got %v", got)
	}
	if got := a.subjects(true); !reflect.DeepEqual(got, []string{"unread"}) {
		t.Fatalf("unread only = %v", got)
	}
	if a.count() != 2 {
		t.Fatal("the count includes read messages")
	}
}

func TestAttentionOfMatchesQuery(t *testing.T) {
	cases := []struct {
		anns []PluginAnnotation
		want Attention
	}{
		{nil, Attention{}},
		{[]PluginAnnotation{{Key: "needs_reply", Value: " True "}}, Attention{NeedsReply: true}},
		{[]PluginAnnotation{{Key: "urgency", Value: "urgent"}, {Key: "importance", Value: "high"}}, Attention{Urgent: true, Important: true}},
		{[]PluginAnnotation{{Key: "category", Value: "security"}, {Key: "needs_reply", Value: "false"}}, Attention{}},
		{[]PluginAnnotation{{PluginID: "a", Key: "needs_reply", Value: "true"}, {PluginID: "b", Key: "needs_reply", Value: "false"}}, Attention{NeedsReply: true}},
	}
	for _, c := range cases {
		if got := AttentionOf(c.anns); got != c.want {
			t.Errorf("AttentionOf(%v) = %+v, want %+v", c.anns, got, c.want)
		}
	}
}

func TestSetNeedsYouDismissedRejectsBadID(t *testing.T) {
	a := newAttentionDB(t)
	if err := a.SetNeedsYouDismissed(0, true); err == nil {
		t.Fatal("expected an error")
	}
	if err := a.SetNeedsYouDismissed(999999, true); err == nil {
		t.Fatal("dismissing a message that does not exist should fail the foreign key")
	}
}
