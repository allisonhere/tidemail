package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) openSaveAttachPicker(dir string) {
	fe, err := listDirEntries(dir, m.saveAttachPicker.showHidden)
	if err != nil {
		// Fall back to immediate save
		m.overlay = overlayNone
		return
	}

	// Prepend "select this folder" entry above the parent-dir entry.
	fe = append([]fileEntry{{name: "✓ select this folder"}}, fe...)

	m.saveAttachPicker.currentDir = dir
	m.saveAttachPicker.entries = fe
	m.saveAttachPicker.cursor = 0
	m.saveAttachPicker.active = true
}

func (m *Model) saveAttachPickerUp() {
	if m.saveAttachPicker.cursor > 0 {
		m.saveAttachPicker.cursor--
	}
}

func (m *Model) saveAttachPickerDown() {
	if m.saveAttachPicker.cursor < len(m.saveAttachPicker.entries)-1 {
		m.saveAttachPicker.cursor++
	}
}

func (m *Model) saveAttachPickerEnterDir() {
	entry := m.saveAttachPicker.entries[m.saveAttachPicker.cursor]
	if entry.name == ".." {
		parent := filepath.Dir(m.saveAttachPicker.currentDir)
		m.openSaveAttachPicker(parent)
		return
	}
	if entry.isDir {
		sub := filepath.Join(m.saveAttachPicker.currentDir, entry.name)
		m.openSaveAttachPicker(sub)
		return
	}
}

func (m *Model) saveAttachPickerUpDir() {
	parent := filepath.Dir(m.saveAttachPicker.currentDir)
	if parent == m.saveAttachPicker.currentDir {
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return
	}
	m.openSaveAttachPicker(parent)
}

func (m Model) handleSaveAttachPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return m, nil

	case keyMatches(msg, m.keys.Up):
		m.saveAttachPickerUp()
		return m, nil

	case keyMatches(msg, m.keys.Down):
		m.saveAttachPickerDown()
		return m, nil

	case keyMatches(msg, m.keys.Confirm):
		entry := m.saveAttachPicker.entries[m.saveAttachPicker.cursor]
		if entry.isDir {
			m.saveAttachPickerEnterDir()
			return m, nil
		}
		// "select this folder" entry or a file — choose current directory
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return m, saveAttachmentsCmdTo(m.contentAttachments, m.saveAttachPicker.currentDir)

	case keyMatches(msg, m.keys.Left), keyMatches(msg, m.keys.Back):
		m.saveAttachPickerUpDir()
		return m, nil

	case msg.String() == ".":
		m.saveAttachPicker.showHidden = !m.saveAttachPicker.showHidden
		m.openSaveAttachPicker(m.saveAttachPicker.currentDir)
		return m, nil

	default:
		// Single-key quick-jump: press a letter to jump to first entry starting with it
		if len(msg.String()) == 1 && msg.String() >= "a" && msg.String() <= "z" || msg.String() >= "A" && msg.String() <= "Z" {
			lower := strings.ToLower(msg.String())
			for i, e := range m.saveAttachPicker.entries {
				if strings.HasPrefix(strings.ToLower(e.name), lower) {
					m.saveAttachPicker.cursor = i
					return m, nil
				}
			}
		}
		return m, nil
	}
}

func saveAttachmentsCmdTo(atts []db.Attachment, dir string) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return AttachmentsSavedMsg{Err: fmt.Errorf("create dir: %w", err)}
		}
		saved := 0
		for _, a := range atts {
			path := filepath.Join(dir, safeFilename(a.Filename))
			if err := os.WriteFile(path, a.Data, 0o644); err != nil {
				return AttachmentsSavedMsg{Err: fmt.Errorf("write %s: %w", a.Filename, err)}
			}
			saved++
		}
		return AttachmentsSavedMsg{Path: dir, Count: saved}
	}
}

func (m Model) renderSaveAttachPicker(width, height int, chrome managerChrome) string {
	// Current directory display
	dirStr := m.saveAttachPicker.currentDir
	if dirStr == "" {
		dirStr = "~"
	}
	dirLine := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2).
		Render(clampView(dirStr, width-2, 1, chrome.baseBg))

	// Entry list — scroll within available height
	labelW := max(1, width-2) // minus the 2-cell rail
	listH := max(1, height-6)
	entries := m.saveAttachPicker.entries

	// Calculate visible range
	start := 0
	if m.saveAttachPicker.cursor >= listH {
		start = m.saveAttachPicker.cursor - listH + 1
	}
	end := min(start+listH, len(entries))
	visible := entries[start:end]

	var rows []string
	for i, e := range visible {
		idx := start + i
		selected := idx == m.saveAttachPicker.cursor

		// Selection is the accent rail; entries keep their semantic colour.
		fg := chrome.text
		var text string
		switch {
		case e.name == "✓ select this folder":
			text = e.name
			if !selected {
				fg = chrome.successFg
			}
		case e.isDir && e.name == "..":
			text = "📁 " + e.name
			fg = chrome.accent
		case e.isDir:
			text = "📁 " + e.name
			fg = chrome.accent
		default:
			text = "📄 " + e.name
		}
		if selected {
			fg = chrome.text
		}
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + text)
		cell = truncateStyled(cell, labelW, chrome.baseBg)
		rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}

	// Fill remaining rows to maintain consistent height
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Render(""))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)

	hints := renderSoftHints(width, chrome,
		"↵", "open/confirm",
		"←", "parent",
		".", "hidden",
		"esc", "cancel",
	)

	return lipgloss.JoinVertical(lipgloss.Left, dirLine, body, hints)
}

func saveAttachmentsCmd(atts []db.Attachment) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return AttachmentsSavedMsg{Err: fmt.Errorf("home dir: %w", err)}
		}
		dir := filepath.Join(home, "Downloads", "tidemail-attachments")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return AttachmentsSavedMsg{Err: fmt.Errorf("create dir: %w", err)}
		}
		saved := 0
		for _, a := range atts {
			path := filepath.Join(dir, safeFilename(a.Filename))
			if err := os.WriteFile(path, a.Data, 0o644); err != nil {
				return AttachmentsSavedMsg{Err: fmt.Errorf("write %s: %w", a.Filename, err)}
			}
			saved++
		}
		return AttachmentsSavedMsg{Path: dir, Count: saved}
	}
}
