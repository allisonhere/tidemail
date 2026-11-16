package ui

import (
	"fmt"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
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

// autoLoadTrustedImages loads remote images for the message in the reader
// when its sender is on [display] trusted_image_senders. It does nothing for
// untrusted senders, when images are off, or once consent is already given.
func (m *Model) autoLoadTrustedImages() tea.Cmd {
	if len(m.cfg.Display.TrustedImageSenders) == 0 || m.contentMessageID == 0 ||
		m.images == nil || !m.images.enabled || !m.images.graphics() ||
		m.images.isAllowed(m.contentMessageID) {
		return nil
	}
	cur := m.currentContentMessage()
	if cur == nil || !config.IsTrustedImageSender(m.cfg.Display.TrustedImageSenders, cur.From) {
		return nil
	}
	fetch, _ := m.loadRemoteImagesForCurrent()
	return fetch
}

// toggleTrustedImageSender adds the current sender to the trusted list (and
// loads this message's images) or removes them, then saves the config.
func (m Model) toggleTrustedImageSender() (tea.Model, tea.Cmd) {
	cur := m.currentContentMessage()
	if cur == nil {
		return m, nil
	}
	prev := m.cfg.Display.TrustedImageSenders
	next, added, addr := config.ToggleTrustedImageSender(append([]string(nil), prev...), cur.From)
	if addr == "" {
		m.setStatus("no sender address to trust", true)
		return m, m.clearStatusCmd()
	}
	m.cfg.Display.TrustedImageSenders = next
	if err := m.saveConfig(); err != nil {
		m.cfg.Display.TrustedImageSenders = prev
		return m, m.clearStatusCmd()
	}
	if !added {
		m.setStatus(fmt.Sprintf("%s removed from trusted image senders", addr), false)
		return m, m.clearStatusCmd()
	}
	var fetch tea.Cmd
	if m.images != nil && m.images.enabled && m.images.graphics() {
		fetch, _ = m.loadRemoteImagesForCurrent()
	}
	m.setStatus(fmt.Sprintf("remote images will always load from %s", addr), false)
	return m, tea.Batch(m.clearStatusCmd(), fetch)
}
