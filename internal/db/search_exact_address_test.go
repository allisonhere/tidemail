package db

import (
	"reflect"
	"sort"
	"testing"
)

// A finished address matches that address only. FTS phrase matching alone also
// matched longer addresses that merely start with it (work@example.com.au).
func TestCompleteAddressSearchIsExact(t *testing.T) {
	d := newTestDB(t)
	acct, _ := d.AddAccount("", "Work", "")
	mb, err := d.UpsertMailbox(Mailbox{AccountID: acct, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	var uid uint32
	add := func(subject, to string) {
		t.Helper()
		uid++
		if err := d.UpsertMessage(Message{MailboxID: mb, UID: uid, Subject: subject, To: to, From: "x@y.org", BodyText: "b"}); err != nil {
			t.Fatal(err)
		}
	}
	add("bare", "work@example.com")
	add("named", `"Doe, Work" <Work@Example.com>`)
	add("list", "a@b.org, Work <work@example.com>, c@d.org")
	add("longer tld", "Work <work@example.com.au>")
	add("longer tld mx", "work@example.com.mx")
	add("other host", "work@example.company")
	add("longer local", "network-work@example.com")

	msgs, err := d.SearchAllMessages("to:work@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	var got []string
	for _, m := range msgs {
		got = append(got, m.Subject)
	}
	sort.Strings(got)
	if want := []string{"bare", "list", "named"}; !reflect.DeepEqual(got, want) {
		t.Fatalf("to:work@example.com = %v, want %v", got, want)
	}
}
