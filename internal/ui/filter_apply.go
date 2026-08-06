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
//
// It returns acted=true when the message was actually changed (moved, deleted,
// marked, ...). acted=false with a nil error means the action was a no-op.
func applyFilterAction(ctx context.Context, database *db.DB, client *imapClient.Client, source db.Mailbox, msg db.Message, action filter.Action) (bool, error) {
	switch action.Type {
	case filter.ActionMarkRead:
		if client != nil && msg.UID != 0 {
			if err := client.MarkSeen(ctx, source.Name, msg.UID, true); err != nil {
				return false, err
			}
		}
		if err := database.MarkRead(msg.ID, true); err != nil {
			return false, err
		}
		return true, nil

	case filter.ActionArchive:
		target, err := database.FindArchiveMailbox(source.AccountID)
		if err != nil {
			return false, err
		}
		return moveMessage(ctx, database, client, source, msg, target)

	case filter.ActionSpam:
		target, err := database.FindJunkMailbox(source.AccountID)
		if err != nil {
			target, err = resolveOrCreateFolder(ctx, database, client, source.AccountID, "Junk")
			if err != nil {
				return false, err
			}
		}
		return moveMessage(ctx, database, client, source, msg, target)

	case filter.ActionMove:
		target, err := resolveOrCreateFolder(ctx, database, client, source.AccountID, action.Target)
		if err != nil {
			return false, err
		}
		return moveMessage(ctx, database, client, source, msg, target)

	case filter.ActionDelete:
		var trash *db.Mailbox
		if t, err := database.FindTrashMailbox(source.AccountID); err == nil {
			trash = &t
		}
		if err := database.DeleteMessage(msg.ID); err != nil {
			return false, err
		}
		if client != nil && msg.UID != 0 {
			plan, target := remoteDeletePlan(source, trash)
			if plan == remoteDeleteMoveToTrash {
				if err := client.MoveMessage(ctx, source.Name, msg.UID, target.Name); err != nil {
					return false, err
				}
			} else if err := client.DeleteMessage(ctx, source.Name, msg.UID); err != nil {
				return false, err
			}
		}
		return true, nil

	default:
		return false, fmt.Errorf("unknown filter action %q", action.Type)
	}
}

// moveMessage moves msg from source to target. It returns acted=true when a move
// actually happened (false when target == source, a no-op).
func moveMessage(ctx context.Context, database *db.DB, client *imapClient.Client, source db.Mailbox, msg db.Message, target db.Mailbox) (bool, error) {
	if target.ID == source.ID {
		return false, nil
	}
	if client != nil && msg.UID != 0 {
		if err := client.MoveMessage(ctx, source.Name, msg.UID, target.Name); err != nil {
			return false, err
		}
	}
	if err := database.MoveMessage(msg.ID, target.ID); err != nil {
		return false, err
	}
	return true, nil
}

// resolveFolder returns the account's mailbox named name (case insensitive)
// without creating it. found is false when no such folder exists.
func resolveFolder(database *db.DB, accountID int64, name string) (db.Mailbox, bool, error) {
	mbs, err := database.ListMailboxes(accountID)
	if err != nil {
		return db.Mailbox{}, false, err
	}
	for _, mb := range mbs {
		if strings.EqualFold(mb.Name, name) || strings.EqualFold(mb.DisplayName, name) {
			return mb, true, nil
		}
	}
	return db.Mailbox{}, false, nil
}

// resolveOrCreateFolder returns the account's mailbox named name (case
// insensitive), creating it on the server (when client != nil) and locally when
// it does not yet exist.
func resolveOrCreateFolder(ctx context.Context, database *db.DB, client *imapClient.Client, accountID int64, name string) (db.Mailbox, error) {
	if mb, found, err := resolveFolder(database, accountID, name); err != nil || found {
		return mb, err
	}
	mbs, err := database.ListMailboxes(accountID)
	if err != nil {
		return db.Mailbox{}, err
	}
	// Servers with an "INBOX." personal namespace (Dovecot, Courier) reject a
	// bare top-level CREATE, so qualify the name before asking for it.
	fullName, delimiter := qualifyFolderName(name, mbs)
	if client != nil {
		if err := client.CreateMailbox(ctx, fullName); err != nil {
			return db.Mailbox{}, err
		}
	}
	mailbox := db.Mailbox{
		AccountID:   accountID,
		Name:        fullName,
		DisplayName: cleanDisplayName(fullName),
		Delimiter:   delimiter,
	}
	id, err := database.UpsertMailbox(mailbox)
	if err != nil {
		return db.Mailbox{}, err
	}
	mailbox.ID = id
	return mailbox, nil
}
