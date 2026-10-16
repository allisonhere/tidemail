package ui

import (
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func dictModel(t *testing.T) Model {
	t.Helper()
	m := settingsSaveTestModel(t, false)
	m.width, m.height = 100, 40
	m.cfg.Display.ComposeSpell = true
	setEditorSpell(true, nil)
	t.Cleanup(func() { setEditorSpell(false, nil); editorSpellNames = nil })
	m.openSpellDictionary()
	return m
}

func dictKey(m Model, k tea.KeyMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

func dictType(m Model, s string) Model {
	for _, r := range s {
		m = dictKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

func TestSpellDictionaryOpensFromSettingsAndEscReturns(t *testing.T) {
	m := settingsSaveTestModel(t, false)
	m.width, m.height = 100, 40
	m.settings.setActiveSection(ssEditor)
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setFocusedField(sfSpellDictionary)
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlaySpellDictionary {
		t.Fatalf("Enter on the setting should open the dictionary, overlay=%v", m.overlay)
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "personal dictionary") || !strings.Contains(view, "No words yet") {
		t.Fatalf("empty dictionary not rendered:\n%s", view)
	}
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlaySettings {
		t.Fatalf("Esc should return to Settings, overlay=%v", m.overlay)
	}
}

func TestSpellDictionaryAddRemoveUpdatesLiveChecker(t *testing.T) {
	m := dictModel(t)
	if editorSpell.Known("Zorblax") {
		t.Fatal("precondition: Zorblax is unknown")
	}

	m = dictKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	if !m.spellDict.adding {
		t.Fatal("'a' should start adding")
	}
	m = dictType(m, "Zorblax")
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.spellDict.adding || !slices.Equal(m.spellDict.words, []string{"zorblax"}) {
		t.Fatalf("after Enter: adding=%v words=%v msg=%q", m.spellDict.adding, m.spellDict.words, m.spellDict.message)
	}
	if got, _ := m.db.ListSpellWords(); !slices.Equal(got, []string{"zorblax"}) {
		t.Fatalf("not persisted: %v", got)
	}
	if !editorSpell.Known("Zorblax") {
		t.Fatal("the live checker should know the new word immediately")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, "zorblax") {
		t.Fatalf("word not listed:\n%s", view)
	}

	m = dictKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if len(m.spellDict.words) != 0 {
		t.Fatalf("'d' should remove the selected word, words=%v", m.spellDict.words)
	}
	if got, _ := m.db.ListSpellWords(); len(got) != 0 {
		t.Fatalf("not removed from the database: %v", got)
	}
	if editorSpell.Known("Zorblax") {
		t.Fatal("a removed word must stop being accepted straight away")
	}
}

func TestSpellDictionaryRejectsBadWordsAndCancels(t *testing.T) {
	m := dictModel(t)
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = dictType(m, "abc123")
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if !m.spellDict.adding || m.spellDict.message == "" || len(m.spellDict.words) != 0 {
		t.Fatalf("a word with digits must be rejected with a message: adding=%v msg=%q words=%v",
			m.spellDict.adding, m.spellDict.message, m.spellDict.words)
	}
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.spellDict.adding || m.overlay != overlaySpellDictionary {
		t.Fatalf("Esc while adding should cancel the add only: adding=%v overlay=%v", m.spellDict.adding, m.overlay)
	}
}

func TestSpellDictionaryKeepsCorrespondentNamesWhenRemoving(t *testing.T) {
	m := dictModel(t)
	addSpellNames([]string{"Priya Natarajan <priya@acme.com>"})
	if err := m.db.AddSpellWord("zorblax"); err != nil {
		t.Fatal(err)
	}
	m.reloadSpellDictionary()
	m = dictKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	if !editorSpell.Known("Priya") {
		t.Fatal("rebuilding the checker after a removal must keep correspondents' names")
	}
}
