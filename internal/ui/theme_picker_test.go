package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
)

func themePickerNames(entries []themePickerEntry, pt []Theme) []string {
	var out []string
	for _, e := range entries {
		if e.header == "" {
			out = append(out, pt[e.idx].Name)
		}
	}
	return out
}

func TestThemePickerGroupsVariantsUnderTheirFamily(t *testing.T) {
	pt := PickableThemes()
	entries := themePickerEntries(pt, "")
	if entries[0].header != "Catppuccin" {
		t.Fatalf("first entry = %+v, want the Catppuccin header", entries[0])
	}
	var solo []string
	for _, e := range entries {
		if e.solo {
			solo = append(solo, pt[e.idx].Name)
		}
	}
	for _, want := range []string{"nord", "palenight", "vt52", "match-omarchy"} {
		found := false
		for _, name := range solo {
			found = found || name == want
		}
		if !found {
			t.Errorf("%s should be a header-less single-theme row (solo = %v)", want, solo)
		}
	}
	if got := len(themePickerNames(entries, pt)); got != len(pt) {
		t.Fatalf("unfiltered picker lists %d themes, want all %d", got, len(pt))
	}
}

func TestThemePickerFilter(t *testing.T) {
	pt := PickableThemes()
	names := func(filter string) []string { return themePickerNames(themePickerEntries(pt, filter), pt) }

	if got := names("latte"); len(got) != 1 || got[0] != "catppuccin-latte" {
		t.Errorf(`filter "latte" = %v`, got)
	}
	if got := names("rose dawn"); len(got) != 1 || got[0] != "rose-pine-dawn" {
		t.Errorf(`filter "rose dawn" = %v, want every word to match`, got)
	}
	if got := names("zzzz"); len(got) != 0 {
		t.Errorf(`filter "zzzz" = %v, want none`, got)
	}
	light := names("light")
	if len(light) < 8 {
		t.Fatalf(`filter "light" = %v, want the light themes`, light)
	}
	for _, name := range light {
		th, _ := ThemeByName(name)
		if !th.IsLight() {
			t.Errorf(`filter "light" matched dark theme %s`, name)
		}
	}
}

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

func TestThemePickerPreviewsConfirmsAndReverts(t *testing.T) {
	m := openedThemePickerModel(t)
	startBg := m.styles.Theme.Bg

	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.activeTheme != 1 || m.themeCursor != 1 {
		t.Fatalf("down did not preview the next theme: active %d cursor %d", m.activeTheme, m.themeCursor)
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
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyDown})
	want := BuiltinThemes[2].Name
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEnter})
	if m.overlay != overlayNone || m.confirmedTheme != 2 || m.cfg.Theme != want {
		t.Fatalf("enter did not confirm %s: overlay %v confirmed %d cfg %q", want, m.overlay, m.confirmedTheme, m.cfg.Theme)
	}
}

func TestThemePickerSearchJumpsAndEscClears(t *testing.T) {
	m := openedThemePickerModel(t)
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("/")})
	if !m.themeFiltering {
		t.Fatal("/ should start the search")
	}
	for _, r := range "nord" {
		m = pickerKey(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	// "nord" also matches Nightfox's Nordfox; the cursor lands on the first match.
	if got := m.styles.Theme.Name; got != "nord" {
		t.Fatalf("typing a filter should preview the first match, got %s", got)
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.themeFilter != "" || !m.themeFiltering {
		t.Fatalf("first esc clears the filter and keeps searching: filter %q filtering %v", m.themeFilter, m.themeFiltering)
	}
	m = pickerKey(m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.themeFiltering || m.overlay != overlayThemePicker {
		t.Fatal("second esc leaves the search, not the picker")
	}
}

func TestThemePickerRendersGroupsAndSample(t *testing.T) {
	m := openedThemePickerModel(t)
	chrome := newManagerChrome(46, m.styles.Theme, false)
	view := ansi.Strip(m.renderThemePicker(46, 26, chrome))
	for _, want := range []string{"/ to search", "Catppuccin", "Mocha", "selected row", "link", "quote", "error", "match"} {
		if !strings.Contains(view, want) {
			t.Errorf("picker view is missing %q:\n%s", want, view)
		}
	}
	m.themeFilter = "zzzz"
	if view := ansi.Strip(m.renderThemePicker(46, 26, newManagerChrome(46, m.styles.Theme, false))); !strings.Contains(view, "no matching themes") {
		t.Errorf("an empty result should say so:\n%s", view)
	}
}
