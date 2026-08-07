package ui

import (
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
)

// mailboxNamespace infers the personal-namespace prefix and hierarchy delimiter
// under which a new top-level folder must be created for an account.
//
// Dovecot and Courier commonly expose the personal namespace as "INBOX." —
// every folder lives below INBOX (INBOX.Sent, INBOX.Archive) and a bare
// `CREATE "Newsletters"` is rejected as an invalid mailbox name. Gmail and most
// other servers use an empty prefix. We infer this from the mailbox list rather
// than issuing NAMESPACE so the answer is also available offline, straight from
// the DB, and so it costs no extra round trip.
//
// The prefix is only claimed when the evidence is unambiguous: at least one
// folder under INBOX<delim> and no folder outside it. A single top-level folder
// (as on Gmail) means the server accepts top-level creates, so we stay out of
// the way.
func mailboxNamespace(mbs []db.Mailbox) (prefix, delimiter string) {
	delimiter = "/"
	for _, mb := range mbs {
		if mb.Delimiter != "" {
			delimiter = mb.Delimiter
			break
		}
	}
	inboxPrefix := "INBOX" + delimiter
	nested := 0
	for _, mb := range mbs {
		switch {
		case strings.EqualFold(mb.Name, "INBOX"):
			// The INBOX itself says nothing about where folders may live.
		case strings.HasPrefix(strings.ToUpper(mb.Name), strings.ToUpper(inboxPrefix)):
			nested++
		default:
			// A folder outside INBOX proves top-level creates are allowed.
			return "", delimiter
		}
	}
	if nested == 0 {
		return "", delimiter
	}
	return inboxPrefix, delimiter
}

// accountMailboxes narrows a mixed-account mailbox slice (as the Model holds)
// to one account — namespace inference is per-server.
func accountMailboxes(mbs []db.Mailbox, accountID int64) []db.Mailbox {
	out := make([]db.Mailbox, 0, len(mbs))
	for _, mb := range mbs {
		if mb.AccountID == accountID {
			out = append(out, mb)
		}
	}
	return out
}

// qualifyFolderName places a user- or AI-supplied folder name inside the
// account's personal namespace, leaving already-qualified names alone.
func qualifyFolderName(name string, mbs []db.Mailbox) (fullName, delimiter string) {
	prefix, delimiter := mailboxNamespace(mbs)
	if prefix == "" || strings.HasPrefix(strings.ToUpper(name), strings.ToUpper(prefix)) {
		return name, delimiter
	}
	return prefix + name, delimiter
}
