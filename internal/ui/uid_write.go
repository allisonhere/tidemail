package ui

import (
	"context"

	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
)

// uidWriteContext tags ctx with the UIDVALIDITY that the mailbox's cached UIDs
// belong to, so an IMAP write by UID refuses to run if the server has
// renumbered the mailbox since the last sync (the UID would now name a
// different message). A mailbox not yet synced has no stored value and is
// written without the check, as before.
func uidWriteContext(ctx context.Context, database *db.DB, mailboxID int64, mailboxName string) context.Context {
	if database == nil || mailboxID == 0 {
		return ctx
	}
	validity, err := database.MailboxUIDValidity(mailboxID)
	if err != nil || validity == 0 {
		return ctx
	}
	return imapClient.WithUIDValidity(ctx, mailboxName, validity)
}
