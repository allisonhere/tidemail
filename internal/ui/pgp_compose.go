package ui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailapp"
	"github.com/allisonhere/tidemail/internal/plugin"
	"github.com/allisonhere/tidemail/internal/smtp"
)

// Sending PGP mail. Sign (Alt+S) and Encrypt (Alt+E; F8 and F9 also work) are per-message toggles in
// compose, offered when a plugin with the message_compose permission is
// installed. The plugin runs after the user sends and before the message is
// queued, so the Outbox, retries, and the Sent copy all hold the protected
// form and never need the plugin or a passphrase again. If the plugin fails
// (no key for a recipient, passphrase cancelled, timeout) nothing is queued
// and compose stays open with the reason: mail is never sent less protected
// than the user asked.

// pgpSignTimeout bounds one prepare, including any passphrase prompt.
const pgpSignTimeout = 3 * time.Minute

// OutgoingProtectedMsg carries the outcome of protecting a queued message.
type OutgoingProtectedMsg struct {
	Queued SendQueuedMsg
	Err    error
}

// pgpComposePlugin returns the plugin that signs and encrypts: the first, by
// ID, that declares message_compose.
func (m Model) pgpComposePlugin() (plugin.Plugin, bool) {
	for _, p := range m.plugins.manager.Plugins() {
		if p.Manifest.Permissions.MessageCompose {
			return p, true
		}
	}
	return plugin.Plugin{}, false
}

// protectOutgoing signs and/or encrypts a queued message in the background.
func (m Model) protectOutgoing(q SendQueuedMsg) (tea.Model, tea.Cmd) {
	p, ok := m.pgpComposePlugin()
	if !ok {
		m.compose.busy = false
		m.compose.isErr = true
		m.compose.statusMsg = "PGP: no plugin with the message_compose permission is installed"
		return m, nil
	}
	m.compose.busy = true
	m.compose.isErr = false
	switch {
	case q.Encrypt:
		m.compose.statusMsg = "Encrypting…"
	default:
		m.compose.statusMsg = "Signing…"
	}
	mgr, parent, pluginID := m.plugins.manager, m.plugins.ctx, p.Manifest.ID
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(parent, pgpSignTimeout)
		defer cancel()
		protected, err := mailapp.Protect(ctx, func(ctx context.Context, req plugin.PrepareRequest) (plugin.PrepareResult, error) {
			return mgr.Prepare(ctx, pluginID, req)
		}, q.Msg, q.Sign, q.Encrypt)
		if err != nil {
			return OutgoingProtectedMsg{Queued: q, Err: err}
		}
		q.Msg, q.Sign, q.Encrypt = protected, false, false
		return OutgoingProtectedMsg{Queued: q}
	}
}

func (m Model) handleOutgoingProtected(msg OutgoingProtectedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		if m.overlay == overlayCompose {
			m.compose.busy = false
			m.compose.isErr = true
			m.compose.statusMsg = humanPGPError(msg.Err)
		}
		return m, nil
	}
	next, cmd := m.handleSendQueued(msg.Queued)
	return next, cmd
}

var noKeyRe = regexp.MustCompile(`^no usable key for (\S+)(?: \((.*)\))?$`)

// humanPGPError turns a PGP plugin failure into one short line for compose's
// status row, which is a single line cut at the window width: the part that
// matters (what, and for whom) comes first and the way out is brief. Anything
// not recognised keeps the plugin's own text.
func humanPGPError(err error) string {
	var perr *plugin.Error
	if !errors.As(err, &perr) {
		return "Not sent. PGP: " + sanitizePluginLine(err.Error())
	}
	text := sanitizePluginLine(perr.Message)
	lower := strings.ToLower(text)
	switch {
	case perr.Code == "no_public_key":
		if m := noKeyRe.FindStringSubmatch(text); m != nil {
			if m[2] == "" || m[2] == "no such key" {
				return fmt.Sprintf("Not sent: no PGP key for %s (alt+e turns encryption off)", m[1])
			}
			return fmt.Sprintf("Not sent: can't use the PGP key for %s: %s (alt+e turns encryption off)", m[1], m[2])
		}
		return "Not sent: a recipient has no usable PGP key (alt+e turns encryption off)"
	case perr.Code == "no_secret_key" || strings.HasPrefix(lower, "no secret key to sign"):
		who := strings.TrimSpace(strings.TrimPrefix(text, "no secret key to sign as"))
		if who != "" && who != text {
			return fmt.Sprintf("Not sent: no PGP signing key for %s (alt+s turns signing off)", who)
		}
		return "Not sent: you have no PGP signing key for this sender (alt+s turns signing off)"
	case strings.Contains(lower, "bad passphrase"):
		return "Not sent: wrong passphrase"
	case strings.Contains(lower, "couldn't get your passphrase"):
		return "Not sent: GnuPG got no passphrase (cancelled, or no prompt could open)"
	}
	return "Not sent. PGP: " + text
}

// protectionFlags reports which toggles a queued message was sent with, so
// editing it brings compose back with the same protection on. Losing the
// encrypt flag would let an edited message go out in the clear.
func protectionFlags(messageJSON []byte) (sign, encrypt bool) {
	var msg smtp.OutgoingMessage
	if json.Unmarshal(messageJSON, &msg) != nil || msg.Protected == nil {
		return false, false
	}
	return len(msg.Protected.Signature) > 0, len(msg.Protected.Encrypted) > 0
}

func (c ComposeModel) withProtection(sign, encrypt bool) ComposeModel {
	c.pgpSign, c.pgpEncrypt = sign, encrypt
	return c
}

// pgpLabel is the compose footer text for the toggles.
func pgpToggleLabel(name string, on bool) string {
	if on {
		return fmt.Sprintf("%s ✓", name)
	}
	return name
}

// composeRestored reopens a queued message for editing, with the PGP toggles
// it was sent with.
func (m Model) composeRestored(draft db.Draft, messageJSON []byte) ComposeModel {
	sign, encrypt := protectionFlags(messageJSON)
	return NewComposeFromDraft(draft, m.cfg.Accounts, m.addressBook).withProtection(sign, encrypt)
}
