// Command editortest is a throwaway harness for driving the ripple editor in
// isolation. It hosts the real ripple.Model
// in a minimal Bubble Tea program so you can type, select, move by word, and
// resize the terminal to watch soft-wrap reflow.
//
//	go run ./cmd/editortest
//
// Keys: esc quits · arrows move · shift+arrows select · ctrl+arrows word ·
// home/end · pgup/pgdn · ctrl+a select all · ctrl+z undo · ctrl+y redo ·
// ctrl+x cut · ctrl+v paste · bracketed paste. Copy/cut/paste are owned by the
// editor (wired via SetClipboard); this harness only routes keys and surfaces
// the resulting CopiedMsg/PasteMsg.
package main

import (
	"fmt"
	"os"

	"github.com/allisonhere/ripple"
	"github.com/allisonhere/tidemail/internal/clipboard"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// sysClipboard adapts internal/clipboard to ripple.Clipboard.
type sysClipboard struct{}

func (sysClipboard) Read() (string, error)   { return clipboard.Read() }
func (sysClipboard) Write(text string) error { return clipboard.Copy(text) }

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
	ed     ripple.Model
	w, h   int
	ready  bool
	status string
}

func initialModel() model {
	ed := ripple.New()
	ed.SetClipboard(sysClipboard{})
	ed.SetValue(seed)
	return model{ed: ed}
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
	case ripple.CopiedMsg:
		if msg.Err != nil {
			m.status = "copy failed: " + msg.Err.Error()
		} else {
			m.status = "copied to clipboard"
		}
		return m, nil
	case ripple.PasteMsg:
		if msg.Err != nil {
			m.status = "paste failed: " + msg.Err.Error()
			return m, nil
		}
		m.ed, _ = m.ed.Update(msg) // editor inserts the pasted text
		m.status = fmt.Sprintf("pasted %d chars", len([]rune(msg.Text)))
		return m, nil
	case tea.KeyMsg:
		if msg.Type == tea.KeyEsc {
			return m, tea.Quit
		}
		var cmd tea.Cmd
		m.ed, cmd = m.ed.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m model) View() string {
	if !m.ready {
		return "starting…"
	}
	body := m.ed.View(ripple.Options{
		Cursor:   cursorStyle.Render(" "),
		Selected: func(s string) string { return selStyle.Render(s) },
	})
	box := boxStyle.Width(m.w - 2).Render(body)
	help := helpStyle.Render("esc quit · shift+arrows select · ctrl+arrows word · ctrl+a all · ctrl+z/y undo/redo · ctrl+c copy · ctrl+x cut · ctrl+v paste")
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
