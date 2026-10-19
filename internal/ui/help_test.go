package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
)

func TestRenderHelpDocumentsCredentialSafety(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"App Password", "keychain", "redacted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document credential safety term %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsContactsAndNotifications(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"contacts", "autocomplete", "vCard", "desktop notifications", "compose to selected contact"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document contacts/notifications term %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsComposeAndMessageActionUpdates(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"cancel queued send", "sender picker", "Signature", "send delay", "unsubscribe"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsAccountManagerShortcutAsUppercaseM(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	if !strings.Contains(view, "M accounts") {
		t.Fatalf("expected help to document uppercase account shortcut, got %q", view)
	}
	if strings.Contains(view, "m accounts") {
		t.Fatalf("expected help not to document lowercase account shortcut, got %q", view)
	}
}

func TestRenderHelpDocumentsContactManagerShortcutAsUppercaseC(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	if !strings.Contains(view, "C contacts") {
		t.Fatalf("expected help to document uppercase contact shortcut, got %q", view)
	}
	if strings.Contains(view, "c contacts") {
		t.Fatalf("expected help not to document lowercase contact shortcut, got %q", view)
	}
}

func TestRenderHelpScopesModalShortcuts(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"Compose Modal", "ctrl+g", "Account Manager Modal", "Contact Manager Modal", "Filters Modal"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to include scoped shortcut term %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "Messages / Content") || strings.Contains(view, "Accounts / Contacts") {
		t.Fatalf("expected mixed help sections to be replaced with scoped modal sections, got %q", view)
	}
}

func TestRenderHelpDocumentsSettingsCtrlSSave(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	settingsStart := strings.Index(view, "Settings Modal")
	if settingsStart < 0 {
		t.Fatalf("expected Settings Modal section, got %q", view)
	}
	settingsEnd := strings.Index(view[settingsStart+len("Settings Modal"):], "Pickers And Overlays")
	settingsSection := view[settingsStart:]
	if settingsEnd >= 0 {
		settingsSection = view[settingsStart : settingsStart+len("Settings Modal")+settingsEnd]
	}

	if !strings.Contains(settingsSection, "ctrl+s") || !strings.Contains(settingsSection, "save settings") {
		t.Fatalf("expected Settings help to document ctrl+s save, got %q", settingsSection)
	}
	if strings.Contains(settingsSection, "save and close") {
		t.Fatalf("expected Settings help not to document escape as save-and-close, got %q", settingsSection)
	}
}

func TestRenderHelpDocumentsNativeMessageSelection(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"v/V", "visual select", "y/ctrl+c", "copy selected message text", "`"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document native message copy term %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "v                  open AI summary overlay") {
		t.Fatalf("expected AI summary to move off v, got %q", view)
	}
}

func TestRenderHelpDocumentsThreadToggle(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	if !strings.Contains(view, "g") || !strings.Contains(view, "toggle threaded conversations") {
		t.Fatalf("expected help to document threaded conversation toggle, got %q", view)
	}
}

func TestRenderHelpDocumentsPaneResizeShortcuts(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"shift+←", "shift+→", "resize accounts pane", "shift+↑", "shift+↓", "resize messages/content split"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document pane resize term %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsCurrentHeaderSearchAndOAuthBehavior(t *testing.T) {
	view := ansi.Strip(renderHelp(120, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{
		"Pane header bars",
		"off restores one row",
		"focused pane title and shortcuts",
		"dedicated row above message subjects",
		"paused pending Google approval",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document current behavior %q, got %q", want, view)
		}
	}
	for _, stale := range []string{"status bar always shows", "Search overlays"} {
		if strings.Contains(view, stale) {
			t.Fatalf("expected stale help copy %q to be removed, got %q", stale, view)
		}
	}
}

func TestRenderLogViewerRowsFillModalWidth(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.styles = BuildStyles(CatppuccinMocha, "comfortable", "square")
	m.logBuffer = []logEntry{{
		Time:    time.Date(2026, 7, 30, 12, 34, 56, 0, time.UTC),
		Message: "short log message",
	}}

	view := ansi.Strip(m.renderLogViewer(50, 8))
	lines := strings.Split(view, "\n")
	if len(lines) == 0 {
		t.Fatal("expected log viewer output")
	}
	want := ansi.StringWidth(lines[0])
	if want == 0 {
		t.Fatalf("expected non-empty log viewer line, got %q", view)
	}
	for i, line := range lines {
		if got := ansi.StringWidth(line); got != want {
			t.Fatalf("line %d width = %d, want %d: %q\nfull view:\n%s", i, got, want, line, view)
		}
	}
}

func TestRenderHelpFiltersByQuery(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, "vim"))

	if !strings.Contains(view, "Vim Mode") {
		t.Fatalf("expected vim filter to keep the vim section, got %q", view)
	}
	if strings.Contains(view, "Security And Storage") {
		t.Fatalf("expected vim filter to drop unrelated sections, got %q", view)
	}
	if !strings.Contains(view, `match "vim"`) {
		t.Fatalf("expected filter summary line, got %q", view)
	}
}

func TestRenderHelpFilterMatchingSectionKeepsAllEntries(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, "filters modal"))

	for _, want := range []string{"Filters Modal", "new rule", "enable or disable selected rule"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected section-name match to keep whole section (missing %q), got %q", want, view)
		}
	}
}

func TestRenderHelpFilterNoMatches(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, "xyzzy"))

	if !strings.Contains(view, `No shortcuts match "xyzzy"`) {
		t.Fatalf("expected empty-result message, got %q", view)
	}
	if strings.Contains(view, "Global") {
		t.Fatalf("expected all sections dropped for a non-matching query, got %q", view)
	}
}

func TestHelpOverlaySearchKeyFlow(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = next.(Model)

	key := func(r rune) {
		next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
		m = next.(Model)
	}
	press := func(kt tea.KeyType) {
		next, _ := m.Update(tea.KeyMsg{Type: kt})
		m = next.(Model)
	}

	key('?')
	if m.overlay != overlayHelp {
		t.Fatalf("expected help overlay, got %v", m.overlay)
	}

	key('/')
	if !m.helpSearchActive {
		t.Fatal("expected / to activate help search")
	}
	for _, r := range "vim" {
		key(r)
	}
	view := ansi.Strip(m.helpVP.View())
	if !strings.Contains(view, "Vim Mode") || strings.Contains(view, "Security And Storage") {
		t.Fatalf("expected filtered help content while typing, got %q", view)
	}

	// enter keeps the filter but stops editing; q must close (not type).
	press(tea.KeyEnter)
	if m.helpSearchActive {
		t.Fatal("expected enter to stop search editing")
	}
	if m.helpSearchInput.Value() != "vim" {
		t.Fatalf("expected filter to persist after enter, got %q", m.helpSearchInput.Value())
	}

	// esc clears the filter first, second esc closes help.
	press(tea.KeyEsc)
	if m.overlay != overlayHelp || m.helpSearchInput.Value() != "" {
		t.Fatalf("expected first esc to clear filter and keep help open, overlay=%v query=%q", m.overlay, m.helpSearchInput.Value())
	}
	press(tea.KeyEsc)
	if m.overlay != overlayNone {
		t.Fatalf("expected second esc to close help, got %v", m.overlay)
	}
}

func TestHelpStatusBarIntroWrapsInsteadOfClipping(t *testing.T) {
	for _, width := range []int{100, 60} {
		view := ansi.Strip(renderHelp(width, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
		flat := strings.Join(strings.Fields(view), " ")
		for _, want := range []string{"Status bar", "Pane headers on", "? help", "Pane headers off", "Accounts also keeps the global shortcuts"} {
			if !strings.Contains(flat, want) {
				t.Fatalf("width %d: help intro lost %q:\n%s", width, want, view)
			}
		}
	}
}

func TestRenderHelpDocumentsMouse(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable", "square"), DefaultKeys, ""))
	for _, want := range []string{"Mouse support", "double-click", "scroll wheel", "right-click"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document mouse term %q, got %q", want, view)
		}
	}
}

func helpModel(t *testing.T, w, h int) Model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: w, Height: h})
	m = next.(Model)
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'?'}})
	return next.(Model)
}

func helpPress(m Model, k tea.KeyMsg) Model {
	next, _ := m.Update(k)
	return next.(Model)
}

func TestHelpTwoPaneNavigation(t *testing.T) {
	m := helpModel(t, 120, 40)
	sections := m.helpSectionsFiltered()
	if m.helpCat != 0 || m.helpRight {
		t.Fatalf("help should open on the first category, left pane: cat=%d right=%v", m.helpCat, m.helpRight)
	}
	first := ansi.Strip(m.helpVP.View())
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.helpCat != 1 || ansi.Strip(m.helpVP.View()) == first {
		t.Errorf("down should choose the next category and change the right pane (cat=%d)", m.helpCat)
	}
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyUp})
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyUp})
	if m.helpCat != 0 {
		t.Errorf("up should stop at the first category, got %d", m.helpCat)
	}
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyRight})
	if !m.helpRight {
		t.Fatal("right should focus the content pane")
	}
	before := m.helpCat
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyDown})
	if m.helpCat != before {
		t.Error("down in the content pane scrolls; it must not change the category")
	}
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyLeft})
	if m.helpRight {
		t.Error("left should return to the category list")
	}
	for i := 0; i < len(sections)+3; i++ {
		m = helpPress(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	if m.helpCat != len(sections)-1 {
		t.Errorf("down should stop at the last category, got %d of %d", m.helpCat, len(sections))
	}
}

func TestHelpTwoPaneRendersBothPanes(t *testing.T) {
	for _, size := range [][2]int{{120, 40}, {64, 24}} {
		m := helpModel(t, size[0], size[1])
		view := ansi.Strip(m.View())
		if !strings.Contains(view, "Status bar") || !strings.Contains(view, "Global") || !strings.Contains(view, "│") {
			t.Errorf("%v: expected a category list and divider:\n%s", size, view)
		}
		for _, line := range strings.Split(view, "\n") {
			if w := len([]rune(line)); w > size[0] {
				t.Errorf("%v: line wider than the terminal (%d): %q", size, w, line)
			}
		}
	}
}

func TestHelpDocumentsTemplatesAndTags(t *testing.T) {
	chrome := newManagerChrome(100, CatppuccinMocha, false)
	var text string
	for _, sec := range helpSections(DefaultKeys) {
		if sec.name == "Templates" {
			text = ansi.Strip(strings.Join(helpSectionLines(sec, 100, chrome, true), "\n"))
		}
	}
	flat := strings.Join(strings.Fields(text), " ")
	for _, want := range []string{"{first_name}", "{name}", "{my_name}", "{date}", "first To recipient", "templates folder"} {
		if !strings.Contains(flat, want) {
			t.Errorf("Templates help missing %q:\n%s", want, text)
		}
	}
}

func TestHelpSearchNoMatchShowsMessage(t *testing.T) {
	m := helpModel(t, 120, 40)
	m = helpPress(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'/'}})
	for _, r := range "zzzzqq" {
		m = helpPress(m, tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}})
	}
	if v := ansi.Strip(m.helpVP.View()); !strings.Contains(v, "No shortcuts match") {
		t.Errorf("pane = %q", v)
	}
}
