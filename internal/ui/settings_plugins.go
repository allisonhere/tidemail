package ui

import (
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Settings owns navigation; plugin state and persistence remain on Model.
func (m Model) handleSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	load := m.prepareSettingsPlugins()
	var next tea.Model
	var cmd tea.Cmd
	if m.settings.activeSection == ssPlugins && m.settings.focusedPane == settingsPaneDetail {
		next, cmd = m.handleSettingsPluginPane(msg)
	} else {
		next, cmd = m.updateSettings(msg)
	}
	updated := next.(Model)
	nextLoad := updated.prepareSettingsPlugins()
	return updated, tea.Batch(load, cmd, nextLoad)
}

func (m *Model) prepareSettingsPlugins() tea.Cmd {
	if m.overlay != overlaySettings || m.settings.activeSection != ssPlugins || m.settings.pluginPaneLoaded {
		return nil
	}
	m.settings.pluginPaneLoaded = true
	m.plugins.listCursor = 0
	m.plugins.scroll = 0
	m.refreshPluginEventStatus()
	return loadPluginAnnotationCountsCmd(m.db)
}

func (m Model) handleSettingsPluginPane(msg tea.Msg) (tea.Model, tea.Cmd) {
	key, isKey := msg.(tea.KeyMsg)
	if !isKey {
		if m.settings.pluginEditing && m.plugins.settings.editing {
			var cmd tea.Cmd
			m.plugins.settings.input, cmd = m.plugins.settings.input.Update(msg)
			return m, cmd
		}
		return m.updateSettings(msg)
	}
	// Typed secrets must reach the masked input before navigation shortcuts.
	if m.settings.pluginEditing && m.plugins.settings.editing {
		return m.handlePluginSettingsKey(key)
	}
	if key.String() == "ctrl+s" {
		return m.updateSettings(msg)
	}
	if key.Type == tea.KeyTab || key.Type == tea.KeyShiftTab {
		m.settings.setFocusedPane(settingsPaneSidebar)
		return m, nil
	}
	if m.settings.pluginEditing {
		if keyMatches(key, m.keys.Cancel, m.keys.Back) {
			m.settings.pluginEditing = false
			m.plugins.scroll = 0
			m.revealSettingsPluginCursor()
			return m, nil
		}
		return m.handlePluginSettingsKey(key)
	}
	if keyMatches(key, m.keys.Cancel, m.keys.Back, m.keys.Left) {
		m.settings.setFocusedPane(settingsPaneSidebar)
		return m, nil
	}
	if keyMatches(key, m.keys.Up, m.keys.Down) {
		previous := m.plugins.listCursor
		count := len(m.pluginListEntries())
		step := 1
		if keyMatches(key, m.keys.Up) {
			step = -1
		}
		m.plugins.listCursor = clamp(previous+step, 0, max(0, count-1))
		if previous != m.plugins.listCursor {
			m.revealSettingsPluginCursor()
		} else {
			width, height, chrome := m.settingsPluginPaneSize()
			lines, pairs, _ := m.settingsPluginContent(width, chrome)
			bodyH := settingsPluginBodyHeight(width, height, chrome, pairs)
			m.plugins.scroll = clamp(m.plugins.scroll+step, 0, max(0, len(lines)-bodyH))
		}
		return m, nil
	}
	return m.handlePluginListKey(key)
}

func (m Model) settingsPluginPaneSize() (int, int, managerChrome) {
	width := min(m.width-4, settingsOverlayMaxW)
	height := min(m.height-4, 36)
	chrome := newManagerChrome(width, m.styles.Theme, m.styles.PlainUI)
	_, rightW := settingsSplitWidths(width)
	bodyH := max(1, height-1-lipgloss.Height(m.settings.viewHints(width, chrome)))
	return rightW, bodyH, chrome
}

func (m Model) settingsPluginContent(width int, chrome managerChrome) ([]string, []string, int) {
	if m.settings.pluginEditing {
		return m.pluginSettingsContent(width, chrome)
	}
	lines, starts := m.pluginListLines(width, chrome)
	if len(lines) == 0 {
		lines = []string{"No plugins installed."}
	}
	anchor := 0
	if m.plugins.listCursor >= 0 && m.plugins.listCursor < len(starts) {
		anchor = starts[m.plugins.listCursor]
	}
	pairs := append(m.pluginListHintPairs(), "esc", "sections")
	return lines, pairs, anchor
}

func settingsPluginBodyHeight(width, height int, chrome managerChrome, pairs []string) int {
	return max(1, height-2-lipgloss.Height(renderSoftHints(width, chrome, pairs...)))
}

func (m *Model) revealSettingsPluginCursor() {
	width, height, chrome := m.settingsPluginPaneSize()
	lines, pairs, anchor := m.settingsPluginContent(width, chrome)
	bodyH := settingsPluginBodyHeight(width, height, chrome, pairs)
	m.plugins.scroll = clamp(m.plugins.scroll, 0, max(0, len(lines)-bodyH))
	if anchor < m.plugins.scroll {
		m.plugins.scroll = anchor
	} else if anchor >= m.plugins.scroll+bodyH {
		m.plugins.scroll = anchor - bodyH + 1
	}
}

func (m Model) renderSettingsPluginPane(width, height int, chrome managerChrome) string {
	lines, pairs, anchor := m.settingsPluginContent(width, chrome)
	bodyH := settingsPluginBodyHeight(width, height, chrome, pairs)
	scroll := clamp(m.plugins.scroll, 0, max(0, len(lines)-bodyH))
	if m.settings.pluginEditing {
		// Keep the selected field (or the secret input) visible on resize.
		scroll = clamp(anchor-bodyH+1, 0, max(0, len(lines)-bodyH))
	}
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	title := softRail(chrome, m.settings.focusedPane == settingsPaneDetail, chrome.baseBg) +
		base.Foreground(chrome.accent).Bold(true).Render("Plugins")
	body := clampView(strings.Join(lines[scroll:min(len(lines), scroll+bodyH)], "\n"), width, bodyH, chrome.baseBg)
	hints := renderSoftHints(width, chrome, pairs...)
	return clampView(lipgloss.JoinVertical(lipgloss.Left, padStyled(title, width, chrome.baseBg), padStyled("", width, chrome.baseBg), body, hints), width, height, chrome.baseBg)
}
