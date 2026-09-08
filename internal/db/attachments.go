package db

import "fmt"

type Attachment struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	ContentID   string
	Data        []byte
	Size        int64
}

func (db *DB) SaveAttachment(msgID int64, a Attachment) (int64, error) {
	if a.Filename == "" {
		a.Filename = "untitled"
	}
	if a.Size == 0 && a.Data != nil {
		a.Size = int64(len(a.Data))
	}
	res, err := db.Exec(`
		INSERT INTO attachments
			(message_id, filename, content_type, content_id, data, size)
		VALUES (?, ?, ?, ?, ?, ?)`,
		msgID, a.Filename, a.ContentType, a.ContentID, a.Data, a.Size)
	if err != nil {
		return 0, fmt.Errorf("save attachment: %w", err)
	}
	return res.LastInsertId()
}

func (db *DB) GetAttachments(msgID int64) ([]Attachment, error) {
	rows, err := db.Query(`
		SELECT id, message_id, filename, content_type, content_id, data, size
		FROM attachments WHERE message_id = ?
		ORDER BY id`, msgID)
	if err != nil {
		return nil, fmt.Errorf("get attachments: %w", err)
	}
	defer rows.Close()

	var atts []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType, &a.ContentID, &a.Data, &a.Size); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

func (db *DB) DeleteAttachmentsForMessage(msgID int64) error {
	_, err := db.Exec(`DELETE FROM attachments WHERE message_id = ?`, msgID)
	if err != nil {
		return fmt.Errorf("delete attachments: %w", err)
	}
	return nil
}

// Existing caches have no CID metadata; leave those associations unknown.
func (db *DB) migrateAttachmentContentID() error {
	rows, err := db.Query(`PRAGMA table_info(attachments)`)
	if err != nil {
		return err
	}
	found := false
	for rows.Next() {
		var cid, notNull, pk int
		var name, typ string
		var defaultValue interface{}
		if err := rows.Scan(&cid, &name, &typ, &notNull, &defaultValue, &pk); err != nil {
			rows.Close()
			return err
		}
		found = found || name == "content_id"
	}
	err = rows.Err()
	rows.Close()
	if err != nil {
		return err
	}
	if found {
		return nil
	}
	_, err = db.Exec(`ALTER TABLE attachments ADD COLUMN content_id TEXT NOT NULL DEFAULT ''`)
	return err
}
