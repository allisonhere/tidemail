package ui

import (
	"fmt"
	"os"
	"strings"
	"unicode/utf8"

	"github.com/allisonhere/tidemail/internal/browser"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

type emailBrowserOpenedMsg struct{ err error }

var fetchEmailSource = Model.messageSourceFetcher
var openEmailBrowser = browser.OpenLocalFile

func (m Model) openEmailInBrowser(msg db.Message) (tea.Model, tea.Cmd) {
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg, err := m.accountCfgForMailbox(msg.MailboxID)
	if mailbox == nil || err != nil || acfg.IMAPHost == "" || msg.UID == 0 {
		m.setStatus("This message can't be fetched from a server", true)
		return m, m.clearStatusCmd()
	}
	fetch := fetchEmailSource(m, msg)
	m.setStatus("Opening message in browser…", false)
	return m, func() tea.Msg {
		raw, err := fetch()
		if err != nil {
			return emailBrowserOpenedMsg{err}
		}
		return emailBrowserOpenedMsg{openEmailHTML(raw)}
	}
}

func openEmailHTML(raw []byte) error {
	html := imapClient.ExtractHTML(raw)
	if html == "" {
		return fmt.Errorf("this message has no HTML body")
	}
	if !utf8.ValidString(html) {
		return fmt.Errorf("the message HTML could not be decoded as UTF-8")
	}
	// MIME decoding produces UTF-8. A leading BOM makes browsers use UTF-8
	// even when the original HTML still declares a different charset, without
	// rewriting its markup. Preserve an existing BOM rather than adding two.
	html = "\ufeff" + strings.TrimPrefix(html, "\ufeff")
	file, err := os.CreateTemp("", "tidemail-email-*.html")
	if err != nil {
		return fmt.Errorf("create temporary HTML: %w", err)
	}
	path := file.Name()
	// Remove failed exports immediately; successful ones stay available for reloads.
	keep := false
	defer func() {
		if !keep {
			_ = os.Remove(path)
		}
	}()
	_, writeErr := file.WriteString(html)
	closeErr := file.Close()
	if writeErr != nil {
		return fmt.Errorf("write temporary HTML: %w", writeErr)
	}
	if closeErr != nil {
		return fmt.Errorf("close temporary HTML: %w", closeErr)
	}
	// Always use the system default browser for local email exports.
	if err := openEmailBrowser(path); err != nil {
		return fmt.Errorf("could not launch default browser: %w", err)
	}
	keep = true
	// A launcher may exit before the browser reads the file. Leave successful
	// exports in the system temporary directory, subject to its cleanup policy.
	return nil
}
