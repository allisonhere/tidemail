package db

import (
	"path/filepath"
	"testing"
	"time"

	_ "modernc.org/sqlite"
)

func openDraftTestDB(t *testing.T) *DB {
	t.Helper()
	database, err := openSQLite(filepath.Join(t.TempDir(), "mail.db"))
	if err != nil {
		t.Fatal(err)
	}
	if err := database.init(); err != nil {
		database.Close()
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestDBSaveGetUpdateAndDeleteDraft(t *testing.T) {
	database := openDraftTestDB(t)

	created := time.Unix(1710000000, 0)
	updated := time.Unix(1710000100, 0)
	id, err := database.SaveDraft(Draft{
		AccountName:     "Personal",
		AccountUser:     "allie@example.com",
		AccountIndex:    1,
		MailboxID:       42,
		RemoteUID:       99,
		RemoteMessageID: "<draft@example.com>",
		To:              "bob@example.com",
		CC:              "carol@example.com",
		Subject:         "Draft subject",
		BodyText:        "Draft body",
		InReplyTo:       "<orig@example.com>",
		References:      "<orig@example.com>",
		CreatedAt:       created,
		UpdatedAt:       updated,
		LastRemoteSync:  time.Unix(1710000050, 0),
		Dirty:           true,
		Attachments: []DraftAttachment{
			{Filename: "one.txt", Path: "/tmp/one.txt", ContentType: "text/plain", Data: []byte("one"), Size: 3, Position: 0},
			{Filename: "two.txt", Data: []byte("two"), Size: 3, Position: 1},
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	draft, err := database.GetDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	if draft.ID != id || draft.AccountName != "Personal" || draft.AccountUser != "allie@example.com" || draft.AccountIndex != 1 {
		t.Fatalf("unexpected draft identity: %+v", draft)
	}
	if draft.To != "bob@example.com" || draft.CC != "carol@example.com" || draft.Subject != "Draft subject" || draft.BodyText != "Draft body" {
		t.Fatalf("unexpected draft content: %+v", draft)
	}
	if draft.MailboxID != 42 || draft.RemoteUID != 99 || draft.RemoteMessageID != "<draft@example.com>" || !draft.Dirty {
		t.Fatalf("unexpected remote/dirty fields: %+v", draft)
	}
	if !draft.CreatedAt.Equal(created) || !draft.UpdatedAt.Equal(updated) {
		t.Fatalf("unexpected timestamps: created=%v updated=%v", draft.CreatedAt, draft.UpdatedAt)
	}
	if len(draft.Attachments) != 2 || string(draft.Attachments[0].Data) != "one" || draft.Attachments[1].Filename != "two.txt" {
		t.Fatalf("unexpected attachments: %+v", draft.Attachments)
	}

	draft.Subject = "Updated"
	draft.BodyText = "Updated body"
	draft.Attachments = []DraftAttachment{{Filename: "three.txt", Data: []byte("three"), Size: 5, Position: 0}}
	if _, err := database.SaveDraft(draft); err != nil {
		t.Fatal(err)
	}
	updatedDraft, err := database.GetDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	if updatedDraft.Subject != "Updated" || updatedDraft.BodyText != "Updated body" {
		t.Fatalf("draft was not updated: %+v", updatedDraft)
	}
	if len(updatedDraft.Attachments) != 1 || updatedDraft.Attachments[0].Filename != "three.txt" {
		t.Fatalf("attachments were not replaced: %+v", updatedDraft.Attachments)
	}

	if err := database.DeleteDraft(id); err != nil {
		t.Fatal(err)
	}
	if _, err := database.GetDraft(id); err == nil {
		t.Fatal("expected deleted draft lookup to fail")
	}
	var attachmentCount int
	if err := database.QueryRow(`SELECT COUNT(*) FROM draft_attachments WHERE draft_id = ?`, id).Scan(&attachmentCount); err != nil {
		t.Fatal(err)
	}
	if attachmentCount != 0 {
		t.Fatalf("expected draft attachments deleted, got %d", attachmentCount)
	}
}

func TestDBListDraftsSortsByUpdatedAndFiltersAccount(t *testing.T) {
	database := openDraftTestDB(t)

	older, err := database.SaveDraft(Draft{AccountName: "Personal", AccountUser: "allie@example.com", Subject: "older", UpdatedAt: time.Unix(10, 0)})
	if err != nil {
		t.Fatal(err)
	}
	newer, err := database.SaveDraft(Draft{AccountName: "Personal", AccountUser: "allie@example.com", Subject: "newer", UpdatedAt: time.Unix(20, 0)})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := database.SaveDraft(Draft{AccountName: "Work", AccountUser: "allie@work.example", Subject: "hidden", UpdatedAt: time.Unix(30, 0)}); err != nil {
		t.Fatal(err)
	}

	drafts, err := database.ListDrafts("Personal", "allie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 2 {
		t.Fatalf("expected two drafts, got %+v", drafts)
	}
	if drafts[0].ID != newer || drafts[1].ID != older {
		t.Fatalf("expected newest first, got %+v", drafts)
	}

	count, err := database.DraftCount("Personal", "allie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if count != 2 {
		t.Fatalf("expected draft count 2, got %d", count)
	}
}

func TestDBFindDraftsMailboxPrefersSpecialUseThenCommonNames(t *testing.T) {
	database := openDraftTestDB(t)
	accountID, err := database.AddAccount("Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	plainID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "INBOX/Drafts", DisplayName: "Drafts"})
	if err != nil {
		t.Fatal(err)
	}
	specialID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "[Gmail]/Drafts", Flags: []string{"\\Drafts"}})
	if err != nil {
		t.Fatal(err)
	}

	got, err := database.FindDraftsMailbox(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != specialID {
		t.Fatalf("expected special-use drafts mailbox %d, got %+v", specialID, got)
	}

	if err := database.DeleteMailbox(specialID); err != nil {
		t.Fatal(err)
	}
	got, err = database.FindDraftsMailbox(accountID)
	if err != nil {
		t.Fatal(err)
	}
	if got.ID != plainID {
		t.Fatalf("expected common-name drafts mailbox %d, got %+v", plainID, got)
	}
}

func TestSaveDraftUpdatePreservesRemoteLinkage(t *testing.T) {
	database := openDraftTestDB(t)

	id, err := database.SaveDraft(Draft{
		AccountName:     "Acct",
		AccountUser:     "a@x",
		MailboxID:       7,
		RemoteUID:       42,
		RemoteMessageID: "<remote@x>",
		Subject:         "Original",
		BodyText:        "Original body",
		CreatedAt:       time.Unix(1710000000, 0),
		LastRemoteSync:  time.Unix(1710000050, 0),
	})
	if err != nil {
		t.Fatal(err)
	}

	// An autosave rebuilds the draft from compose state: no mailbox, remote
	// linkage, or creation time. Those must survive the update.
	if _, err := database.SaveDraft(Draft{
		ID:          id,
		AccountName: "Acct",
		AccountUser: "a@x",
		Subject:     "Edited",
		BodyText:    "Edited body",
		Dirty:       true,
	}); err != nil {
		t.Fatal(err)
	}

	d, err := database.GetDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	if d.Subject != "Edited" || d.BodyText != "Edited body" || !d.Dirty {
		t.Fatalf("expected content update, got %+v", d)
	}
	if d.MailboxID != 7 || d.RemoteUID != 42 || d.RemoteMessageID != "<remote@x>" {
		t.Fatalf("expected remote linkage preserved, got %+v", d)
	}
	if d.CreatedAt.Unix() != 1710000000 || d.LastRemoteSync.Unix() != 1710000050 {
		t.Fatalf("expected timestamps preserved, got created=%v lastSync=%v", d.CreatedAt, d.LastRemoteSync)
	}
}

func TestImportRemoteDraftIsIdempotentAndTracksUnmirrored(t *testing.T) {
	database := openDraftTestDB(t)

	accountID, err := database.AddAccount("Acct", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(Mailbox{AccountID: accountID, Name: "Drafts", Flags: []string{"\\Drafts"}})
	if err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 1, MessageID: "<m1@x>", Subject: "one"}); err != nil {
		t.Fatal(err)
	}
	if err := database.UpsertMessage(Message{MailboxID: mailboxID, UID: 2, MessageID: "<m2@x>", Subject: "two"}); err != nil {
		t.Fatal(err)
	}

	if n, err := database.UnmirroredDraftMessageCount(mailboxID); err != nil || n != 2 {
		t.Fatalf("expected 2 unmirrored before import, got %d err=%v", n, err)
	}

	mirror := Draft{AccountName: "Acct", MailboxID: mailboxID, RemoteUID: 1, RemoteMessageID: "<m1@x>", Subject: "one"}
	if err := database.ImportRemoteDraft(mirror); err != nil {
		t.Fatal(err)
	}
	// Importing the same remote draft again (e.g. a racing second sync) must be a
	// no-op, not a duplicate — guards the unique mirror index.
	if err := database.ImportRemoteDraft(mirror); err != nil {
		t.Fatal(err)
	}
	if n, err := database.DraftCount("Acct", ""); err != nil || n != 1 {
		t.Fatalf("expected exactly 1 mirrored draft after double import, got %d err=%v", n, err)
	}
	if n, err := database.UnmirroredDraftMessageCount(mailboxID); err != nil || n != 1 {
		t.Fatalf("expected 1 unmirrored after import, got %d err=%v", n, err)
	}
}
