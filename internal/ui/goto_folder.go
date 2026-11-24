package ui

import (
	"strings"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// gotoFolder is the "go to folder" prompt: type part of a folder's name and
// jump the sidebar straight to it, across every account.
type gotoFolder struct {
	input   textinput.Model
	matches []folderCandidate
	cursor  int
}

// gotoFolderCandidates lists the folders the sidebar can show. Hidden folders
// are left out unless hidden folders are being shown. With more than one
// account the label is prefixed with the account name.
func (m Model) gotoFolderCandidates() []folderCandidate {
	view := folderView{prefs: m.mailboxPrefs, showHidden: m.showHiddenFolders}
	multi := len(m.accounts) > 1
	var out []folderCandidate
	for _, mb := range view.visible(m.mailboxes) {
		if mb.Name == "" {
			continue
		}
		label := mb.Name
		if multi {
			label = m.accountName(mb.AccountID) + " / " + label
		}
		out = append(out, folderCandidate{mailboxID: mb.ID, label: label})
	}
	return out
}

func (m *Model) openGotoFolder() {
	if len(m.mailboxes) == 0 {
		m.setStatus("no folders to go to yet", false)
		return
	}
	input := textinput.New()
	input.Placeholder = "type part of a folder name"
	input.Focus()
	m.gotoFolder = gotoFolder{input: input}
	m.refreshGotoFolder()
	m.overlay = overlayGotoFolder
}

func (m *Model) refreshGotoFolder() {
	m.gotoFolder.matches = rankFolders(m.gotoFolder.input.Value(), m.gotoFolderCandidates())
	m.gotoFolder.cursor = clamp(m.gotoFolder.cursor, 0, max(0, len(m.gotoFolder.matches)-1))
}

func (m Model) handleGotoFolder(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.gotoFolder = gotoFolder{}
		m.overlay = overlayNone
		return m, nil
	case keyMatches(msg, m.keys.Confirm):
		if len(m.gotoFolder.matches) == 0 {
			return m, nil
		}
		id := m.gotoFolder.matches[m.gotoFolder.cursor].mailboxID
		m.gotoFolder = gotoFolder{}
		m.overlay = overlayNone
		return m.goToMailbox(id)
	case msg.Type == tea.KeyUp, msg.String() == "ctrl+p":
		if m.gotoFolder.cursor > 0 {
			m.gotoFolder.cursor--
		}
		return m, nil
	case msg.Type == tea.KeyDown, msg.String() == "ctrl+n":
		if m.gotoFolder.cursor < len(m.gotoFolder.matches)-1 {
			m.gotoFolder.cursor++
		}
		return m, nil
	}
	var cmd tea.Cmd
	before := m.gotoFolder.input.Value()
	m.gotoFolder.input, cmd = m.gotoFolder.input.Update(msg)
	if m.gotoFolder.input.Value() != before {
		m.gotoFolder.cursor = 0
	}
	m.refreshGotoFolder()
	return m, cmd
}

// goToMailbox puts the sidebar cursor on a folder, opening any collapsed
// account or parent folders above it, and loads it like a cursor step would.
func (m Model) goToMailbox(id int64) (tea.Model, tea.Cmd) {
	target := m.mailboxByID(id)
	if target == nil {
		m.setStatus("folder not found", true)
		return m, m.clearStatusCmd()
	}
	if m.sidebarRowIndex(id) < 0 {
		m.collapsedAccounts[target.AccountID] = false
		for _, mb := range m.mailboxes {
			if mailboxIsDescendant(mb, *target) {
				m.collapsedSections[folderCollapseKey(mb)] = false
			}
		}
		m.saveCollapseState()
		m.rebuildSidebar()
	}
	idx := m.sidebarRowIndex(id)
	if idx < 0 {
		m.setStatus("folder is not shown in the sidebar", false)
		return m, m.clearStatusCmd()
	}
	if idx == m.sidebarCursor {
		// Already there: still move focus to the sidebar and reload the folder.
		next, focusCmd := m.focusPane(paneAccounts)
		m = next.(Model)
		return m, tea.Batch(focusCmd, m.loadSelectedFolderCmd())
	}
	return m.selectSidebarRow(idx)
}

func (m Model) sidebarRowIndex(mailboxID int64) int {
	for i, r := range m.sidebarRows {
		if r.kind == rowKindMailbox && r.mailboxID == mailboxID {
			return i
		}
	}
	return -1
}

func (m Model) renderGotoFolder(width, height int, chrome managerChrome) string {
	in := m.gotoFolder.input
	in.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent)
	in.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	in.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	prompt := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(0, 2).
		Render(clampView("go to: "+in.View(), max(1, width-4), 1, chrome.baseBg))

	labelW := max(1, width-2) // minus the 2-cell rail
	listH := max(1, height-6)
	start := 0
	if m.gotoFolder.cursor >= listH {
		start = m.gotoFolder.cursor - listH + 1
	}
	end := min(start+listH, len(m.gotoFolder.matches))
	rows := make([]string, 0, listH)
	for i := start; i < end; i++ {
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + truncate(m.gotoFolder.matches[i].label, max(1, labelW-1)))
		rows = append(rows, softRail(chrome, i == m.gotoFolder.cursor, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	if len(m.gotoFolder.matches) == 0 {
		note := "no folder matches"
		if strings.TrimSpace(m.gotoFolder.input.Value()) == "" {
			note = "no folders"
		}
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 2).Render(note))
	}
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	}
	hints := renderSoftHints(width, chrome, "type", "filter", "↑/↓", "choose", "enter", "go", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, prompt, lipgloss.JoinVertical(lipgloss.Left, rows...), hints)
}
