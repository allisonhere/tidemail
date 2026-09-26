package db

import (
	"fmt"
	"strings"
)

type Attachment struct {
	ID          int64
	MessageID   int64
	Filename    string
	ContentType string
	// ContentID is the normalized RFC 2045 Content-ID (angle brackets removed)
	// for MIME parts that carry one, most importantly inline images referenced
	// from HTML as src="cid:...". Empty for ordinary attachments.
	ContentID string
	// Disposition is the lower-cased Content-Disposition type ("inline" or
	// "attachment"), or "" when the part declared none.
	Disposition string
	// ContentLocation is the normalized Content-Location header, an alternate
	// way HTML can reference an embedded part.
	ContentLocation string
	// Inline reports whether the part is embedded in the message body rather
	// than a standalone downloadable attachment.
	Inline bool
	Data   []byte
	Size   int64
}

func (db *DB) SaveAttachment(msgID int64, a Attachment) (int64, error) {
	if a.Filename == "" {
		a.Filename = "untitled"
	}
	if a.Size == 0 && a.Data != nil {
		a.Size = int64(len(a.Data))
	}
	inline := 0
	if a.Inline {
		inline = 1
	}
	res, err := db.Exec(`
		INSERT INTO attachments
			(message_id, filename, content_type, content_id, content_disposition, content_location, inline, data, size)
		VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?)`,
		msgID, a.Filename, a.ContentType, a.ContentID, a.Disposition, a.ContentLocation, inline, a.Data, a.Size)
	if err != nil {
		return 0, fmt.Errorf("save attachment: %w", err)
	}
	return res.LastInsertId()
}

// GetAttachmentsMeta lists a message's attachments without their contents.
// The reading pane only shows names and sizes, and it is redrawn on every
// cursor move — pulling multi-megabyte blobs for that put the whole
// attachment store through memory on each keystroke.
func (db *DB) GetAttachmentsMeta(msgID int64) ([]Attachment, error) {
	rows, err := db.Query(`
		SELECT id, message_id, filename, content_type, content_id, content_disposition, content_location, inline, size
		FROM attachments WHERE message_id = ?
		ORDER BY id`, msgID)
	if err != nil {
		return nil, fmt.Errorf("get attachment metadata: %w", err)
	}
	defer rows.Close()

	var atts []Attachment
	for rows.Next() {
		var a Attachment
		var inline int
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType,
			&a.ContentID, &a.Disposition, &a.ContentLocation, &inline, &a.Size); err != nil {
			return nil, fmt.Errorf("scan attachment metadata: %w", err)
		}
		a.Inline = inline != 0
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

// GetAttachmentData loads the contents of one attachment, for the point of
// use — saving it to disk — rather than for display.
func (db *DB) GetAttachmentData(id int64) ([]byte, error) {
	var data []byte
	if err := db.QueryRow(`SELECT data FROM attachments WHERE id = ?`, id).Scan(&data); err != nil {
		return nil, fmt.Errorf("get attachment data: %w", err)
	}
	return data, nil
}

func (db *DB) GetAttachments(msgID int64) ([]Attachment, error) {
	rows, err := db.Query(`
		SELECT id, message_id, filename, content_type, content_id, content_disposition, content_location, inline, data, size
		FROM attachments WHERE message_id = ?
		ORDER BY id`, msgID)
	if err != nil {
		return nil, fmt.Errorf("get attachments: %w", err)
	}
	defer rows.Close()

	var atts []Attachment
	for rows.Next() {
		var a Attachment
		var inline int
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType,
			&a.ContentID, &a.Disposition, &a.ContentLocation, &inline, &a.Data, &a.Size); err != nil {
			return nil, fmt.Errorf("scan attachment: %w", err)
		}
		a.Inline = inline != 0
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

// GetImageParts loads only the image-typed MIME parts of a message, with their
// bytes and identity metadata. The reading pane may need every inline image's
// data to render a newsletter, so this deliberately fetches data — unlike
// GetAttachmentsMeta — but keeps non-image parts (PDFs, archives, video) out of
// memory. Callers that only need names and sizes should keep using the meta
// query.
func (db *DB) GetImageParts(msgID int64) ([]Attachment, error) {
	rows, err := db.Query(`
		SELECT id, message_id, filename, content_type, content_id, content_disposition, content_location, inline, data, size
		FROM attachments
		WHERE message_id = ? AND (content_type LIKE 'image/%' OR content_id != '')
		ORDER BY id`, msgID)
	if err != nil {
		return nil, fmt.Errorf("get image parts: %w", err)
	}
	defer rows.Close()

	var atts []Attachment
	for rows.Next() {
		var a Attachment
		var inline int
		if err := rows.Scan(&a.ID, &a.MessageID, &a.Filename, &a.ContentType,
			&a.ContentID, &a.Disposition, &a.ContentLocation, &inline, &a.Data, &a.Size); err != nil {
			return nil, fmt.Errorf("scan image part: %w", err)
		}
		a.Inline = inline != 0
		atts = append(atts, a)
	}
	return atts, rows.Err()
}

// IsInlineImage reports whether an attachment is an image embedded in the
// message body and therefore not worth listing as a downloadable attachment.
// A part is treated as inline when it declared Content-Disposition: inline, or
// when it carries a Content-ID and was not explicitly marked as an attachment.
func (a Attachment) IsInlineImage() bool {
	if !strings.HasPrefix(strings.ToLower(a.ContentType), "image/") {
		return false
	}
	if strings.EqualFold(a.Disposition, "attachment") {
		return false
	}
	return a.Inline || a.Disposition == "inline" || a.ContentID != ""
}

func (db *DB) DeleteAttachmentsForMessage(msgID int64) error {
	_, err := db.Exec(`DELETE FROM attachments WHERE message_id = ?`, msgID)
	if err != nil {
		return fmt.Errorf("delete attachments: %w", err)
	}
	return nil
}
