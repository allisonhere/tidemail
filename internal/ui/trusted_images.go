package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

// loadRemoteImagesForCurrent grants remote-image consent for the email shown
// in the reader, re-renders it, and returns the fetch command plus the number
// of remote images requested.
func (m *Model) loadRemoteImagesForCurrent() (tea.Cmd, int) {
	// Consent covers only the selected email displayed in the reader.
	for _, id := range m.imageConsentMessageIDs() {
		m.images.allow(id)
	}
	m.bodyCache.clear()
	m.viewportCache.clear()
	urls := m.remoteImageURLsForCurrent()
	fetch := fetchRemoteImagesCmd(m.images, m.contentMessageID, urls)
	m.setViewportForCurrentRow()
	return fetch, len(urls)
}

// trustedMessagesInView returns the displayed messages whose own sender is on
// [display] trusted_image_senders and whose images are not loaded yet. Trust is
// decided per message: in a view with several senders, an untrusted message
// never inherits consent from a trusted one.
func (m *Model) trustedMessagesInView() []db.Message {
	trusted := m.cfg.Display.TrustedImageSenders
	if len(trusted) == 0 || m.images == nil {
		return nil
	}
	var out []db.Message
	for _, msg := range m.currentViewMessages() {
		if msg.ID != 0 && !m.images.isAllowed(msg.ID) && config.IsTrustedImageSender(trusted, msg.From) {
			out = append(out, msg)
		}
	}
	return out
}

// loadTrustedImagesInView grants remote-image consent to the trusted messages
// in view only, re-renders, and returns the fetch command (nil if none).
func (m *Model) loadTrustedImagesInView() tea.Cmd {
	if m.contentMessageID == 0 || m.images == nil || !m.images.enabled || !m.images.graphics() {
		return nil
	}
	msgs := m.trustedMessagesInView()
	if len(msgs) == 0 {
		return nil
	}
	for _, msg := range msgs {
		m.images.allow(msg.ID)
	}
	m.bodyCache.clear()
	m.viewportCache.clear()
	urls := m.remoteImageURLsFor(msgs)
	fetch := fetchRemoteImagesCmd(m.images, m.contentMessageID, urls)
	m.setViewportForCurrentRow()
	return fetch
}

// autoLoadTrustedImages loads remote images for the displayed messages whose
// sender is on [display] trusted_image_senders. It does nothing for untrusted
// senders, when images are off, or once consent is already given.
func (m *Model) autoLoadTrustedImages() tea.Cmd {
	return m.loadTrustedImagesInView()
}

// toggleTrustedImageSender adds the current sender to the trusted list (and
// loads this message's images) or removes them, then saves the config.
func (m Model) toggleTrustedImageSender() (tea.Model, tea.Cmd) {
	cur := m.currentContentMessage()
	if cur == nil {
		return m, nil
	}
	prev := m.cfg.Display.TrustedImageSenders
	next, change, addr := config.ToggleTrustedImageSender(append([]string(nil), prev...), cur.From)
	if change == config.TrustUnchanged {
		m.setStatus("no sender address to trust", true)
		return m, m.clearStatusCmd()
	}
	m.cfg.Display.TrustedImageSenders = next
	if err := m.saveConfig(); err != nil {
		m.cfg.Display.TrustedImageSenders = prev
		return m, m.clearStatusCmd()
	}
	switch change {
	case config.TrustRemoved:
		m.setStatus(fmt.Sprintf("%s removed from trusted image senders", addr), false)
		return m, m.clearStatusCmd()
	case config.TrustExcluded:
		m.setStatus(fmt.Sprintf("%s excluded; the rest of @%s stays trusted", addr, domainOf(addr)), false)
		return m, m.clearStatusCmd()
	}
	fetch := m.loadTrustedImagesInView()
	if change == config.TrustExclusionDrop {
		m.setStatus(fmt.Sprintf("%s trusted again through @%s", addr, domainOf(addr)), false)
	} else {
		m.setStatus(fmt.Sprintf("remote images will always load from %s", addr), false)
	}
	return m, tea.Batch(m.clearStatusCmd(), fetch)
}

func domainOf(addr string) string {
	if i := strings.LastIndex(addr, "@"); i >= 0 {
		return addr[i+1:]
	}
	return addr
}
