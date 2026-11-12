package db

import "testing"

func TestListAddressInboxMatchesOnlyRecipientOnAccount(t *testing.T) {
	database := newTestDB(t)
	first, err := database.AddAccount("first", "iCloud", "")
	if err != nil {
		t.Fatal(err)
	}
	second, err := database.AddAccount("second", "Other", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, accountID := range []int64{first, second} {
		inbox, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX"})
		if err != nil {
			t.Fatal(err)
		}
		for i, to := range []string{"Work <work@example.com>", "other@example.com", "notwork@example.com"} {
			if err := database.UpsertMessage(Message{MailboxID: inbox, UID: uint32(i + 1), To: to, Subject: to}); err != nil {
				t.Fatal(err)
			}
		}
	}
	got, err := database.ListAddressInbox(first, "work@example.com", false)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].To != "Work <work@example.com>" {
		t.Fatalf("view = %+v", got)
	}
}

func TestDraftSenderFromRoundTrip(t *testing.T) {
	database := newTestDB(t)
	id, err := database.SaveDraft(Draft{AccountConfigID: "icloud", SenderFrom: "work@example.com", To: "person@example.com"})
	if err != nil {
		t.Fatal(err)
	}
	draft, err := database.GetDraft(id)
	if err != nil || draft.SenderFrom != "work@example.com" {
		t.Fatalf("saved sender = %q, err = %v", draft.SenderFrom, err)
	}
	draft.SenderFrom = "personal@example.com"
	if _, err := database.SaveDraft(draft); err != nil {
		t.Fatal(err)
	}
	draft, err = database.GetDraft(id)
	if err != nil || draft.SenderFrom != "personal@example.com" {
		t.Fatalf("updated sender = %q, err = %v", draft.SenderFrom, err)
	}
}
