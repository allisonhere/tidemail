package db

import "testing"

func TestEvictionPreservesCollidingDestinationAndAllowsReturn(t *testing.T) {
	d := newTestDB(t)
	account, err := d.AddAccount("safety", "Safety", "")
	if err != nil {
		t.Fatal(err)
	}
	source, err := d.UpsertMailbox(Mailbox{AccountID: account, Name: "INBOX", UnreadCount: 1})
	if err != nil {
		t.Fatal(err)
	}
	target, err := d.UpsertMailbox(Mailbox{AccountID: account, Name: "Archive"})
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range []Message{{MailboxID: source, UID: 1, MessageID: "<source>", Subject: "source"}, {MailboxID: target, UID: 1, MessageID: "<other>", Subject: "other"}} {
		if err := d.UpsertMessage(m); err != nil {
			t.Fatal(err)
		}
	}
	id, err := d.MessageIDByUID(source, 1)
	if err != nil {
		t.Fatal(err)
	}
	if err := d.EvictMessage(id); err != nil {
		t.Fatal(err)
	}
	dest, err := d.ListMessages(target)
	if err != nil || len(dest) != 1 || dest[0].Subject != "other" {
		t.Fatalf("destination damaged: %v %v", dest, err)
	}
	deleted, err := d.MessageDeletedLocally(source, 1, "<source>")
	if err != nil || deleted {
		t.Fatalf("eviction created tombstone: %v %v", deleted, err)
	}
	if err := d.UpsertMessage(Message{MailboxID: target, UID: 2, MessageID: "<source>"}); err != nil {
		t.Fatal(err)
	}
	if err := d.UpsertMessage(Message{MailboxID: source, UID: 3, MessageID: "<source>"}); err != nil {
		t.Fatal(err)
	}
	if err := d.MarkAllRead(source); err != nil {
		t.Fatal(err)
	}
	mb, err := d.GetMailbox(source)
	if err != nil || mb.UnreadCount != 0 {
		t.Fatalf("persisted unread count: %v %v", mb, err)
	}
}
