package ui

import (
	"regexp"
	"slices"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/allisonhere/tidemail/internal/config"
)

func spellCompose(t *testing.T, body string) ComposeModel {
	t.Helper()
	setEditorSpell(true, nil)
	t.Cleanup(func() { setEditorSpell(false, nil) })
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput.SetValue(body)
	return c
}

func spellKey(c ComposeModel, k tea.KeyMsg) ComposeModel {
	c, _, _ = c.Update(k, DefaultKeys)
	return c
}

func spellRune(c ComposeModel, r rune) ComposeModel {
	return spellKey(c, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
}

func misspelled(c ComposeModel) []string {
	var out []string
	for _, s := range c.bodyInput.Misspellings() {
		out = append(out, s.Word)
	}
	return out
}

func TestSpellOffByDefaultDoesNothing(t *testing.T) {
	setEditorSpell(false, nil)
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.SetValue("I recieve teh email")
	if got := c.bodyInput.Misspellings(); got != nil {
		t.Fatalf("spell check is off; got misspellings %v", got)
	}
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyF7})
	if c.spellOpen || !strings.Contains(c.statusMsg, "off") {
		t.Fatalf("F7 with spell check off should say so: open=%v status=%q", c.spellOpen, c.statusMsg)
	}
}

func TestSpellFlagsUserTextButNeverTheQuote(t *testing.T) {
	c := spellCompose(t, "I recieve it.\n\nOn Mon, Bob wrote:\n> teh quoted wrold")
	if got := misspelled(c); !slices.Equal(got, []string{"recieve"}) {
		t.Fatalf("misspelled = %v; want only the user's own text", got)
	}
	flags := c.bodyInput.typoFlags()
	for i, want := range map[int]bool{1: false, 2: true, 8: true, 9: false} {
		if flags[i] != want {
			t.Fatalf("flags[%d] = %v; want %v", i, flags[i], want)
		}
	}
}

func TestSpellStepThroughReplacing(t *testing.T) {
	c := spellCompose(t, "I recieve teh email")
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyF7})
	if !c.spellOpen || len(c.spellSuggest) == 0 || c.spellSuggest[0] != "receive" {
		t.Fatalf("panel should open on 'recieve' with receive first: open=%v sugg=%v", c.spellOpen, c.spellSuggest)
	}
	view := ansi.Strip(c.View(100, 30, BuildStyles(CatppuccinMocha, "compact", "square")))
	if !strings.Contains(view, "spelling 1/2") || !strings.Contains(view, "recieve") {
		t.Fatalf("panel not shown in the footer:\n%s", view)
	}

	c = spellKey(c, tea.KeyMsg{Type: tea.KeyEnter})
	if got := c.bodyInput.Value(); got != "I receive teh email" {
		t.Fatalf("after first replace: %q", got)
	}
	if !c.spellOpen {
		t.Fatal("panel should stay open for the next word")
	}
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyEnter})
	if got := c.bodyInput.Value(); got != "I receive the email" {
		t.Fatalf("after second replace: %q", got)
	}
	if c.spellOpen || !strings.Contains(c.statusMsg, "fixed 2") {
		t.Fatalf("should finish: open=%v status=%q", c.spellOpen, c.statusMsg)
	}
	// One undo step per replacement.
	c.bodyInput.ed.Undo()
	if got := c.bodyInput.Value(); got != "I receive teh email" {
		t.Fatalf("a single undo should revert one replacement, got %q", got)
	}
}

func TestSpellChooseSuggestionByNumberAndArrows(t *testing.T) {
	c := spellCompose(t, "definately")
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyF7})
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyRight})
	if c.spellPick != 1 {
		t.Fatalf("right arrow should choose the next suggestion, pick=%d", c.spellPick)
	}
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyLeft})
	c = spellRune(c, '1')
	if got := c.bodyInput.Value(); got != "definitely" {
		t.Fatalf("'1' should replace with the first suggestion, got %q", got)
	}
}

func TestSpellSkipIgnoreAndAdd(t *testing.T) {
	c := spellCompose(t, "Zorblax qwertyish plmokn")
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyF7})
	c = spellRune(c, 'a') // add Zorblax
	if !slices.Equal(c.spellAdded, []string{"Zorblax"}) {
		t.Fatalf("spellAdded = %v", c.spellAdded)
	}
	if got := misspelled(c); !slices.Equal(got, []string{"qwertyish", "plmokn"}) {
		t.Fatalf("an added word must stop being flagged, got %v", got)
	}
	c = spellRune(c, 's') // skip qwertyish
	if c.spellIdx != 1 {
		t.Fatalf("skip should advance, idx=%d", c.spellIdx)
	}
	c = spellRune(c, 'i') // ignore plmokn for this session
	if c.spellOpen {
		t.Fatalf("panel should close when nothing is left")
	}
	if got := misspelled(c); !slices.Equal(got, []string{"qwertyish"}) {
		t.Fatalf("ignored word should stop being flagged, skipped one stays: %v", got)
	}
	if c.bodyInput.Value() != "Zorblax qwertyish plmokn" {
		t.Fatalf("skip/ignore/add must not edit the text, got %q", c.bodyInput.Value())
	}
}

func TestSpellEscClosesWithoutChanges(t *testing.T) {
	c := spellCompose(t, "I recieve it")
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyF7})
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyEsc})
	if c.spellOpen || c.bodyInput.Value() != "I recieve it" {
		t.Fatalf("esc should close and leave the text alone: open=%v body=%q", c.spellOpen, c.bodyInput.Value())
	}
}

func TestSpellCleanTextReportsNoMisspellings(t *testing.T) {
	c := spellCompose(t, "All good here.")
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyF7})
	if c.spellOpen || !strings.Contains(c.statusMsg, "no misspellings") {
		t.Fatalf("open=%v status=%q", c.spellOpen, c.statusMsg)
	}
}

func TestSpellAddedWordsArePersisted(t *testing.T) {
	m := settingsSaveTestModel(t, false)
	setEditorSpell(true, nil)
	t.Cleanup(func() { setEditorSpell(false, nil) })
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.spellAdded = []string{"Zorblax"}
	m.persistSpellWords()
	if m.compose.spellAdded != nil {
		t.Fatalf("pending words should be cleared, got %v", m.compose.spellAdded)
	}
	words, err := m.db.ListSpellWords()
	if err != nil || !slices.Equal(words, []string{"zorblax"}) {
		t.Fatalf("stored words = %v (err %v)", words, err)
	}
	// A new session loads them back.
	if got := loadSpellWords(m.db); !slices.Equal(got, []string{"zorblax"}) {
		t.Fatalf("loadSpellWords = %v", got)
	}
}

func TestSettingsSpellToggleRoundTrips(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	if s.ApplyTo(config.DefaultConfig()).Display.ComposeSpell {
		t.Fatal("spell check must default to off")
	}
	s.setActiveSection(ssEditor)
	if !slices.Contains(s.sectionFields(ssEditor), sfComposeSpell) {
		t.Fatal("Editor section is missing the spell check toggle")
	}
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfComposeSpell)
	s, _, _ = s.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if !s.ApplyTo(config.DefaultConfig()).Display.ComposeSpell {
		t.Fatal("toggling should turn spell check on in the saved config")
	}
}

// Ripple calls Style for every run, including unflagged ones, so the
// underline must be applied only to the flagged word.
func TestSpellUnderlineOnlyOnMisspelledWord(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })
	c := spellCompose(t, "I recieve it")
	raw := c.bodyInput.View()
	if plain := ansi.Strip(raw); !strings.Contains(plain, "I recieve it") {
		t.Fatalf("text changed: %q", plain)
	}
	var underlined strings.Builder
	for _, m := range regexp.MustCompile(`\x1b\[4;[0-9;]*m(.)\x1b\[0m`).FindAllStringSubmatch(raw, -1) {
		underlined.WriteString(m[1])
	}
	if underlined.String() != "recieve" {
		t.Fatalf("underlined text = %q; want exactly the misspelled word\n%q", underlined.String(), raw)
	}
}

func TestSpellNameWordsExtractsNamesLocalPartsAndDomains(t *testing.T) {
	got := spellNameWords([]string{
		"Priya Natarajan <priya.n@acme-corp.com>",
		`"O'Brien, Siobhán" <sobrien@example.co.uk>`,
		"not an address",
		"x@y.io", // fragments too short to keep
	})
	for _, want := range []string{"priya", "natarajan", "acme", "corp", "o'brien", "siobhán", "sobrien", "example"} {
		if !slices.Contains(got, want) {
			t.Errorf("missing %q in %v", want, got)
		}
	}
	for _, bad := range []string{"com", "uk", "n", "io"} {
		if slices.Contains(got, bad) {
			t.Errorf("should not keep %q: %v", bad, got)
		}
	}
	if len(slices.Compact(slices.Clone(got))) != len(got) {
		t.Errorf("duplicates in %v", got)
	}
}

func TestSpellAcceptsCorrespondentNamesAndKeepsThemAcrossToggle(t *testing.T) {
	editorSpellNames = nil
	t.Cleanup(func() { editorSpellNames = nil; setEditorSpell(false, nil) })
	setEditorSpell(true, nil)

	c := spellCompose(t, "Thanks Priya, Natarajan from Acme says hi")
	if got := misspelled(c); len(got) == 0 {
		t.Fatalf("test needs names to start out flagged, got none")
	}
	addSpellNames([]string{"Priya Natarajan <priya@acme.com>"})
	c = spellCompose(t, "Thanks Priya, Natarajan from Acme says hi")
	if got := misspelled(c); len(got) != 0 {
		t.Fatalf("correspondent names should not be flagged, got %v", got)
	}
	// Turning the setting off and on again rebuilds the Checker; names survive.
	setEditorSpell(false, nil)
	setEditorSpell(true, nil)
	c = spellCompose(t, "Thanks Priya, Natarajan from Acme says hi")
	if got := misspelled(c); len(got) != 0 {
		t.Fatalf("names must survive a settings re-save, got %v", got)
	}
}

func TestSpellDoesNotUnderlineTheWordBeingTyped(t *testing.T) {
	c := spellCompose(t, "I recieve it") // SetValue parks the caret at the end
	flagged := func() string {
		var sb strings.Builder
		for i, f := range c.bodyInput.typoFlags() {
			if f {
				sb.WriteRune([]rune(c.bodyInput.Value())[i])
			}
		}
		return sb.String()
	}
	if got := flagged(); got != "recieve" {
		t.Fatalf("caret after the word 'it' should leave 'recieve' underlined, got %q", got)
	}

	// Caret right after the misspelled word: still being typed, so not underlined.
	c.bodyInput.SetValue("I recieve")
	if got := flagged(); got != "" {
		t.Fatalf("a word under the caret must not be underlined, got %q", got)
	}
	// ...but the F7 list still sees it.
	if got := misspelled(c); !slices.Equal(got, []string{"recieve"}) {
		t.Fatalf("Misspellings must include the caret word, got %v", got)
	}
	// Typing a space moves the caret off the word, which is now flagged.
	c = spellKey(c, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{' '}})
	if got := flagged(); got != "recieve" {
		t.Fatalf("after a space the word should be underlined, got %q", got)
	}
}
