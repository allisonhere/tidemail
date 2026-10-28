package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/key"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

func openedThemePickerModel(t *testing.T) Model {
	t.Helper()
	orig := configSave
	configSave = func(config.Config) error { return nil }
	t.Cleanup(func() { configSave = orig })
	m, _ := newNeedsYouModel(t)
	m.cfg.Theme = "catppuccin-mocha"
	m.confirmedTheme, m.activeTheme = 0, 0
	m.openThemePicker()
	return m
}

func pickerKey(m Model, key tea.KeyMsg) Model {
	next, _ := m.handleThemePickerKey(key)
	return next.(Model)
}

func typeThemeSearch(m Model, text string) Model {
	for _, r := range text {
		m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	return m
}

// moveThemePicker presses Down n times.
func moveThemePicker(m Model, n int) Model {
	for range n {
		m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	return m
}

func TestThemePickerPreviewsConfirmsAndReverts(t *testing.T) {
	m := openedThemePickerModel(t)
	startBg := m.styles.Theme.Bg

	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.activeTheme != 1 {
		t.Fatalf("down did not preview the next theme: active %d", m.activeTheme)
	}
	if m.styles.Theme.Name != BuiltinThemes[1].Name {
		t.Fatalf("styles still use %q while previewing %q", m.styles.Theme.Name, BuiltinThemes[1].Name)
	}
	if m.cfg.Theme != "catppuccin-mocha" {
		t.Fatalf("previewing must not persist: cfg.Theme = %q", m.cfg.Theme)
	}

	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.overlay != overlayNone || m.activeTheme != 0 || m.styles.Theme.Bg != startBg {
		t.Fatalf("esc did not revert: overlay %v active %d", m.overlay, m.activeTheme)
	}

	m.openThemePicker()
	m = moveThemePicker(m, 2)
	want := BuiltinThemes[2].Name
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone || m.confirmedTheme != 2 || m.cfg.Theme != want {
		t.Fatalf("enter did not confirm %s: overlay %v confirmed %d cfg %q", want, m.overlay, m.confirmedTheme, m.cfg.Theme)
	}
}

func TestThemePickerSearchJumpsAndEscClears(t *testing.T) {
	m := openedThemePickerModel(t)
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !m.themePicker.Filtering() {
		t.Fatal("/ should start the search")
	}
	m = typeThemeSearch(m, "nord")
	// "nord" also matches Nightfox's Nordfox; the cursor lands on the first match.
	if got := m.styles.Theme.Name; got != "nord" {
		t.Fatalf("typing a filter should preview the first match, got %s", got)
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.themePicker.Filter() != "" || !m.themePicker.Filtering() {
		t.Fatalf("first esc clears the filter and keeps searching: filter %q filtering %v", m.themePicker.Filter(), m.themePicker.Filtering())
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.themePicker.Filtering() || m.overlay != overlayThemePicker {
		t.Fatal("second esc leaves the search, not the picker")
	}
}

func TestThemePickerSearchTextIsNotTreatedAsKeyBindings(t *testing.T) {
	m := openedThemePickerModel(t)
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	// j, k and q are bound to navigation and quit elsewhere; in the search they are text.
	m = typeThemeSearch(m, "jkq")
	if got := m.themePicker.Filter(); got != "jkq" {
		t.Fatalf("filter = %q, want the typed text", got)
	}
	if m.overlay != overlayThemePicker {
		t.Fatal("typing in the search must not close the picker")
	}
}

func TestThemePickerHonoursRemappedKeys(t *testing.T) {
	m := openedThemePickerModel(t)
	m.keys.Down = key.NewBinding(key.WithKeys("w"))
	m.keys.Up = key.NewBinding(key.WithKeys("s"))
	press := func(m Model, s string) Model {
		return pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(s)})
	}

	m = press(m, "w")
	if m.activeTheme != 1 {
		t.Fatalf("remapped down key did not move to the next theme: active %d", m.activeTheme)
	}
	m = press(m, "s")
	if m.activeTheme != 0 {
		t.Fatalf("remapped up key did not move back: active %d", m.activeTheme)
	}
	// j was Down in the default map; with it unbound the picker must ignore it.
	m = press(m, "j")
	if m.activeTheme != 0 {
		t.Fatalf("an unbound key moved the picker: active %d", m.activeTheme)
	}
}

func TestThemePickerRendersGroupsAndSample(t *testing.T) {
	m := openedThemePickerModel(t)
	view := ansi.Strip(m.renderThemePicker(46, 26))
	for _, want := range []string{"/ to search", "Catppuccin", "Mocha", "selected row", "link", "quote", "error", "match"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view is missing %q:\n%s", want, view)
		}
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	m = typeThemeSearch(m, "zzzz")
	if view := ansi.Strip(m.renderThemePicker(46, 26)); !strings.Contains(view, "no matching themes") {
		t.Errorf("an empty result should say so:\n%s", view)
	}
}

func TestMatchOmarchyIsListedWithoutASwatch(t *testing.T) {
	m := openedThemePickerModel(t)
	m = moveThemePicker(m, len(PickableThemes())-1)
	// The vt52 theme draws swatches as '#', which makes them easy to find in plain text.
	m.styles = BuildStyles(VT52, "comfortable", "square")
	var row string
	for _, line := range strings.Split(ansi.Strip(m.renderThemePicker(46, 26)), "\n") {
		if strings.Contains(line, ThemeNameMatchOmarchy) {
			row = line
		}
	}
	if row == "" {
		t.Fatal("match-omarchy row not found")
	}
	if strings.Contains(row, "#") {
		t.Errorf("match-omarchy must not show a placeholder swatch: %q", row)
	}
}
