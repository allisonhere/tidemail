package db

import (
	"path/filepath"
	"reflect"
	"testing"

	"github.com/allisonhere/tidemail/internal/mailtag"
)

func TestMailTagsSyncMappingAndSearch(t *testing.T) {
	a := newAttentionDB(t)
	id := a.add(a.inbox, "tagged", 1, "smart category billing")
	other := a.add(a.inbox2, "other account", 2)
	message, _ := a.GetMessage(id)
	second, _ := a.GetMessage(other)
	if err := a.ApplyServerFlags(a.inbox, map[uint32][]string{message.UID: {`\Seen`, "$label4", "Project-X", "literal_percent%", "$Junk"}}); err != nil {
		t.Fatal(err)
	}
	// Percent is an invalid IMAP keyword, and must not become a tag.
	if err := a.ApplyServerFlags(a.inbox2, map[uint32][]string{second.UID: {"Project-X"}}); err != nil {
		t.Fatal(err)
	}
	tags, err := a.ListMailTags()
	if err != nil {
		t.Fatal(err)
	}
	if len(tags) != 3 {
		t.Fatalf("definitions = %+v", tags)
	}
	var accountID int64
	for _, tag := range tags {
		if tag.Keyword == "$label4" {
			accountID = tag.AccountID
		}
	}
	if err := a.SaveMailTag(mailtag.Definition{AccountID: accountID, Keyword: "project-x", Name: "Client Work", Color: "#abc"}); err != nil {
		t.Fatal(err)
	}
	for query, want := range map[string][]string{
		"#$label4": {"tagged"}, `#"To Do"`: {"tagged"}, `#"To D`: {"tagged"},
		`#"Client Work"`: {"tagged"}, "#project-x": {"other account", "tagged"},
		"#billing #project": {"tagged"}, "#junk": {}, "#literal": {},
	} {
		if got := a.search(query); !reflect.DeepEqual(got, want) {
			t.Errorf("%q = %q, want %q", query, got, want)
		}
	}
	if err := a.ApplyServerFlags(a.inbox, map[uint32][]string{message.UID: {`\Seen`}}); err != nil {
		t.Fatal(err)
	}
	if got := a.search(`#"Client Work"`); len(got) != 0 {
		t.Fatalf("removed tag still matches: %v", got)
	}
	if got := a.search("#billing"); !reflect.DeepEqual(got, []string{"tagged"}) {
		t.Fatalf("classification lost: %v", got)
	}
	tags, _ = a.ListMailTags()
	found := false
	for _, tag := range tags {
		if tag.AccountID == accountID && tag.Keyword == "Project-X" {
			found = tag.Name == "Client Work" && tag.Color == "#aabbcc"
		}
	}
	if !found {
		t.Fatalf("mapping or original keyword spelling lost: %+v", tags)
	}
}

func TestMailTagBackfillAndRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "mail.db")
	database, err := OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	accountID, _ := database.AddAccount("", "Test", "")
	mailboxID, _ := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 1, Flags: []string{"$label1", "custom"}}); err != nil {
		t.Fatal(err)
	}
	// Simulate an older database with flags but without discovered definitions.
	if _, err := database.Exec(`DROP TABLE mail_tags`); err != nil {
		t.Fatal(err)
	}
	if _, err := database.Exec(`DELETE FROM settings WHERE key='mail_tags_backfill_v1'`); err != nil {
		t.Fatal(err)
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	database, err = OpenPath(path)
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	tags, err := database.ListMailTags()
	if err != nil || len(tags) != 2 {
		t.Fatalf("backfill = %+v, %v", tags, err)
	}
	if err := database.SaveMailTag(mailtag.Definition{AccountID: accountID, Keyword: "$label1", Name: "Critical", Color: "#123456"}); err != nil {
		t.Fatal(err)
	}
	if err := database.DiscoverCachedMailTags(); err != nil {
		t.Fatal(err)
	}
	tags, _ = database.ListMailTags()
	for _, tag := range tags {
		if tag.Keyword == "$label1" && tag.Name != "Critical" {
			t.Fatal("rediscovery overwrote user mapping")
		}
	}
}

func TestRefreshMailFlagsRejectsStaleListSnapshot(t *testing.T) {
	a := newAttentionDB(t)
	id := a.add(a.inbox, "message", 1)
	message, _ := a.GetMessage(id)
	stale := []Message{message}
	if err := a.ApplyServerFlags(a.inbox, map[uint32][]string{message.UID: {"$label2"}}); err != nil {
		t.Fatal(err)
	}
	if err := a.RefreshMailFlags(stale); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(stale[0].Flags, []string{"$label2"}) {
		t.Fatalf("stale snapshot survived: %+v", stale)
	}
}

func TestMailTagSearchUnicodeAndWildcards(t *testing.T) {
	a := newAttentionDB(t)
	id := a.add(a.inbox, "unicode", 1)
	message, _ := a.GetMessage(id)
	if err := a.ApplyServerFlags(a.inbox, map[uint32][]string{message.UID: {"Project-X"}}); err != nil {
		t.Fatal(err)
	}
	var accountID int64
	if err := a.QueryRow(`SELECT account_id FROM mailboxes WHERE id=?`, a.inbox).Scan(&accountID); err != nil {
		t.Fatal(err)
	}
	if err := a.SaveMailTag(mailtag.Definition{AccountID: accountID, Keyword: "Project-X", Name: `À faire_100%`}); err != nil {
		t.Fatal(err)
	}
	if got := a.search(`#"À FAIRE_100%"`); !reflect.DeepEqual(got, []string{"unicode"}) {
		t.Fatalf("unicode/wildcards: %v", got)
	}
	if got := a.search(`#"à faireX100"`); len(got) != 0 {
		t.Fatalf("underscore became wildcard: %v", got)
	}
	if err := a.SaveMailTag(mailtag.Definition{AccountID: accountID, Keyword: "Project-X", Name: `Client "A"`}); err != nil {
		t.Fatal(err)
	}
	if got := a.search(`#"Client \"A\""`); !reflect.DeepEqual(got, []string{"unicode"}) {
		t.Fatalf("escaped quotes: %v", got)
	}
}
