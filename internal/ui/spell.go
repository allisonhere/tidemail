package ui

import (
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidespell"
)

// Offline spell check for compose (F7). Misspellings are underlined in the
// body while Settings → Editor → Spell check is on; F7 opens a panel in the
// footer that steps through them. The panel lives in the footer so compose's
// existing height accounting (composeLayout measures the hints) just works.

const spellSuggestionCount = 5

// loadSpellWords reads the user's personal dictionary. A nil database (tests,
// previews) or a read error yields no words rather than failing startup.
func loadSpellWords(database *db.DB) []string {
	if database == nil {
		return nil
	}
	words, err := database.ListSpellWords()
	if err != nil {
		return nil
	}
	return words
}

// openSpell starts stepping through the misspellings in the body.
func (c *ComposeModel) openSpell() {
	if editorSpell == nil {
		c.statusMsg = "spell check is off — enable it in Settings → Editor"
		c.isErr = false
		return
	}
	if len(c.bodyInput.Misspellings()) == 0 {
		c.statusMsg = "no misspellings"
		c.isErr = false
		return
	}
	c.spellOpen = true
	c.spellIdx = 0
	c.spellFixed = 0
	c.loadSpellWord()
}

func (c *ComposeModel) closeSpell(done bool) {
	c.spellOpen = false
	c.spellSuggest = nil
	switch {
	case done && c.spellFixed > 0:
		c.statusMsg = fmt.Sprintf("spell check done — fixed %d", c.spellFixed)
	case done:
		c.statusMsg = "spell check done"
	}
	c.isErr = false
}

// loadSpellWord prepares suggestions for the current misspelling, or closes
// the panel when none are left.
func (c *ComposeModel) loadSpellWord() {
	spans := c.bodyInput.Misspellings()
	if c.spellIdx >= len(spans) {
		c.closeSpell(true)
		return
	}
	c.spellSuggest = editorSpell.Suggest(spans[c.spellIdx].Word, spellSuggestionCount)
	c.spellPick = 0
}

func (c ComposeModel) updateSpell(km tea.KeyMsg, keys KeyMap) (ComposeModel, tea.Cmd, bool) {
	spans := c.bodyInput.Misspellings()
	if c.spellIdx >= len(spans) || editorSpell == nil {
		c.closeSpell(true)
		return c, nil, false
	}
	sp := spans[c.spellIdx]
	n := len(c.spellSuggest)
	switch {
	case keyMatches(km, keys.Cancel), km.String() == "q":
		c.closeSpell(false)
	case keyMatches(km, keys.Left), keyMatches(km, keys.Up):
		if n > 0 {
			c.spellPick = (c.spellPick - 1 + n) % n
		}
	case keyMatches(km, keys.Right), keyMatches(km, keys.Down), keyMatches(km, keys.Tab):
		if n > 0 {
			c.spellPick = (c.spellPick + 1) % n
		}
	case len(km.Runes) == 1 && km.Runes[0] >= '1' && km.Runes[0] <= '9' && int(km.Runes[0]-'1') < n:
		c.spellPick = int(km.Runes[0] - '1')
		return c.replaceSpell(sp)
	case keyMatches(km, keys.Confirm):
		return c.replaceSpell(sp)
	case km.String() == "s" || km.String() == "n":
		c.spellIdx++
		c.loadSpellWord()
	case km.String() == "i":
		// Ignore for this session: the word is accepted until restart.
		editorSpell.Add(sp.Word)
		editorSpellGen++
		c.loadSpellWord()
	case km.String() == "a":
		editorSpell.Add(sp.Word)
		editorSpellGen++
		c.spellAdded = append(c.spellAdded, sp.Word)
		c.loadSpellWord()
	}
	return c, nil, false
}

func (c ComposeModel) replaceSpell(sp tidespell.Span) (ComposeModel, tea.Cmd, bool) {
	if len(c.spellSuggest) == 0 {
		c.spellIdx++
		c.loadSpellWord()
		return c, nil, false
	}
	c.bodyInput.ReplaceSpan(sp, c.spellSuggest[c.spellPick])
	c.spellFixed++
	c.loadSpellWord() // the next misspelling now sits at spellIdx
	return c, nil, false
}

// spellPanel renders the footer shown while stepping through misspellings.
func (c ComposeModel) spellPanel(width int, chrome managerChrome) string {
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	spans := c.bodyInput.Misspellings()
	if c.spellIdx >= len(spans) {
		return padStyled(base.Render("  "), width, chrome.baseBg)
	}
	word := spans[c.spellIdx].Word
	head := base.Foreground(chrome.muted).Render(fmt.Sprintf("  spelling %d/%d  ", c.spellIdx+1, len(spans))) +
		base.Foreground(chrome.errorFg).Bold(true).Render(word) +
		base.Foreground(chrome.muted).Render("  →  ")
	if len(c.spellSuggest) == 0 {
		head += base.Foreground(chrome.muted).Render("no suggestions")
	}
	for i, s := range c.spellSuggest {
		label := fmt.Sprintf("%d %s", i+1, s)
		if i == c.spellPick {
			head += base.Foreground(chrome.accent).Bold(true).Render("[" + label + "]")
		} else {
			head += base.Foreground(chrome.text).Render(" " + label + " ")
		}
	}
	keyStyle := base.Foreground(chrome.text)
	lblStyle := base.Foreground(chrome.muted)
	pairs := []string{"enter", "replace", "←→", "choose", "s", "skip", "i", "ignore all", "a", "add word", "esc", "close"}
	var parts []string
	for i := 0; i+1 < len(pairs); i += 2 {
		parts = append(parts, keyStyle.Render(pairs[i])+lblStyle.Render(" "+pairs[i+1]))
	}
	hints := base.Render("  ") + strings.Join(parts, base.Render("   "))
	return lipgloss.JoinVertical(lipgloss.Left,
		padStyled(head, width, chrome.baseBg),
		padStyled(hints, width, chrome.baseBg))
}

// persistSpellWords saves words the user added in the compose panel to their
// personal dictionary, then clears the pending list.
func (m *Model) persistSpellWords() {
	if len(m.compose.spellAdded) == 0 {
		return
	}
	if m.db != nil {
		for _, w := range m.compose.spellAdded {
			if err := m.db.AddSpellWord(w); err != nil {
				m.compose.statusMsg = "couldn't save word: " + err.Error()
				m.compose.isErr = true
			}
		}
	}
	m.compose.spellAdded = nil
}
