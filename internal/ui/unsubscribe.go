package ui

import (
	"context"
	"fmt"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/PuerkitoBio/goquery"
	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/db"
)

// storedHeaderValue finds a header in a message's stored headers
// ("Label\nvalue\n" pairs, see imap.parseAuthHeaders). Returns "" when absent.
func storedHeaderValue(headers, label string) string {
	lines := strings.Split(headers, "\n")
	for i, line := range lines {
		if line == label && i+1 < len(lines) {
			return lines[i+1]
		}
	}
	return ""
}

// listUnsubscribeTarget extracts the best unsubscribe URI from a message's
// stored headers. The header value is a comma-separated list of <uri> entries
// per RFC 2369; an https/http URL is preferred over mailto. Returns "" when
// absent.
func listUnsubscribeTarget(headers string) string {
	value := storedHeaderValue(headers, "List-Unsubscribe")
	if value == "" {
		return ""
	}
	var mailto string
	for {
		open := strings.Index(value, "<")
		if open < 0 {
			break
		}
		close := strings.Index(value[open:], ">")
		if close < 0 {
			break
		}
		uri := strings.TrimSpace(value[open+1 : open+close])
		value = value[open+close+1:]
		switch {
		case strings.HasPrefix(uri, "https:"), strings.HasPrefix(uri, "http:"):
			return uri
		case strings.HasPrefix(uri, "mailto:") && mailto == "":
			mailto = uri
		}
	}
	return mailto
}

// messageUnsubscribeTarget prefers the sender-provided List-Unsubscribe
// header, then falls back to an explicitly labelled link in the saved body.
// The fallback matters for messages cached before TideMail retained the
// List-Unsubscribe header during IMAP sync.
func messageUnsubscribeTarget(msg db.Message) string {
	if target := listUnsubscribeTarget(msg.Headers); target != "" {
		return target
	}
	if target := htmlUnsubscribeTarget(msg.BodyHTML); target != "" {
		return target
	}
	return textUnsubscribeTarget(msg.BodyText)
}

func htmlUnsubscribeTarget(body string) string {
	if strings.TrimSpace(body) == "" {
		return ""
	}
	doc, err := goquery.NewDocumentFromReader(strings.NewReader(body))
	if err != nil {
		return ""
	}
	var mailto string
	doc.Find("a[href]").EachWithBreak(func(_ int, link *goquery.Selection) bool {
		label := strings.Join([]string{
			link.Text(),
			attrValue(link, "title"),
			attrValue(link, "aria-label"),
		}, " ")
		if !strings.Contains(strings.ToLower(label), "unsubscribe") {
			return true
		}
		target := validUnsubscribeURI(attrValue(link, "href"))
		switch {
		case strings.HasPrefix(target, "http://"), strings.HasPrefix(target, "https://"):
			mailto = target
			return false
		case strings.HasPrefix(target, "mailto:") && mailto == "":
			mailto = target
		}
		return true
	})
	return mailto
}

func textUnsubscribeTarget(body string) string {
	for _, line := range strings.Split(body, "\n") {
		lower := strings.ToLower(line)
		word := strings.Index(lower, "unsubscribe")
		if word < 0 {
			continue
		}
		var best string
		bestDistance := len(line) + 1
		for _, match := range unsubscribeURIPattern.FindAllStringIndex(line, -1) {
			distance := word - match[1]
			if match[0] > word+len("unsubscribe") {
				distance = match[0] - (word + len("unsubscribe"))
			}
			if distance < 0 {
				distance = 0
			}
			if distance > 80 || distance >= bestDistance {
				continue
			}
			if target := validUnsubscribeURI(line[match[0]:match[1]]); target != "" {
				best = target
				bestDistance = distance
			}
		}
		if best != "" {
			return best
		}
	}
	return ""
}

func attrValue(s *goquery.Selection, name string) string {
	value, _ := s.Attr(name)
	return value
}

func validUnsubscribeURI(raw string) string {
	target := strings.TrimSpace(raw)
	target = strings.TrimRight(target, ".,;:!?)]}\"'")
	u, err := url.Parse(target)
	if err != nil {
		return ""
	}
	u.Scheme = strings.ToLower(u.Scheme)
	switch u.Scheme {
	case "http", "https":
		if u.Host == "" {
			return ""
		}
		return u.String()
	case "mailto":
		if u.Opaque == "" {
			return ""
		}
		return u.String()
	default:
		return ""
	}
}

// supportsOneClickUnsubscribe reports whether the sender advertises RFC 8058
// one-click unsubscribe (a background POST, no browser confirmation page).
func supportsOneClickUnsubscribe(headers string) bool {
	v := storedHeaderValue(headers, "List-Unsubscribe-Post")
	return strings.Contains(strings.ToLower(v), "list-unsubscribe=one-click")
}

// parseMailtoURI returns the recipient and subject of a mailto: URI,
// defaulting the subject to "unsubscribe" when the URI carries none.
func parseMailtoURI(uri string) (addr, subject string) {
	subject = "unsubscribe"
	u, err := url.Parse(uri)
	if err != nil || u.Scheme != "mailto" {
		return "", subject
	}
	addr = u.Opaque
	if s := u.Query().Get("subject"); s != "" {
		subject = s
	}
	return addr, subject
}

// UnsubscribeResultMsg reports the outcome of a one-click unsubscribe POST.
// FallbackURL lets the model open the browser when the POST didn't work.
type UnsubscribeResultMsg struct {
	Err         error
	FallbackURL string
}

// oneClickUnsubscribeCmd performs the RFC 8058 POST. The fixed form body is
// the spec's anti-abuse handshake proving a user action, not a crawled link.
func oneClickUnsubscribeCmd(target string) tea.Cmd {
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, target,
			strings.NewReader("List-Unsubscribe=One-Click"))
		if err != nil {
			return UnsubscribeResultMsg{Err: err, FallbackURL: target}
		}
		req.Header.Set("Content-Type", "application/x-www-form-urlencoded")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return UnsubscribeResultMsg{Err: err, FallbackURL: target}
		}
		defer resp.Body.Close()
		if resp.StatusCode >= 400 {
			return UnsubscribeResultMsg{
				Err:         fmt.Errorf("server answered %s", resp.Status),
				FallbackURL: target,
			}
		}
		return UnsubscribeResultMsg{}
	}
}

// handleUnsubscribe reacts to ctrl+u on a message. mailto targets go straight
// to a prefilled compose (sending it is the confirmation); URL targets ask
// first via overlayUnsubscribeConfirm, then performUnsubscribe acts.
func (m Model) handleUnsubscribe(cur db.Message) (tea.Model, tea.Cmd) {
	target := messageUnsubscribeTarget(cur)
	if target == "" {
		m.setStatus("no unsubscribe link in this message", true)
		return m, m.clearStatusCmd()
	}
	if strings.HasPrefix(strings.ToLower(target), "mailto:") {
		addr, subject := parseMailtoURI(target)
		if addr == "" {
			m.setStatus("unsubscribe link is malformed", true)
			return m, m.clearStatusCmd()
		}
		acfg := m.accountCfgForMailbox(cur.MailboxID)
		c := NewCompose(acfg, m.cfg.Accounts, m.addressBook)
		c.toInput.SetValue(addr)
		c.subjectInput.SetValue(subject)
		m.compose = c
		m.overlay = overlayCompose
		m.setStatus("review and send to unsubscribe", false)
		return m, m.clearStatusCmd()
	}
	m.pendingUnsubscribe = cur
	m.overlay = overlayUnsubscribeConfirm
	return m, nil
}

// performUnsubscribe runs the confirmed URL unsubscribe: a background POST
// for one-click senders, the browser otherwise.
func (m Model) performUnsubscribe(cur db.Message) (tea.Model, tea.Cmd) {
	m.pendingUnsubscribe = db.Message{}
	target := messageUnsubscribeTarget(cur)
	if target == "" {
		return m, nil
	}
	if supportsOneClickUnsubscribe(cur.Headers) {
		m.setStatus("unsubscribing...", false)
		return m, oneClickUnsubscribeCmd(target)
	}
	m.setStatus("opening unsubscribe page in browser", false)
	return m, tea.Batch(m.openBrowserCmd(target), m.clearStatusCmd())
}

// unsubscribeHost is the display host for the confirmation overlay.
func unsubscribeHost(msg db.Message) string {
	target := messageUnsubscribeTarget(msg)
	if u, err := url.Parse(target); err == nil && u.Host != "" {
		return u.Host
	}
	return target
}
