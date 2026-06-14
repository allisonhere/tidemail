// Command editortest is a throwaway harness for driving internal/editor in
// isolation, before it is wired into compose. It hosts the real editor.Model
// in a minimal Bubble Tea program so you can type, select, move by word, and
// resize the terminal to watch soft-wrap reflow.
//
//	go run ./cmd/editortest
//
// Keys: ctrl+c / esc quit · arrows move · shift+arrows select ·
// ctrl+arrows word · home/end · pgup/pgdn · ctrl+a select all ·
// ctrl+z undo · ctrl+y redo · ctrl+o copy · ctrl+k cut · ctrl+p paste ·
// bracketed paste.
package main

import (
	"fmt"
	"os"

	"github.com/allisonhere/tide/internal/clipboard"
	"github.com/allisonhere/tide/internal/editor"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

var (
	boxStyle    = lipgloss.NewStyle().Border(lipgloss.RoundedBorder()).Padding(0, 1)
	cursorStyle = lipgloss.NewStyle().Reverse(true)
	selStyle    = lipgloss.NewStyle().Background(lipgloss.Color("63")).Foreground(lipgloss.Color("231"))
	helpStyle   = lipgloss.NewStyle().Faint(true)
	statusStyle = lipgloss.NewStyle().Foreground(lipgloss.Color("244"))
)

const seed = "Type here.\n\n" +
	"Try arrows, shift+arrows to select, ctrl+arrows to jump words,\n" +
	"home/end, pgup/pgdn, ctrl+a to select all, backspace/delete.\n\n" +
	"Resize the terminal to watch soft-wrap reflow."

type model struct {
	ed     editor.Model
	w, h   int
	ready  bool
	status string
}

// copiedMsg / pastedMsg carry the result of an async clipboard command so the
// shell-out never blocks the UI loop.
type copiedMsg struct{ err error }
type pastedMsg struct {
	text string
	err  error
}

func initialModel() model {
	ed := editor.New()
	ed.SetValue(seed)
	return model{ed: ed}
}

func copyCmd(text string) tea.Cmd {
	return func() tea.Msg { return copiedMsg{err: clipboard.Copy(text)} }
}

func pasteCmd() tea.Cmd {
	return func() tea.Msg {
		text, err := clipboard.Read()
		return pastedMsg{text: text, err: err}
	}
}

func (m model) Init() tea.Cmd { return nil }

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.w, m.h = msg.Width, msg.Height
		// Reserve rows/cols for the border, padding, help and status lines.
		ew := msg.Width - 4
		eh := msg.Height - 4
		if ew < 1 {
			ew = 1
		}
		if eh < 1 {
			eh = 1
		}
		m.ed.SetSize(ew, eh)
		m.ready = true
		return m, nil
	case copiedMsg:
		if msg.err != nil {
			m.status = "copy failed: " + msg.err.Error()
		} else {
			m.status = "copied to clipboard"
		}
		return m, nil
	case pastedMsg:
		if msg.err != nil {
			m.status = "paste failed: " + msg.err.Error()
			return m, nil
		}
		m.ed.InsertString(msg.text)
		m.status = fmt.Sprintf("pasted %d chars", len([]rune(msg.text)))
		return m, nil
	case tea.KeyMsg:
		switch msg.Type {
		case tea.KeyCtrlC, tea.KeyEsc:
			return m, tea.Quit
		case tea.KeyCtrlO: // copy
			sel := m.ed.SelectedText()
			if sel == "" {
				m.status = "nothing selected"
				return m, nil
			}
			return m, copyCmd(sel)
		case tea.KeyCtrlK: // cut
			sel := m.ed.SelectedText()
			if sel == "" {
				m.status = "nothing selected"
				return m, nil
			}
			m.ed.DeleteSelection()
			return m, copyCmd(sel)
		case tea.KeyCtrlP: // paste via clipboard read (mirrors the app; terminal paste also works)
			return m, pasteCmd()
		}
		m.ed.UpdateKey(msg)
		return m, nil
	}
	return m, nil
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	body := m.ed.View(editor.Options{
		Cursor:   cursorStyle.Render(" "),
		Selected: func(s string) string { return selStyle.Render(s) },
	})
	box := boxStyle.Width(m.w - 2).Render(body)
	help := helpStyle.Render("ctrl+c/esc quit · shift+arrows select · ctrl+arrows word · ctrl+a all · ctrl+z/y undo/redo · ctrl+o copy · ctrl+k cut · ctrl+p paste")
	status := statusStyle.Render(fmt.Sprintf(
		"idx=%d  top=%d  size=%dx%d  sel=%q  %s",
		m.ed.CursorIndex(), m.ed.ViewportTop(), m.w-4, m.h-4, clip(m.ed.SelectedText()), m.status,
	))
	return lipgloss.JoinVertical(lipgloss.Left, help, box, status)
}

// clip shortens selection text for the single-line status display.
func clip(s string) string {
	const max = 30
	r := []rune(s)
	if len(r) > max {
		return string(r[:max]) + "…"
	}
	return s
}

func main() {
	p := tea.NewProgram(initialModel(), tea.WithAltScreen())
	if _, err := p.Run(); err != nil {
		fmt.Fprintln(os.Stderr, "editortest:", err)
		os.Exit(1)
	}
}
