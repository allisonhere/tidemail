package db

import (
	"path/filepath"
	"testing"
	"time"
)

func TestScopedFiltersBeforePagination(t *testing.T) {
	d, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer d.Close()
	if err = d.init(); err != nil {
		t.Fatal(err)
	}
	a, err := d.AddAccount("work", "Work", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name  string
		flags []string
	}{
		{"INBOX", nil}, {"INBOX.Deleted Messages", nil}, {"Localized", []string{"\\Trash"}}, {"Junk", nil}, {"INBOX/Junk E-mail", nil}, {"Trash", []string{"\\Archive"}},
	} {
		mb, e := d.UpsertMailbox(Mailbox{AccountID: a, Name: tc.name, Flags: tc.flags})
		if e != nil {
			t.Fatal(e)
		}
		for i := 1; i <= 3; i++ {
			if e = d.UpsertMessage(Message{MailboxID: mb, UID: uint32(i), Subject: "invoice", Date: time.Unix(int64(i*100), 0), Starred: i == 2}); e != nil {
				t.Fatal(e)
			}
		}
	}
	for _, tc := range []struct {
		name  string
		scope MessageScope
		total int
	}{
		{"exclude", MessageScope{}, 6},
		{"include", MessageScope{IncludeTrash: true}, 18},
		{"explicit", MessageScope{Mailboxes: []string{"INBOX.Deleted Messages", "Localized"}}, 6},
		{"date and star", MessageScope{Starred: true, Since: time.Unix(200, 0), Before: time.Unix(300, 0)}, 2},
		{"before exclusive", MessageScope{Before: time.Unix(200, 0)}, 2},
		{"account", MessageScope{AccountID: "absent"}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			page, total, e := d.ScopedMessagesPage("", tc.scope, false, 1, 0)
			if e != nil || total != tc.total || len(page) != min(1, tc.total) {
				t.Fatalf("page=%v total=%d err=%v", page, total, e)
			}
			if len(page) > 0 && (page[0].AccountConfigID != "work" || page[0].MailboxRawName == "") {
				t.Fatalf("missing joined identity: %+v", page[0])
			}
		})
	}
	// Date sort must override FTS ranking and use a stable ID tie breaker.
	page, total, err := d.ScopedMessagesPage("invoice", MessageScope{Sort: "date"}, false, 20, 0)
	if err != nil || total != 6 {
		t.Fatalf("total=%d err=%v", total, err)
	}
	for i := 1; i < len(page); i++ {
		if page[i].Date.After(page[i-1].Date) || (page[i].Date.Equal(page[i-1].Date) && page[i].ID > page[i-1].ID) {
			t.Fatal("unstable date ordering")
		}
	}
}
