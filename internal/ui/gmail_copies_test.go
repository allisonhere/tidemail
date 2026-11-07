package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// Found driving a real Gmail account: a deleted message stayed in All Mail,
// even across a restart, until All Mail itself was synced. Gmail shows one
// message under several folders and trashing it removes it from all of them.
func TestGmailDeleteHidesAndEvictsOtherFolderCopies(t *testing.T) {
	for _, gmail := range []bool{true, false} {
		name := "other-server"
		if gmail {
			name = "gmail"
		}
		t.Run(name, func(t *testing.T) {
			database, err := db.OpenPath(t.TempDir() + "/mail.db")
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() { database.Close() })
			accountID, err := database.AddAccount("", "acct", "")
			if err != nil {
				t.Fatal(err)
			}
			mailbox := func(name string, flags ...string) db.Mailbox {
				id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: name, Flags: flags})
				if err != nil {
					t.Fatal(err)
				}
				return db.Mailbox{ID: id, AccountID: accountID, Name: name, Flags: flags}
			}
			inbox, allMail, trash := mailbox("INBOX"), mailbox("[Gmail]/All Mail", "\\All"), mailbox("[Gmail]/Trash", "\\Trash")
			put := func(mb db.Mailbox, uid uint32, messageID, subject string) db.Message {
				if err := database.UpsertMessage(db.Message{MailboxID: mb.ID, UID: uid, MessageID: messageID, Subject: subject}); err != nil {
					t.Fatal(err)
				}
				list, err := database.ListMessages(mb.ID)
				if err != nil {
					t.Fatal(err)
				}
				for _, m := range list {
					if m.UID == uid {
						return m
					}
				}
				t.Fatal("message not stored")
				return db.Message{}
			}
			inboxCopy := put(inbox, 1, "<E2E-G-08@test>", "E2E-G 08")
			allMailCopy := put(allMail, 101, "<e2e-g-08@test>", "E2E-G 08") // case differs, same message
			other := put(allMail, 102, "<other@test>", "unrelated")

			account := config.AccountConfig{Name: "acct", IMAPHost: "mail.example.com"}
			if gmail {
				account = config.AccountConfig{Name: "acct", Provider: "Gmail", IMAPHost: "imap.gmail.com"}
			}
			m := NewModel(database, config.DefaultConfig(), "dev", false)
			m.accounts = []db.Account{{ID: accountID, Name: "acct"}}
			m.mailboxes = []db.Mailbox{inbox, allMail, trash}
			m.pendingDestructiveActions = []pendingDestructiveAction{{ID: 1, Kind: destructiveDelete, Entries: []pendingDestructiveEntry{
				{Message: inboxCopy, Source: inbox, Target: trash, Account: account},
			}}}
			// Viewing All Mail while the delete is pending.
			m.messages = []db.Message{allMailCopy, other}
			m.applyFilter()
			hidden := m.indexOfFilteredMessage(allMailCopy.ID) < 0
			if hidden != gmail {
				t.Fatalf("All Mail copy hidden=%v, want %v", hidden, gmail)
			}
			if m.indexOfFilteredMessage(other.ID) < 0 {
				t.Fatal("an unrelated message was hidden")
			}

			result := executeDestructiveBatches(m.pendingDestructiveActions[0], database, func([]pendingDestructiveEntry) error { return nil })
			_, err = database.GetMessage(allMailCopy.ID)
			stillCached := err == nil
			if stillCached == gmail {
				t.Fatalf("All Mail copy still cached=%v after the delete; want %v", stillCached, !gmail)
			}
			if gmail && (len(result.Evicted) != 1 || result.Evicted[0].ID != allMailCopy.ID) {
				t.Fatalf("evicted %+v, want only the All Mail copy", result.Evicted)
			}
			if !gmail && len(result.Evicted) != 0 {
				t.Fatalf("a non-Gmail delete evicted other folders' copies: %+v", result.Evicted)
			}
			if known, _ := database.KnownMessageUIDs(allMail.ID); gmail && known[101] {
				t.Fatal("eviction left a tombstone: a restore on Gmail could never sync back")
			}
			m.handleDestructiveResult(result)
			if gmail && m.indexOfFilteredMessage(allMailCopy.ID) >= 0 {
				t.Fatal("the All Mail copy came back once the delete finished")
			}
			if !gmail && m.indexOfFilteredMessage(allMailCopy.ID) < 0 {
				t.Fatal("a non-Gmail copy in another folder must stay")
			}
		})
	}
}
