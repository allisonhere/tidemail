package imap

import (
	"context"
	"fmt"
	"strings"

	"github.com/allisonhere/tidemail/internal/mailtag"
	"github.com/emersion/go-imap/v2"
)

// SetKeyword changes exactly one keyword, and returns confirmed server flags.
// UIDVALIDITY must match the cached mailbox before a UID is used for a write.
func (c *Client) SetKeyword(ctx context.Context, mailbox string, uid, uidValidity uint32, keyword string, enabled bool) ([]string, error) {
	if err := mailtag.ValidateKeyword(keyword); err != nil {
		return nil, err
	}
	if uid == 0 || uidValidity == 0 {
		return nil, fmt.Errorf("sync this mailbox before editing tags")
	}
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}
	defer c.applyDeadline(ctx)()
	selected, err := c.conn.Select(mailbox, nil).Wait()
	if err != nil {
		return nil, fmt.Errorf("select %s for writing: %w", mailbox, err)
	}
	if selected.UIDValidity != uidValidity {
		return nil, fmt.Errorf("mailbox identifiers changed; sync before editing tags")
	}
	if !keywordWritable(selected.PermanentFlags, keyword) {
		return nil, fmt.Errorf("this mailbox does not allow saving keyword %q (it may be read-only)", keyword)
	}
	// Fetch first: STORE of a vanished UID can otherwise succeed as a no-op.
	if _, err := c.keywordFlags(uid); err != nil {
		return nil, err
	}
	op := imap.StoreFlagsDel
	if enabled {
		op = imap.StoreFlagsAdd
	}
	err = c.conn.Store(imap.UIDSetNum(imap.UID(uid)), &imap.StoreFlags{Op: op, Silent: true, Flags: []imap.Flag{imap.Flag(keyword)}}, nil).Close()
	if err != nil {
		return nil, fmt.Errorf("tag change was not confirmed; sync to check its state: %w", err)
	}
	flags, err := c.keywordFlags(uid)
	if err != nil {
		return nil, fmt.Errorf("tag change was not confirmed; sync to check its state: %w", err)
	}
	if mailtag.Has(flags, keyword) != enabled {
		return nil, fmt.Errorf("server did not save the tag change")
	}
	return flags, nil
}

func keywordWritable(permanent []imap.Flag, keyword string) bool {
	for _, flag := range permanent {
		if flag == imap.FlagWildcard || strings.EqualFold(string(flag), keyword) {
			return true
		}
	}
	return false
}

func (c *Client) keywordFlags(uid uint32) ([]string, error) {
	msgs, err := c.conn.Fetch(imap.UIDSetNum(imap.UID(uid)), &imap.FetchOptions{UID: true, Flags: true}).Collect()
	if err != nil {
		return nil, err
	}
	if len(msgs) != 1 || uint32(msgs[0].UID) != uid {
		return nil, fmt.Errorf("message is no longer in this mailbox")
	}
	flags := make([]string, len(msgs[0].Flags))
	for i, flag := range msgs[0].Flags {
		flags[i] = string(flag)
	}
	return flags, nil
}
