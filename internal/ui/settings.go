package ui

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/update"
)

// shellCommandKeywords are highlighted as commands inside the manual-install code block.
var shellCommandKeywords = map[string]bool{
	"sudo": true, "su": true, "doas": true, "install": true, "cp": true, "mv": true,
	"chmod": true, "chown": true, "rm": true, "cd": true, "export": true,
	"echo": true, "curl": true, "wget": true, "tar": true, "unzip": true,
	"dnf": true, "apt": true, "pacman": true, "brew": true, "nix-env": true,
	"sh": true, "bash": true, "fish": true, "zsh": true,
}

// ── Field index ───────────────────────────────────────────────────────────────

type settingsField int

const (
	sfIcons settingsField = iota
	sfDateFormat
	sfMarkReadOnOpen
	sfMarkReadOnFocus
	sfFocusLine
	sfDefaultUnreadOnly
	sfActionableLinks
	sfFilterLinks
	sfReadingWidth
	sfDisplayDensity
	sfBrowser
	sfFeedMaxBody
	sfUpdateCheckOnStartup
	sfUpdateCheckNow
	sfUpdateInstallNow
	sfUpdateDismissVersion
	sfUpdateRestartNow
	sfAboutRepo
	sfAboutIssues
	sfViewLogs
	sfProvider
	sfAPIKey    // visible when provider is openai/claude/gemini
	sfOllamaURL // visible when provider is ollama
	sfOllamaModel
	sfTestAIConnection
	sfOpenAIModel
	sfClaudeModel
	sfGeminiModel
	sfSavePath
	sfMarkReadOnSummarize
	sfUpdateManualCommand
	sfRetroBg
	sfRetroFg
	sfRetroAccent
	sfTheme
	sfConfirmQuit
	sfShowHeaders
	// sfBackToSections is the first focusable target in the detail pane.
	// Activating it restores focus to the sidebar so users never auto-land on a text input.
	sfBackToSections
)

type settingsSection int

const (
	ssDisplay settingsSection = iota
	ssFeeds
	ssUpdates
	ssAI
	ssAdvanced
	ssAbout
	settingsSectionCount
)

type settingsPaneFocus int

const (
	settingsPaneSidebar settingsPaneFocus = iota
	settingsPaneDetail
)

type settingsAction int

const (
	settingsActionNone settingsAction = iota
	settingsActionCheckUpdates
	settingsActionInstallUpdate
	settingsActionDismissVersion
	settingsActionRestartAfterUpdate
	settingsActionOpenRepo
	settingsActionOpenIssues
	settingsActionViewLogs
	settingsActionCopyManualInstall
)

const (
	tideRepoURL              = "https://github.com/allisonhere/tidemail"
	tideIssuesURL            = tideRepoURL + "/issues"
	settingsAboutPulsePeriod = 120 * time.Millisecond
	settingsAboutTwoColMinW  = 56
	settingsAboutCardGap     = 2
	settingsAboutFrameReset  = 4096
)

type settingsAboutPulseMsg struct{}

type settingsUpdateState struct {
	currentVersion   string
	state            updateState
	latestVersion    string
	latestIsFresh    bool
	publishedAt      time.Time
	summary          string
	lastChecked      time.Time
	err              string
	dismissed        bool
	manualCommand    string
	restartable      bool
	installedVersion string
}

type settingsSectionBody struct {
	lines   []string
	anchors map[settingsField]int
}

var (
	layoutDensityLabels   = []string{"Comfortable", "Compact"}
	dateFormatLabels      = []string{"Relative", "Absolute", "None"}
	aiProviderLabels      = []string{"none", "OpenAI", "Claude", "Gemini", "Ollama"}
	aiProviderIDs         = []string{"", "openai", "claude", "gemini", "ollama"}
	settingsSectionLabels = [settingsSectionCount]string{
		"DISPLAY",
		"ACCOUNTS",
		"UPDATES",
		"AI",
		"ADVANCED",
		"ABOUT",
	}
)

func dateFormatIndex(s string) int {
	for i, label := range dateFormatLabels {
		if strings.EqualFold(label, s) {
			return i
		}
	}
	return 0 // default to Relative
}

func providerIndex(id string) int {
	for i, p := range aiProviderIDs {
		if p == id {
			return i
		}
	}
	return 0
}

// ── Settings ──────────────────────────────────────────────────────────────────

type Settings struct {
	// Display
	icons                bool
	dateFormatIdx        int // 0=Relative, 1=Absolute, 2=None
	markReadOnOpen       bool
	markReadOnFocus      bool
	focusLine            bool
	defaultUnreadOnly    bool
	actionableLinks      bool
	filterLinks          bool
	confirmQuit          bool
	showHeaders          bool
	layoutDensityIdx     int // 0 = comfortable, 1 = compact
	readingWidthInput    textinput.Model
	browserInput         textinput.Model
	feedMaxBodyInput     textinput.Model
	updateCheckOnStartup bool
	update               settingsUpdateState
	action               settingsAction

	// AI
	providerIdx         int
	openaiInput         textinput.Model
	openaiModelInput    textinput.Model
	claudeInput         textinput.Model
	claudeModelInput    textinput.Model
	geminiInput         textinput.Model
	geminiModelInput    textinput.Model
	ollamaURLInput      textinput.Model
	ollamaModelInput    textinput.Model
	savePathInput       textinput.Model
	markReadOnSummarize bool

	activeSection      settingsSection
	focusedPane        settingsPaneFocus
	detailHeight       int // height of the detail pane, set during View
	sectionField       [settingsSectionCount]settingsField
	focusedField       settingsField
	aboutGradientFrame int
	aiValidatePending  bool
	aiTestError        string
	aiTestOk           bool
	shouldSave         bool
	shouldExit         bool
	themeName          string // current picker selection (drives retro color fields)
	themeIdx           int    // index into BuiltinThemes
	retroBgInput       textinput.Model
	retroFgInput       textinput.Model
	retroAccentInput   textinput.Model
}

func newSettings(cfg config.Config, updateState settingsUpdateState) Settings {
	mkInput := func(value, placeholder string, masked bool) textinput.Model {
		t := textinput.New()
		t.Placeholder = placeholder
		t.CharLimit = 500
		t.SetValue(value)
		if masked {
			t.EchoMode = textinput.EchoPassword
			if ThemeUsesASCII(cfg.Theme) {
				t.EchoCharacter = '*'
			} else {
				t.EchoCharacter = '●'
			}
		}
		return t
	}

	layoutIdx := 0
	if config.NormalizeDisplayDensity(cfg.Display.Density) == "compact" {
		layoutIdx = 1
	}
	var retroTweak config.RetroTerminalTweak
	switch cfg.Theme {
	case ThemeNameVT52:
		retroTweak = cfg.Display.VT52
	case ThemeNameVT100:
		retroTweak = cfg.Display.VT100
	}
	_, themeIdx := ThemeByName(cfg.Theme)
	s := Settings{
		icons:                cfg.Display.Icons,
		themeName:            cfg.Theme,
		themeIdx:             themeIdx,
		retroBgInput:         mkInput(retroTweak.Bg, "optional #rrggbb", false),
		retroFgInput:         mkInput(retroTweak.Fg, "optional #rrggbb", false),
		retroAccentInput:     mkInput(retroTweak.Accent, "optional #rrggbb", false),
		dateFormatIdx:        dateFormatIndex(cfg.Display.DateFormat),
		markReadOnOpen:       cfg.Display.MarkReadOnOpen,
		markReadOnFocus:      cfg.Display.MarkReadOnFocus,
		focusLine:            cfg.Display.FocusLine,
		defaultUnreadOnly:    cfg.Display.DefaultUnreadOnly,
		actionableLinks:      cfg.Display.ActionableLinks,
		filterLinks:          cfg.Display.FilterLinks,
		confirmQuit:          cfg.Display.ConfirmQuit,
		showHeaders:          cfg.Display.ShowHeaders,
		layoutDensityIdx:     layoutIdx,
		readingWidthInput:    mkInput(strconv.Itoa(cfg.Display.ReadingWidth), "0 (no limit)", false),
		browserInput:         mkInput(cfg.Display.Browser, "xdg-open", false),
		feedMaxBodyInput:     mkInput(strconv.Itoa(cfg.Feed.MaxBodyMiB), "10", false),
		updateCheckOnStartup: cfg.Updates.CheckOnStartup,
		update:               updateState,
		providerIdx:          providerIndex(cfg.AI.Provider),
		openaiInput:          mkInput(cfg.AI.OpenAIKey, "sk-...", true),
		openaiModelInput:     mkInput(cfg.AI.OpenAIModel, "gpt-4o-mini", false),
		claudeInput:          mkInput(cfg.AI.ClaudeKey, "sk-ant-...", true),
		claudeModelInput:     mkInput(cfg.AI.ClaudeModel, "claude-sonnet-4", false),
		geminiInput:          mkInput(cfg.AI.GeminiKey, "AIza...", true),
		geminiModelInput:     mkInput(cfg.AI.GeminiModel, "gemini-1.5-flash", false),
		ollamaURLInput:       mkInput(cfg.AI.OllamaURL, "http://localhost:11434", false),
		ollamaModelInput:     mkInput(cfg.AI.OllamaModel, "llama3.2", false),
		savePathInput:        mkInput(cfg.AI.SavePath, "~/", false),
		markReadOnSummarize:  cfg.AI.MarkReadOnSummarize,
		activeSection:        ssDisplay,
		focusedPane:          settingsPaneSidebar,
		sectionField: [settingsSectionCount]settingsField{
			ssDisplay:  sfBackToSections,
			ssFeeds:    sfBackToSections,
			ssUpdates:  sfBackToSections,
			ssAI:       sfBackToSections,
			ssAdvanced: sfBackToSections,
			ssAbout:    sfBackToSections,
		},
		focusedField: sfBackToSections,
	}
	s.syncMaskedEchoChars()
	s.applyFocus()
	return s
}

func (s *Settings) syncMaskedEchoChars() {
	ch := '●'
	if ThemeUsesASCII(s.themeName) {
		ch = '*'
	}
	s.openaiInput.EchoCharacter = ch
	s.claudeInput.EchoCharacter = ch
	s.geminiInput.EchoCharacter = ch
}

// ApplyTo merges the settings screen state back into a Config.
func (s Settings) ApplyTo(cfg config.Config) config.Config {
	if s.themeIdx >= 0 && s.themeIdx < len(BuiltinThemes) {
		cfg.Theme = BuiltinThemes[s.themeIdx].Name
	}
	cfg.Display.Icons = s.icons
	cfg.Display.DateFormat = strings.ToLower(dateFormatLabels[s.dateFormatIdx])
	cfg.Display.MarkReadOnOpen = s.markReadOnOpen
	cfg.Display.MarkReadOnFocus = s.markReadOnFocus
	cfg.Display.FocusLine = s.focusLine
	cfg.Display.DefaultUnreadOnly = s.defaultUnreadOnly
	cfg.Display.ActionableLinks = s.actionableLinks
	cfg.Display.FilterLinks = s.filterLinks
	cfg.Display.ConfirmQuit = s.confirmQuit
	cfg.Display.ShowHeaders = s.showHeaders
	if w, err := strconv.Atoi(strings.TrimSpace(s.readingWidthInput.Value())); err == nil {
		cfg.Display.ReadingWidth = max(0, w)
	}
	if s.layoutDensityIdx == 1 {
		cfg.Display.Density = "compact"
	} else {
		cfg.Display.Density = "comfortable"
	}
	cfg.Display.Browser = strings.TrimSpace(s.browserInput.Value())
	bg := strings.TrimSpace(s.retroBgInput.Value())
	fg := strings.TrimSpace(s.retroFgInput.Value())
	ac := strings.TrimSpace(s.retroAccentInput.Value())
	switch strings.ToLower(strings.TrimSpace(s.themeName)) {
	case ThemeNameVT52:
		cfg.Display.VT52 = config.RetroTerminalTweak{Bg: bg, Fg: fg, Accent: ac}
	case ThemeNameVT100:
		cfg.Display.VT100 = config.RetroTerminalTweak{Bg: bg, Fg: fg, Accent: ac}
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s.feedMaxBodyInput.Value())); err == nil && n > 0 {
		cfg.Feed.MaxBodyMiB = n
	}
	cfg.Updates.CheckOnStartup = s.updateCheckOnStartup

	cfg.AI.Provider = aiProviderIDs[s.providerIdx]
	if value := strings.TrimSpace(s.openaiInput.Value()); value != "" {
		cfg.AI.OpenAIKey = value
	}
	cfg.AI.OpenAIModel = strings.TrimSpace(s.openaiModelInput.Value())
	if value := strings.TrimSpace(s.claudeInput.Value()); value != "" {
		cfg.AI.ClaudeKey = value
	}
	cfg.AI.ClaudeModel = strings.TrimSpace(s.claudeModelInput.Value())
	if value := strings.TrimSpace(s.geminiInput.Value()); value != "" {
		cfg.AI.GeminiKey = value
	}
	cfg.AI.GeminiModel = strings.TrimSpace(s.geminiModelInput.Value())
	cfg.AI.OllamaURL = strings.TrimSpace(s.ollamaURLInput.Value())
	cfg.AI.OllamaModel = strings.TrimSpace(s.ollamaModelInput.Value())
	cfg.AI.SavePath = strings.TrimSpace(s.savePathInput.Value())
	cfg.AI.MarkReadOnSummarize = s.markReadOnSummarize
	return cfg
}

func (s *Settings) setUpdateState(v settingsUpdateState) {
	s.update = v
	s.ensureSectionFieldVisible(ssUpdates)
}

func (s *Settings) takeAction() settingsAction {
	action := s.action
	s.action = settingsActionNone
	return action
}

func (s *Settings) setFocusedField(field settingsField) {
	s.focusedField = field
	s.sectionField[s.activeSection] = field
	s.applyFocus()
}

func (s *Settings) clearAITestFeedback() {
	s.aiTestError = ""
	s.aiTestOk = false
}

// draftAIConfig returns the AI section as it would be written by ApplyTo, for
// network validation before save.
func (s Settings) draftAIConfig() config.AIConfig {
	return config.AIConfig{
		Provider:            aiProviderIDs[s.providerIdx],
		OpenAIKey:           strings.TrimSpace(s.openaiInput.Value()),
		OpenAIModel:         strings.TrimSpace(s.openaiModelInput.Value()),
		ClaudeKey:           strings.TrimSpace(s.claudeInput.Value()),
		ClaudeModel:         strings.TrimSpace(s.claudeModelInput.Value()),
		GeminiKey:           strings.TrimSpace(s.geminiInput.Value()),
		GeminiModel:         strings.TrimSpace(s.geminiModelInput.Value()),
		OllamaURL:           strings.TrimSpace(s.ollamaURLInput.Value()),
		OllamaModel:         strings.TrimSpace(s.ollamaModelInput.Value()),
		SavePath:            strings.TrimSpace(s.savePathInput.Value()),
		MarkReadOnSummarize: s.markReadOnSummarize,
	}
}

func (s *Settings) setFocusedPane(pane settingsPaneFocus) {
	prev := s.focusedPane
	s.focusedPane = pane
	if pane == settingsPaneDetail && prev != settingsPaneDetail {
		field := sfBackToSections
		if s.activeSection == ssAI && s.providerIdx >= 1 && s.providerIdx <= 3 {
			field = sfAPIKey
		}
		s.focusedField = field
		s.sectionField[s.activeSection] = field
	}
	s.applyFocus()
}

func (s *Settings) setActiveSection(section settingsSection) {
	if section < 0 || section >= settingsSectionCount {
		return
	}
	s.activeSection = section
	if section != ssAbout {
		s.aboutGradientFrame = 0
	}
	s.ensureSectionFieldVisible(section)
	s.focusedField = s.sectionField[section]
	s.applyFocus()
}

func (s *Settings) nextSection() settingsSection {
	return (s.activeSection + 1) % settingsSectionCount
}

func (s *Settings) prevSection() settingsSection {
	if s.activeSection == 0 {
		return settingsSectionCount - 1
	}
	return s.activeSection - 1
}

func (s *Settings) ensureSectionFieldVisible(section settingsSection) {
	fields := s.sectionFields(section)
	if len(fields) == 0 {
		return
	}
	current := s.sectionField[section]
	for _, f := range fields {
		if f == current {
			return
		}
	}
	s.sectionField[section] = fields[0]
	if s.activeSection == section {
		s.focusedField = fields[0]
		s.applyFocus()
	}
}

// ── Focus management ──────────────────────────────────────────────────────────

func (s *Settings) applyFocus() {
	s.browserInput.Blur()
	s.retroBgInput.Blur()
	s.retroFgInput.Blur()
	s.retroAccentInput.Blur()
	s.readingWidthInput.Blur()
	s.feedMaxBodyInput.Blur()
	s.openaiInput.Blur()
	s.openaiModelInput.Blur()
	s.claudeInput.Blur()
	s.claudeModelInput.Blur()
	s.geminiInput.Blur()
	s.geminiModelInput.Blur()
	s.ollamaURLInput.Blur()
	s.ollamaModelInput.Blur()
	s.savePathInput.Blur()

	if s.focusedPane != settingsPaneDetail {
		return
	}

	switch s.focusedField {
	case sfBrowser:
		s.browserInput.Focus()
	case sfReadingWidth:
		s.readingWidthInput.Focus()
	case sfFeedMaxBody:
		s.feedMaxBodyInput.Focus()
	case sfAPIKey:
		switch s.providerIdx {
		case 1:
			s.openaiInput.Focus()
		case 2:
			s.claudeInput.Focus()
		case 3:
			s.geminiInput.Focus()
		}
	case sfOpenAIModel:
		s.openaiModelInput.Focus()
	case sfClaudeModel:
		s.claudeModelInput.Focus()
	case sfGeminiModel:
		s.geminiModelInput.Focus()
	case sfOllamaURL:
		s.ollamaURLInput.Focus()
	case sfOllamaModel:
		s.ollamaModelInput.Focus()
	case sfSavePath:
		s.savePathInput.Focus()
	case sfRetroBg:
		s.retroBgInput.Focus()
	case sfRetroFg:
		s.retroFgInput.Focus()
	case sfRetroAccent:
		s.retroAccentInput.Focus()
	}
}

func (s Settings) visibleFields() []settingsField {
	return s.sectionFields(s.activeSection)
}

// updateInstallActionsVisible is true when a release exists on the network that is newer than the running app.
func (s Settings) updateInstallActionsVisible() bool {
	u := s.update
	if u.latestVersion == "" {
		return false
	}
	return update.IsNewerVersion(u.latestVersion, u.currentVersion)
}

// updateNowActionVisible is true when an update can be installed from the app (not when a manual shell command is required).
func (s Settings) updateNowActionVisible() bool {
	return s.updateInstallActionsVisible() && s.update.manualCommand == ""
}

func (s Settings) sectionFields(section settingsSection) []settingsField {
	switch section {
	case ssDisplay:
		fields := []settingsField{sfBackToSections, sfIcons, sfDateFormat, sfMarkReadOnOpen, sfMarkReadOnFocus, sfFocusLine, sfDefaultUnreadOnly, sfTheme, sfDisplayDensity, sfReadingWidth}
		if config.IsRetroTerminalTheme(s.themeName) {
			fields = append(fields, sfRetroBg, sfRetroFg, sfRetroAccent)
		}
		return append(fields, sfActionableLinks, sfFilterLinks, sfBrowser, sfConfirmQuit, sfShowHeaders)
	case ssFeeds:
		return []settingsField{sfBackToSections, sfFeedMaxBody}
	case ssUpdates:
		fields := []settingsField{sfBackToSections, sfUpdateCheckOnStartup, sfUpdateCheckNow}
		if s.updateNowActionVisible() {
			fields = append(fields, sfUpdateInstallNow)
		}
		if s.updateInstallActionsVisible() {
			fields = append(fields, sfUpdateDismissVersion)
		}
		if s.update.manualCommand != "" {
			fields = append(fields, sfUpdateManualCommand)
		}
		if s.update.restartable {
			fields = append(fields, sfUpdateRestartNow)
		}
		return fields
	case ssAI:
		fields := []settingsField{sfBackToSections, sfProvider}
		switch s.providerIdx {
		case 1:
			fields = append(fields, sfAPIKey, sfOpenAIModel)
		case 2:
			fields = append(fields, sfAPIKey, sfClaudeModel)
		case 3:
			fields = append(fields, sfAPIKey, sfGeminiModel)
		case 4:
			fields = append(fields, sfOllamaURL, sfOllamaModel)
		}
		fields = append(fields, sfTestAIConnection, sfSavePath, sfMarkReadOnSummarize)
		return fields
	case ssAdvanced:
		return []settingsField{sfBackToSections, sfViewLogs}

	case ssAbout:
		return []settingsField{sfBackToSections, sfAboutRepo, sfAboutIssues}
	default:
		return nil
	}
}

func settingsActionURL(action settingsAction) string {
	switch action {
	case settingsActionOpenRepo:
		return tideRepoURL
	case settingsActionOpenIssues:
		return tideIssuesURL
	default:
		return ""
	}
}

func settingsAboutPulseCmd() tea.Cmd {
	return tea.Tick(settingsAboutPulsePeriod, func(time.Time) tea.Msg {
		return settingsAboutPulseMsg{}
	})
}

func (s Settings) aboutPulseCmd() tea.Cmd {
	if s.activeSection != ssAbout {
		return nil
	}
	return settingsAboutPulseCmd()
}

func (s Settings) nextField() settingsField {
	fields := s.visibleFields()
	if len(fields) == 0 {
		return s.focusedField
	}
	for i, f := range fields {
		if f == s.focusedField {
			if i < len(fields)-1 {
				return fields[i+1]
			}
			return s.focusedField
		}
	}
	return fields[0]
}

func (s Settings) prevField() settingsField {
	fields := s.visibleFields()
	if len(fields) == 0 {
		return s.focusedField
	}
	for i, f := range fields {
		if f == s.focusedField {
			if i > 0 {
				return fields[i-1]
			}
			return s.focusedField
		}
	}
	return fields[len(fields)-1]
}

func (s Settings) isTextInput() bool {
	if s.focusedPane != settingsPaneDetail {
		return false
	}
	switch s.focusedField {
	case sfBrowser, sfFeedMaxBody, sfReadingWidth, sfAPIKey, sfOpenAIModel, sfClaudeModel, sfGeminiModel, sfOllamaURL, sfOllamaModel, sfSavePath,
		sfRetroBg, sfRetroFg, sfRetroAccent:
		return true
	}
	return false
}

func (s Settings) updateFocusedTextInput(msg tea.Msg) (Settings, tea.Cmd, bool) {
	s.clearAITestFeedback()
	var cmd tea.Cmd
	switch s.focusedField {
	case sfBrowser:
		s.browserInput, cmd = s.browserInput.Update(msg)
	case sfReadingWidth:
		s.readingWidthInput, cmd = s.readingWidthInput.Update(msg)
	case sfFeedMaxBody:
		s.feedMaxBodyInput, cmd = s.feedMaxBodyInput.Update(msg)
	case sfAPIKey:
		switch s.providerIdx {
		case 1:
			s.openaiInput, cmd = s.openaiInput.Update(msg)
		case 2:
			s.claudeInput, cmd = s.claudeInput.Update(msg)
		case 3:
			s.geminiInput, cmd = s.geminiInput.Update(msg)
		}
	case sfOpenAIModel:
		s.openaiModelInput, cmd = s.openaiModelInput.Update(msg)
	case sfClaudeModel:
		s.claudeModelInput, cmd = s.claudeModelInput.Update(msg)
	case sfGeminiModel:
		s.geminiModelInput, cmd = s.geminiModelInput.Update(msg)
	case sfOllamaURL:
		s.ollamaURLInput, cmd = s.ollamaURLInput.Update(msg)
	case sfOllamaModel:
		s.ollamaModelInput, cmd = s.ollamaModelInput.Update(msg)
	case sfSavePath:
		s.savePathInput, cmd = s.savePathInput.Update(msg)
	case sfRetroBg:
		s.retroBgInput, cmd = s.retroBgInput.Update(msg)
	case sfRetroFg:
		s.retroFgInput, cmd = s.retroFgInput.Update(msg)
	case sfRetroAccent:
		s.retroAccentInput, cmd = s.retroAccentInput.Update(msg)
	}
	return s, cmd, false
}

func selectedAIKeyFormat(value string) string {
	value = strings.TrimSpace(value)
	switch {
	case strings.HasPrefix(value, "sk-ant-"):
		return "Claude"
	case strings.HasPrefix(value, "AIza"):
		return "Gemini"
	case strings.HasPrefix(value, "sk-"):
		return "OpenAI"
	default:
		return ""
	}
}

func selectedAIKeyExpectation(providerIdx int) (providerName, expectedPrefix string) {
	switch providerIdx {
	case 1:
		return "OpenAI", "sk-"
	case 2:
		return "Claude", "sk-ant-"
	case 3:
		return "Gemini", "AIza"
	default:
		return "", ""
	}
}

func (s Settings) selectedAIKeyValue() string {
	switch s.providerIdx {
	case 1:
		return strings.TrimSpace(s.openaiInput.Value())
	case 2:
		return strings.TrimSpace(s.claudeInput.Value())
	case 3:
		return strings.TrimSpace(s.geminiInput.Value())
	default:
		return ""
	}
}

func (s Settings) selectedAIKeyValidation() (string, bool) {
	providerName, expectedPrefix := selectedAIKeyExpectation(s.providerIdx)
	if providerName == "" {
		return "", true
	}

	value := s.selectedAIKeyValue()
	if value == "" {
		return fmt.Sprintf("%s key is empty; expected %s", providerName, expectedPrefix), false
	}

	format := selectedAIKeyFormat(value)
	if format == "" {
		return fmt.Sprintf("%s keys usually start with %s", providerName, expectedPrefix), false
	}
	if format != providerName {
		return fmt.Sprintf("Looks like %s, but %s is selected.", format, providerName), false
	}
	return fmt.Sprintf("Format looks like %s", providerName), true
}

func (s Settings) focusedTextInputCursorPosition() int {
	switch s.focusedField {
	case sfBrowser:
		return s.browserInput.Position()
	case sfReadingWidth:
		return s.readingWidthInput.Position()
	case sfFeedMaxBody:
		return s.feedMaxBodyInput.Position()
	case sfAPIKey:
		switch s.providerIdx {
		case 1:
			return s.openaiInput.Position()
		case 2:
			return s.claudeInput.Position()
		case 3:
			return s.geminiInput.Position()
		}
	case sfOpenAIModel:
		return s.openaiModelInput.Position()
	case sfClaudeModel:
		return s.claudeModelInput.Position()
	case sfGeminiModel:
		return s.geminiModelInput.Position()
	case sfOllamaURL:
		return s.ollamaURLInput.Position()
	case sfOllamaModel:
		return s.ollamaModelInput.Position()
	case sfSavePath:
		return s.savePathInput.Position()
	case sfRetroBg:
		return s.retroBgInput.Position()
	case sfRetroFg:
		return s.retroFgInput.Position()
	case sfRetroAccent:
		return s.retroAccentInput.Position()
	}
	return -1
}

func (s Settings) isPickerField() bool {
	switch s.focusedField {
	case sfProvider, sfDisplayDensity, sfTheme, sfDateFormat:
		return true
	}
	return false
}

// ── Update ────────────────────────────────────────────────────────────────────

func (s Settings) Update(msg tea.Msg, keys KeyMap) (Settings, tea.Cmd, bool) {
	if _, ok := msg.(settingsAboutPulseMsg); ok {
		if s.activeSection != ssAbout {
			s.aboutGradientFrame = 0
			return s, nil, false
		}
		s.aboutGradientFrame++
		if s.aboutGradientFrame >= settingsAboutFrameReset {
			s.aboutGradientFrame = 0
		}
		return s, settingsAboutPulseCmd(), false
	}

	if res, ok := msg.(AIValidateDoneMsg); ok {
		s.aiValidatePending = false
		if res.Err != nil {
			s.aiTestError = res.Err.Error()
			s.aiTestOk = false
		} else {
			s.aiTestError = ""
			s.aiTestOk = true
		}
		return s, nil, false
	}

	// Route cursor-blink ticks to the active text input.
	if _, ok := msg.(tea.KeyMsg); !ok {
		if s.isTextInput() {
			return s.updateFocusedTextInput(msg)
		}
		return s, nil, false
	}

	key := msg.(tea.KeyMsg)
	if s.isTextInput() {
		switch key.Type {
		case tea.KeyRunes, tea.KeySpace, tea.KeyBackspace, tea.KeyDelete, tea.KeyEnter:
			return s.updateFocusedTextInput(msg)
		}
	}

	// Global: ctrl+s saves immediately. Esc from detail keeps edits in the
	// settings model and moves back to categories; esc from categories saves.
	switch key.String() {
	case "ctrl+s":
		return s.saveAndExit()
	case "esc":
		if s.focusedPane == settingsPaneDetail {
			s.setFocusedPane(settingsPaneSidebar)
			return s, nil, false
		}
		return s.saveAndExit()
	case "q":
		if s.focusedPane != settingsPaneSidebar {
			break
		}
		s.shouldSave = false
		s.shouldExit = true
		return s, nil, true
	}

	if s.focusedPane == settingsPaneSidebar {
		switch {
		case keyMatches(key, keys.Up):
			s.setActiveSection(s.prevSection())
			return s, s.aboutPulseCmd(), false
		case keyMatches(key, keys.Down):
			s.setActiveSection(s.nextSection())
			return s, s.aboutPulseCmd(), false
		case keyMatches(key, keys.Right), keyMatches(key, keys.Enter), keyMatches(key, keys.Tab):
			s.setFocusedPane(settingsPaneDetail)
			return s, nil, false
		default:
			return s, nil, false
		}
	}

	if s.isTextInput() && key.Type == tea.KeyLeft {
		if s.focusedTextInputCursorPosition() == 0 {
			s.setFocusedPane(settingsPaneSidebar)
			return s, nil, false
		}
		return s.updateFocusedTextInput(msg)
	}

	if !s.isTextInput() && !s.isPickerField() && keyMatches(key, keys.Left) {
		s.setFocusedPane(settingsPaneSidebar)
		return s, nil, false
	}

	// Tab / shift+tab navigate within the active section.
	switch {
	case keyMatches(key, keys.Tab):
		s.setFocusedField(s.nextField())
		return s, nil, false

	case key.String() == "shift+tab":
		s.setFocusedField(s.prevField())
		return s, nil, false
	}

	// Field-specific handling.
	switch s.focusedField {
	case sfBackToSections:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter), keyMatches(key, keys.Left):
			s.setFocusedPane(settingsPaneSidebar)
			return s, nil, false
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
			return s, nil, false
		case keyMatches(key, keys.Up):
			// Up from the back link stays put; it's the first focusable row.
			return s, nil, false
		}
		return s, nil, false

	case sfIcons:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.icons = !s.icons
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfDateFormat:
		if keyMatches(key, keys.Left) {
			s.dateFormatIdx = (s.dateFormatIdx + len(dateFormatLabels) - 1) % len(dateFormatLabels)
			s.setFocusedField(sfDateFormat)
		} else if keyMatches(key, keys.Right) || keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.dateFormatIdx = (s.dateFormatIdx + 1) % len(dateFormatLabels)
			s.setFocusedField(sfDateFormat)
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfDisplayDensity:
		switch {
		case keyMatches(key, keys.Left):
			s.layoutDensityIdx = (s.layoutDensityIdx + len(layoutDensityLabels) - 1) % len(layoutDensityLabels)
			s.setFocusedField(sfDisplayDensity)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.layoutDensityIdx = (s.layoutDensityIdx + 1) % len(layoutDensityLabels)
			s.setFocusedField(sfDisplayDensity)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfTheme:
		switch {
		case keyMatches(key, keys.Left):
			s.themeIdx = (s.themeIdx + len(BuiltinThemes) - 1) % len(BuiltinThemes)
			s.themeName = BuiltinThemes[s.themeIdx].Name
			s.syncMaskedEchoChars()
			s.ensureSectionFieldVisible(ssDisplay)
			s.setFocusedField(sfTheme)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.themeIdx = (s.themeIdx + 1) % len(BuiltinThemes)
			s.themeName = BuiltinThemes[s.themeIdx].Name
			s.syncMaskedEchoChars()
			s.ensureSectionFieldVisible(ssDisplay)
			s.setFocusedField(sfTheme)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfMarkReadOnOpen:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.markReadOnOpen = !s.markReadOnOpen
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfMarkReadOnFocus:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.markReadOnFocus = !s.markReadOnFocus
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfFocusLine:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.focusLine = !s.focusLine
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfDefaultUnreadOnly:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.defaultUnreadOnly = !s.defaultUnreadOnly
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfActionableLinks:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.actionableLinks = !s.actionableLinks
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfFilterLinks:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.filterLinks = !s.filterLinks
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfConfirmQuit:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.confirmQuit = !s.confirmQuit
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfShowHeaders:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.showHeaders = !s.showHeaders
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfMarkReadOnSummarize:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.markReadOnSummarize = !s.markReadOnSummarize
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfUpdateCheckOnStartup:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.updateCheckOnStartup = !s.updateCheckOnStartup
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfUpdateCheckNow:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionCheckUpdates
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfUpdateInstallNow:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionInstallUpdate
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfUpdateDismissVersion:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionDismissVersion
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfUpdateManualCommand:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.CopyText) || key.String() == "c":
			s.action = settingsActionCopyManualInstall
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfUpdateRestartNow:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionRestartAfterUpdate
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfAboutRepo:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionOpenRepo
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfViewLogs:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionViewLogs
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfAboutIssues:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.action = settingsActionOpenIssues
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfTestAIConnection:
		switch {
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			if s.aiValidatePending {
				return s, nil, false
			}
			if s.providerIdx == 0 {
				s.aiTestError = "choose an AI provider first"
				s.aiTestOk = false
				return s, nil, false
			}
			if msg, ok := s.selectedAIKeyValidation(); !ok {
				s.aiTestError = msg
				s.aiTestOk = false
				return s, nil, false
			}
			s.aiValidatePending = true
			s.aiTestError = ""
			s.aiTestOk = false
			cfg := s.draftAIConfig()
			return s, validateAICredentialsCmd(cfg), false
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}
		return s, nil, false

	case sfProvider:
		switch {
		case keyMatches(key, keys.Left):
			s.clearAITestFeedback()
			s.providerIdx = (s.providerIdx + len(aiProviderLabels) - 1) % len(aiProviderLabels)
			s.ensureSectionFieldVisible(ssAI)
			s.setFocusedField(sfProvider)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.clearAITestFeedback()
			s.providerIdx = (s.providerIdx + 1) % len(aiProviderLabels)
			s.ensureSectionFieldVisible(ssAI)
			s.setFocusedField(sfProvider)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}

	case sfBrowser, sfFeedMaxBody, sfReadingWidth, sfAPIKey, sfOpenAIModel, sfClaudeModel, sfGeminiModel, sfOllamaURL, sfOllamaModel, sfSavePath,
		sfRetroBg, sfRetroFg, sfRetroAccent:
		// Enter advances to next field; everything else goes to the text input.
		if keyMatches(key, keys.Enter) {
			s.setFocusedField(s.nextField())
			return s, nil, false
		}
		if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
			return s, nil, false
		}
		if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
			return s, nil, false
		}
		return s.updateFocusedTextInput(msg)
	}

	return s, nil, false
}

func (s Settings) saveAndExit() (Settings, tea.Cmd, bool) {
	s.shouldSave = true
	s.shouldExit = true
	return s, nil, true
}

// ── View ──────────────────────────────────────────────────────────────────────

func (s *Settings) View(width, height int, chrome managerChrome) string {
	header := renderManagerHeader("SETTINGS", width, chrome)
	gap := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	hints := s.viewHints(width, chrome)
	bodyH := max(1, height-lipgloss.Height(header)-lipgloss.Height(gap)-lipgloss.Height(hints))
	body := s.viewSplit(width, bodyH, chrome)

	return lipgloss.JoinVertical(lipgloss.Left, header, gap, body, hints)
}

func (s *Settings) viewSplit(width, height int, chrome managerChrome) string {
	leftW := clamp(width/4, 14, 18)
	if width-leftW-1 < 32 {
		leftW = max(14, width-33)
	}
	rightW := max(18, width-leftW-1)
	left := s.viewSectionsPane(leftW, height, chrome)
	right := s.viewSectionPane(rightW, height, chrome)
	sepLines := make([]string, height)
	for i := range sepLines {
		sepLines[i] = "│"
	}
	separator := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.border).
		Render(strings.Join(sepLines, "\n"))
	return lipgloss.JoinHorizontal(lipgloss.Top, left, separator, right)
}

func (s Settings) viewSectionsPane(width, height int, chrome managerChrome) string {
	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	rows := make([]string, 0, settingsSectionCount*2)
	for i, label := range settingsSectionLabels {
		selected := settingsSection(i) == s.activeSection
		subtitle := ""
		if settingsSection(i) == ssAI && s.providerIdx > 0 {
			subtitle = strings.ToLower(aiProviderLabels[s.providerIdx])
		}
		if i > 0 {
			rows = append(rows, blank)
		}
		rows = append(rows, s.renderSectionNavRow(width, label, subtitle, selected, s.focusedPane == settingsPaneSidebar, chrome))
	}
	body := lipgloss.JoinVertical(lipgloss.Left, append([]string{blank}, rows...)...)
	title := "CATEGORIES"
	if s.focusedPane == settingsPaneSidebar {
		title = "CATEGORIES >"
	}
	section := clampView(renderManagerSection(title, body, chrome, s.focusedPane == settingsPaneSidebar), width, height, chrome.baseBg)
	return lipgloss.NewStyle().Width(width).Height(height).Background(chrome.baseBg).Render(section)
}

func (s *Settings) viewSectionPane(width, height int, chrome managerChrome) string {
	s.detailHeight = height - 2 // title row + gap
	title := settingsSectionLabels[s.activeSection]
	if s.focusedPane == settingsPaneDetail {
		title += " >"
	}
	body := s.viewSectionBody(width, chrome)
	titleStyle := chrome.sectionLabel
	if s.focusedPane == settingsPaneDetail {
		titleStyle = chrome.sectionLabelActive
	}
	titleRow := titleStyle.Width(width).Render(title)
	paneBg := chrome.baseBg
	if s.activeSection == ssAbout {
		paneBg = lipgloss.Color("#000000")
	}
	headingGap := lipgloss.NewStyle().Background(paneBg).Width(width).Render("")
	bodyHeight := max(1, height-2)
	section := lipgloss.JoinVertical(lipgloss.Left, titleRow, headingGap, s.scrollSectionBody(body, width, bodyHeight, paneBg))
	return lipgloss.NewStyle().Width(width).Height(height).Background(paneBg).Render(section)
}

func (s Settings) scrollSectionBody(body settingsSectionBody, width, height int, bg lipgloss.Color) string {
	if len(body.lines) == 0 {
		return clampView("", width, height, bg)
	}
	offset := 0
	if anchor, ok := body.anchors[s.focusedField]; ok {
		offset = settingsScrollOffset(len(body.lines), anchor, height)
	}
	end := min(len(body.lines), offset+height)
	return clampView(strings.Join(body.lines[offset:end], "\n"), width, height, bg)
}

func settingsScrollOffset(totalLines, anchorLine, height int) int {
	if totalLines <= height || height <= 0 {
		return 0
	}
	anchorLine = clamp(anchorLine, 0, totalLines-1)
	offset := anchorLine - height/2
	return clamp(offset, 0, totalLines-height)
}

func (s Settings) viewSectionBody(width int, chrome managerChrome) settingsSectionBody {
	if s.activeSection == ssAbout {
		body := s.renderAboutSection(width, chrome)
		body = s.prependBackLink(body, width, chrome)
		return body
	}

	b := newSettingsFormBuilder(s, width, chrome)
	b.addBackLink()
	switch s.activeSection {
	case ssDisplay:
		b.addGroup("Display")
		b.addToggle("Icons", s.icons, sfIcons)
		b.addDateFormatSelector()
		b.addToggle("Mark read on open", s.markReadOnOpen, sfMarkReadOnOpen)
		b.addToggle("Mark read on focus", s.markReadOnFocus, sfMarkReadOnFocus)
		b.addToggle("Focus line", s.focusLine, sfFocusLine)
		b.addToggle("Default to unread only", s.defaultUnreadOnly, sfDefaultUnreadOnly)
		b.addThemeSelector()
		b.addDensitySelector()
		b.addInput("Reading width (columns)", s.readingWidthInput, sfReadingWidth)
		if config.IsRetroTerminalTheme(s.themeName) {
			b.addGroup("Terminal colors")
			b.addInput("VT background (#rrggbb)", s.retroBgInput, sfRetroBg)
			b.addInput("VT foreground (#rrggbb)", s.retroFgInput, sfRetroFg)
			b.addInput("VT accent (#rrggbb)", s.retroAccentInput, sfRetroAccent)
		}
		b.addToggle("Actionable article links", s.actionableLinks, sfActionableLinks)
		b.addToggle("Filter links from articles", s.filterLinks, sfFilterLinks)
		b.addInput("Browser command", s.browserInput, sfBrowser)
		b.addToggle("Confirm before quitting", s.confirmQuit, sfConfirmQuit)
		b.addToggle("Show email headers", s.showHeaders, sfShowHeaders)

	case ssFeeds:
		b.addGroup("Storage")
		b.addInput("Max body size (MiB)", s.feedMaxBodyInput, sfFeedMaxBody)

	case ssUpdates:
		b.addGroup("Updates")
		b.addValue("Current version", s.update.currentVersion, false)
		b.addToggle("Check on startup", s.updateCheckOnStartup, sfUpdateCheckOnStartup)
		if !s.update.lastChecked.IsZero() {
			b.addValue("Last checked", relativeTime(s.update.lastChecked), false)
		}
		b.addAction("Check now", "", sfUpdateCheckNow)
		if s.update.latestVersion != "" {
			b.addValue("Latest version", s.update.latestVersion, false)
		}
		if !s.update.publishedAt.IsZero() {
			b.addValue("Published", s.update.publishedAt.Format("Jan 2, 2006"), false)
		}
		b.addValue("Status", s.update.statusLabel(), false)
		if s.update.summary != "" {
			summaryIndent := "  "
			sumW := max(1, b.contentW-lipgloss.Width(summaryIndent))
			sumLines := wrapShellCommand(s.update.summary, sumW)
			hintStyle := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
			for _, sl := range sumLines {
				b.addLine(hintStyle.Width(b.contentW).Render(summaryIndent + sl))
			}
			b.addBlank()
		}
		if s.updateNowActionVisible() {
			b.addAction("Update now", "Update now", sfUpdateInstallNow)
		}
		if s.updateInstallActionsVisible() {
			b.addAction("Ignore", "", sfUpdateDismissVersion)
		}
		if s.update.manualCommand != "" {
			focused := s.focusedField == sfUpdateManualCommand
			b.markAnchor(sfUpdateManualCommand)
			for _, line := range s.manualInstallCommandLines(b.width, s.update.manualCommand, focused, chrome) {
				b.addLine(line)
			}
			if hint := s.fieldHint(sfUpdateManualCommand); hint != "" {
				b.addHint(hint)
			}
			b.addBlank()
		}
		if s.update.restartable {
			b.addAction("Restart now", "launch updated Tidemail", sfUpdateRestartNow)
		}

	case ssAI:
		b.addAISection()

	case ssAdvanced:
		b.addAdvancedSection()

	}

	if len(b.body.lines) == 0 {
		b.body.lines = append(b.body.lines, b.blank)
	}

	return b.body
}

type settingsFormBuilder struct {
	s          Settings
	width      int
	contentW   int
	labelW     int
	hintIndent string
	chrome     managerChrome
	ind        lipgloss.Style
	blank      string
	body       settingsSectionBody
}

func newSettingsFormBuilder(s Settings, width int, chrome managerChrome) settingsFormBuilder {
	contentW := max(1, width)
	labelW := formLabelWidth(contentW)
	return settingsFormBuilder{
		s:          s,
		width:      width,
		contentW:   contentW,
		labelW:     labelW,
		hintIndent: "",
		chrome:     chrome,
		ind:        lipgloss.NewStyle().Background(chrome.baseBg).Width(width),
		blank:      lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""),
		body:       settingsSectionBody{anchors: make(map[settingsField]int)},
	}
}

func (b *settingsFormBuilder) addLine(line string) {
	b.body.lines = append(b.body.lines, b.ind.Render(line))
}

func (b *settingsFormBuilder) addHint(text string) {
	for _, line := range renderFormHintLines(b.hintIndent+text, b.contentW, b.chrome) {
		b.addLine(line)
	}
}

func (b *settingsFormBuilder) addBlank() {
	b.body.lines = append(b.body.lines, b.blank)
}

func (b *settingsFormBuilder) markAnchor(field settingsField) {
	b.body.anchors[field] = len(b.body.lines)
}

func (b *settingsFormBuilder) addBackLink() {
	b.markAnchor(sfBackToSections)
	b.addLine(b.s.renderBackLinkRow(b.s.focusedField == sfBackToSections, b.width, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addGroup(label string) {
	b.addLine(renderFormGroupTitle(label, b.contentW, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addControl(label string, field settingsField, control string) {
	b.markAnchor(field)
	b.addLine(renderFormRow(label, b.s.focusedField == field, control, b.contentW, b.labelW, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addToggle(label string, on bool, field settingsField) {
	focused := b.s.focusedField == field
	b.markAnchor(field)
	b.addLine(b.s.renderToggle(label, on, focused, b.width, b.chrome))
	if hint := b.s.fieldHint(field); hint != "" {
		b.addHint(hint)
	}
	b.addBlank()
}

func (b *settingsFormBuilder) addInput(label string, input textinput.Model, field settingsField) {
	focused := b.s.focusedField == field
	rowFieldW := max(1, b.contentW-b.labelW)
	controlW := max(1, rowFieldW-4)
	fieldW := min(controlW, b.s.inputWidth(field, controlW))
	control := renderInsetControl(renderTextInput(input, fieldW, focused, false, b.chrome), rowFieldW, 2, b.chrome)
	b.markAnchor(field)
	b.addLine(renderFormRow(label, focused, control, b.contentW, b.labelW, b.chrome))
	if hint := b.s.fieldHint(field); hint != "" {
		b.addHint(hint)
	}
	b.addBlank()
}

func (b *settingsFormBuilder) addBareInput(input textinput.Model, field settingsField) {
	focused := b.s.focusedField == field
	inputW := max(1, b.contentW-4)
	b.markAnchor(field)
	b.addLine(renderInsetControl(renderTextInput(input, inputW, focused, false, b.chrome), b.contentW, 2, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addValue(label, value string, focused bool) {
	if value == "" {
		return
	}
	b.addLine(b.s.renderValueRow(label, value, focused, b.width, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addAction(label, hint string, field settingsField) {
	focused := b.s.focusedField == field
	b.markAnchor(field)
	b.addLine(b.s.renderActionRow(label, hint, focused, b.width, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addThemeSelector() {
	b.markAnchor(sfTheme)
	b.addLine(b.s.renderThemeSelector(b.width, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addDensitySelector() {
	b.markAnchor(sfDisplayDensity)
	b.addLine(b.s.renderDensitySelector(b.width, b.chrome))
	if hint := b.s.fieldHint(sfDisplayDensity); hint != "" {
		b.addHint(hint)
	}
	b.addBlank()
}

func (b *settingsFormBuilder) addDateFormatSelector() {
	b.markAnchor(sfDateFormat)
	b.addLine(b.s.renderDateFormatSelector(b.width, b.chrome))
	if hint := b.s.fieldHint(sfDateFormat); hint != "" {
		b.addHint(hint)
	}
	b.addBlank()
}

func (b *settingsFormBuilder) addAISection() {
	b.addGroup("Provider credentials")
	b.addControl("Provider", sfProvider, renderSettingsPicker(min(max(12, b.contentW-b.labelW), 18), aiProviderLabels[b.s.providerIdx], b.s.focusedField == sfProvider, b.chrome))

	switch b.s.providerIdx {
	case 1:
		b.addBareInput(b.s.openaiInput, sfAPIKey)
		b.addInput("Model", b.s.openaiModelInput, sfOpenAIModel)
	case 2:
		b.addBareInput(b.s.claudeInput, sfAPIKey)
		b.addInput("Model", b.s.claudeModelInput, sfClaudeModel)
	case 3:
		b.addBareInput(b.s.geminiInput, sfAPIKey)
		b.addInput("Model", b.s.geminiModelInput, sfGeminiModel)
	case 4:
		b.addInput("Ollama URL", b.s.ollamaURLInput, sfOllamaURL)
		b.addInput("Model", b.s.ollamaModelInput, sfOllamaModel)
	}

	b.addAITestConnection()
	b.addGroup("Summary output")
	b.addBareInput(b.s.savePathInput, sfSavePath)

	toggleLabel := "OFF"
	if b.s.markReadOnSummarize {
		toggleLabel = "ON"
	}
	b.addControl("Mark read on summarize", sfMarkReadOnSummarize, b.s.renderBadge(toggleLabel, b.s.focusedField == sfMarkReadOnSummarize, b.chrome))
}

func (b *settingsFormBuilder) addAITestConnection() {
	focused := b.s.focusedField == sfTestAIConnection
	b.markAnchor(sfTestAIConnection)
	badge := b.s.renderAITestBadge(focused, b.chrome)
	status := b.s.renderAIConnectionStatus(max(1, b.contentW-b.labelW-lipgloss.Width(badge)-1), focused, b.chrome)
	gap := lipgloss.NewStyle().Background(b.chrome.baseBg).Render(" ")
	b.addLine(renderFormControlRow(badge+gap+status, b.contentW, b.chrome, focused))
	b.addBlank()
}

func (b *settingsFormBuilder) addAdvancedSection() {
	b.addGroup("Logs")
	b.addAction("View Logs", "review errors and status messages", sfViewLogs)
}

type aiConnectionState int

const (
	aiConnectionIdle aiConnectionState = iota
	aiConnectionPending
	aiConnectionSuccess
	aiConnectionError
)

func (s Settings) aiConnectionState() aiConnectionState {
	switch {
	case s.aiValidatePending:
		return aiConnectionPending
	case s.aiTestError != "":
		return aiConnectionError
	case s.aiTestOk:
		return aiConnectionSuccess
	default:
		return aiConnectionIdle
	}
}

func (s Settings) aiConnectionStatusLabel() string {
	switch s.aiConnectionState() {
	case aiConnectionPending:
		return "checking"
	case aiConnectionError:
		return "failed"
	case aiConnectionSuccess:
		return "ok"
	default:
		return "ready"
	}
}

func (s Settings) inputWidth(field settingsField, maxWidth int) int {
	switch field {
	case sfFeedMaxBody, sfReadingWidth:
		return min(maxWidth, 12)
	case sfRetroBg, sfRetroFg, sfRetroAccent:
		return min(maxWidth, 44)
	case sfBrowser, sfSavePath:
		return min(maxWidth, 36)
	case sfOllamaURL, sfOllamaModel:
		return min(maxWidth, 44)
	default:
		return min(maxWidth, 32)
	}
}

// Wide enough for "Feed max size (MiB)" with sectionLabel horizontal padding inside Width().
const labelColW = 22

func (s Settings) viewHints(width int, chrome managerChrome) string {
	if s.focusedPane == settingsPaneSidebar {
		return renderManagerActions(width, chrome,
			"↑/↓", "section",
			"→", "edit",
			"esc", "save & close",
			"q", "discard",
		)
	}
	if s.isPickerField() {
		return renderManagerActions(width, chrome,
			"←/→", "change",
			"↑/↓", "field",
			"tab", "next",
			"esc", "categories",
		)
	}
	if s.activeSection == ssUpdates && s.focusedField == sfUpdateManualCommand {
		return renderManagerActions(width, chrome,
			"enter", "copy",
			"c", "copy",
			"tab", "next",
			"esc", "categories",
		)
	}
	return renderManagerActions(width, chrome,
		"←", "sections",
		"↑/↓", "field",
		"tab", "next",
		"esc", "categories",
	)
}

func (s Settings) renderSectionNavRow(width int, label, subtitle string, selected, paneFocused bool, chrome managerChrome) string {
	bg, fg, subFg := chrome.baseBg, chrome.text, chrome.muted
	bold := false
	if selected {
		bg = chrome.surfaceBg
		fg = chrome.highlight
		subFg = chrome.highlight
		bold = true
		if paneFocused {
			bg = chrome.highlight
			fg = chrome.highlightFg
			subFg = chrome.highlightFg
		}
	}

	innerW := max(1, width-2)
	var row string
	if subtitle == "" {
		row = padRight(truncate(label, innerW), innerW)
	} else {
		subW := lipgloss.Width(subtitle)
		labelW := max(1, innerW-subW-1)
		left := lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(bold).Width(labelW).Render(truncate(label, labelW))
		spacer := lipgloss.NewStyle().Background(bg).Width(1).Render("")
		right := lipgloss.NewStyle().Background(bg).Foreground(subFg).Render(subtitle)
		row = left + spacer + right
	}
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Bold(bold).
		Padding(0, 1).
		Render(row)
}

func (s Settings) renderFieldLabel(label string, focused bool, _ int, chrome managerChrome) string {
	style := chrome.sectionLabel
	if focused {
		style = chrome.sectionLabelActive
	}
	return style.Width(labelColW).Render(padRight(label, labelColW))
}

func (s Settings) renderValueRow(label, value string, focused bool, width int, chrome managerChrome) string {
	labelW := formLabelWidth(width)
	valueW := max(1, width-labelW)
	trimmed := strings.TrimSpace(value)
	lines := wrapShellCommand(trimmed, valueW)
	valueStyle := chrome.body.Foreground(chrome.text)
	if len(lines) == 1 {
		return renderFormRow(label, focused, valueStyle.Width(valueW).Render(lines[0]), width, labelW, chrome)
	}
	padCont := lipgloss.NewStyle().Background(chrome.baseBg).Width(labelW).Render("")
	var b strings.Builder
	for i, line := range lines {
		if i == 0 {
			b.WriteString(renderFormRow(label, focused, valueStyle.Render(line), width, labelW, chrome))
			continue
		}
		b.WriteString("\n")
		b.WriteString(padCont)
		b.WriteString(valueStyle.Render(line))
	}
	return b.String()
}

// manualInstallCommandLines renders the "Copy Command" label, COPY badge, and solid black bordered code block (one terminal line each).
// rowContentW is the usable width for this row; the box is drawn 5 cells narrower than that so it does not span the full detail width.
func (s Settings) manualInstallCommandLines(rowContentW int, command string, focused bool, chrome managerChrome) []string {
	labelRow := s.renderFieldLabel("Copy Command", focused, rowContentW, chrome) + s.renderBadge("COPY", focused, chrome)
	borderFg := lipgloss.Color("#2a2a2a")
	if focused {
		borderFg = chrome.highlight
	}
	codeBg := lipgloss.Color("#000000")
	kwFg := lipgloss.Color("#a6e3a1")
	flagFg := lipgloss.Color("#fab387")
	urlFg := lipgloss.Color("#f9e2af")
	pipeFg := lipgloss.Color("#89dceb")
	defFg := lipgloss.Color("#e8e8e8")
	boxW := max(1, rowContentW-5)
	// Border (2) + horizontal padding (2+2).
	innerTextW := max(1, boxW-6)
	wrapped := wrapShellCommand(command, innerTextW)
	styledLines := make([]string, 0, len(wrapped))
	for _, line := range wrapped {
		lineStyled := styleShellCommandLine(line, codeBg, kwFg, flagFg, urlFg, pipeFg, defFg)
		styledLines = append(styledLines, padStyledCodeLine(lineStyled, codeBg, innerTextW))
	}
	inner := lipgloss.JoinVertical(lipgloss.Left, styledLines...)
	box := lipgloss.NewStyle().
		Background(codeBg).
		Border(lipPaneBorder(chrome.plainUI)).
		BorderForeground(borderFg).
		PaddingTop(1).PaddingBottom(1).PaddingLeft(2).PaddingRight(2).
		Width(boxW).
		Align(lipgloss.Left).
		Render(inner)
	lines := []string{labelRow}
	lines = append(lines, strings.Split(box, "\n")...)
	return lines
}

func styleShellCommandLine(line string, bg, kwFg, flagFg, urlFg, pipeFg, defFg lipgloss.Color) string {
	parts := strings.Fields(line)
	if len(parts) == 0 {
		return lipgloss.NewStyle().Background(bg).Foreground(defFg).Render("")
	}
	var b strings.Builder
	for i, tok := range parts {
		if i > 0 {
			b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(defFg).Render(" "))
		}
		fg := shellTokenForeground(tok, kwFg, flagFg, urlFg, pipeFg, defFg)
		b.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fg).Render(tok))
	}
	return b.String()
}

func shellTokenForeground(tok string, kwFg, flagFg, urlFg, pipeFg, defFg lipgloss.Color) lipgloss.Color {
	if tok == "|" {
		return pipeFg
	}
	trim := strings.Trim(tok, `"'`)
	if strings.Contains(trim, "://") || strings.HasPrefix(strings.ToLower(trim), "http") {
		return urlFg
	}
	if strings.HasPrefix(tok, "-") {
		return flagFg
	}
	low := strings.ToLower(trim)
	if shellCommandKeywords[low] {
		return kwFg
	}
	if strings.Contains(tok, "/") || strings.HasPrefix(tok, "~/") {
		return defFg
	}
	if isNumericShellToken(trim) {
		return flagFg
	}
	return defFg
}

func isNumericShellToken(s string) bool {
	if s == "" {
		return false
	}
	for _, r := range s {
		if r < '0' || r > '9' {
			return false
		}
	}
	return true
}

func padStyledCodeLine(styled string, bg lipgloss.Color, targetCells int) string {
	w := lipgloss.Width(styled)
	if w >= targetCells {
		return styled
	}
	pad := targetCells - w
	return styled + lipgloss.NewStyle().Background(bg).Render(strings.Repeat(" ", pad))
}

func wrapShellCommand(s string, maxW int) []string {
	s = strings.TrimSpace(s)
	if maxW < 1 {
		maxW = 1
	}
	if s == "" {
		return []string{""}
	}
	words := strings.Fields(s)
	var out []string
	var cur strings.Builder
	flush := func() {
		if cur.Len() > 0 {
			out = append(out, cur.String())
			cur.Reset()
		}
	}
	for _, w := range words {
		if lipgloss.Width(w) > maxW {
			flush()
			out = append(out, hardWrapString(w, maxW)...)
			continue
		}
		try := w
		if cur.Len() > 0 {
			try = cur.String() + " " + w
		}
		if lipgloss.Width(try) <= maxW {
			if cur.Len() > 0 {
				cur.WriteString(" ")
			}
			cur.WriteString(w)
		} else {
			flush()
			cur.WriteString(w)
		}
	}
	flush()
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func hardWrapString(s string, maxW int) []string {
	if maxW < 1 {
		maxW = 1
	}
	var out []string
	runes := []rune(s)
	for len(runes) > 0 {
		var b strings.Builder
		for len(runes) > 0 {
			nextR := runes[0]
			cand := b.String() + string(nextR)
			if b.Len() > 0 && lipgloss.Width(cand) > maxW {
				break
			}
			if b.Len() == 0 && lipgloss.Width(string(nextR)) > maxW {
				b.WriteRune(nextR)
				runes = runes[1:]
				break
			}
			b.WriteRune(nextR)
			runes = runes[1:]
		}
		if b.Len() > 0 {
			out = append(out, b.String())
		}
	}
	if len(out) == 0 {
		return []string{""}
	}
	return out
}

func (s Settings) renderBackLinkRow(focused bool, width int, chrome managerChrome) string {
	label := "← Back to sections"
	style := chrome.body.Foreground(chrome.muted)
	if focused {
		style = chrome.sectionLabelActive
	}
	return style.Width(max(1, width)).Render(label)
}

// prependBackLink inserts the back-link row and a blank separator at the top of an
// already-rendered section body, bumping existing anchors by the insertion count.
func (s Settings) prependBackLink(body settingsSectionBody, width int, chrome managerChrome) settingsSectionBody {
	ind := lipgloss.NewStyle().Background(chrome.baseBg).Width(width)
	blank := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	prefix := []string{
		ind.Render(s.renderBackLinkRow(s.focusedField == sfBackToSections, width, chrome)),
		blank,
	}
	shift := len(prefix)
	out := settingsSectionBody{
		lines:   append(prefix, body.lines...),
		anchors: make(map[settingsField]int, len(body.anchors)+1),
	}
	out.anchors[sfBackToSections] = 0
	for f, i := range body.anchors {
		out.anchors[f] = i + shift
	}
	return out
}

func (s Settings) renderActionRow(label, hint string, focused bool, width int, chrome managerChrome) string {
	labelW := formLabelWidth(width)
	badge := s.renderBadge("ENTER", focused, chrome)
	hintText := ""
	if hint != "" {
		hintText = "  " + hint
	}
	controlW := max(1, width-2-labelW)
	control := badge + renderFormInlineStatus(hintText, max(1, controlW-lipgloss.Width(badge)), chrome)
	return renderFormRow(label, focused, control, width, labelW, chrome)
}

func (s Settings) renderToggle(label string, on bool, focused bool, width int, chrome managerChrome) string {
	val := "OFF"
	if on {
		val = "ON"
	}
	badge := s.renderBadge(val, focused, chrome)
	control := badge + s.renderToggleHint(on, "enabled", "disabled", focused, chrome)
	return renderFormRow(label, focused, control, width, formLabelWidth(width), chrome)
}

func (u settingsUpdateState) statusLabel() string {
	switch u.state {
	case updateStateChecking:
		return "checking for Tidemail updates..."
	case updateStateAvailable:
		if u.dismissed {
			return "Tidemail update dismissed"
		}
		return "Tidemail update available"
	case updateStateDownloading:
		return "downloading Tidemail update..."
	case updateStateInstalling:
		return "installing Tidemail update..."
	case updateStateInstalled:
		if u.installedVersion != "" {
			return "Tidemail updated to " + u.installedVersion
		}
		return "Tidemail update installed"
	case updateStateNeedsElevation:
		return "admin permission required"
	case updateStateError:
		if u.err != "" {
			return u.err
		}
		return "update failed"
	default:
		if u.lastChecked.IsZero() {
			return "not checked yet"
		}
		return "up to date"
	}
}

func (s Settings) renderBadge(text string, focused bool, chrome managerChrome) string {
	return renderFormBadge(text, focused, chrome)
}

func (s Settings) aiTestBadgeColors(focused bool, chrome managerChrome) (lipgloss.Color, lipgloss.Color, bool) {
	switch s.aiConnectionState() {
	case aiConnectionPending:
		return chrome.pendingFg, contrastFg(chrome.pendingFg), true
	case aiConnectionSuccess:
		return chrome.successFg, contrastFg(chrome.successFg), true
	case aiConnectionError:
		return chrome.errorFg, contrastFg(chrome.errorFg), true
	default:
		if focused {
			return chrome.highlight, chrome.highlightFg, true
		}
		return chrome.accent, chrome.accentFg, false
	}
}

func (s Settings) renderAITestBadge(focused bool, chrome managerChrome) string {
	text := "TEST"
	bg, fg, bold := s.aiTestBadgeColors(focused, chrome)
	return renderFormBadgeStyled(text, max(7, lipgloss.Width(text)+2), bg, fg, bold)
}

func (s Settings) renderAIConnectionStatus(width int, focused bool, chrome managerChrome) string {
	state := s.aiConnectionState()
	if width <= 0 {
		return ""
	}

	glyph := aiConnectionStatusGlyph(chrome.plainUI, state)
	indicatorFg := chrome.muted
	labelFg := chrome.muted
	switch state {
	case aiConnectionPending:
		indicatorFg = chrome.pendingFg
	case aiConnectionSuccess:
		indicatorFg = chrome.successFg
		labelFg = chrome.text
	case aiConnectionError:
		indicatorFg = chrome.errorFg
		labelFg = chrome.errorFg
	}
	if focused && state == aiConnectionIdle {
		indicatorFg = chrome.text
		labelFg = chrome.text
	}

	indicator := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(indicatorFg).
		Bold(state != aiConnectionIdle).
		Render(glyph)
	if width <= lipgloss.Width(indicator) {
		return indicator
	}

	labelWidth := max(1, width-lipgloss.Width(indicator)-1)
	label := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(labelFg).
		Render(truncate(s.aiConnectionStatusLabel(), labelWidth))
	gap := lipgloss.NewStyle().Background(chrome.baseBg).Render(" ")
	return indicator + gap + label
}

// renderToggleHint appends a small hint showing the current value label.
func (s Settings) renderToggleHint(active bool, trueLabel, falseLabel string, focused bool, chrome managerChrome) string {
	val := falseLabel
	if active {
		val = trueLabel
	}
	style := chrome.keyLabel
	if !focused {
		style = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	}
	return style.Render("  " + val)
}

func (s Settings) renderProviderSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfProvider
	providerName := aiProviderLabels[s.providerIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW)
	return renderFormRow("Provider", focused, renderSettingsPicker(pickerW, providerName, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderDateFormatSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfDateFormat
	name := dateFormatLabels[s.dateFormatIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW)
	return renderFormRow("Dates", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderDensitySelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfDisplayDensity
	name := layoutDensityLabels[s.layoutDensityIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW)
	return renderFormRow("Layout density", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderThemeSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfTheme
	name := BuiltinThemes[s.themeIdx].Name
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW)
	return renderFormRow("Theme", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func renderSettingsPicker(width int, value string, focused bool, chrome managerChrome) string {
	// Chrome cells: 2 (left chevron) + 2 (right chevron) + 2 (horizontal padding) = 6.
	maxTextW := max(1, width-6)
	bg := chrome.fieldBg
	fg := chrome.text
	accentFg := chrome.muted
	if focused {
		bg = chrome.highlight
		fg = chrome.highlightFg
		accentFg = chrome.highlightFg
	}
	value = truncate(value, maxTextW)
	text := lipgloss.NewStyle().Background(bg).Foreground(fg)
	accent := lipgloss.NewStyle().Background(bg).Foreground(accentFg).Bold(true)
	line := accent.Render(chrome.pickerChevronLeft()) + text.Render(value) + accent.Render(chrome.pickerChevronRight())
	return lipgloss.NewStyle().Background(bg).Padding(0, 1).MaxWidth(width).Render(line)
}

func (s Settings) fieldHint(field settingsField) string {
	switch field {
	case sfBrowser:
		return "leave blank to use the system default browser"
	case sfFeedMaxBody:
		return "larger bodies need more memory; default is 10 MiB"
	case sfReadingWidth:
		return "max columns for article text; 0 = no limit (e.g. 80, 100)"
	case sfTestAIConnection:
		if s.aiValidatePending {
			return "Contacting provider..."
		}
		if s.aiTestError != "" {
			return s.aiTestError
		}
		if s.aiTestOk {
			return "Connection OK."
		}
		return "Checks the current draft with the live provider."

	case sfAPIKey:
		if msg, ok := s.selectedAIKeyValidation(); msg != "" {
			if ok {
				return msg + "."
			}
			return msg
		}
		return "Only the active provider key is used."
	case sfOllamaURL:
		return "Local Ollama endpoint."
	case sfOpenAIModel:
		return "type the OpenAI model name (e.g. gpt-4o-mini)"
	case sfClaudeModel:
		return "type the Claude model name (e.g. claude-sonnet-4)"
	case sfGeminiModel:
		return "type the Gemini model name (e.g. gemini-2.0-flash)"
	case sfOllamaModel:
		return "type the Ollama model name (e.g. llama3.2)"
	case sfSavePath:
		return "Directory for exported markdown summaries."
	case sfRetroBg, sfRetroFg, sfRetroAccent:
		return "leave blank to use the built-in palette for this theme"
	case sfDisplayDensity:
		return "comfortable adds vertical spacing in lists; compact fits more rows on small terminals"
	case sfActionableLinks:
		return "enable ctrl+n / ctrl+p to select links in article content; o opens selected link"
	case sfFilterLinks:
		return "strip bare URLs from the article body text"
	case sfFocusLine:
		return "highlight the current readable line in the content pane"
	case sfUpdateManualCommand:
		return "enter or c copies the command"
	default:
		return ""
	}
}

func (s Settings) renderAboutSection(width int, chrome managerChrome) settingsSectionBody {
	ind := lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Width(width)
	blank := lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Width(width).Render("")
	bodyW := max(1, width)
	lines := []string{}
	addBlock := func(block string) int {
		start := len(lines)
		lines = append(lines, strings.Split(block, "\n")...)
		return start
	}
	addBlock(ind.Render(s.renderAboutHero(bodyW, chrome)))
	lines = append(lines, blank)
	// Backronym below hero
	tideLine := lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Foreground(chrome.muted).Italic(true).Width(bodyW).Align(lipgloss.Center).Render("terminal information delivery engine")
	lines = append(lines, tideLine, blank)
	// Version info
	verLine := lipgloss.NewStyle().Background(lipgloss.Color("#000000")).Foreground(chrome.muted).Width(bodyW).Align(lipgloss.Center).Render(s.update.currentVersion)
	lines = append(lines, verLine, blank)
	addBlock(ind.Render(s.renderAboutClosingNote(bodyW, chrome)))
	lines = append(lines, blank)
	linksStart := addBlock(ind.Render(s.renderAboutLinks(bodyW, chrome)))
	// Fill remaining space to pane bottom
	if s.detailHeight > len(lines) {
		for i := len(lines); i < s.detailHeight; i++ {
			lines = append(lines, blank)
		}
	}
	return settingsSectionBody{
		lines: lines,
		anchors: map[settingsField]int{
			sfAboutRepo:   linksStart,
			sfAboutIssues: linksStart,
		},
	}
}

func (s Settings) renderAboutHero(width int, chrome managerChrome) string {
	panelW := max(1, width-4)
	contentW := max(1, panelW-2)

	titleTxt := "TIDEMAIL"
	tagline := "your mail, your rules"

	// Centered title on row 0
	titleCentered := aboutCenterText(titleTxt, contentW)
	// Centered tagline on row 1
	taglineCentered := aboutCenterText(tagline, contentW)

	// Signal bar on row 1 — smooth ping-pong, decelerates at edges
	angle := float64(s.aboutGradientFrame) * 0.065
	norm := math.Asin(math.Sin(angle)) / (math.Pi / 2) // -1..1
	signalPos := int(math.Round((norm + 1) / 2 * float64(contentW-1)))

	lines := []string{
		s.renderAboutHeroTextLine(titleCentered, contentW, 0, true),
		renderSignalBar(contentW, int(signalPos), float64(s.aboutGradientFrame)),
		s.renderAboutHeroTextLine(taglineCentered, contentW, 1, false),
		s.renderAboutHeroTextLine("", contentW, 3, false),
	}

	panelBg := lipgloss.Color("#000000")
	panel := lipgloss.NewStyle().
		Width(panelW).
		Background(panelBg).
		Border(lipPaneBorder(chrome.plainUI)).
		BorderForeground(lipgloss.Color("#143a4a")).
		BorderBackground(panelBg).
		Padding(0, 1).
		Render(strings.Join(lines, "\n"))

	return lipgloss.NewStyle().Width(width).Background(lipgloss.Color("#000000")).Align(lipgloss.Center).Render(panel)
}

// renderSignalBar renders a thin horizontal line across the full row width.
// The line is rainbow-colored with a bright white point at the eye position,
// fading smoothly in both directions through the spectrum.
func renderSignalBar(w, head int, frame float64) string {
	spread := math.Max(3, float64(w)*0.60)
	bg := lipgloss.Color("#000000")
	flicker := 0.90 + 0.10*math.Sin(frame*0.11)

	var b strings.Builder
	for i := 0; i < w; i++ {
		dist := math.Abs(float64(i - head))
		intensity := math.Exp(-(dist*dist)/(spread*spread)) * flicker
		intensity = clamp01(intensity)
		fg := cylonGlowColor(intensity)
		cell := lipgloss.NewStyle().Background(bg).Foreground(fg).Render("─")
		b.WriteString(cell)
	}
	return b.String()
}

// cylonGlowColor maps an intensity (0..1) to a color along a rainbow spectrum:
// purple → blue → cyan → green → yellow → orange → red → white.
func cylonGlowColor(t float64) lipgloss.Color {
	t = clamp01(t)
	type stop struct{ pos, r, g, b float64 }
	ramp := []stop{
		{0.00, 0x40, 0x00, 0x60}, // deep purple
		{0.10, 0x20, 0x20, 0xcc}, // blue
		{0.24, 0x00, 0x80, 0xcc}, // cyan
		{0.38, 0x00, 0xaa, 0x44}, // green
		{0.52, 0x88, 0xcc, 0x00}, // yellow-green
		{0.66, 0xee, 0xaa, 0x00}, // yellow
		{0.80, 0xff, 0x55, 0x00}, // orange
		{0.92, 0xff, 0x22, 0x22}, // red
		{1.00, 0xff, 0xff, 0xff}, // white
	}
	for i := 1; i < len(ramp); i++ {
		if t <= ramp[i].pos {
			prev := ramp[i-1]
			next := ramp[i]
			frac := (t - prev.pos) / (next.pos - prev.pos)
			r := prev.r + (next.r-prev.r)*frac
			g := prev.g + (next.g-prev.g)*frac
			b := prev.b + (next.b-prev.b)*frac
			return lipgloss.Color(fmt.Sprintf("#%02x%02x%02x",
				uint8(math.Round(r)),
				uint8(math.Round(g)),
				uint8(math.Round(b))))
		}
	}
	return "#ffffff"
}

func (s Settings) renderAboutLinks(width int, chrome managerChrome) string {
	aboutBg := lipgloss.Color("#000000")
	renderBtn := func(label string, focused bool) string {
		bg := aboutBg
		fg := chrome.accent
		if focused {
			bg = chrome.accent
			fg = readableText(chrome.accent, chrome.accent, 4.5)
		}
		return lipgloss.NewStyle().
			Background(bg).
			Foreground(fg).
			Bold(focused).
			Padding(0, 2).
			Render(" " + label + " ")
	}
	repoBtn := renderBtn("Repository", s.focusedField == sfAboutRepo)
	issuesBtn := renderBtn("Issues", s.focusedField == sfAboutIssues)
	gap := lipgloss.NewStyle().Background(aboutBg).Render("  ")
	line := lipgloss.JoinHorizontal(lipgloss.Center, repoBtn, gap, issuesBtn)
	return lipgloss.NewStyle().Background(aboutBg).Width(width).Align(lipgloss.Center).Render(line)
}

func aboutCenterText(s string, width int) string {
	runes := []rune(s)
	if len(runes) >= width {
		return string(runes[:width])
	}
	left := (width - len(runes)) / 2
	right := width - len(runes) - left
	return strings.Repeat(" ", left) + s + strings.Repeat(" ", right)
}

func (s Settings) renderAboutClosingNote(width int, chrome managerChrome) string {
	aboutBg := lipgloss.Color("#000000")
	signoff := lipgloss.NewStyle().
		Background(aboutBg).
		Foreground(chrome.muted).
		Italic(true).
		Width(max(1, width)).
		Align(lipgloss.Center).
		Render("Thanks for taking a look -allie")

	heart := lipgloss.NewStyle().
		Background(aboutBg).
		Foreground(lipgloss.Color("#e64553")).
		Width(max(1, width)).
		Align(lipgloss.Center).
		Render("❤")

	heartBlock := lipgloss.NewStyle().
		Background(aboutBg).
		Render(heart)

	return lipgloss.NewStyle().
		Background(aboutBg).
		PaddingTop(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, signoff, heartBlock))
}

func (s Settings) renderAboutHeroTextLine(text string, width, row int, bold bool) string {
	runes := []rune(text)
	if len(runes) > width {
		runes = []rune(truncate(text, width))
	}

	var b strings.Builder
	for i := 0; i < width; i++ {
		ch := ' '
		if i < len(runes) {
			ch = runes[i]
		}
		bg := aboutHeroBackground(s.aboutGradientFrame, row, i, width)
		fg := aboutHeroTextForeground(bg, row, ch)
		b.WriteString(renderAboutHeroCell(ch, bg, fg, bold && ch != ' '))
	}
	return b.String()
}

func renderAboutHeroCell(ch rune, bg, fg lipgloss.Color, bold bool) string {
	return lipgloss.NewStyle().
		Background(bg).
		Foreground(fg).
		Bold(bold).
		Render(string(ch))
}

func aboutHeroBackground(frame, row, col, width int) lipgloss.Color {
	// CRT terminal: near-black with subtle scanline alternating
	// and a very faint vertical scan variation.
	base := lipgloss.Color("#0a0e14")
	scanAlt := lipgloss.Color("#0b0f16")

	// Horizontal scanline: alternate slightly every row
	if row%2 == 0 {
		return base
	}
	return scanAlt
}

func aboutHeroTextForeground(bg lipgloss.Color, row int, ch rune) lipgloss.Color {
	// Title row (row 0): bright accent for "TIDEMAIL"
	if row == 0 && ch != ' ' {
		return lipgloss.Color("#84c5d4")
	}
	// Tagline row (row 1): dimmed text
	if row == 1 && ch != ' ' {
		return readableText(lipgloss.Color("#84c5d4"), bg, 3.5)
	}
	// Signal bar (row 2): Cylon red-white glow
	if row == 2 {
		return readableText(lipgloss.Color("#ff4444"), bg, 4.5)
	}
	// Default: dim text for spaces etc.
	return readableText(lipgloss.Color("#b3b1ad"), bg, 4.5)
}

func clamp01(v float64) float64 {
	return math.Max(0, math.Min(1, v))
}
