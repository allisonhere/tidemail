package imap

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/textproto"
	"strings"
	"time"

	"github.com/allisonhere/tide/internal/db"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapclient"
	"github.com/emersion/go-message"
	// Registers the global message.CharsetReader so non-UTF-8 parts
	// (ISO-8859-1, Windows-1252, Shift_JIS, …) decode to UTF-8 instead of
	// producing U+FFFD replacement chars — and so an unknown charset no longer
	// aborts NextPart and drops the rest of the message.
	_ "github.com/emersion/go-message/charset"
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
	defer c.applyDeadline(ctx)()

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
	headerSection := &imap.FetchItemBodySection{Specifier: imap.PartSpecifierHeader}
	fetchOptions := &imap.FetchOptions{
		UID:           true,
		Flags:         true,
		Envelope:      true,
		InternalDate:  true,
		BodyStructure: &imap.FetchItemBodyStructure{},
		BodySection:   []*imap.FetchItemBodySection{bodySection, headerSection},
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
		if f == imap.FlagFlagged {
			m.Starred = true
		}
	}

	if env := msg.Envelope; env != nil {
		m.Subject = sanitizeControl(env.Subject)
		m.MessageID = sanitizeControl(env.MessageID)
		if len(env.InReplyTo) > 0 {
			m.InReplyTo = sanitizeControl(strings.Join(env.InReplyTo, " "))
		}
		if !env.Date.IsZero() {
			m.Date = env.Date
		}
		m.From = sanitizeControl(addressList(env.From))
		m.To = sanitizeControl(addressList(env.To))
		m.CC = sanitizeControl(addressList(env.Cc))
		m.ReplyTo = sanitizeControl(addressList(env.ReplyTo))
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
		// Distinguish header section from body section
		if section.Section != nil && section.Section.Specifier == imap.PartSpecifierHeader {
			m.Headers = sanitizeControl(parseAuthHeaders(raw))
			m.References = sanitizeControl(parseHeaderValue(raw, "References"))
			continue
		}
		text, html, atts := parseBody(raw)
		if text != "" {
			m.BodyText = sanitizeControl(text)
		}
		if html != "" {
			// Raw control bytes are stripped here; HTML entity-encoded escapes
			// (e.g. &#27;) are neutralized downstream after decoding, before the
			// text is styled and written to the terminal.
			m.BodyHTML = sanitizeControl(html)
		}
		if len(atts) > 0 {
			m.HasAttachment = true
			m.AttachmentData = make([]db.Attachment, len(atts))
			for i, a := range atts {
				m.AttachmentData[i] = db.Attachment{
					Filename:    sanitizeControl(a.Filename),
					ContentType: a.ContentType,
					Data:        a.Data,
					Size:        int64(len(a.Data)),
				}
			}
		}
	}

	return m, nil
}

// sanitizeControl strips C0 control bytes (and DEL) that a terminal would
// interpret as escape sequences from untrusted message text, preserving only
// tab, newline, and carriage return. Message content is rendered straight to
// the user's terminal, so without this an attacker could embed ANSI/OSC
// sequences in a subject, address, filename, or body and — when the message is
// viewed — hijack the clipboard (OSC 52), spoof the UI, or move the cursor.
// C1 controls (U+0080–U+009F) are intentionally left alone: on a UTF-8 terminal
// they are not control-interpreted, and dropping them would corrupt decoded text.
func sanitizeControl(s string) string {
	return strings.Map(func(r rune) rune {
		switch r {
		case '\t', '\n', '\r':
			return r
		}
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
}

func parseHeaderValue(raw []byte, name string) string {
	tr := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw)))
	hdr, err := tr.ReadMIMEHeader()
	if err != nil {
		return ""
	}
	return normalizeHeaderWhitespace(hdr.Get(name))
}

func normalizeHeaderWhitespace(s string) string {
	return strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
}

// parseAuthHeaders extracts SPF, DKIM, DMARC, Return-Path, Received, and
// List-Unsubscribe headers from raw MIME header text. Returns a compact
// display string of "Label\nvalue\n" pairs.
func parseAuthHeaders(raw []byte) string {
	// Parse headers using net/textproto
	tr := textproto.NewReader(bufio.NewReader(bytes.NewReader(raw)))
	hdr, err := tr.ReadMIMEHeader()
	if err != nil {
		return ""
	}

	type authLine struct{ label, value string }
	var lines []authLine

	// Authentication-Results: compound auth info
	if v := hdr.Get("Authentication-Results"); v != "" {
		lines = append(lines, authLine{"Authentication-Results", v})
	}
	// Received-SPF: SPF check result
	if v := hdr.Get("Received-SPF"); v != "" {
		lines = append(lines, authLine{"Received-SPF", v})
	}
	// DKIM-Signature (truncated — just show presence and domain)
	if v := hdr.Get("DKIM-Signature"); v != "" {
		// Extract d= tag if present
		domain := ""
		for _, part := range strings.Split(v, ";") {
			kv := strings.SplitN(strings.TrimSpace(part), "=", 2)
			if len(kv) == 2 && strings.TrimSpace(kv[0]) == "d" {
				domain = strings.TrimSpace(kv[1])
			}
		}
		if domain != "" {
			lines = append(lines, authLine{"DKIM-Signature", "present (d=" + domain + ")"})
		} else {
			lines = append(lines, authLine{"DKIM-Signature", "present"})
		}
	}
	// Return-Path
	if v := hdr.Get("Return-Path"); v != "" {
		lines = append(lines, authLine{"Return-Path", v})
	}
	// Received: most recent hop only
	if v := hdr.Get("Received"); v != "" {
		// Show only first Received header (most recent hop)
		lines = append(lines, authLine{"Received", v})
	}
	// X-Spam headers
	if v := hdr.Get("X-Spam-Status"); v != "" {
		lines = append(lines, authLine{"X-Spam-Status", v})
	}
	if v := hdr.Get("X-Spam-Score"); v != "" {
		lines = append(lines, authLine{"X-Spam-Score", v})
	}
	// List-Unsubscribe: kept so the UI can offer one-key unsubscribe. The
	// -Post companion marks RFC 8058 one-click support (unsubscribe via a
	// single background POST instead of a browser round-trip).
	if v := hdr.Get("List-Unsubscribe"); v != "" {
		lines = append(lines, authLine{"List-Unsubscribe", v})
		if p := hdr.Get("List-Unsubscribe-Post"); p != "" {
			lines = append(lines, authLine{"List-Unsubscribe-Post", p})
		}
	}

	if len(lines) == 0 {
		return ""
	}

	var out strings.Builder
	for _, l := range lines {
		out.WriteString(l.label)
		out.WriteByte('\n')
		out.WriteString(l.value)
		out.WriteByte('\n')
	}
	return out.String()
}

func parseBody(raw []byte) (text, html string, attachments []bodyAttachment) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return strings.TrimSpace(string(raw)), "", nil
	}

	for {
		part, err := r.NextPart()
		if errors.Is(err, io.EOF) {
			break
		}
		// An unknown charset still yields a usable part whose body reads as raw
		// bytes, so keep going; only a genuine error aborts the message.
		if err != nil && !message.IsUnknownCharset(err) {
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
					if stripped := stripHTML(string(data)); meaningfulBodyText(stripped) {
						text = stripped
					}
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

func meaningfulBodyText(s string) bool {
	text := strings.Join(strings.Fields(strings.TrimSpace(s)), " ")
	if text == "" {
		return false
	}
	phrase := strings.ToLower(text)
	switch phrase {
	case "view in browser", "open in browser", "view this email in your browser", "unsubscribe", "manage preferences":
		return false
	}
	words := strings.Fields(strings.NewReplacer(
		"[", " ",
		"]", " ",
		":", " ",
		"|", " ",
		"-", " ",
		"_", " ",
	).Replace(phrase))
	contentWords := 0
	for _, word := range words {
		switch word {
		case "image", "logo", "icon", "spacer", "pixel", "tracking", "open", "view", "browser":
			continue
		default:
			contentWords++
		}
	}
	return contentWords > 0
}
