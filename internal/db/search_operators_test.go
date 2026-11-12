package db

import (
	"reflect"
	"sort"
	"testing"
)

func TestParseSearchOperators(t *testing.T) {
	cols := func(name string) []string { return searchFieldColumns[name] }
	for name, tc := range map[string]struct {
		query string
		want  SearchTerms
	}{
		"to":              {"to:work@example.com", SearchTerms{Fields: []FieldTerm{{cols("to"), "work@example.com", false}}, LastIsField: true}},
		"partial name":    {"to:wor", SearchTerms{Fields: []FieldTerm{{cols("to"), "wor", true}}, LastIsField: true}},
		"case, then text": {"FROM:jo invoice", SearchTerms{Text: "invoice", Fields: []FieldTerm{{cols("from"), "jo", false}}}},
		"quoted":          {`from:"Jo Smith" due`, SearchTerms{Text: "due", Fields: []FieldTerm{{cols("from"), "Jo Smith", false}}}},
		"unterminated":    {`subject:"hello wor`, SearchTerms{Fields: []FieldTerm{{cols("subject"), "hello wor", true}}, LastIsField: true}},
		"no value yet":    {"invoice to:", SearchTerms{Text: "invoice"}},
		"open quote only": {`to:"`, SearchTerms{}},
		"tag and field":   {"#billing cc:ann@example.com", SearchTerms{Tags: []string{"billing"}, Fields: []FieldTerm{{cols("cc"), "ann@example.com", false}}, LastIsField: true}},
		"unknown name":    {"re:hello", SearchTerms{Text: "re:hello"}},
		"two operators":   {"from:a to:b x", SearchTerms{Text: "x", Fields: []FieldTerm{{cols("from"), "a", false}, {cols("to"), "b", false}}}},
	} {
		t.Run(name, func(t *testing.T) {
			if got := ParseSearch(tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("ParseSearch(%q) = %+v, want %+v", tc.query, got, tc.want)
			}
		})
	}
}

func TestSearchByOperators(t *testing.T) {
	d := newTestDB(t)
	acct, _ := d.AddAccount("", "iCloud", "")
	other, _ := d.AddAccount("", "Other", "")
	box := func(id int64) int64 {
		mb, err := d.UpsertMailbox(Mailbox{AccountID: id, Name: "INBOX"})
		if err != nil {
			t.Fatal(err)
		}
		return mb
	}
	inbox, inbox2 := box(acct), box(other)
	var uid uint32
	add := func(mailbox int64, subject, from, to, cc string) {
		t.Helper()
		uid++
		if err := d.UpsertMessage(Message{MailboxID: mailbox, UID: uid, Subject: subject, From: from, To: to, CC: cc, BodyText: "body"}); err != nil {
			t.Fatal(err)
		}
	}
	add(inbox, "to work", "Pat <pat@friend.org>", "Work <work@example.com>", "")
	add(inbox, "cc work", "Pat <pat@friend.org>", "me@icloud.com", "work@example.com")
	add(inbox, "to personal", "Jo Smith <jo@shop.net>", "me@icloud.com", "")
	add(inbox, "to company", "Pat <pat@friend.org>", "work@example.company", "")
	add(inbox2, "other account work", "Sam <sam@x.io>", "Work <work@example.com>", "")
	add(inbox, "mentions work@example.com in subject only", "Kim <kim@x.io>", "me@icloud.com", "")

	search := func(q string) []string {
		t.Helper()
		msgs, err := d.SearchAllMessages(q, false)
		if err != nil {
			t.Fatalf("%q: %v", q, err)
		}
		var out []string
		for _, m := range msgs {
			out = append(out, m.Subject)
		}
		sort.Strings(out)
		return out
	}
	for name, tc := range map[string]struct {
		query string
		want  []string
	}{
		"to covers To and Cc":       {"to:work@example.com", []string{"cc work", "other account work", "to work"}},
		"finished address is exact": {"to:work@example.com", []string{"cc work", "other account work", "to work"}}, // not work@example.company
		"cc is Cc alone":            {"cc:work@example.com", []string{"cc work"}},
		"from":                      {"from:pat@friend.org", []string{"cc work", "to company", "to work"}},
		"quoted name":               {`from:"Jo Smith"`, []string{"to personal"}},
		"subject":                   {"subject:personal", []string{"to personal"}},
		"field plus free text":      {"from:pat@friend.org company", []string{"to company"}},
		"free text after":           {"to:work@example.com zzz", nil},
		"prefix while typing":       {"to:wor", []string{"cc work", "other account work", "to company", "to work"}},
		"nothing typed yet":         {"to:", nil},
		"punctuation only":          {"to:@", nil},
	} {
		t.Run(name, func(t *testing.T) {
			if got := search(tc.query); !reflect.DeepEqual(got, tc.want) {
				t.Fatalf("search %q = %v, want %v", tc.query, got, tc.want)
			}
		})
	}
}
