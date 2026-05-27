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

func (c *Client) fetchMessages(ctx context.Context, mailboxName string, limit int, since time.Time) ([]db.Message, error) {
	if c.conn == nil {
		return nil, fmt.Errorf("not connected")
	}

	selectData, err := c.conn.Select(mailboxName, nil).Wait()
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

	for _, section := range msg.BodySection {
		raw := section.Bytes
		if len(raw) == 0 {
			continue
		}
		text, html, hasAttach := parseBody(raw)
		if text != "" {
			m.BodyText = text
		}
		if html != "" {
			m.BodyHTML = html
		}
		m.HasAttachment = hasAttach
		break
	}

	return m, nil
}

func parseBody(raw []byte) (text, html string, hasAttach bool) {
	r, err := mail.CreateReader(bytes.NewReader(raw))
	if err != nil {
		return strings.TrimSpace(string(raw)), "", false
	}

	for {
		part, err := r.NextPart()
		if err != nil {
			break
		}
		ct, _, _ := mime.ParseMediaType(part.Header.Get("Content-Type"))
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
				hasAttach = true
			}
		}
	}
	return text, html, hasAttach
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
	for _, r := range s {
		switch {
		case r == '<':
			inTag = true
		case r == '>':
			inTag = false
		case !inTag:
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
