package imap

import (
	"bytes"
	"context"
	"fmt"
	"io"
	"mime"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/db"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message/mail"
)

type bodyAttachment struct {
	Filename    string
	ContentType string
	Data        []byte
}

func (c *Client) fetchMessages(ctx context.Context, mailboxName string, limit int, since time.Time) ([]db.Message, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	selectData, err := c.conn.Select(mailboxName, &imap.SelectOptions{ReadOnly: true}).Wait()
	if err != nil {
		return nil, fmt.Errorf("select %s: %w", mailboxName, err)
	}

	total := selectData.NumMessages
	if total == 0 {
		return nil, nil
	}

	var numSet imap.NumSet

	if !since.IsZero() {
		// Incremental sync: use UID SEARCH SINCE to ask the server for
		// only messages received since the last sync. This avoids fetching
		// and discarding the full body of every previously-seen message.
		searchData, err := c.conn.UIDSearch(&imap.SearchCriteria{
			Since: since,
		}, nil).Wait()
		if err != nil {
			return nil, fmt.Errorf("uid search: %w", err)
		}
		uids := searchData.AllUIDs()
		if len(uids) == 0 {
			return nil, nil
		}
		numSet = imap.UIDSetNum(uids...)
	} else {
		// Initial sync: fetch last N messages by sequence number.
		start := uint32(1)
		if limit > 0 && total > uint32(limit) {
			start = total - uint32(limit) + 1
		}
		seqSet := imap.SeqSetNum()
		for i := start; i <= total; i++ {
			seqSet.AddNum(i)
		}
		numSet = seqSet
	}

	bodySection := &imap.FetchItemBodySection{}
	fetchOptions := &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		Envelope:      true,
		InternalDate:  true,
		BodyStructure: &imap.FetchItemBodyStructure{},
		BodySection:   []*imap.FetchItemBodySection{bodySection},
	}

	cmd := c.conn.Fetch(numSet, fetchOptions)
	msgs, err := cmd.Collect()
	if err != nil {
		return nil, fmt.Errorf("fetch: %w", err)
	}

	var results []db.Message
	for _, msg := range msgs {
		parsed, err := parseIMAPMessage(msg)
		if err != nil {
			continue
		}
		results = append(results, parsed)
	}
	return results, nil
}

func parseIMAPMessage(msg *imapclient.FetchMessageBuffer) (db.Message, error) {
	m := db.Message{
		UID: uint32(msg.UID),
	}

	for _, f := range msg.Flags {
		m.Flags = append(m.Flags, string(f))
		if f == imap.FlagSeen {
			m.Read = true
		}
	}

	if env := msg.Envelope; env != nil {
		m.Subject = env.Subject
		m.MessageID = env.MessageID
		if !env.Date.IsZero() {
			m.Date = env.Date
		}
		m.From = addressList(env.From)
		m.To = addressList(env.To)
		m.CC = addressList(env.Cc)
		m.ReplyTo = addressList(env.ReplyTo)
	}
	// Fallback: some servers (Dovecot) may omit envelope date for system messages.
	// INTERNALDATE is always present and never zero.
	if m.Date.IsZero() && !msg.InternalDate.IsZero() {
		m.Date = msg.InternalDate
	}

	for _, section := range msg.BodySection {
		raw := section.Bytes
		if len(raw) == 0 {
			continue
		}
		text, html, atts := parseBody(raw)
		if text != "" {
			m.BodyText = text
		}
		if html != "" {
			m.BodyHTML = html
		}
		if len(atts) > 0 {
			m.HasAttachment = true
			m.AttachmentData = make([]db.Attachment, len(atts))
			for i, a := range atts {
				m.AttachmentData[i] = db.Attachment{
					Filename:    a.Filename,
					ContentType: a.ContentType,
					Data:        a.Data,
					Size:        int64(len(a.Data)),
				}
			}
		}
		break
	}

	return m, nil
}

func parseBody(raw []byte) (text, html string, attachments []bodyAttachment) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return strings.TrimSpace(string(raw)), "", nil
	}

	for {
		part, err := r.NextPart()
		if err != nil {
			break
		}
		ct, params, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
		if ct == "" {
			ct = "text/plain"
		}
		data, readErr := io.ReadAll(part.Body)
		if readErr != nil {
			continue
		}
		switch ct {
		case "text/plain":
			if text == "" {
				text = strings.TrimSpace(string(data))
			}
		case "text/html":
			if html == "" {
				html = string(data)
				if text == "" {
					text = stripHTML(string(data))
				}
			}
		default:
			if ct != "" && !strings.HasPrefix(ct, "multipart/") {
				attachments = append(attachments, bodyAttachment{
					Filename:    filenameFromPart(part, params, ct),
					ContentType: ct,
					Data:        data,
				})
			}
		}
	}
	return text, html, attachments
}

// filenameFromPart extracts a filename from a MIME part's Content-Disposition
// or Content-Type params, falling back to "attachment.ext" based on content type.
func filenameFromPart(part *mail.Part, ctParams map[string]string, ct string) string {
	if fn := part.Header.Get("Content-Disposition"); fn != "" {
		if _, dispParams, err := mime.ParseMediaType(fn); err == nil {
			if name := dispParams["filename"]; name != "" {
				return name
			}
		}
	}
	if name := ctParams["name"]; name != "" {
		return name
	}
	// Fallback based on Content-Type
	ext := ".bin"
	switch {
	case strings.HasPrefix(ct, "application/pdf"):
		ext = ".pdf"
	case strings.HasPrefix(ct, "application/zip"):
		ext = ".zip"
	case strings.HasPrefix(ct, "image/"):
		parts := strings.Split(ct, "/")
		if len(parts) == 2 {
			ext = "." + parts[1]
		}
	case strings.HasPrefix(ct, "text/"):
		parts := strings.Split(ct, "/")
		if len(parts) == 2 {
			ext = "." + parts[1]
		}
	}
	return "attachment" + ext
}

func addressList(addrs []imap.Address) string {
	parts := make([]string, 0, len(addrs))
	for _, a := range addrs {
		if a.Name != "" {
			parts = append(parts, fmt.Sprintf("%s <%s@%s>", a.Name, a.Mailbox, a.Host))
		} else {
			parts = append(parts, fmt.Sprintf("%s@%s", a.Mailbox, a.Host))
		}
	}
	return strings.Join(parts, ", ")
}

func stripHTML(s string) string {
	var b strings.Builder
	inTag := false
	skipContent := false
	tagBuf := strings.Builder{}
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
			tagBuf.Reset()
		case r == '>':
			inTag = false
			tagName := strings.ToLower(strings.TrimSpace(tagBuf.String()))
			// Strip leading /
			tagName = strings.TrimPrefix(tagName, "/")
			// Get just the tag name (first word)
			if idx := strings.IndexAny(tagName, " \t\n"); idx >= 0 {
				tagName = tagName[:idx]
			}
			switch tagName {
			case "script", "style":
				// Opening tag: skip content until closing tag
				// Closing tag: resume output
				if strings.HasPrefix(strings.ToLower(strings.TrimSpace(tagBuf.String())), "/") {
					skipContent = false
				} else {
					skipContent = true
				}
			case "br", "p", "div", "tr", "li", "h1", "h2", "h3", "h4", "h5", "h6":
				b.WriteByte('\n')
			}
		case inTag:
			tagBuf.WriteRune(r)
		case !skipContent && !inTag:
			b.WriteRune(r)
		}
	}
	lines := strings.Split(b.String(), "\n")
	var out []string
	for _, l := range lines {
		l = strings.TrimSpace(l)
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}
