package ui

import (
	"errors"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
	"github.com/muesli/termenv"

	"github.com/allisonhere/tide/internal/config"
)

func TestSettingsFieldNavigationClampsAtEnds(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssDisplay)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfNotifications)
	if got := s.nextField(); got != sfNotifications {
		t.Fatalf("nextField at last visible field: got %v want sfNotifications", got)
	}
	s.setFocusedField(sfBackToSections)
	if got := s.prevField(); got != sfBackToSections {
		t.Fatalf("prevField at first visible field: got %v want sfBackToSections", got)
	}
}

func TestSettingsTextInputsAcceptMovementRunes(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	s.focusedField = sfBrowser
	s.browserInput.SetValue("")
	s.applyFocus()

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, DefaultKeys)

	if next.focusedField != sfBrowser {
		t.Fatalf("expected browser field to keep focus, got %v", next.focusedField)
	}
	if got := next.browserInput.Value(); got != "k" {
		t.Fatalf("expected browser input to receive typed rune, got %q", got)
	}
}

func TestSettingsTextInputsAcceptHAtCursorStart(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	s.focusedField = sfBrowser
	s.browserInput.SetValue("")
	s.browserInput.CursorStart()
	s.applyFocus()

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'h'}}, DefaultKeys)

	if next.focusedPane != settingsPaneDetail {
		t.Fatalf("expected browser field to keep detail focus when typing h, got %v", next.focusedPane)
	}
	if got := next.browserInput.Value(); got != "h" {
		t.Fatalf("expected browser input to receive typed h, got %q", got)
	}
}

func TestSettingsTextInputsAcceptOpenBracket(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	s.focusedField = sfBrowser
	s.browserInput.SetValue("")
	s.applyFocus()

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'['}}, DefaultKeys)

	if next.focusedField != sfBrowser {
		t.Fatalf("expected browser field to keep focus, got %v", next.focusedField)
	}
	if got := next.browserInput.Value(); got != "[" {
		t.Fatalf("expected browser input to receive typed rune, got %q", got)
	}
}

func TestSettingsBadgeWidthStaysStableAcrossFocus(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	chrome := newManagerChrome(62, CatppuccinMocha, false)

	focused := s.renderBadge("ON", true, chrome)
	unfocused := s.renderBadge("ON", false, chrome)

	if lipgloss.Width(focused) != lipgloss.Width(unfocused) {
		t.Fatalf("expected focused and unfocused badges to have equal width, got %d and %d", lipgloss.Width(focused), lipgloss.Width(unfocused))
	}
}

func TestSettingsUpdateActionBadgeUsesFocusedStyle(t *testing.T) {
	lipgloss.SetColorProfile(termenv.TrueColor)
	t.Cleanup(func() { lipgloss.SetColorProfile(termenv.Ascii) })

	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	chrome := newManagerChrome(80, CatppuccinMocha, false)

	focused := s.renderActionRow("Check now", "", true, 60, chrome)
	unfocused := s.renderActionRow("Check now", "", false, 60, chrome)
	focusedBadge := s.renderBadge("ENTER", true, chrome)
	unfocusedBadge := s.renderBadge("ENTER", false, chrome)

	if !strings.Contains(focused, focusedBadge) {
		t.Fatalf("expected focused action row to include focused ENTER badge\nrow: %q\nbadge: %q", focused, focusedBadge)
	}
	if strings.Contains(focused, unfocusedBadge) {
		t.Fatalf("expected focused action row not to include unfocused ENTER badge\nrow: %q\nbadge: %q", focused, unfocusedBadge)
	}
	if !strings.Contains(unfocused, unfocusedBadge) {
		t.Fatalf("expected unfocused action row to include unfocused ENTER badge\nrow: %q\nbadge: %q", unfocused, unfocusedBadge)
	}
}

func TestSettingsAITestBadgeColorMatchesResultState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	chrome := newManagerChrome(62, CatppuccinMocha, false)

	tests := []struct {
		name      string
		configure func(*Settings)
		wantBg    lipgloss.Color
	}{
		{
			name:      "pending",
			configure: func(s *Settings) { s.aiValidatePending = true },
			wantBg:    chrome.pendingFg,
		},
		{
			name:      "success",
			configure: func(s *Settings) { s.aiTestOk = true },
			wantBg:    chrome.successFg,
		},
		{
			name:      "error",
			configure: func(s *Settings) { s.aiTestError = "bad key" },
			wantBg:    chrome.errorFg,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSettings(cfg, settingsUpdateState{})
			tt.configure(&s)

			bg, _, bold := s.aiTestBadgeColors(false, chrome)
			if bg != tt.wantBg {
				t.Fatalf("expected AI test badge background %s, got %s", tt.wantBg, bg)
			}
			if !bold {
				t.Fatal("expected result badge to render bold")
			}
			if got := lipgloss.Width(s.renderAITestBadge(false, chrome)); got != 7 {
				t.Fatalf("expected stable TEST badge width 7, got %d", got)
			}
		})
	}
}

func TestSettingsApplyToLayoutDensity(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.layoutDensityIdx = 1
	cfg := s.ApplyTo(config.DefaultConfig())
	if cfg.Display.Density != "compact" {
		t.Fatalf("ApplyTo: expected compact, got %q", cfg.Display.Density)
	}
	s.layoutDensityIdx = 0
	cfg = s.ApplyTo(config.DefaultConfig())
	if cfg.Display.Density != "comfortable" {
		t.Fatalf("ApplyTo: expected comfortable, got %q", cfg.Display.Density)
	}
}

func TestSettingsLoadsAndAppliesMarkReadOnFocus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = true

	s := newSettings(cfg, settingsUpdateState{})
	if !s.markReadOnFocus {
		t.Fatal("expected settings to load mark-read-on-focus from config")
	}

	s.markReadOnFocus = false
	next := s.ApplyTo(cfg)
	if next.Display.MarkReadOnFocus {
		t.Fatal("expected ApplyTo to save disabled mark-read-on-focus")
	}
}

func TestSettingsLoadsAndAppliesNotifications(t *testing.T) {
	if !config.DefaultConfig().Display.Notifications {
		t.Fatal("expected notifications to default on")
	}

	cfg := config.DefaultConfig()
	cfg.Display.Notifications = false

	s := newSettings(cfg, settingsUpdateState{})
	if s.notifications {
		t.Fatal("expected settings to load disabled notifications from config")
	}

	s.notifications = true
	next := s.ApplyTo(cfg)
	if !next.Display.Notifications {
		t.Fatal("expected ApplyTo to save enabled notifications")
	}
}

func TestSettingsLoadsAndAppliesFocusLine(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.FocusLine = false

	s := newSettings(cfg, settingsUpdateState{})
	if s.focusLine {
		t.Fatal("expected settings to load disabled focus line from config")
	}

	s.focusLine = true
	next := s.ApplyTo(cfg)
	if !next.Display.FocusLine {
		t.Fatal("expected ApplyTo to save enabled focus line")
	}
}

func TestSettingsViewIncludesFocusLineToggle(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	v := s.View(62, 24, newManagerChrome(62, CatppuccinMocha, false))
	if !strings.Contains(v, "Focus line") {
		t.Fatal("expected settings view to contain focus line toggle")
	}
}

func TestSettingsViewIncludesLayoutDensity(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	v := s.View(62, 30, newManagerChrome(62, CatppuccinMocha, false))
	if !strings.Contains(v, "Layout density") {
		t.Fatal("expected settings view to contain layout density label")
	}
}

func TestSettingsStartsFocusedOnSidebar(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})

	if s.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected settings to start focused on sidebar, got %v", s.focusedPane)
	}
	if s.activeSection != ssDisplay {
		t.Fatalf("expected settings to start on DISPLAY section, got %v", s.activeSection)
	}
}

func TestSettingsLeftKeyMovesFocusToSidebar(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected left key to move focus to sidebar, got %v", next.focusedPane)
	}
	if next.activeSection != ssDisplay {
		t.Fatalf("expected active section to remain DISPLAY, got %v", next.activeSection)
	}
}

func TestSettingsSidebarDownChangesSection(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneSidebar)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)

	if next.activeSection != ssFeeds {
		t.Fatalf("expected active section to move to FEEDS, got %v", next.activeSection)
	}
	if next.focusedField != sfBackToSections {
		t.Fatalf("expected sidebar navigation to keep back-link focus within detail pane, got %v", next.focusedField)
	}
}

func TestSettingsSidebarRightEntersDetail(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneSidebar)
	s.setActiveSection(ssFeeds)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)

	if next.focusedPane != settingsPaneDetail {
		t.Fatalf("expected right key to enter detail pane, got %v", next.focusedPane)
	}
	if next.focusedField != sfBackToSections {
		t.Fatalf("expected entry into detail pane to land on back-link (not a text input), got %v", next.focusedField)
	}
}

func TestSettingsAISidebarRightEntersDetailOnAPIKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	s := newSettings(cfg, settingsUpdateState{})
	s.setFocusedPane(settingsPaneSidebar)
	s.setActiveSection(ssAI)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)

	if next.focusedPane != settingsPaneDetail {
		t.Fatalf("expected right key to enter detail pane, got %v", next.focusedPane)
	}
	if next.focusedField != sfAPIKey {
		t.Fatalf("expected AI detail pane to land on API key, got %v", next.focusedField)
	}
}

func TestSettingsEscMovesToSidebarThenSavesAndExits(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	s.setActiveSection(ssFeeds)
	s.feedMaxBodyInput.SetValue("20")

	next, _, done := s.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if done {
		t.Fatal("expected first esc from detail not to exit")
	}
	if next.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected first esc to focus sidebar, got %v", next.focusedPane)
	}
	if next.shouldSave {
		t.Fatal("expected first esc from detail not to save yet")
	}
	if got := next.feedMaxBodyInput.Value(); got != "20" {
		t.Fatalf("expected first esc to keep pending edit, got %q", got)
	}

	next, _, done = next.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if !done {
		t.Fatal("expected second esc from sidebar to exit")
	}
	if !next.shouldSave {
		t.Fatal("expected second esc from sidebar to save edits")
	}
}

func TestSettingsRestoresLastFocusedFieldPerSection(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssDisplay)
	s.setFocusedField(sfBrowser)
	s.setActiveSection(ssAI)
	s.setFocusedField(sfSavePath)

	s.setActiveSection(ssDisplay)
	if s.focusedField != sfBrowser {
		t.Fatalf("expected DISPLAY section to restore browser focus, got %v", s.focusedField)
	}

	s.setActiveSection(ssAI)
	if s.focusedField != sfSavePath {
		t.Fatalf("expected AI section to restore save path focus, got %v", s.focusedField)
	}
}

func TestSettingsTextInputLeftArrowMovesToSidebarAtCursorStart(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssDisplay)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfBrowser)
	s.browserInput.SetValue("abc")
	s.browserInput.CursorStart()

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected text input to move focus to sidebar at cursor start, got %v", next.focusedPane)
	}
	if next.activeSection != ssDisplay {
		t.Fatalf("expected active section to remain DISPLAY, got %v", next.activeSection)
	}
}

func TestSettingsTextInputLeftArrowStaysInDetailWhenCursorCanMoveLeft(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssDisplay)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfBrowser)
	s.browserInput.SetValue("abc")

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedPane != settingsPaneDetail {
		t.Fatalf("expected text input to keep detail focus, got %v", next.focusedPane)
	}
	if got := next.browserInput.Position(); got != 2 {
		t.Fatalf("expected browser cursor to move left to position 2, got %d", got)
	}
}

func TestSettingsAPIKeyLeftArrowMovesToSidebarAtCursorStart(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-test"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfAPIKey)
	s.openaiInput.CursorStart()

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected API key field to move focus to sidebar at cursor start, got %v", next.focusedPane)
	}
	if next.activeSection != ssAI {
		t.Fatalf("expected active section to remain AI, got %v", next.activeSection)
	}
}

func TestSettingsSavedAPIKeyTypingEditsExistingValue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-old"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfAPIKey)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'n'}}, DefaultKeys)

	if got := next.openaiInput.Value(); got != "sk-oldn" {
		t.Fatalf("expected typed rune to edit saved key normally, got %q", got)
	}
}

func TestSettingsSavedAPIKeyPasteEditsExistingValue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-old"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfAPIKey)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("sk-new"), Paste: true}, DefaultKeys)

	if got := next.openaiInput.Value(); got != "sk-oldsk-new" {
		t.Fatalf("expected pasted key to edit saved key normally, got %q", got)
	}
}

func TestSettingsSavedAPIKeyNavigationPreservesExistingValue(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-old"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfAPIKey)
	s.openaiInput.CursorStart()

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)

	if got := next.openaiInput.Value(); got != "sk-old" {
		t.Fatalf("expected navigation to preserve saved key, got %q", got)
	}
	if got := next.openaiInput.Position(); got != 1 {
		t.Fatalf("expected right arrow to move within saved key, got cursor %d", got)
	}
}

func TestSettingsProviderLeftArrowCyclesBackwardAndKeepsDetailFocus(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfProvider)
	s.providerIdx = providerIndex("openai")

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedPane != settingsPaneDetail {
		t.Fatalf("expected provider picker to keep detail focus, got %v", next.focusedPane)
	}
	if got := aiProviderIDs[next.providerIdx]; got != "" {
		t.Fatalf("expected left arrow to move provider backward to none, got %q", got)
	}
}

func TestSettingsProviderRightArrowCyclesForward(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfProvider)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)

	if got := aiProviderIDs[next.providerIdx]; got != "openai" {
		t.Fatalf("expected right arrow to move provider forward to openai, got %q", got)
	}
}

func TestSettingsAPIKeyHintFlagsProviderMismatch(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "claude"
	cfg.AI.ClaudeKey = "sk-proj-test123"

	s := newSettings(cfg, settingsUpdateState{})

	if got := s.fieldHint(sfAPIKey); !strings.Contains(got, "Looks like OpenAI, but Claude is selected") {
		t.Fatalf("expected mismatch hint, got %q", got)
	}
}

func TestSettingsAPIKeyHintConfirmsMatchingFormat(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "gemini"
	cfg.AI.GeminiKey = "AIzaTestKey"

	s := newSettings(cfg, settingsUpdateState{})

	if got := s.fieldHint(sfAPIKey); !strings.Contains(got, "Format looks like Gemini") {
		t.Fatalf("expected matching format hint, got %q", got)
	}
}

func TestSettingsCtrlSSavesWithSuspiciousSelectedProviderKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "claude"
	cfg.AI.ClaudeKey = "sk-proj-test123"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfSavePath)

	next, _, done := s.Update(tea.KeyMsg{Type: tea.KeyCtrlS}, DefaultKeys)

	if !done {
		t.Fatal("expected ctrl+s to finish settings")
	}
	if !next.shouldSave {
		t.Fatal("expected suspicious key format not to block save")
	}
}

func TestSettingsEscFromSidebarSavesWithSuspiciousSelectedProviderKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "claude"
	cfg.AI.ClaudeKey = "sk-proj-test123"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneSidebar)
	s.setFocusedField(sfSavePath)

	next, _, done := s.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)

	if !done {
		t.Fatal("expected esc from sidebar to finish settings")
	}
	if !next.shouldSave {
		t.Fatal("expected suspicious key format not to block esc save")
	}
}

func TestSettingsQDiscardsWithSuspiciousSelectedProviderKey(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "bad"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneSidebar)

	next, _, done := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}}, DefaultKeys)
	if !done {
		t.Fatal("expected q to exit settings")
	}
	if next.shouldSave {
		t.Fatal("expected q to discard without saving")
	}
}

func TestSettingsTestConnectionEmptyActiveProviderShowsInlineFeedback(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfTestAIConnection)

	next, cmd, done := s.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	if done {
		t.Fatal("expected test connection to keep settings open")
	}
	if cmd != nil {
		t.Fatal("expected empty key to avoid live validation command")
	}
	if next.aiValidatePending {
		t.Fatal("expected empty key not to enter pending validation state")
	}
	if !strings.Contains(next.aiTestError, "OpenAI key is empty") {
		t.Fatalf("expected inline empty-key feedback, got %q", next.aiTestError)
	}
}

func TestSettingsTestConnectionDispatchesValidationForDraftConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-test"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfTestAIConnection)

	next, cmd, done := s.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	if done {
		t.Fatal("expected test connection to keep settings open")
	}
	if cmd == nil {
		t.Fatal("expected valid-looking key to dispatch validation command")
	}
	if !next.aiValidatePending {
		t.Fatal("expected validation to enter pending state")
	}
}

func TestSettingsAIValidateDoneUpdatesFeedbackWithoutClosing(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.aiValidatePending = true

	next, cmd, done := s.Update(AIValidateDoneMsg{Err: nil}, DefaultKeys)
	if done {
		t.Fatal("expected validation success to keep settings open")
	}
	if cmd != nil {
		t.Fatal("expected validation result not to dispatch another command")
	}
	if next.aiValidatePending || !next.aiTestOk || next.aiTestError != "" {
		t.Fatalf("expected success feedback, pending=%v ok=%v err=%q", next.aiValidatePending, next.aiTestOk, next.aiTestError)
	}

	next.aiValidatePending = true
	next, cmd, done = next.Update(AIValidateDoneMsg{Err: errors.New("openai: HTTP 401: bad key")}, DefaultKeys)
	if done {
		t.Fatal("expected validation error to keep settings open")
	}
	if cmd != nil {
		t.Fatal("expected validation error not to dispatch another command")
	}
	if next.aiValidatePending || next.aiTestOk || !strings.Contains(next.aiTestError, "bad key") {
		t.Fatalf("expected error feedback, pending=%v ok=%v err=%q", next.aiValidatePending, next.aiTestOk, next.aiTestError)
	}
}

func TestSettingsDetailHintsUseEscCategories(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfIcons)

	view := s.View(80, 24, newManagerChrome(80, CatppuccinMocha, false))
	text := strings.ToLower(ansi.Strip(view))

	if strings.Contains(text, "ctrl+s") {
		t.Fatalf("expected settings detail hints not to reference ctrl+s, got %q", view)
	}
	if !strings.Contains(text, "categories") {
		t.Fatalf("expected settings detail hints to mention categories, got %q", view)
	}
}

func TestSettingsAboutViewShowsLinksAndTagline(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)

	view := s.View(100, 30, newManagerChrome(100, CatppuccinMocha, false))

	for _, want := range []string{
		"ABOUT",
		"Repository",
		"Thanks for taking a look -allie",
		"your mail, your rules",
		"terminal information delivery engine",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected about view to contain %q, got %q", want, view)
		}
	}
}

func TestSettingsAboutActions(t *testing.T) {
	tests := []struct {
		name       string
		field      settingsField
		wantAction settingsAction
	}{
		{name: "repo", field: sfAboutRepo, wantAction: settingsActionOpenRepo},
		{name: "issues", field: sfAboutIssues, wantAction: settingsActionOpenIssues},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSettings(config.DefaultConfig(), settingsUpdateState{})
			s.setActiveSection(ssAbout)
			s.setFocusedPane(settingsPaneDetail)
			s.setFocusedField(tt.field)

			next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

			if got := next.takeAction(); got != tt.wantAction {
				t.Fatalf("expected action %v, got %v", tt.wantAction, got)
			}
		})
	}
}

func TestSettingsAboutAnimationTicksOnlyWhenVisible(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)

	next, cmd, _ := s.Update(settingsAboutPulseMsg{}, DefaultKeys)
	if next.aboutGradientFrame != 1 {
		t.Fatalf("expected about gradient frame to advance to 1, got %d", next.aboutGradientFrame)
	}
	if cmd == nil {
		t.Fatal("expected about animation tick to schedule another tick")
	}

	next.setActiveSection(ssDisplay)
	after, cmd, _ := next.Update(settingsAboutPulseMsg{}, DefaultKeys)
	if after.aboutGradientFrame != 0 {
		t.Fatalf("expected about gradient frame to reset outside ABOUT, got %d", after.aboutGradientFrame)
	}
	if cmd != nil {
		t.Fatal("expected no follow-up tick when ABOUT is not visible")
	}
}

func TestSettingsAboutAnimationDoesNotWrapEarly(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)

	for i := 0; i < 30; i++ {
		next, _, _ := s.Update(settingsAboutPulseMsg{}, DefaultKeys)
		s = next
	}

	if s.aboutGradientFrame != 30 {
		t.Fatalf("expected about gradient frame to continue smoothly past 24 ticks, got %d", s.aboutGradientFrame)
	}
}

func TestAboutHeroBackgroundStaysStaticAcrossFrames(t *testing.T) {
	for _, col := range []int{4, 11, 19} {
		if got, want := aboutHeroBackground(0, 2, col, 24), aboutHeroBackground(8, 2, col, 24); got != want {
			t.Fatalf("expected static hero background at col %d, got %q want %q", col, got, want)
		}
	}
}

func TestAboutHeroSpacerRowsAreBlank(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	for _, row := range []int{1, 3} {
		line := ansi.Strip(s.renderAboutHeroTextLine("", 28, row, false))
		if strings.TrimSpace(line) != "" {
			t.Fatalf("expected blank spacer row %d, got %q", row, line)
		}
	}
}

func TestSettingsAboutTaglineStaysOnOneLine(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	s.aboutGradientFrame = 18
	view := s.renderAboutHero(56, newManagerChrome(84, CatppuccinMocha, false))
	if strings.Count(ansi.Strip(view), "your mail, your rules") != 1 {
		t.Fatalf("expected tagline to render once on a single line, got %q", ansi.Strip(view))
	}
}

func TestSettingsAboutHeroKeepsPreviousHeight(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	view := s.renderAboutHero(56, newManagerChrome(84, CatppuccinMocha, false))
	if got := lipgloss.Height(view); got != 6 {
		t.Fatalf("expected about hero height 6, got %d", got)
	}
}

func TestSettingsAboutGradientDoesNotShiftHeroLayout(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	chrome := newManagerChrome(84, CatppuccinMocha, false)

	before := s.renderAboutHero(56, chrome)
	s.aboutGradientFrame = 7
	after := s.renderAboutHero(56, chrome)

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("line count changed across about animation frames: before=%d after=%d", len(beforeLines), len(afterLines))
	}
	for i := range beforeLines {
		if lipgloss.Width(beforeLines[i]) != lipgloss.Width(afterLines[i]) {
			t.Fatalf("line %d width changed across about animation frames: before=%d after=%d", i+1, lipgloss.Width(beforeLines[i]), lipgloss.Width(afterLines[i]))
		}
	}
}

func TestSettingsAboutLinksCompactLayout(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	chrome := newManagerChrome(96, CatppuccinMocha, false)

	view := s.renderAboutLinks(60, chrome)
	if !strings.Contains(view, "Repository") || !strings.Contains(view, "Issues") {
		t.Fatalf("expected compact about links to contain Repository and Issues, got %q", view)
	}
}

func TestSettingsAboutLinksSitOnBottomRow(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAbout)
	chrome := newManagerChrome(96, CatppuccinMocha, false)
	bodyHeight := 22
	s.detailHeight = bodyHeight

	body := s.viewSectionBody(60, chrome)
	view := ansi.Strip(s.scrollSectionBody(body, 60, bodyHeight, lipgloss.Color("#000000")))
	lines := strings.Split(view, "\n")
	if len(lines) != bodyHeight {
		t.Fatalf("expected %d rendered lines, got %d: %q", bodyHeight, len(lines), view)
	}
	buttonRows := strings.Join(lines[len(lines)-3:], "\n")
	if !strings.Contains(buttonRows, "Repository") || !strings.Contains(buttonRows, "Issues") {
		t.Fatalf("expected about button block at bottom, got %q in view %q", buttonRows, view)
	}
	if strings.TrimSpace(lines[len(lines)-1]) == "" {
		t.Fatalf("expected bottom row to be button border, got blank in view %q", view)
	}
}

func TestSettingsActionURLMapping(t *testing.T) {
	if got := settingsActionURL(settingsActionOpenRepo); got != tideRepoURL {
		t.Fatalf("expected repo action URL %q, got %q", tideRepoURL, got)
	}
	if got := settingsActionURL(settingsActionOpenIssues); got != tideIssuesURL {
		t.Fatalf("expected issues action URL %q, got %q", tideIssuesURL, got)
	}
}

func TestSettingsScrollOffsetCentersFocusedLineWhenNeeded(t *testing.T) {
	if got := settingsScrollOffset(5, 1, 8); got != 0 {
		t.Fatalf("expected no scroll when content fits, got %d", got)
	}
	if got := settingsScrollOffset(20, 12, 6); got != 9 {
		t.Fatalf("expected centered scroll offset 9, got %d", got)
	}
	if got := settingsScrollOffset(20, 19, 6); got != 14 {
		t.Fatalf("expected scroll to clamp at end, got %d", got)
	}
}

func TestSettingsRightPaneScrollsToFocusedFieldOnShortView(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.providerIdx = 4
	s.ensureSectionFieldVisible(ssAI)
	s.setFocusedField(sfSavePath)

	chrome := newManagerChrome(62, CatppuccinMocha, false)
	view := ansi.Strip(s.View(62, 12, chrome))

	if !strings.Contains(view, "~/") {
		t.Fatalf("expected short settings view to scroll save path into view, got %q", view)
	}
	if strings.Contains(view, "Provider") {
		t.Fatalf("expected provider row to scroll out of the short detail pane, got %q", view)
	}
}

func TestSettingsSectionsUseSharedRightPaneGroupHeaders(t *testing.T) {
	tests := []struct {
		section settingsSection
		want    string
	}{
		{section: ssDisplay, want: "━ DISPLAY"},
		{section: ssFeeds, want: "━ STORAGE"},
		{section: ssUpdates, want: "━ UPDATES"},
		{section: ssAI, want: "━ PROVIDER CREDENTIALS"},
	}

	for _, tt := range tests {
		t.Run(tt.want, func(t *testing.T) {
			s := newSettings(config.DefaultConfig(), settingsUpdateState{})
			s.setActiveSection(tt.section)

			body := s.viewSectionBody(60, newManagerChrome(72, CatppuccinMocha, false))
			text := ansi.Strip(strings.Join(body.lines, "\n"))
			if !strings.Contains(text, tt.want) {
				t.Fatalf("expected shared group header %q, got %q", tt.want, text)
			}
		})
	}
}

func TestSettingsAPIKeyInputAlwaysVisibleAndMasked(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-abcdefghijklmnopqrstuvwxyz123456"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedField(sfSavePath)

	view := ansi.Strip(s.View(84, 22, newManagerChrome(84, CatppuccinMocha, false)))

	if strings.Contains(view, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("expected API key input to hide the raw key, got %q", view)
	}
	if !strings.Contains(view, "> ●") {
		t.Fatalf("expected always-visible masked API key input, got %q", view)
	}
}

func TestSettingsAPIKeyFocusKeepsStableMaskedInput(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedField(sfAPIKey)

	view := ansi.Strip(s.View(84, 22, newManagerChrome(84, CatppuccinMocha, false)))

	if strings.Contains(view, "OpenAI key") || strings.Contains(strings.ToLower(view), "empty") {
		t.Fatalf("expected focused API key row to omit header/status text, got %q", view)
	}
	if !strings.Contains(view, "sk-...") {
		t.Fatalf("expected stable key input to show placeholder, got %q", view)
	}
}

func TestSettingsAPIKeyInputHeightStaysStableAcrossFocus(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.OpenAIKey = "sk-abcdefghijklmnopqrstuvwxyz123456"

	unfocused := newSettings(cfg, settingsUpdateState{})
	unfocused.setActiveSection(ssAI)
	unfocused.setFocusedField(sfSavePath)

	focused := newSettings(cfg, settingsUpdateState{})
	focused.setActiveSection(ssAI)
	focused.setFocusedField(sfAPIKey)

	unfocusedBody := unfocused.viewSectionBody(48, newManagerChrome(64, CatppuccinMocha, false))
	focusedBody := focused.viewSectionBody(48, newManagerChrome(64, CatppuccinMocha, false))

	if len(unfocusedBody.lines) != len(focusedBody.lines) {
		t.Fatalf("expected AI section line count to stay stable, unfocused=%d focused=%d", len(unfocusedBody.lines), len(focusedBody.lines))
	}
	if strings.Contains(ansi.Strip(strings.Join(focusedBody.lines, "\n")), "OpenAI key") {
		t.Fatalf("expected focused body to omit API key header")
	}
}

func TestSettingsProviderSelectorStaysSingleLineInNarrowPane(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)

	row := s.renderProviderSelector(42, newManagerChrome(62, CatppuccinMocha, false))
	if got := lipgloss.Height(row); got != 1 {
		t.Fatalf("expected provider selector to stay on one line, got height %d", got)
	}
	stripped := ansi.Strip(row)
	if !strings.Contains(stripped, "◀") || !strings.Contains(stripped, "▶") {
		t.Fatalf("expected provider selector to render side arrows, got %q", stripped)
	}
	if got := lipgloss.Width(row); got != 42 {
		t.Fatalf("expected provider selector to fill the stable form row width 42, got width %d", got)
	}
}

func TestSettingsFocusedAPIKeyInputStaysSingleLine(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedField(sfAPIKey)

	body := s.viewSectionBody(48, newManagerChrome(64, CatppuccinMocha, false))
	for _, line := range body.lines {
		if strings.Contains(ansi.Strip(line), "sk-...") {
			if got := lipgloss.Height(line); got != 1 {
				t.Fatalf("expected API key input to stay on one line, got height %d in %q", got, ansi.Strip(line))
			}
			return
		}
	}
	t.Fatal("expected focused API key input to be present")
}

func TestSettingsAITestConnectionRowStaysCompactInNarrowPane(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)

	body := s.viewSectionBody(48, newManagerChrome(64, CatppuccinMocha, false))
	stripped := ansi.Strip(strings.Join(body.lines, "\n"))

	if strings.Contains(stripped, "Checks the current draft") {
		t.Fatalf("expected compact test connection status, got %q", stripped)
	}
	if strings.Contains(stripped, "Test connection") || !strings.Contains(strings.ToLower(stripped), "ready") || !strings.Contains(strings.ToLower(stripped), "test") {
		t.Fatalf("expected compact test connection row, got %q", stripped)
	}
	for _, line := range body.lines {
		if strings.Contains(strings.ToLower(ansi.Strip(line)), "ready") && strings.Contains(strings.ToLower(ansi.Strip(line)), "test") {
			if got := lipgloss.Height(line); got != 1 {
				t.Fatalf("expected test connection row to stay on one line, got height %d in %q", got, ansi.Strip(line))
			}
			return
		}
	}
	t.Fatal("expected test connection row to be present")
}

func TestSettingsHintsWrapWithoutEllipsis(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssDisplay)
	s.setFocusedField(sfDisplayDensity)

	width := 44
	body := s.viewSectionBody(width, newManagerChrome(64, CatppuccinMocha, false))
	var hintLines []string
	collecting := false
	for _, line := range body.lines {
		stripped := ansi.Strip(line)
		if strings.Contains(stripped, "comfortable") {
			collecting = true
		}
		if collecting {
			if strings.TrimSpace(stripped) == "" {
				break
			}
			hintLines = append(hintLines, stripped)
		}
	}
	text := strings.Join(hintLines, "\n")

	if len(hintLines) == 0 || !strings.HasPrefix(hintLines[0], "  · ") {
		t.Fatalf("expected hint to start at label column, got %q", text)
	}
	if strings.Contains(text, "...") || strings.Contains(text, "…") {
		t.Fatalf("expected wrapped hints without ellipsis, got %q", text)
	}
	for _, want := range []string{
		"comfortable",
		"adds vertical",
		"compact",
		"small",
		"terminals",
	} {
		if !strings.Contains(text, want) {
			t.Fatalf("expected wrapped hint to contain %q, got %q", want, text)
		}
	}
	for _, line := range body.lines {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("expected settings line width <= %d, got %d in %q", width, got, ansi.Strip(line))
		}
	}
}

func TestSettingsAITestConnectionStatusStatesRender(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	tests := []struct {
		name      string
		configure func(*Settings)
		want      string
	}{
		{
			name:      "idle",
			configure: func(s *Settings) {},
			want:      "○ ready",
		},
		{
			name:      "pending",
			configure: func(s *Settings) { s.aiValidatePending = true },
			want:      "◔ checking",
		},
		{
			name:      "success",
			configure: func(s *Settings) { s.aiTestOk = true },
			want:      "● ok",
		},
		{
			name:      "error",
			configure: func(s *Settings) { s.aiTestError = "bad key" },
			want:      "● failed",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			s := newSettings(cfg, settingsUpdateState{})
			s.setActiveSection(ssAI)
			tt.configure(&s)

			body := s.viewSectionBody(56, newManagerChrome(72, CatppuccinMocha, false))
			text := ansi.Strip(strings.Join(body.lines, "\n"))
			if !strings.Contains(text, tt.want) {
				t.Fatalf("expected status %q, got %q", tt.want, text)
			}
		})
	}
}

func TestSettingsAITestConnectionStatusUsesASCIIFallbacks(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.aiValidatePending = true

	body := s.viewSectionBody(56, newManagerChrome(72, CatppuccinMocha, true))
	text := ansi.Strip(strings.Join(body.lines, "\n"))
	if !strings.Contains(text, "... checking") {
		t.Fatalf("expected ASCII pending indicator, got %q", text)
	}

	s.aiValidatePending = false
	s.aiTestOk = true
	body = s.viewSectionBody(56, newManagerChrome(72, CatppuccinMocha, true))
	text = ansi.Strip(strings.Join(body.lines, "\n"))
	if !strings.Contains(text, "OK ok") {
		t.Fatalf("expected ASCII success indicator, got %q", text)
	}
}

func TestSettingsSavePathRowStaysSingleLineInNarrowPane(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"
	cfg.AI.SavePath = "~/summaries/export"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedField(sfSavePath)

	body := s.viewSectionBody(48, newManagerChrome(64, CatppuccinMocha, false))
	for _, line := range body.lines {
		if strings.Contains(ansi.Strip(line), "~/summaries/export") {
			if got := lipgloss.Height(line); got != 1 {
				t.Fatalf("expected save path row to stay on one line, got height %d in %q", got, ansi.Strip(line))
			}
			return
		}
	}
	t.Fatal("expected save path row to be present")
}

func TestSettingsAIMarkReadRowStaysSingleLineInNarrowPane(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.Provider = "openai"

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssAI)
	s.setFocusedField(sfMarkReadOnSummarize)

	body := s.viewSectionBody(48, newManagerChrome(64, CatppuccinMocha, false))
	stripped := ansi.Strip(strings.Join(body.lines, "\n"))
	if strings.Contains(stripped, "Mark read on\n") || strings.Contains(stripped, "\nsummarize") {
		t.Fatalf("expected mark-read label not to split across lines, got %q", stripped)
	}
	for _, line := range body.lines {
		if strings.Contains(ansi.Strip(line), "Mark read on summarize") {
			if got := lipgloss.Height(line); got != 1 {
				t.Fatalf("expected mark-read row to stay on one line, got height %d in %q", got, ansi.Strip(line))
			}
			return
		}
	}
	t.Fatal("expected mark-read row to be present")
}

func TestSettingsFeedsSectionShowsFeedMaxSizeFieldOnly(t *testing.T) {
	cfg := config.DefaultConfig()

	s := newSettings(cfg, settingsUpdateState{})
	s.setActiveSection(ssFeeds)

	view := ansi.Strip(s.View(96, 24, newManagerChrome(96, CatppuccinMocha, false)))

	if !strings.Contains(view, "Max body size (MiB)") {
		t.Fatalf("expected feeds settings view to contain feed max size, got %q", view)
	}
	for _, unwanted := range []string{"GReader API URL", "Login", "Password", "Source"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("expected feeds settings view to hide %q, got %q", unwanted, view)
		}
	}
}

func TestSettingsApplyToPreservesAccountConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{
		Name:     "Personal",
		IMAPHost: "imap.example.com",
		IMAPPort: 993,
		IMAPTLS:  true,
		SMTPHost: "smtp.example.com",
		SMTPPort: 587,
		SMTPTLS:  true,
		User:     "alice",
		Password: "secret",
		From:     "alice@example.com",
	}}

	s := newSettings(cfg, settingsUpdateState{})
	got := s.ApplyTo(cfg)

	if len(got.Accounts) != len(cfg.Accounts) || got.Accounts[0] != cfg.Accounts[0] {
		t.Fatalf("expected settings save to preserve account config, got %#v want %#v", got.Accounts, cfg.Accounts)
	}
}

func TestSettingsApplyToPreservesSavedAIKeysWhenDraftsAreEmpty(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.AI.OpenAIKey = "sk-old-openai"
	cfg.AI.ClaudeKey = "sk-ant-old"
	cfg.AI.GeminiKey = "AIzaOld"

	s := newSettings(cfg, settingsUpdateState{})
	s.openaiInput.SetValue("")
	s.claudeInput.SetValue("")
	s.geminiInput.SetValue("")

	got := s.ApplyTo(cfg)

	if got.AI.OpenAIKey != cfg.AI.OpenAIKey {
		t.Fatalf("expected empty OpenAI draft to preserve saved key, got %q", got.AI.OpenAIKey)
	}
	if got.AI.ClaudeKey != cfg.AI.ClaudeKey {
		t.Fatalf("expected empty Claude draft to preserve saved key, got %q", got.AI.ClaudeKey)
	}
	if got.AI.GeminiKey != cfg.AI.GeminiKey {
		t.Fatalf("expected empty Gemini draft to preserve saved key, got %q", got.AI.GeminiKey)
	}
}

func TestSettingsManualInstallCommandRendersBlock(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{
		currentVersion: "v1.0.0",
		latestVersion:  "v1.1.0",
		state:          updateStateNeedsElevation,
		manualCommand:  "sudo install -m 0755 /tmp/tide /usr/local/bin/tide",
	})
	s.setActiveSection(ssUpdates)
	s.setFocusedPane(settingsPaneDetail)
	s.setFocusedField(sfUpdateManualCommand)

	view := ansi.Strip(s.View(80, 40, newManagerChrome(80, CatppuccinMocha, false)))
	if !strings.Contains(view, "Copy Command") {
		t.Fatalf("expected Copy Command label, got %q", view)
	}
	if !strings.Contains(view, "sudo install") {
		t.Fatalf("expected command text in view, got %q", view)
	}
	if !strings.Contains(view, "│") {
		t.Fatalf("expected bordered code block in view, got %q", view)
	}
	if !strings.Contains(view, "Ignore") {
		t.Fatalf("expected Ignore when update is available with manual install, got %q", view)
	}
	if strings.Contains(view, "Update now") {
		t.Fatalf("did not expect Update now when manual install is required, got %q", view)
	}
}

func TestSettingsManualInstallCopyKeySetsAction(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{
		manualCommand: "echo hello",
	})
	s.setFocusedPane(settingsPaneDetail)
	s.setActiveSection(ssUpdates)
	s.setFocusedField(sfUpdateManualCommand)

	next, _, _ := s.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}}, DefaultKeys)
	if got := next.takeAction(); got != settingsActionCopyManualInstall {
		t.Fatalf("expected copy manual install action, got %v", got)
	}
}
