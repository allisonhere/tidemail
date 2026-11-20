package cli

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
)

// replyHeaders resolves a cached message ID (as printed by messages list, for
// example cache:123) to the In-Reply-To and References values for a reply to
// it. They are built as the interactive reply does: In-Reply-To is the
// original's Message-ID and References is its References plus that ID. On
// failure it returns the exit code, error ID and message for mailResult.
func replyHeaders(value string) (inReplyTo, references string, code int, id, msg string) {
	cacheID, err := readID(value)
	if err != nil {
		return "", "", 2, "invalid_arguments", "invalid --in-reply-to; use an ID from messages list, such as cache:123"
	}
	database, err := db.Open()
	if err != nil {
		return "", "", 1, "database_error", "cannot open mail cache"
	}
	defer database.Close()
	m, err := database.GetMessage(cacheID)
	if errors.Is(err, sql.ErrNoRows) {
		return "", "", 1, "not_found", "cached message for --in-reply-to not found"
	}
	if err != nil {
		return "", "", 1, "database_error", "cannot read cached message"
	}
	if strings.TrimSpace(m.MessageID) == "" {
		return "", "", 2, "invalid_arguments", "that message has no Message-ID to reply to"
	}
	return m.MessageID, strings.TrimSpace(m.References + " " + m.MessageID), 0, "", ""
}
