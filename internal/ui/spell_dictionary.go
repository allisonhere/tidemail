package ui

import (
	"fmt"
	"strings"
	"unicode"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// Settings → Editor → Personal dictionary: the words added to spell check.
// Words are added from the F7 panel; this overlay is where a mistaken one is
// removed, and where a word can be added without composing a message.

type spellDictState struct {
	words   []string
	cursor  int
	adding  bool
	input   textinput.Model
	message string // last error or confirmation, shown under the list
}

const spellDictRows = 10

func (m *Model) openSpellDictionary() {
	in := textinput.New()
	in.Placeholder = "word to add"
	in.CharLimit = 40
	m.spellDict = spellDictState{input: in}
	m.reloadSpellDictionary()
	m.overlay = overlaySpellDictionary
}

func (m *Model) reloadSpellDictionary() {
	m.spellDict.words = loadSpellWords(m.db)
	m.spellDict.cursor = clamp(m.spellDict.cursor, 0, max(0, len(m.spellDict.words)-1))
}

// refreshSpellChecker rebuilds the live checker from the database so an added
// or removed word takes effect in open and future compose windows.
func (m *Model) refreshSpellChecker() {
	setEditorSpell(m.cfg.Display.ComposeSpell, m.cfg.Display.ComposeSpellLang, loadSpellWords(m.db))
}

func validSpellWord(w string) bool {
	if len([]rune(w)) < 2 {
		return false
	}
	for _, r := range w {
		if !unicode.IsLetter(r) && r != '\'' && r != '’' && r != '-' {
			return false
		}
	}
	return true
}

func (m Model) handleSpellDictionaryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	d := &m.spellDict
	if d.adding {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			d.adding = false
			d.input.Blur()
			d.input.Reset()
			d.message = ""
		case keyMatches(msg, m.keys.Confirm):
			w := strings.TrimSpace(d.input.Value())
			switch {
			case !validSpellWord(w):
				d.message = "enter a word of letters (apostrophes and hyphens allowed)"
			case m.db == nil:
				d.message = "no database"
			default:
				if err := m.db.AddSpellWord(w); err != nil {
					d.message = "couldn't save: " + err.Error()
				} else {
					d.message = fmt.Sprintf("added %q", strings.ToLower(w))
					d.adding = false
					d.input.Blur()
					d.input.Reset()
					m.reloadSpellDictionary()
					m.refreshSpellChecker()
				}
			}
		default:
			var cmd tea.Cmd
			d.input, cmd = d.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	n := len(d.words)
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = overlaySettings
	case keyMatches(msg, m.keys.Up):
		if n > 0 {
			d.cursor = (d.cursor - 1 + n) % n
		}
	case keyMatches(msg, m.keys.Down):
		if n > 0 {
			d.cursor = (d.cursor + 1) % n
		}
	case msg.String() == "a" || msg.String() == "n":
		d.adding = true
		d.message = ""
		d.input.Reset()
		d.input.Focus()
	case msg.String() == "d" || msg.String() == "x" || msg.Type == tea.KeyDelete || msg.Type == tea.KeyBackspace:
		if n == 0 || m.db == nil {
			return m, nil
		}
		w := d.words[d.cursor]
		if err := m.db.RemoveSpellWord(w); err != nil {
			d.message = "couldn't remove: " + err.Error()
		} else {
			d.message = fmt.Sprintf("removed %q", w)
			m.reloadSpellDictionary()
			m.refreshSpellChecker()
		}
	}
	return m, nil
}

func (m Model) renderSpellDictionary(width int, chrome managerChrome) string {
	d := m.spellDict
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	rows := []string{}
	if d.adding {
		input := d.input
		input.Width = max(1, width-8)
		input.PromptStyle = base.Foreground(chrome.accent).Bold(true)
		input.TextStyle = base.Foreground(chrome.text)
		input.PlaceholderStyle = base.Foreground(chrome.muted)
		input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(accentReadableOn(chrome.text, chrome.accent, 4.5))
		input.Cursor.TextStyle = input.Cursor.Style
		rows = append(rows, inputViewWithCursor(input, true), "")
	}
	switch {
	case len(d.words) == 0:
		rows = append(rows, base.Foreground(chrome.muted).Render("No words yet. Press a to add one, or use a in the F7 panel while composing."))
	default:
		limit := min(spellDictRows, len(d.words))
		start := clamp(d.cursor-limit/2, 0, len(d.words)-limit)
		labelW := max(1, width-4-2)
		for i := start; i < start+limit; i++ {
			selected := i == d.cursor && !d.adding
			label := base.Foreground(chrome.text).Render(" " + truncate(d.words[i], max(1, labelW-1)))
			rows = append(rows, softRail(chrome, selected, chrome.baseBg)+padStyled(label, labelW, chrome.baseBg))
		}
	}
	if d.message != "" {
		rows = append(rows, "", base.Foreground(chrome.muted).Render(d.message))
	}
	body := base.Foreground(chrome.text).Width(width).Padding(1, 2).Render(strings.Join(rows, "\n"))
	var hints string
	if d.adding {
		hints = renderSoftHints(width, chrome, "enter", "add", "esc", "cancel")
	} else {
		hints = renderSoftHints(width, chrome, "a", "add", "d", "remove", "↑↓", "move", "esc", "back")
	}
	return lipgloss.JoinVertical(lipgloss.Left, body, hints)
}
