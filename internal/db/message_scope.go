package db

import (
	"strings"
	"time"
)

// MessageScope restricts cached CLI reads before pagination. Empty scopes on
// list retain the unified inbox; search with an empty query matches all mail.
type MessageScope struct {
	AccountID     string
	Mailboxes     []string
	Unread        bool
	Starred       bool
	Since, Before time.Time
	IncludeTrash  bool
	Sort          string
	Exact         bool // CLI search: no implicit prefix on the last word
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
	if s.Starred {
		parts = append(parts, "messages.starred = 1")
	}
	if !s.Since.IsZero() {
		parts = append(parts, "messages.date >= ?")
		args = append(args, s.Since.Unix())
	}
	if !s.Before.IsZero() {
		parts = append(parts, "messages.date < ?")
		args = append(args, s.Before.Unix())
	}
	if !s.IncludeTrash && len(s.Mailboxes) == 0 {
		parts = append(parts, excludedMailboxesPredicate)
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
	mode := searchCLI
	if scope.Exact {
		mode = searchCLIExact
	}
	return db.searchScopedMessages(query, false, scope.Unread, limit, offset, &scope, inbox, mode)
}

// Special-use flags take priority over folder names. Fallbacks cover common
// leaf names in dot/slash hierarchies and display names.
const excludedMailboxesPredicate = `NOT (
 EXISTS (SELECT 1 FROM json_each(mailboxes.flags) f WHERE lower(f.value) IN ('\trash','\junk'))
 OR ((NOT EXISTS (SELECT 1 FROM json_each(mailboxes.flags) f WHERE substr(f.value,1,1)='\' AND lower(f.value) NOT IN ('\haschildren','\hasnochildren','\noselect','\marked','\unmarked','\noinferiors','\subscribed')))
 AND (
 lower(mailboxes.display_name) IN ('trash','junk','junk email','junk e-mail','spam','deleted','deleted messages','deleted items')
 OR lower(mailboxes.name) IN ('trash','junk','junk email','junk e-mail','spam','deleted','deleted messages','deleted items')
 OR lower(mailboxes.name) GLOB '*.trash' OR lower(mailboxes.name) GLOB '*/trash'
 OR lower(mailboxes.name) GLOB '*.junk' OR lower(mailboxes.name) GLOB '*/junk'
 OR lower(mailboxes.name) GLOB '*.junk email' OR lower(mailboxes.name) GLOB '*/junk email'
 OR lower(mailboxes.name) GLOB '*.junk e-mail' OR lower(mailboxes.name) GLOB '*/junk e-mail'
 OR lower(mailboxes.name) GLOB '*.spam' OR lower(mailboxes.name) GLOB '*/spam'
 OR lower(mailboxes.name) GLOB '*.deleted' OR lower(mailboxes.name) GLOB '*/deleted'
 OR lower(mailboxes.name) GLOB '*.deleted messages' OR lower(mailboxes.name) GLOB '*/deleted messages'
 OR lower(mailboxes.name) GLOB '*.deleted items' OR lower(mailboxes.name) GLOB '*/deleted items'
 )))
`
