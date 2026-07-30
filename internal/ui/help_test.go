package ui

import (
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestRenderHelpDocumentsCredentialSafety(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	for _, want := range []string{"App Password", "keychain", "redacted"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document credential safety term %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsContactsAndNotifications(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	for _, want := range []string{"contacts", "autocomplete", "vCard", "desktop notifications", "compose to selected contact"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document contacts/notifications term %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsComposeAndMessageActionUpdates(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	for _, want := range []string{"cancel queued send", "sender picker", "Signature", "send delay", "unsubscribe"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document %q, got %q", want, view)
		}
	}
}

func TestRenderHelpDocumentsAccountManagerShortcutAsUppercaseM(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	if !strings.Contains(view, "M accounts") {
		t.Fatalf("expected help to document uppercase account shortcut, got %q", view)
	}
	if strings.Contains(view, "m accounts") {
		t.Fatalf("expected help not to document lowercase account shortcut, got %q", view)
	}
}

func TestRenderHelpDocumentsContactManagerShortcutAsUppercaseC(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	if !strings.Contains(view, "C contacts") {
		t.Fatalf("expected help to document uppercase contact shortcut, got %q", view)
	}
	if strings.Contains(view, "c contacts") {
		t.Fatalf("expected help not to document lowercase contact shortcut, got %q", view)
	}
}

func TestRenderHelpScopesModalShortcuts(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
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
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
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
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
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
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	if !strings.Contains(view, "g") || !strings.Contains(view, "toggle threaded conversations") {
		t.Fatalf("expected help to document threaded conversation toggle, got %q", view)
	}
}

func TestRenderHelpDocumentsPaneResizeShortcuts(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, ""))
	for _, want := range []string{"shift+←", "shift+→", "resize accounts pane", "shift+↑", "shift+↓", "resize messages/content split"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected help to document pane resize term %q, got %q", want, view)
		}
	}
}

func TestRenderLogViewerRowsFillModalWidth(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.styles = BuildStyles(CatppuccinMocha, "comfortable")
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
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, "vim"))

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
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, "filters modal"))

	for _, want := range []string{"Filters Modal", "new rule", "enable or disable selected rule"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected section-name match to keep whole section (missing %q), got %q", want, view)
		}
	}
}

func TestRenderHelpFilterNoMatches(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys, "xyzzy"))

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
