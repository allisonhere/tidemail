package db

import (
	"reflect"
	"sort"
	"testing"
	"time"
)

func TestExcludedFromWaiting(t *testing.T) {
	cases := []struct {
		mb   Mailbox
		want bool
	}{
		{Mailbox{Name: "INBOX"}, false},
		{Mailbox{Name: "Sent"}, false},
		{Mailbox{Name: "[Gmail]/All Mail"}, false},
		{Mailbox{Name: "Archive"}, false},
		{Mailbox{Name: "Trash"}, true},
		{Mailbox{Name: "INBOX.Trash"}, true},
		{Mailbox{Name: "[Gmail]/Spam"}, true},
		{Mailbox{Name: "Junk"}, true},
		{Mailbox{Name: "Drafts"}, true},
		{Mailbox{Name: "Bin", Flags: []string{`\Trash`}}, true},
		{Mailbox{Name: "Unwanted", Flags: []string{`\Junk`}}, true},
		{Mailbox{Name: "WIP", Flags: []string{`\Drafts`}}, true},
	}
	for _, c := range cases {
		if got := ExcludedFromWaiting(c.mb); got != c.want {
			t.Errorf("ExcludedFromWaiting(%+v) = %v", c.mb, got)
		}
	}
	if !IsSentMailbox(Mailbox{Name: "[Gmail]/Sent Mail"}) || !IsSentMailbox(Mailbox{Name: "Out", Flags: []string{`\Sent`}}) || IsSentMailbox(Mailbox{Name: "INBOX"}) {
		t.Fatal("IsSentMailbox misclassifies")
	}
}

func TestListWaitingCandidatesSkipsExcludedFoldersAndBodies(t *testing.T) {
	d := newTestDB(t)
	acc, _ := d.AddAccount("", "Work", "")
	mk := func(name string, flags ...string) int64 {
		id, err := d.UpsertMailbox(Mailbox{AccountID: acc, Name: name, Delimiter: "/", Flags: flags})
		if err != nil {
			t.Fatal(err)
		}
		return id
	}
	inbox, sent, archive := mk("INBOX"), mk("Sent", `\Sent`), mk("Archive")
	trash, spam, drafts := mk("Trash", `\Trash`), mk("Spam"), mk("Drafts", `\Drafts`)
	uid := uint32(0)
	for _, mb := range []int64{inbox, sent, archive, trash, spam, drafts} {
		uid++
		if err := d.UpsertMessage(Message{MailboxID: mb, UID: uid, MessageID: "<m" + string(rune('0'+uid)) + "@x>",
			Subject: "s", From: "a@x", To: "b@y", BodyText: "BODY", BodyHTML: "<b>HTML</b>", Date: time.Unix(1000, 0)}); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := d.ListWaitingCandidates()
	if err != nil {
		t.Fatal(err)
	}
	var got []int64
	for _, m := range msgs {
		got = append(got, m.MailboxID)
		if m.BodyText != "" || m.BodyHTML != "" || m.Headers != "" {
			t.Fatalf("candidate carries body data: %+v", m)
		}
		if m.MessageID == "" || m.From == "" || m.To == "" || m.Date.IsZero() {
			t.Fatalf("candidate missing header data: %+v", m)
		}
	}
	sort.Slice(got, func(i, j int) bool { return got[i] < got[j] })
	if !reflect.DeepEqual(got, []int64{inbox, sent, archive}) {
		t.Fatalf("candidate mailboxes = %v", got)
	}
}

func TestListMessagesByIDsKeepsOrder(t *testing.T) {
	d, _, ids := newAnnotationTestDB(t, 3)
	msgs, err := d.ListMessagesByIDs([]int64{ids[2], 999999, ids[0]})
	if err != nil {
		t.Fatal(err)
	}
	if len(msgs) != 2 || msgs[0].ID != ids[2] || msgs[1].ID != ids[0] {
		t.Fatalf("got %+v", msgs)
	}
}

func TestWaitingKey(t *testing.T) {
	if got := WaitingKey(Message{MessageID: " <ABC@Host.example> "}); got != "<abc@host.example>" {
		t.Fatalf("key = %q", got)
	}
	if got := WaitingKey(Message{ID: 42}); got != "row:42" {
		t.Fatalf("key = %q", got)
	}
}

func TestWaitingStoppedPersistsAndPrunes(t *testing.T) {
	d, mailbox, ids := newAnnotationTestDB(t, 2)
	if _, err := d.Exec(`UPDATE messages SET message_id = '<Keep@x>' WHERE id = ?`, ids[0]); err != nil {
		t.Fatal(err)
	}
	keys := []string{"<keep@x>", "row:" + itoa(ids[1]), "<gone@x>", "row:999999"}
	if err := d.SetWaitingStopped(keys, true); err != nil {
		t.Fatal(err)
	}
	got, err := d.WaitingStopped()
	if err != nil || len(got) != 4 {
		t.Fatalf("stopped = %v %v", got, err)
	}
	// Dismissals for messages no longer cached are pruned; live ones stay.
	if err := d.PruneWaitingDismissals(); err != nil {
		t.Fatal(err)
	}
	got, _ = d.WaitingStopped()
	if !got["<keep@x>"] || !got["row:"+itoa(ids[1])] || got["<gone@x>"] || got["row:999999"] {
		t.Fatalf("after prune = %v", got)
	}
	// Resume removes it.
	if err := d.SetWaitingStopped([]string{"<keep@x>"}, false); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.WaitingStopped(); got["<keep@x>"] {
		t.Fatal("resume did not clear the dismissal")
	}
	// Deleting the message makes its dismissal prunable, not a crash.
	if err := d.DeleteMessage(ids[1]); err != nil {
		t.Fatal(err)
	}
	if err := d.PruneWaitingDismissals(); err != nil {
		t.Fatal(err)
	}
	if got, _ := d.WaitingStopped(); len(got) != 0 {
		t.Fatalf("stale dismissal kept: %v", got)
	}
	_ = mailbox
	if err := d.SetWaitingStopped([]string{" "}, true); err == nil {
		t.Fatal("empty key should be refused")
	}
}

func itoa(n int64) string {
	return WaitingKey(Message{ID: n})[len("row:"):]
}
