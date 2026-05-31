package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func (m *Model) openSaveAttachPicker(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Fall back to immediate save
		m.overlay = overlayNone
		return
	}

	var fe []fileEntry
	// Add "select this folder" entry at the top
	fe = append(fe, fileEntry{name: "✓ select this folder", isDir: false, size: 0})
	// Add parent-dir entry unless we're at filesystem root
	if dir != "/" {
		fe = append(fe, fileEntry{name: "..", isDir: true})
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Skip hidden files
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fe = append(fe, fileEntry{
			name:  e.Name(),
			isDir: e.IsDir(),
			size:  info.Size(),
		})
	}

	// Sort: dirs first, then files; alphabetically within each group
	sort.Slice(fe, func(i, j int) bool {
		if fe[i].isDir != fe[j].isDir {
			return fe[i].isDir
		}
		return strings.ToLower(fe[i].name) < strings.ToLower(fe[j].name)
	})

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
	header := renderManagerHeader("SAVE ATTACHMENTS", width, chrome)

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
	listH := max(1, height-7) // header(1) + dir(1) + actions(1) + padding
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
		cursor := idx == m.saveAttachPicker.cursor

		entryStyle := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Padding(0, 2)

		if cursor {
			entryStyle = entryStyle.
				Background(chrome.accent).
				Foreground(contrastFg(chrome.accent))
		} else {
			entryStyle = entryStyle.Foreground(chrome.text)
		}

		var label string
		switch {
		case e.name == "✓ select this folder":
			label = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Render(e.name)
			if cursor {
				label = lipgloss.NewStyle().
					Background(chrome.accent).
					Foreground(contrastFg(chrome.accent)).
					Render(e.name)
			} else {
				label = lipgloss.NewStyle().
					Background(chrome.baseBg).
					Foreground(lipgloss.Color("2")).
					Render(e.name)
			}
		case e.isDir && e.name == "..":
			label = lipgloss.NewStyle().
				Foreground(chrome.accent).
				Render("📁 " + e.name)
			if cursor {
				label = lipgloss.NewStyle().
					Background(chrome.accent).
					Foreground(contrastFg(chrome.accent)).
					Render("📁 " + e.name)
			} else {
				label = lipgloss.NewStyle().
					Background(chrome.baseBg).
					Foreground(chrome.accent).
					Render("📁 " + e.name)
			}
		case e.isDir:
			label = "📁 " + e.name
			if !cursor {
				label = lipgloss.NewStyle().
					Foreground(chrome.accent).
					Render(label)
			}
		default:
			label = "📄 " + e.name
		}

		// Truncate to fit width
		entryWidth := max(1, width-4)
		label = clampView(label, entryWidth, 1, chrome.baseBg)

		row := entryStyle.Render(label)
		rows = append(rows, row)
	}

	// Fill remaining rows to maintain consistent height
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Padding(0, 2).
			Render(""))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Actions
	actions := renderManagerActions(width, chrome,
		"↵", "open/confirm",
		"←", "parent",
		"esc", "cancel",
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, dirLine, body, actions)
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
