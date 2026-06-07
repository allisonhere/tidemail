package ui

import (
	"context"
	"fmt"
	"strings"

	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/filter"
	imapClient "github.com/allisonhere/tide/internal/imap"
)

// applyFilterAction performs a rule's action on msg, mutating both the IMAP
// server (when client != nil and the message has a UID) and the local database.
// It resolves — and for move/spam creates — the destination folder as needed.
// client may be nil for local-only accounts (no IMAPHost) and dry tests.
func applyFilterAction(ctx context.Context, database *db.DB, client *imapClient.Client, source db.Mailbox, msg db.Message, action filter.Action) error {
	switch action.Type {
	case filter.ActionMarkRead:
		if client != nil && msg.UID != 0 {
			if err := client.MarkSeen(ctx, source.Name, msg.UID, true); err != nil {
				return err
			}
		}
		return database.MarkRead(msg.ID, true)

	case filter.ActionArchive:
		target, err := database.FindArchiveMailbox(source.AccountID)
		if err != nil {
			return err
		}
		return moveMessage(ctx, database, client, source, msg, target)

	case filter.ActionSpam:
		target, err := database.FindJunkMailbox(source.AccountID)
		if err != nil {
			target, err = resolveOrCreateFolder(ctx, database, client, source.AccountID, "Junk")
			if err != nil {
				return err
			}
		}
		return moveMessage(ctx, database, client, source, msg, target)

	case filter.ActionMove:
		target, err := resolveOrCreateFolder(ctx, database, client, source.AccountID, action.Target)
		if err != nil {
			return err
		}
		return moveMessage(ctx, database, client, source, msg, target)

	case filter.ActionDelete:
		var trash *db.Mailbox
		if t, err := database.FindTrashMailbox(source.AccountID); err == nil {
			trash = &t
		}
		if err := database.DeleteMessage(msg.ID); err != nil {
			return err
		}
		if client != nil && msg.UID != 0 {
			plan, target := remoteDeletePlan(source, trash)
			if plan == remoteDeleteMoveToTrash {
				return client.MoveMessage(ctx, source.Name, msg.UID, target.Name)
			}
			return client.DeleteMessage(ctx, source.Name, msg.UID)
		}
		return nil

	default:
		return fmt.Errorf("unknown filter action %q", action.Type)
	}
}

func moveMessage(ctx context.Context, database *db.DB, client *imapClient.Client, source db.Mailbox, msg db.Message, target db.Mailbox) error {
	if target.ID == source.ID {
		return nil
	}
	if client != nil && msg.UID != 0 {
		if err := client.MoveMessage(ctx, source.Name, msg.UID, target.Name); err != nil {
			return err
		}
	}
	return database.MoveMessage(msg.ID, target.ID)
}

// resolveOrCreateFolder returns the account's mailbox named name (case
// insensitive), creating it on the server (when client != nil) and locally when
// it does not yet exist.
func resolveOrCreateFolder(ctx context.Context, database *db.DB, client *imapClient.Client, accountID int64, name string) (db.Mailbox, error) {
	mbs, err := database.ListMailboxes(accountID)
	if err != nil {
		return db.Mailbox{}, err
	}
	for _, mb := range mbs {
		if strings.EqualFold(mb.Name, name) || strings.EqualFold(mb.DisplayName, name) {
			return mb, nil
		}
	}
	delimiter := "/"
	for _, mb := range mbs {
		if mb.Delimiter != "" {
			delimiter = mb.Delimiter
			break
		}
	}
	if client != nil {
		if err := client.CreateMailbox(ctx, name); err != nil {
			return db.Mailbox{}, err
		}
	}
	id, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: name, Delimiter: delimiter})
	if err != nil {
		return db.Mailbox{}, err
	}
	return db.Mailbox{ID: id, AccountID: accountID, Name: name, Delimiter: delimiter}, nil
}
