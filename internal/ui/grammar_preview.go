package ui

import (
	"fmt"
	"strings"

	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Grammar preview: the AI-corrected reply shown before it replaces the body.
// A long email must neither run off the bottom of the screen nor hide the
// accept/cancel hints, so the text lives in a viewport sized from the screen.

// grammarGeometry returns the modal's inner width and height (borders are
// drawn outside both) and the viewport's text width and height. Chrome inside
// the box is a title row, a blank, the text, a blank, and the hint row.
func (m Model) grammarGeometry() (winW, winH, bodyW, bodyH int) {
	winW = max(20, min(m.width-10, 100))
	winH = max(8, m.height-4) // the border adds two rows; leave a margin
	bodyW = max(1, winW-4)
	bodyH = max(1, winH-4)
	return winW, winH, bodyW, bodyH
}

// syncGrammarVP sizes the viewport and loads the corrected text, keeping the
// scroll position when nothing changed (sizing clamps it if the screen shrank).
func (m *Model) syncGrammarVP() {
	_, _, bodyW, bodyH := m.grammarGeometry()
	text := m.grammarCorrected
	if text == "" {
		text = "(no changes)"
	}
	t := m.styles.Theme
	bg := modalSurface(t)
	content := lipgloss.NewStyle().Background(bg).Foreground(t.Fg).Width(bodyW).Render(text)
	m.grammarVP.Width = bodyW
	m.grammarVP.Height = bodyH
	m.grammarVP.Style = lipgloss.NewStyle().Background(bg)
	m.grammarVP.SetContent(content)
}

// openGrammarPreview shows corrected, starting at the top.
func (m *Model) openGrammarPreview() {
	m.grammarVP = viewport.New(1, 1)
	m.syncGrammarVP()
	m.grammarVP.GotoTop()
	m.overlay = overlayGrammarPreview
}

func (m Model) handleGrammarPreviewKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Yes):
		m.compose.bodyInput.SetValue(m.grammarCorrected + m.grammarQuoteSuffix)
		m.grammarQuoteSuffix = ""
		m.compose.statusMsg = "grammar checked"
		m.compose.isErr = false
		m.overlay = overlayCompose
		// Return focus to the body at the top in Insert mode — SetValue parks
		// the caret at the end (inside the quote, off-screen), which made it
		// look like typing did nothing.
		m.compose.focusBodyAtStart()
		return m, nil
	case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
		m.grammarQuoteSuffix = ""
		m.compose.statusMsg = ""
		m.overlay = overlayCompose
		m.compose.focusBodyAtStart()
		return m, nil
	}
	m.syncGrammarVP() // the screen may have been resized since the last key
	switch msg.String() {
	case "g", "home":
		m.grammarVP.GotoTop()
	case "G", "end":
		m.grammarVP.GotoBottom()
	default:
		var cmd tea.Cmd
		m.grammarVP, cmd = m.grammarVP.Update(msg)
		return m, cmd
	}
	return m, nil
}

func (m Model) renderGrammarPreview() string {
	winW, winH, _, _ := m.grammarGeometry()
	m.syncGrammarVP()
	t := m.styles.Theme
	bg := modalSurface(t)
	border := t.OverlayBorder
	if border == "" {
		border = t.BorderFocus
	}
	dim := readableText(t.Dimmed, bg, 3.0)

	title := lipgloss.NewStyle().Background(bg).Foreground(t.BorderFocus).Bold(true).Width(winW).Padding(0, 1).Render("Grammar Preview")
	blank := lipgloss.NewStyle().Background(bg).Width(winW).Render("")
	body := lipgloss.NewStyle().Background(bg).Width(winW).PaddingLeft(2).Render(m.grammarVP.View())

	hint := "y  accept    n  cancel"
	if !m.grammarVP.AtTop() || !m.grammarVP.AtBottom() {
		hint += fmt.Sprintf("    ↑↓/pgup/pgdn  scroll    %d%%", int(m.grammarVP.ScrollPercent()*100))
	}
	hints := lipgloss.NewStyle().Background(bg).Foreground(dim).Width(winW).Padding(0, 1).Render(truncate(hint, max(1, winW-2)))

	return lipgloss.NewStyle().
		Background(bg).
		Border(lipPaneBorder(m.styles.PlainUI)).
		BorderForeground(border).
		Width(winW).Height(winH).
		Render(strings.Join([]string{title, blank, body, blank, hints}, "\n"))
}
