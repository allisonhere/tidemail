package db

import "fmt"

type Attachment struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
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
			(message_id, filename, content_type, data, size)
		VALUES (?, ?, ?, ?, ?)`,
		msgID, a.Filename, a.ContentType, a.Data, a.Size)
	if err != nil {
		return 0, fmt.Errorf("save attachment: %w", err)
	}
	return res.LastInsertId()
}

func (db *DB) GetAttachments(msgID int64) ([]Attachment, error) {
	rows, err := db.Query(`
		SELECT id, message_id, filename, content_type, data, size
		FROM attachments WHERE message_id = ?
		ORDER BY id`, msgID)
	if err != nil {
		return nil, fmt.Errorf("get attachments: %w", err)
	}
	defer rows.Close()

	var atts []Attachment
	for rows.Next() {
		var a Attachment
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType, &a.Data, &a.Size); err != nil {
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
