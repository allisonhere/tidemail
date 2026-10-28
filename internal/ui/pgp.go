package ui

import (
	"context"
	"fmt"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/pgpmail"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// Reading PGP mail. A plugin with the message_raw permission decrypts and
// verifies on request (D, or "Decrypt / verify" in the palette). TideMail
// fetches that one message's exact bytes, splits out the protected part
// (internal/pgpmail), and shows the result in the reading pane. The decrypted
// text lives only in pgpState: it is never written to the database, the
// search index, or the image cache, and it is gone when TideMail exits.

// pgpTimeout bounds one decode, including any passphrase prompt.
const pgpTimeout = 3 * time.Minute

type pgpEntry struct {
	pending bool
	err     string
	view    *pgpmail.View
}

type pgpState struct {
	entries map[int64]*pgpEntry
}

// PGPDecodedMsg carries the outcome of one decode back to the model.
type PGPDecodedMsg struct {
	MessageID int64
	View      *pgpmail.View
	Err       error
}

// pgpPlugin returns the plugin that decrypts and verifies mail: the first,
// by ID, that declares message_raw.
func (m Model) pgpPlugin() (plugin.Plugin, bool) {
	for _, p := range m.plugins.manager.Plugins() {
		if p.Manifest.Permissions.MessageRaw {
			return p, true
		}
	}
	return plugin.Plugin{}, false
}

// pgpLooksProtected is a cheap check on what TideMail has stored: PGP/MIME
// leaves its ciphertext or signature as attachments, and inline PGP leaves
// armor in the text.
func pgpLooksProtected(msg db.Message, atts []db.Attachment) bool {
	if strings.Contains(msg.BodyText, "-----BEGIN PGP ") {
		return true
	}
	for _, a := range atts {
		switch strings.ToLower(a.ContentType) {
		case "application/pgp-encrypted", "application/pgp-signature":
			return true
		}
	}
	return false
}

// pgpOverlay swaps in the decoded body for display. Only the rendered copy
// changes; the stored message is untouched.
func (m Model) pgpOverlay(msg db.Message) db.Message {
	e := m.pgp.entries[msg.ID]
	if e == nil || e.view == nil {
		return msg
	}
	body := e.view.Message
	msg.BodyText, msg.BodyHTML = body.BodyText, body.BodyHTML
	if n := len(body.AttachmentData); n > 0 {
		names := make([]string, 0, n)
		for _, a := range body.AttachmentData {
			names = append(names, a.Filename)
		}
		msg.BodyText += "\n\n[Attachments inside the protected message are not available yet: " + strings.Join(names, ", ") + "]"
	}
	return msg
}

// pgpStatusLine is the one line shown under the message's meta line, or ""
// when the message is not PGP-protected.
func (m Model) pgpStatusLine(msg db.Message, width int) string {
	var text string
	bad := false
	e := m.pgp.entries[msg.ID]
	switch {
	case e != nil && e.pending:
		text = "PGP: working…"
	case e != nil && e.err != "":
		text, bad = "PGP: "+e.err, true
	case e != nil && e.view != nil:
		text, bad = describePGP(*e.view)
	case m.contentMessageID == msg.ID && pgpLooksProtected(msg, m.contentAttachments):
		if _, ok := m.pgpPlugin(); !ok {
			return ""
		}
		text = "PGP-protected message. Press " + m.keys.PGPDecode.Help().Key + " to decrypt or verify."
	default:
		return ""
	}
	style := m.styles.ContentMeta.Width(width)
	if bad {
		style = style.Foreground(m.styles.Theme.Error)
	}
	return " " + style.Render(truncate(sanitizePluginLine(text), max(1, width-m.styles.ContentMeta.GetHorizontalFrameSize())))
}

func (m Model) hasPGPPlugin() bool { _, ok := m.pgpPlugin(); return ok }

// pgpHintApplies says whether the key hints should offer the decrypt key: a
// PGP plugin is installed and the message in the content pane is protected and
// not yet decoded (a failed decode keeps the hint, as a retry).
func (m *Model) pgpHintApplies() bool {
	if _, ok := m.pgpPlugin(); !ok || m.contentMessageID == 0 {
		return false
	}
	msg := m.currentContentMessage()
	if msg == nil || !pgpLooksProtected(*msg, m.contentAttachments) {
		return false
	}
	e := m.pgp.entries[msg.ID]
	return e == nil || (e.view == nil && !e.pending)
}

// pgpEncryptedUnread reports a PGP/MIME encrypted message with no text of its
// own, which would otherwise render as "No message body." It reads the stored
// attachment list instead of the content pane's, so it is right for any
// message it is asked about, in any view.
func (m Model) pgpEncryptedUnread(msg db.Message) bool {
	if !msg.HasAttachment || m.db == nil || strings.TrimSpace(msg.BodyText) != "" || strings.TrimSpace(msg.BodyHTML) != "" {
		return false
	}
	if e := m.pgp.entries[msg.ID]; e != nil && (e.pending || e.view != nil) {
		return false
	}
	atts, err := m.db.GetAttachmentsMeta(msg.ID)
	if err != nil {
		return false
	}
	for _, a := range atts {
		if strings.EqualFold(a.ContentType, "application/pgp-encrypted") {
			return true
		}
	}
	return false
}

// pgpCallout is shown in place of "No message body." for such a message.
func (m Model) pgpCallout() string {
	if _, ok := m.pgpPlugin(); ok {
		return "This message is PGP-encrypted. Press " + m.keyHint(m.keys.PGPDecode) + " to decrypt it."
	}
	return "This message is PGP-encrypted. Install a PGP plugin to read it: Settings → Plugins → b."
}

func describePGP(v pgpmail.View) (text string, bad bool) {
	prefix := "PGP: "
	if v.Encrypted {
		prefix += "decrypted · "
	}
	s := v.Signature
	who := s.Signer
	if who == "" {
		who = s.Fingerprint
	}
	switch s.Status {
	case plugin.SignatureGood:
		return prefix + "good signature from " + who, false
	case plugin.SignatureBad:
		return prefix + "BAD SIGNATURE: the message may have been altered", true
	case plugin.SignatureUnknownKey:
		return prefix + "signed with a key you don't have" + suffixIf(who), false
	case plugin.SignatureExpired:
		return prefix + "signature from an expired key" + suffixIf(who), true
	}
	return prefix + "not signed", false
}

func suffixIf(who string) string {
	if who == "" {
		return ""
	}
	return " (" + who + ")"
}

// startPGPDecode begins decrypting or verifying msg.
func (m Model) startPGPDecode(msg db.Message) (tea.Model, tea.Cmd) {
	p, ok := m.pgpPlugin()
	if !ok {
		m.setStatus("No PGP plugin installed (it must declare the message_raw permission)", true)
		return m, m.clearStatusCmd()
	}
	if e := m.pgp.entries[msg.ID]; e != nil && e.pending {
		return m, nil
	}
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg, err := m.accountCfgForMailbox(msg.MailboxID)
	if mailbox == nil || err != nil || acfg.IMAPHost == "" || msg.UID == 0 {
		m.setStatus("PGP: this message can't be fetched from a server", true)
		return m, m.clearStatusCmd()
	}
	if m.pgp.entries == nil {
		m.pgp.entries = map[int64]*pgpEntry{}
	}
	m.pgp.entries[msg.ID] = &pgpEntry{pending: true}
	m.bodyCache.clear()
	m.viewportCache.clear()
	m.setViewportForCurrentRow()

	sessions, mgr, parent := m.sessions, m.plugins.manager, m.plugins.ctx
	mailboxName, uid, id, pluginID := mailbox.Name, msg.UID, msg.ID, p.Manifest.ID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, pgpTimeout)
		defer cancel()
		var raw []byte
		if err := sessions.Do(ctx, acfg, func(client *imapClient.Client) error {
			var ferr error
			raw, ferr = client.FetchRaw(ctx, mailboxName, uid)
			return ferr
		}); err != nil {
			return PGPDecodedMsg{MessageID: id, Err: fmt.Errorf("couldn't fetch the message: %w", err)}
		}
		view, err := pgpmail.Decode(raw, id, func(req plugin.DecodeRequest) (plugin.DecodeResult, error) {
			return mgr.Decode(ctx, pluginID, req)
		})
		if err != nil {
			return PGPDecodedMsg{MessageID: id, Err: err}
		}
		return PGPDecodedMsg{MessageID: id, View: &view}
	}
}

func (m Model) handlePGPDecoded(msg PGPDecodedMsg) (tea.Model, tea.Cmd) {
	if m.pgp.entries == nil {
		m.pgp.entries = map[int64]*pgpEntry{}
	}
	e := &pgpEntry{view: msg.View}
	if msg.Err != nil {
		e.err = msg.Err.Error()
	}
	m.pgp.entries[msg.MessageID] = e
	m.bodyCache.clear()
	m.viewportCache.clear()
	if m.contentMessageID != 0 {
		m.setViewportForCurrentRow()
	}
	return m, nil
}
