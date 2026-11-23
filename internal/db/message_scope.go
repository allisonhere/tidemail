package db

import "strings"

// MessageScope restricts cached CLI reads before pagination. Empty scopes on
// list retain the unified inbox; search with an empty query matches all mail.
type MessageScope struct {
	AccountID string
	Mailboxes []string
	Unread    bool
}

func (s MessageScope) where(inbox bool) (string, []any) {
	var parts []string
	var args []any
	if s.AccountID != "" {
		parts = append(parts, "accounts.config_id = ?")
		args = append(args, s.AccountID)
	}
	if len(s.Mailboxes) > 0 {
		var names []string
		for _, name := range s.Mailboxes {
			names = append(names, "(mailboxes.name = ? COLLATE NOCASE OR mailboxes.display_name = ? COLLATE NOCASE)")
			args = append(args, name, name)
		}
		parts = append(parts, "("+strings.Join(names, " OR ")+")")
	}
	if inbox && s.AccountID == "" && len(s.Mailboxes) == 0 {
		parts = append(parts, inboxMailboxPredicate)
	}
	if s.Unread {
		parts = append(parts, "messages.read = 0")
	}
	if len(parts) == 0 {
		return "1", args
	}
	return strings.Join(parts, " AND "), args
}

// ScopedMessagesPage applies a scope to both the count and the bounded page.
func (db *DB) ScopedMessagesPage(query string, scope MessageScope, inbox bool, limit, offset int) ([]Message, int, error) {
	return db.searchScopedMessages(query, false, scope.Unread, limit, offset, &scope, inbox)
}
