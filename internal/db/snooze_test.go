package db

import (
	"testing"
	"time"
)

var snoozeNow = time.Date(2026, 9, 27, 12, 0, 0, 0, time.UTC)

func TestSnoozeSetReplaceDelete(t *testing.T) {
	d := newTestDB(t)
	later := snoozeNow.Add(3 * time.Hour)
	if err := d.SetSnooze(SnoozeMessage, "<a@x>", later); err != nil {
		t.Fatal(err)
	}
	if err := d.SetSnooze(SnoozeThread, "<b@x>", snoozeNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	// Replacing moves the wake time.
	if err := d.SetSnooze(SnoozeMessage, "<a@x>", snoozeNow.Add(30*time.Minute)); err != nil {
		t.Fatal(err)
	}
	got, err := d.ListSnoozes()
	if err != nil || len(got) != 2 {
		t.Fatalf("snoozes = %v %v", got, err)
	}
	if got[0].TargetKey != "<a@x>" || !got[0].Until.Equal(snoozeNow.Add(30*time.Minute)) || got[1].TargetType != SnoozeThread {
		t.Fatalf("order/replace wrong: %+v", got)
	}
	if err := d.DeleteSnooze(SnoozeMessage, "<a@x>"); err != nil {
		t.Fatal(err)
	}
	if err := d.DeleteSnooze(SnoozeMessage, "<nope@x>"); err != nil {
		t.Fatal("deleting a missing snooze should succeed")
	}
	if got, _ := d.ListSnoozes(); len(got) != 1 {
		t.Fatalf("after delete: %v", got)
	}
}

func TestSnoozeValidation(t *testing.T) {
	d := newTestDB(t)
	for _, c := range []struct {
		typ, key string
		until    time.Time
	}{
		{"folder", "<a@x>", snoozeNow},
		{SnoozeMessage, " ", snoozeNow},
		{SnoozeMessage, "<a@x>", time.Time{}},
	} {
		if err := d.SetSnooze(c.typ, c.key, c.until); err == nil {
			t.Errorf("SetSnooze(%q, %q, %v) should fail", c.typ, c.key, c.until)
		}
	}
	if err := d.DeleteSnooze("folder", "<a@x>"); err == nil {
		t.Fatal("unknown type should be rejected on delete too")
	}
}

func TestSnoozeDeadlinesAndExpiry(t *testing.T) {
	d := newTestDB(t)
	if _, ok, err := d.NextSnoozeDeadline(snoozeNow); ok || err != nil {
		t.Fatal("no snoozes, no deadline")
	}
	_ = d.SetSnooze(SnoozeMessage, "<due1@x>", snoozeNow.Add(-2*time.Hour))
	_ = d.SetSnooze(SnoozeMessage, "<due2@x>", snoozeNow)
	_ = d.SetSnooze(SnoozeThread, "<soon@x>", snoozeNow.Add(time.Hour))
	_ = d.SetSnooze(SnoozeMessage, "<later@x>", snoozeNow.Add(24*time.Hour))
	next, ok, err := d.NextSnoozeDeadline(snoozeNow)
	if err != nil || !ok || !next.Equal(snoozeNow.Add(time.Hour)) {
		t.Fatalf("next = %v %v %v", next, ok, err)
	}
	n, err := d.DeleteDueSnoozes(snoozeNow)
	if err != nil || n != 2 {
		t.Fatalf("expired %d, %v", n, err)
	}
	if got, _ := d.ListSnoozes(); len(got) != 2 {
		t.Fatalf("remaining = %v", got)
	}
}

func TestSnoozedMessagesAndPrune(t *testing.T) {
	d, mailbox, ids := newAnnotationTestDB(t, 2)
	if _, err := d.Exec(`UPDATE messages SET message_id = '<Keep@X>' WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	// The same message in a second folder is one snoozed item.
	archive, _ := d.UpsertMailbox(Mailbox{AccountID: 1, Name: "Archive"})
	if err := d.UpsertMessage(Message{MailboxID: archive, UID: 99, MessageID: "<keep@x>", Subject: "copy"}); err != nil {
		t.Fatal(err)
	}
	row := MessageKey(Message{ID: ids[1]})
	_ = d.SetSnooze(SnoozeMessage, "<keep@x>", snoozeNow.Add(2*time.Hour))
	_ = d.SetSnooze(SnoozeThread, row, snoozeNow.Add(time.Hour))
	_ = d.SetSnooze(SnoozeMessage, "<gone@x>", snoozeNow.Add(time.Hour))

	got, err := d.ListSnoozedMessages()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 || got[0].Message.ID != ids[1] || got[1].Message.ID != ids[0] {
		t.Fatalf("snoozed messages = %+v", got)
	}
	if err := d.PruneStaleSnoozes(); err != nil {
		t.Fatal(err)
	}
	if all, _ := d.ListSnoozes(); len(all) != 2 {
		t.Fatalf("prune kept stale rows: %v", all)
	}
	// Moving a message keeps its snooze (keys don't depend on folders).
	if err := d.MoveMessage(ids[0], archive); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.ListSnoozedMessages(); len(got) != 2 {
		t.Fatal("a moved message lost its snooze")
	}
	_ = mailbox
}

// MessageKey and messageKeySQL must agree on every Message-ID, or snoozes and
// stop-waiting dismissals stored with the Go key miss in SQL and are pruned.
func TestMessageKeySQLMatchesGo(t *testing.T) {
	ids := []string{
		"<ABC@Host>", " <x@y> ", "", "<>", "   ", "<<a@b>>", "plain@host",
		"\t<tab@x>\r\n", "\v<vt@x>\f", "<a b@x>", "<a\tb@x>", "<a\rb@x>", "<a\nb@x>",
		"<a\vb@x>", "< a@x>", "<ÄBC@Höst>", "\u00a0<nbsp@x>\u00a0", "<a\u2003b@x>",
	}
	d, _, rows := newAnnotationTestDB(t, len(ids))
	for i, mid := range ids {
		if _, err := d.Exec(`UPDATE messages SET message_id = ? WHERE id = ?`, mid, rows[i]); err != nil {
			t.Fatal(err)
		}
		var sqlKey string
		if err := d.QueryRow(`SELECT `+messageKeySQL+` FROM messages WHERE id = ?`, rows[i]).Scan(&sqlKey); err != nil {
			t.Fatal(err)
		}
		if goKey := MessageKey(Message{ID: rows[i], MessageID: mid}); goKey != sqlKey {
			t.Errorf("message_id %q: Go %q, SQL %q", mid, goKey, sqlKey)
		}
	}
}

// Normal Message-IDs keep the keys existing snoozes were stored under.
func TestMessageKeyKeepsNormalKeys(t *testing.T) {
	for mid, want := range map[string]string{
		"<ABC@Host.example>":  "<abc@host.example>",
		" <x@y> ":             "<x@y>",
		"\t<tab@x>\r\n":       "<tab@x>",
		"no-brackets@Example": "<no-brackets@example>",
		"<a\tb@x>":            "row:7",
		"<ÄBC@x>":             "<Äbc@x>",
	} {
		if got := MessageKey(Message{ID: 7, MessageID: mid}); got != want {
			t.Errorf("MessageKey(%q) = %q, want %q", mid, got, want)
		}
	}
}

// A message whose Message-ID has an internal tab is keyed by its row in Go;
// SQL must agree so its snooze hides it, survives pruning, and lists it.
func TestSnoozeWithTabbedMessageID(t *testing.T) {
	a := newAttentionDB(t)
	id := a.add(a.inbox, "snoozed", 1, "p needs_reply true")
	a.add(a.inbox, "visible", 2, "p urgency high")
	if _, err := a.Exec(`UPDATE messages SET message_id = ? WHERE id = ?`, "<a\tb@x>", id); err != nil {
		t.Fatal(err)
	}
	key := MessageKey(Message{ID: id, MessageID: "<a\tb@x>"})
	if err := a.SetSnooze(SnoozeMessage, key, snoozeNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := a.subjects(false); len(got) != 1 || got[0] != "visible" {
		t.Fatalf("snoozed message still in Needs You: %v", got)
	}
	if err := a.PruneStaleSnoozes(); err != nil {
		t.Fatal(err)
	}
	if all, _ := a.ListSnoozes(); len(all) != 1 {
		t.Fatalf("startup prune dropped a live snooze: %v", all)
	}
	if got, _ := a.ListSnoozedMessages(); len(got) != 1 || got[0].Message.ID != id {
		t.Fatalf("snoozed list = %+v", got)
	}
}

func TestNeedsYouExcludesSnoozedMessages(t *testing.T) {
	a := newAttentionDB(t)
	id := a.add(a.inbox, "snoozed", 1, "p needs_reply true")
	a.add(a.inbox, "visible", 2, "p urgency high")
	if _, err := a.Exec(`UPDATE messages SET message_id = '<s@x>' WHERE id = ?`, id); err != nil {
		t.Fatal(err)
	}
	if err := a.SetSnooze(SnoozeMessage, "<s@x>", snoozeNow.Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if got := a.subjects(false); len(got) != 1 || got[0] != "visible" || a.count() != 1 {
		t.Fatalf("got %v", got)
	}
	// Annotations are untouched; unsnoozing brings it back with them.
	if anns, _ := a.ListPluginAnnotations(id); len(anns) != 1 {
		t.Fatal("snooze changed annotations")
	}
	if err := a.DeleteSnooze(SnoozeMessage, "<s@x>"); err != nil {
		t.Fatal(err)
	}
	if a.count() != 2 {
		t.Fatal("unsnooze did not restore it")
	}
	// A dismissal still hides it after the snooze is gone.
	if err := a.SetNeedsYouDismissed(id, true); err != nil {
		t.Fatal(err)
	}
	_ = a.SetSnooze(SnoozeMessage, "<s@x>", snoozeNow.Add(time.Hour))
	_, _ = a.DeleteDueSnoozes(snoozeNow.Add(2 * time.Hour))
	if a.count() != 1 {
		t.Fatal("dismissal must still win after a snooze expires")
	}
}
