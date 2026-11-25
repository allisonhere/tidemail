package cli

import (
	"database/sql"
	"encoding/json"
	"fmt"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
)

func cachedMailboxPage(database *db.DB, account string, limit, offset int) (map[string]any, error) {
	rows, err := database.Query(`SELECT mb.id, a.config_id, a.name, mb.name,
 CASE WHEN mb.delimiter = '' THEN mb.name ELSE COALESCE(NULLIF(mb.display_name,''),mb.name) END,
 mb.flags, mb.server_count, mb.server_count_at, COUNT(m.id), COALESCE(SUM(CASE WHEN m.read=0 THEN 1 ELSE 0 END),0)
 FROM mailboxes mb JOIN accounts a ON a.id=mb.account_id
 LEFT JOIN messages m ON m.mailbox_id=mb.id
 WHERE (?='' OR a.config_id=?) GROUP BY mb.id ORDER BY a.config_id,mb.name,mb.id`, account, account)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	items := []map[string]any{}
	for rows.Next() {
		var id int64
		var accountID, accountName, name, display, flags string
		var cached, unread int
		var server, serverAt sql.NullInt64
		if err := rows.Scan(&id, &accountID, &accountName, &name, &display, &flags, &server, &serverAt, &cached, &unread); err != nil {
			return nil, err
		}
		special := []string{}
		if err := json.Unmarshal([]byte(flags), &special); err != nil {
			return nil, fmt.Errorf("mailbox flags: %w", err)
		}
		// Null until a sync has observed the mailbox.
		var serverCount, serverCountAt any
		if server.Valid && serverAt.Valid {
			serverCount = server.Int64
			serverCountAt = time.Unix(serverAt.Int64, 0).UTC().Format(time.RFC3339)
		}
		items = append(items, map[string]any{"id": id, "account_id": accountID, "account_name": accountName, "name": name, "mailbox": name, "mailbox_display": display, "special_use": special, "cached": cached, "unread": unread, "server_count": serverCount, "server_count_at": serverCountAt})
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return readPage(items, limit, offset), nil
}
