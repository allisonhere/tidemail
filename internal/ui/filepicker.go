package ui

import (
	"os"
	"path/filepath"
	"strings"

	"github.com/charmbracelet/lipgloss"
)

// pathPicker is a small full-pane directory browser for choosing one file. It
// carries navigation and rendering in one place so a screen only decides what
// to do with the chosen path. (Compose and the contact manager predate it and
// keep their own copies.)
type pathPicker struct {
	filePicker
}

// open lists dir and puts the cursor on the first entry.
func (p *pathPicker) open(dir string) error {
	entries, err := listDirEntries(dir, p.showHidden)
	if err != nil {
		return err
	}
	p.currentDir, p.entries, p.cursor, p.active = dir, entries, 0, true
	return nil
}

// openAt starts in the folder of path when it exists, otherwise in fallback.
func (p *pathPicker) openAt(path, fallback string) error {
	if dir := filepath.Dir(path); path != "" && dirExists(dir) {
		if err := p.open(dir); err == nil {
			for i, e := range p.entries {
				if e.name == filepath.Base(path) {
					p.cursor = i
				}
			}
			return nil
		}
	}
	return p.open(fallback)
}

func (p *pathPicker) move(delta int) {
	p.cursor = clamp(p.cursor+delta, 0, max(0, len(p.entries)-1))
}

// jump moves to the first entry starting with the typed letter.
func (p *pathPicker) jump(letter string) {
	letter = strings.ToLower(letter)
	if len(letter) != 1 || letter[0] < 'a' || letter[0] > 'z' {
		return
	}
	for i, e := range p.entries {
		if e.name != ".." && strings.HasPrefix(strings.ToLower(e.name), letter) {
			p.cursor = i
			return
		}
	}
}

// parent opens the enclosing folder; at the filesystem root it does nothing.
func (p *pathPicker) parent() error {
	up := filepath.Dir(p.currentDir)
	if up == p.currentDir {
		return nil
	}
	return p.open(up)
}

// enter opens a folder, or returns the chosen file's full path.
func (p *pathPicker) enter() (path string, picked bool, err error) {
	if p.cursor < 0 || p.cursor >= len(p.entries) {
		return "", false, nil
	}
	e := p.entries[p.cursor]
	switch {
	case e.isDir && e.name == "..":
		return "", false, p.parent()
	case e.isDir:
		return "", false, p.open(filepath.Join(p.currentDir, e.name))
	}
	return filepath.Join(p.currentDir, e.name), true, nil
}

// view draws the folder line, the scrolling list, an optional status line and
// the given key hints.
func (p pathPicker) view(width, height int, chrome managerChrome, status string, hints ...string) string {
	dirLine := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Width(width).Padding(0, 2).
		Render(clampView(p.currentDir, max(1, width-4), 1, chrome.baseBg))
	listH := max(1, height-lipgloss.Height(dirLine)-3)
	start := 0
	if p.cursor >= listH {
		start = p.cursor - listH + 1
	}
	end := min(start+listH, len(p.entries))
	labelW := max(1, width-2) // minus the 2-cell rail
	var rows []string
	for i := start; i < end; i++ {
		entry := p.entries[i]
		fg := chrome.text
		if entry.isDir && i != p.cursor {
			fg = chrome.accent
		}
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Render(" " + truncate(contactFileEntryLabel(entry), max(1, labelW-1)))
		rows = append(rows, softRail(chrome, i == p.cursor, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	}
	parts := []string{dirLine, lipgloss.JoinVertical(lipgloss.Left, rows...)}
	if status != "" {
		parts = append(parts, status)
	}
	parts = append(parts, renderSoftHints(width, chrome, hints...))
	return lipgloss.JoinVertical(lipgloss.Left, parts...)
}

// homeDir is the picker's fallback start folder.
func homeDir() string {
	if home, err := os.UserHomeDir(); err == nil {
		return home
	}
	return string(filepath.Separator)
}
