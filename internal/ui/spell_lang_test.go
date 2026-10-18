package ui

import "testing"

func TestSpellLangIndex(t *testing.T) {
	for id, want := range map[string]int{"en": 0, "es": 1, "": 0, "fr": 0} {
		if got := spellLangIndex(id); got != want {
			t.Errorf("spellLangIndex(%q) = %d; want %d", id, got, want)
		}
	}
}

func TestSetEditorSpellLanguage(t *testing.T) {
	t.Cleanup(func() { setEditorSpell(false, "", nil) })

	setEditorSpell(true, "es", nil)
	if !editorSpell.Known("información") || editorSpell.Known("information") {
		t.Error("es should accept Spanish and reject English-only words")
	}
	setEditorSpell(true, "", nil)
	if !editorSpell.Known("information") || editorSpell.Known("información") {
		t.Error("an empty language should mean English")
	}
}
