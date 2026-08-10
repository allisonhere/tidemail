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

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/update"
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
	sfShowSender
	sfThreadedConversations
	sfDefaultUnreadOnly
	sfUnreadFirst
	sfStarredFirst
	sfActionableLinks
	sfFilterLinks
	sfReadingWidth
	sfDisplayDensity
	sfPaneCorners
	sfBrowser
	sfFeedMaxBody
	sfUpdateCheckOnStartup
	sfUpdateCheckNow
	sfUpdateInstallNow
	sfUpdateDismissVersion
	sfUpdateRestartNow
	sfAboutHeart
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
	sfNotifications
	sfComposeVim
	sfSendDelay
	// sfBackToSections is the first focusable target in the detail pane.
	// Activating it restores focus to the sidebar so users never auto-land on a text input.
	sfBackToSections
)

type settingsSection int

const (
	ssDisplay settingsSection = iota
	ssEditor
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
	settingsAboutRevealStart = 8
	settingsAboutRevealEnd   = 28
	settingsAboutRevealTotal = 40
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
	paneCornersLabels     = []string{"Square", "Round"}
	dateFormatLabels      = []string{"Relative", "Absolute", "None"}
	aiProviderLabels      = []string{"none", "OpenAI", "Claude", "Gemini", "Ollama"}
	aiProviderIDs         = []string{"", "openai", "claude", "gemini", "ollama"}
	openaiModelLabels     = []string{"gpt-4o-mini", "gpt-4o", "gpt-4.1", "o3-mini", "o1"}
	claudeModelLabels     = []string{"claude-sonnet-4", "claude-haiku", "claude-opus-4"}
	geminiModelLabels     = []string{"gemini-2.0-flash", "gemini-1.5-pro", "gemini-1.5-flash"}
	ollamaModelLabels     = []string{"llama3.2", "llama3.1", "mistral", "gemma3", "phi4"}
	settingsSectionLabels = [settingsSectionCount]string{
		"DISPLAY",
		"EDITOR",
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

func openaiModelIndex(s string) int {
	for i, label := range openaiModelLabels {
		if strings.EqualFold(label, s) {
			return i
		}
	}
	return 0
}

func claudeModelIndex(s string) int {
	for i, label := range claudeModelLabels {
		if strings.EqualFold(label, s) {
			return i
		}
	}
	return 0
}

func geminiModelIndex(s string) int {
	for i, label := range geminiModelLabels {
		if strings.EqualFold(label, s) {
			return i
		}
	}
	return 0
}

func ollamaModelIndex(s string) int {
	for i, label := range ollamaModelLabels {
		if strings.EqualFold(label, s) {
			return i
		}
	}
	return 0
}

// ── Settings ──────────────────────────────────────────────────────────────────

type Settings struct {
	// Display
	icons                 bool
	dateFormatIdx         int // 0=Relative, 1=Absolute, 2=None
	markReadOnOpen        bool
	markReadOnFocus       bool
	focusLine             bool
	showSender            bool
	threadedConversations bool
	defaultUnreadOnly     bool
	unreadFirst           bool
	starredFirst          bool
	actionableLinks       bool
	filterLinks           bool
	confirmQuit           bool
	showHeaders           bool
	notifications         bool
	composeVim            bool
	layoutDensityIdx      int // 0 = comfortable, 1 = compact
	paneCornersIdx        int // 0 = square, 1 = round
	readingWidthInput     textinput.Model
	sendDelayInput        textinput.Model
	browserInput          textinput.Model
	feedMaxBodyInput      textinput.Model
	updateCheckOnStartup  bool
	update                settingsUpdateState
	action                settingsAction

	// AI
	providerIdx         int
	openaiModelIdx      int
	claudeModelIdx      int
	geminiModelIdx      int
	ollamaModelIdx      int
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
	aboutRevealFrame   int
	aboutRevealActive  bool
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
	paneCornersIdx := 0
	if config.NormalizePaneCorners(cfg.Display.PaneCorners) == "round" {
		paneCornersIdx = 1
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
		icons:                 cfg.Display.Icons,
		themeName:             cfg.Theme,
		themeIdx:              themeIdx,
		retroBgInput:          mkInput(retroTweak.Bg, "optional #rrggbb", false),
		retroFgInput:          mkInput(retroTweak.Fg, "optional #rrggbb", false),
		retroAccentInput:      mkInput(retroTweak.Accent, "optional #rrggbb", false),
		dateFormatIdx:         dateFormatIndex(cfg.Display.DateFormat),
		markReadOnOpen:        cfg.Display.MarkReadOnOpen,
		markReadOnFocus:       cfg.Display.MarkReadOnFocus,
		focusLine:             cfg.Display.FocusLine,
		showSender:            cfg.Display.ShowSender,
		threadedConversations: cfg.Display.ThreadedConversations,
		defaultUnreadOnly:     cfg.Display.DefaultUnreadOnly,
		unreadFirst:           cfg.Display.UnreadFirst,
		starredFirst:          cfg.Display.StarredFirst,
		actionableLinks:       cfg.Display.ActionableLinks,
		filterLinks:           cfg.Display.FilterLinks,
		confirmQuit:           cfg.Display.ConfirmQuit,
		showHeaders:           cfg.Display.ShowHeaders,
		notifications:         cfg.Display.Notifications,
		composeVim:            cfg.Display.ComposeVim,
		layoutDensityIdx:      layoutIdx,
		paneCornersIdx:        paneCornersIdx,
		readingWidthInput:     mkInput(strconv.Itoa(cfg.Display.ReadingWidth), "0 (no limit)", false),
		sendDelayInput:        mkInput(strconv.Itoa(cfg.Display.SendDelaySeconds), "5 (0 = immediate)", false),
		browserInput:          mkInput(cfg.Display.Browser, "xdg-open", false),
		feedMaxBodyInput:      mkInput(strconv.Itoa(cfg.Feed.MaxBodyMiB), "10", false),
		updateCheckOnStartup:  cfg.Updates.CheckOnStartup,
		update:                updateState,
		providerIdx:           providerIndex(cfg.AI.Provider),
		openaiModelIdx:        openaiModelIndex(cfg.AI.OpenAIModel),
		claudeModelIdx:        claudeModelIndex(cfg.AI.ClaudeModel),
		geminiModelIdx:        geminiModelIndex(cfg.AI.GeminiModel),
		ollamaModelIdx:        ollamaModelIndex(cfg.AI.OllamaModel),
		openaiInput:           mkInput(cfg.AI.OpenAIKey, "sk-...", true),
		openaiModelInput:      mkInput(cfg.AI.OpenAIModel, "gpt-4o-mini", false),
		claudeInput:           mkInput(cfg.AI.ClaudeKey, "sk-ant-...", true),
		claudeModelInput:      mkInput(cfg.AI.ClaudeModel, "claude-sonnet-4", false),
		geminiInput:           mkInput(cfg.AI.GeminiKey, "AIza...", true),
		geminiModelInput:      mkInput(cfg.AI.GeminiModel, "gemini-1.5-flash", false),
		ollamaURLInput:        mkInput(cfg.AI.OllamaURL, "http://localhost:11434", false),
		ollamaModelInput:      mkInput(cfg.AI.OllamaModel, "llama3.2", false),
		savePathInput:         mkInput(cfg.AI.SavePath, "~/", false),
		markReadOnSummarize:   cfg.AI.MarkReadOnSummarize,
		activeSection:         ssDisplay,
		focusedPane:           settingsPaneSidebar,
		sectionField: [settingsSectionCount]settingsField{
			ssDisplay:  sfBackToSections,
			ssEditor:   sfBackToSections,
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
	cfg.Display.ShowSender = s.showSender
	cfg.Display.ThreadedConversations = s.threadedConversations
	cfg.Display.DefaultUnreadOnly = s.defaultUnreadOnly
	cfg.Display.UnreadFirst = s.unreadFirst
	cfg.Display.StarredFirst = s.starredFirst
	cfg.Display.ActionableLinks = s.actionableLinks
	cfg.Display.FilterLinks = s.filterLinks
	cfg.Display.ConfirmQuit = s.confirmQuit
	cfg.Display.ShowHeaders = s.showHeaders
	cfg.Display.Notifications = s.notifications
	cfg.Display.ComposeVim = s.composeVim
	if w, err := strconv.Atoi(strings.TrimSpace(s.readingWidthInput.Value())); err == nil {
		cfg.Display.ReadingWidth = max(0, w)
	}
	if n, err := strconv.Atoi(strings.TrimSpace(s.sendDelayInput.Value())); err == nil {
		cfg.Display.SendDelaySeconds = max(0, n)
	}
	if s.layoutDensityIdx == 1 {
		cfg.Display.Density = "compact"
	} else {
		cfg.Display.Density = "comfortable"
	}
	if s.paneCornersIdx == 1 {
		cfg.Display.PaneCorners = "round"
	} else {
		cfg.Display.PaneCorners = "square"
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
	cfg.AI.OpenAIModel = openaiModelLabels[s.openaiModelIdx]
	if value := strings.TrimSpace(s.claudeInput.Value()); value != "" {
		cfg.AI.ClaudeKey = value
	}
	cfg.AI.ClaudeModel = claudeModelLabels[s.claudeModelIdx]
	if value := strings.TrimSpace(s.geminiInput.Value()); value != "" {
		cfg.AI.GeminiKey = value
	}
	cfg.AI.GeminiModel = geminiModelLabels[s.geminiModelIdx]
	cfg.AI.OllamaURL = strings.TrimSpace(s.ollamaURLInput.Value())
	cfg.AI.OllamaModel = ollamaModelLabels[s.ollamaModelIdx]
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
		s.aboutRevealFrame = 0
		s.aboutRevealActive = false
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
	s.sendDelayInput.Blur()
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
	case sfSendDelay:
		s.sendDelayInput.Focus()
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
	case sfOllamaURL:
		s.ollamaURLInput.Focus()
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
		// Keep this order in lockstep with the ssDisplay case in viewSectionBody:
		// Appearance, Terminal colors (retro themes), Message list, Reading, Behavior.
		fields := []settingsField{sfBackToSections, sfTheme, sfDisplayDensity, sfPaneCorners, sfIcons, sfDateFormat, sfFocusLine}
		if config.IsRetroTerminalTheme(s.themeName) {
			fields = append(fields, sfRetroBg, sfRetroFg, sfRetroAccent)
		}
		fields = append(fields, sfShowSender, sfThreadedConversations, sfDefaultUnreadOnly, sfUnreadFirst, sfStarredFirst)
		fields = append(fields, sfReadingWidth, sfShowHeaders, sfMarkReadOnOpen, sfMarkReadOnFocus, sfActionableLinks, sfFilterLinks)
		return append(fields, sfBrowser, sfConfirmQuit, sfNotifications)
	case ssEditor:
		return []settingsField{sfBackToSections, sfComposeVim, sfSendDelay}
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
		return []settingsField{sfBackToSections, sfViewLogs, sfFeedMaxBody}

	case ssAbout:
		return []settingsField{sfBackToSections, sfAboutHeart, sfAboutRepo, sfAboutIssues}
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
	case sfBrowser, sfFeedMaxBody, sfReadingWidth, sfSendDelay, sfAPIKey, sfOllamaURL, sfSavePath,
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
	case sfSendDelay:
		s.sendDelayInput, cmd = s.sendDelayInput.Update(msg)
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
	case sfOllamaURL:
		s.ollamaURLInput, cmd = s.ollamaURLInput.Update(msg)
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
	case sfSendDelay:
		return s.sendDelayInput.Position()
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
	case sfOllamaURL:
		return s.ollamaURLInput.Position()
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
	case sfProvider, sfDisplayDensity, sfPaneCorners, sfTheme, sfDateFormat,
		sfOpenAIModel, sfClaudeModel, sfGeminiModel, sfOllamaModel:
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
		if s.aboutRevealActive {
			s.aboutRevealFrame++
			if s.aboutRevealFrame >= settingsAboutRevealTotal {
				s.aboutRevealFrame = 0
				s.aboutRevealActive = false
			}
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
	// settings model and moves back to categories; esc from categories cancels.
	switch key.String() {
	case "ctrl+s":
		return s.saveAndExit()
	case "esc":
		if s.focusedPane == settingsPaneDetail {
			s.setFocusedPane(settingsPaneSidebar)
			return s, nil, false
		}
		s.shouldSave = false
		s.shouldExit = true
		return s, nil, true
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

	case sfPaneCorners:
		switch {
		case keyMatches(key, keys.Left):
			s.paneCornersIdx = (s.paneCornersIdx + len(paneCornersLabels) - 1) % len(paneCornersLabels)
			s.setFocusedField(sfPaneCorners)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.paneCornersIdx = (s.paneCornersIdx + 1) % len(paneCornersLabels)
			s.setFocusedField(sfPaneCorners)
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

	case sfShowSender:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.showSender = !s.showSender
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfThreadedConversations:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.threadedConversations = !s.threadedConversations
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

	case sfUnreadFirst:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.unreadFirst = !s.unreadFirst
		} else if keyMatches(key, keys.Down) {
			s.setFocusedField(s.nextField())
		} else if keyMatches(key, keys.Up) {
			s.setFocusedField(s.prevField())
		}

	case sfStarredFirst:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.starredFirst = !s.starredFirst
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

	case sfComposeVim:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.composeVim = !s.composeVim
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

	case sfNotifications:
		if keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) {
			s.notifications = !s.notifications
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

	case sfAboutHeart:
		switch {
		case key.Type == tea.KeySpace || key.Type == tea.KeyEnter || keyMatches(key, keys.Space) || keyMatches(key, keys.Enter):
			s.aboutRevealActive = true
			s.aboutRevealFrame = 0
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

	case sfOpenAIModel:
		switch {
		case keyMatches(key, keys.Left):
			s.openaiModelIdx = (s.openaiModelIdx + len(openaiModelLabels) - 1) % len(openaiModelLabels)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.openaiModelIdx = (s.openaiModelIdx + 1) % len(openaiModelLabels)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}
		return s, nil, false

	case sfClaudeModel:
		switch {
		case keyMatches(key, keys.Left):
			s.claudeModelIdx = (s.claudeModelIdx + len(claudeModelLabels) - 1) % len(claudeModelLabels)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.claudeModelIdx = (s.claudeModelIdx + 1) % len(claudeModelLabels)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}
		return s, nil, false

	case sfGeminiModel:
		switch {
		case keyMatches(key, keys.Left):
			s.geminiModelIdx = (s.geminiModelIdx + len(geminiModelLabels) - 1) % len(geminiModelLabels)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.geminiModelIdx = (s.geminiModelIdx + 1) % len(geminiModelLabels)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}
		return s, nil, false

	case sfOllamaModel:
		switch {
		case keyMatches(key, keys.Left):
			s.ollamaModelIdx = (s.ollamaModelIdx + len(ollamaModelLabels) - 1) % len(ollamaModelLabels)
		case keyMatches(key, keys.Space) || keyMatches(key, keys.Enter) || keyMatches(key, keys.Right):
			s.ollamaModelIdx = (s.ollamaModelIdx + 1) % len(ollamaModelLabels)
		case keyMatches(key, keys.Down):
			s.setFocusedField(s.nextField())
		case keyMatches(key, keys.Up):
			s.setFocusedField(s.prevField())
		}
		return s, nil, false

	case sfBrowser, sfFeedMaxBody, sfReadingWidth, sfSendDelay, sfAPIKey, sfOllamaURL, sfSavePath,
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
	gap := lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render("")
	hints := s.viewHints(width, chrome)
	bodyH := max(1, height-lipgloss.Height(gap)-lipgloss.Height(hints))
	body := s.viewSplit(width, bodyH, chrome)

	return lipgloss.JoinVertical(lipgloss.Left, gap, body, hints)
}

func (s *Settings) viewSplit(width, height int, chrome managerChrome) string {
	leftW := clamp(width/4, 14, 18)
	if width-leftW-1 < 32 {
		leftW = max(14, width-33)
	}
	rightW := max(18, width-leftW-1)
	left := s.viewSectionsPane(leftW, height, chrome)
	right := s.viewSectionPane(rightW, height, chrome)
	sepGlyph := "│"
	if chrome.plainUI {
		sepGlyph = "|"
	}
	sepLines := make([]string, height)
	for i := range sepLines {
		sepLines[i] = sepGlyph
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
	body := lipgloss.JoinVertical(lipgloss.Left, rows...)
	section := clampView(body, width, height, chrome.baseBg)
	return lipgloss.NewStyle().Width(width).Height(height).Background(chrome.baseBg).Render(section)
}

func (s *Settings) viewSectionPane(width, height int, chrome managerChrome) string {
	s.detailHeight = height - 2 // title row + gap
	title := titleCaseSectionLabel(settingsSectionLabels[s.activeSection])
	body := s.viewSectionBody(width, chrome)
	titleBg := chrome.baseBg
	titleFg := chrome.accent
	if s.activeSection == ssAbout {
		titleBg = lipgloss.Color("#000000")
		titleFg = lipgloss.Color("#ffffff")
	}
	titleRow := softRail(chrome, s.focusedPane == settingsPaneDetail, titleBg) +
		lipgloss.NewStyle().Background(titleBg).Foreground(titleFg).Bold(true).Render(title)
	titleRow = padStyled(titleRow, width, titleBg)
	paneBg := s.sectionDetailBg(chrome)
	headingGap := lipgloss.NewStyle().Background(paneBg).Width(width).Render("")
	bodyHeight := max(1, height-2)
	section := lipgloss.JoinVertical(lipgloss.Left, titleRow, headingGap, s.scrollSectionBody(body, width, bodyHeight, paneBg))
	return lipgloss.NewStyle().Width(width).Height(height).Background(paneBg).Render(section)
}

func (s Settings) sectionDetailBg(chrome managerChrome) lipgloss.Color {
	if s.activeSection == ssAbout {
		return lipgloss.Color("#000000")
	}
	return chrome.baseBg
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
		// Keep this order in lockstep with the ssDisplay case in sectionFields.
		b.addGroup("Appearance")
		b.addThemeSelector()
		b.addDensitySelector()
		b.addPaneCornersSelector()
		b.addToggle("Icons", s.icons, sfIcons)
		b.addDateFormatSelector()
		b.addToggle("Focus line", s.focusLine, sfFocusLine)
		if config.IsRetroTerminalTheme(s.themeName) {
			b.addGroup("Terminal colors")
			b.addInput("VT background (#rrggbb)", s.retroBgInput, sfRetroBg)
			b.addInput("VT foreground (#rrggbb)", s.retroFgInput, sfRetroFg)
			b.addInput("VT accent (#rrggbb)", s.retroAccentInput, sfRetroAccent)
		}
		b.addGroup("Message list")
		b.addToggle("Show sender", s.showSender, sfShowSender)
		b.addToggle("Threaded conversations", s.threadedConversations, sfThreadedConversations)
		b.addToggle("Default to unread only", s.defaultUnreadOnly, sfDefaultUnreadOnly)
		b.addToggle("Unread first", s.unreadFirst, sfUnreadFirst)
		b.addToggle("Starred first", s.starredFirst, sfStarredFirst)
		b.addGroup("Reading")
		b.addInput("Reading width (columns)", s.readingWidthInput, sfReadingWidth)
		b.addToggle("Show email headers", s.showHeaders, sfShowHeaders)
		b.addToggle("Mark read on open", s.markReadOnOpen, sfMarkReadOnOpen)
		b.addToggle("Mark read on focus", s.markReadOnFocus, sfMarkReadOnFocus)
		b.addToggle("Actionable article links", s.actionableLinks, sfActionableLinks)
		b.addToggle("Filter links from articles", s.filterLinks, sfFilterLinks)
		b.addGroup("Behavior")
		b.addInput("Browser command", s.browserInput, sfBrowser)
		b.addToggle("Confirm before quitting", s.confirmQuit, sfConfirmQuit)
		b.addToggle("Desktop notifications", s.notifications, sfNotifications)

	case ssEditor:
		b.addGroup("Compose")
		b.addToggle("Vim keys in compose", s.composeVim, sfComposeVim)
		b.addInput("Send delay (seconds)", s.sendDelayInput, sfSendDelay)

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
	b.addLine(renderSoftGroupTitle(label, b.contentW, b.chrome))
	b.addBlank()
}

func (b *settingsFormBuilder) addControl(label string, field settingsField, control string) {
	b.markAnchor(field)
	b.addLine(renderSoftRow(label, b.s.focusedField == field, control, b.contentW, b.labelW, b.chrome))
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
	b.addLine(renderSoftRow(label, focused, control, b.contentW, b.labelW, b.chrome))
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

func (b *settingsFormBuilder) addPaneCornersSelector() {
	b.markAnchor(sfPaneCorners)
	b.addLine(b.s.renderPaneCornersSelector(b.width, b.chrome))
	if hint := b.s.fieldHint(sfPaneCorners); hint != "" {
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
	b.addControl("Provider", sfProvider, renderSettingsPicker(min(max(12, b.contentW-b.labelW-2), 18), aiProviderLabels[b.s.providerIdx], b.s.focusedField == sfProvider, b.chrome))

	switch b.s.providerIdx {
	case 1:
		b.addBareInput(b.s.openaiInput, sfAPIKey)
		b.addControl("Model", sfOpenAIModel, renderSettingsPicker(min(max(12, b.contentW-b.labelW-2), 24), openaiModelLabels[b.s.openaiModelIdx], b.s.focusedField == sfOpenAIModel, b.chrome))
	case 2:
		b.addBareInput(b.s.claudeInput, sfAPIKey)
		b.addControl("Model", sfClaudeModel, renderSettingsPicker(min(max(12, b.contentW-b.labelW-2), 24), claudeModelLabels[b.s.claudeModelIdx], b.s.focusedField == sfClaudeModel, b.chrome))
	case 3:
		b.addBareInput(b.s.geminiInput, sfAPIKey)
		b.addControl("Model", sfGeminiModel, renderSettingsPicker(min(max(12, b.contentW-b.labelW-2), 24), geminiModelLabels[b.s.geminiModelIdx], b.s.focusedField == sfGeminiModel, b.chrome))
	case 4:
		b.addInput("Ollama URL", b.s.ollamaURLInput, sfOllamaURL)
		b.addControl("Model", sfOllamaModel, renderSettingsPicker(min(max(12, b.contentW-b.labelW-2), 24), ollamaModelLabels[b.s.ollamaModelIdx], b.s.focusedField == sfOllamaModel, b.chrome))
	}

	b.addAITestConnection()
	b.addGroup("Summary output")
	b.addBareInput(b.s.savePathInput, sfSavePath)

	b.addControl("Mark read on summarize", sfMarkReadOnSummarize,
		renderSoftToggle(b.s.markReadOnSummarize, b.s.focusedField == sfMarkReadOnSummarize, b.chrome))
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
	b.addGroup("Storage")
	b.addInput("Max body size (MiB)", b.s.feedMaxBodyInput, sfFeedMaxBody)
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
	case sfFeedMaxBody, sfReadingWidth, sfSendDelay:
		return min(maxWidth, 12)
	case sfRetroBg, sfRetroFg, sfRetroAccent:
		return min(maxWidth, 44)
	case sfBrowser, sfSavePath:
		return min(maxWidth, 36)
	case sfOllamaURL:
		return min(maxWidth, 44)
	default:
		return min(maxWidth, 32)
	}
}

// Wide enough for "Feed max size (MiB)" with sectionLabel horizontal padding inside Width().
const labelColW = 22

func (s Settings) viewHints(width int, chrome managerChrome) string {
	if s.focusedPane == settingsPaneSidebar {
		return renderSoftHints(width, chrome,
			"↑↓", "section",
			"→", "edit",
			"^s", "save",
			"esc", "cancel",
			"q", "discard",
		)
	}
	if s.isPickerField() {
		return renderSoftHints(width, chrome,
			chrome.softChevrons(), "change",
			"↑↓", "field",
			"tab", "next",
			"^s", "save",
			"esc", "sections",
		)
	}
	if s.activeSection == ssUpdates && s.focusedField == sfUpdateManualCommand {
		return renderSoftHints(width, chrome,
			"enter", "copy",
			"tab", "next",
			"^s", "save",
			"esc", "sections",
		)
	}
	return renderSoftHints(width, chrome,
		"←", "sections",
		"↑↓", "field",
		"tab", "next",
		"^s", "save",
		"esc", "sections",
	)
}

func (s Settings) renderSectionNavRow(width int, label, subtitle string, selected, paneFocused bool, chrome managerChrome) string {
	fg, subFg := chrome.muted, chrome.muted
	bold := false
	display := titleCaseSectionLabel(label)
	if selected {
		display = strings.ToUpper(label)
		fg = chrome.accent
		subFg = chrome.muted
		bold = true
	}

	marker := softRail(chrome, selected && paneFocused, chrome.baseBg)
	innerW := max(1, width-lipgloss.Width(marker)-1)
	var row string
	if subtitle == "" {
		row = padRight(truncate(display, innerW), innerW)
	} else {
		subW := lipgloss.Width(subtitle)
		labelW := max(1, innerW-subW-1)
		left := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(fg).Bold(bold).Width(labelW).Render(truncate(display, labelW))
		spacer := lipgloss.NewStyle().Background(chrome.baseBg).Width(1).Render("")
		right := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(subFg).Render(subtitle)
		row = left + spacer + right
	}
	styled := marker + lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(fg).
		Bold(bold).
		Render(row)
	return padStyled(styled, width, chrome.baseBg)
}

// titleCaseSectionLabel converts an uppercase section label ("DISPLAY", "AI")
// to its quiet sidebar form ("Display", "AI").
func titleCaseSectionLabel(label string) string {
	if label == "" || strings.ToUpper(label) != label {
		return label
	}
	if len([]rune(label)) <= 2 { // acronyms like AI stay uppercase
		return label
	}
	lower := strings.ToLower(label)
	return strings.ToUpper(lower[:1]) + lower[1:]
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
		return renderSoftRow(label, focused, valueStyle.Width(valueW).Render(lines[0]), width, labelW, chrome)
	}
	padCont := lipgloss.NewStyle().Background(chrome.baseBg).Width(labelW).Render("")
	var b strings.Builder
	for i, line := range lines {
		if i == 0 {
			b.WriteString(renderSoftRow(label, focused, valueStyle.Render(line), width, labelW, chrome))
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
	marker := softRail(chrome, focused, s.sectionDetailBg(chrome))
	fg := chrome.muted
	if focused {
		fg = chrome.text
	}
	row := marker + lipgloss.NewStyle().Background(s.sectionDetailBg(chrome)).Foreground(fg).Render(label)
	return padStyled(row, max(1, width), s.sectionDetailBg(chrome))
}

// prependBackLink inserts the back-link row and a blank separator at the top of an
// already-rendered section body, bumping existing anchors by the insertion count.
func (s Settings) prependBackLink(body settingsSectionBody, width int, chrome managerChrome) settingsSectionBody {
	bg := s.sectionDetailBg(chrome)
	ind := lipgloss.NewStyle().Background(bg).Width(width)
	blank := lipgloss.NewStyle().Background(bg).Width(width).Render("")
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
	return renderSoftRow(label, focused, control, width, labelW, chrome)
}

func (s Settings) renderToggle(label string, on bool, focused bool, width int, chrome managerChrome) string {
	control := renderSoftToggle(on, focused, chrome)
	return renderSoftRow(label, focused, control, width, formLabelWidth(width), chrome)
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

func (s Settings) renderProviderSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfProvider
	providerName := aiProviderLabels[s.providerIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW-2)
	return renderSoftRow("Provider", focused, renderSettingsPicker(pickerW, providerName, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderDateFormatSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfDateFormat
	name := dateFormatLabels[s.dateFormatIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW-2)
	return renderSoftRow("Dates", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderDensitySelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfDisplayDensity
	name := layoutDensityLabels[s.layoutDensityIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW-2)
	return renderSoftRow("Layout density", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderPaneCornersSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfPaneCorners
	name := paneCornersLabels[s.paneCornersIdx]
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW-2)
	return renderSoftRow("Pane corners", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func (s Settings) renderThemeSelector(width int, chrome managerChrome) string {
	focused := s.focusedField == sfTheme
	name := BuiltinThemes[s.themeIdx].Name
	labelW := formLabelWidth(width)
	pickerW := max(1, width-labelW-2)
	return renderSoftRow("Theme", focused, renderSettingsPicker(pickerW, name, focused, chrome), width, labelW, chrome)
}

func renderSettingsPicker(width int, value string, focused bool, chrome managerChrome) string {
	return renderSoftPicker(width, value, focused, chrome)
}

func (s Settings) fieldHint(field settingsField) string {
	switch field {
	case sfBrowser:
		return "leave blank to use the system default browser"
	case sfFeedMaxBody:
		return "larger bodies need more memory; default is 10 MiB"
	case sfReadingWidth:
		return "max columns for article text; 0 = no limit (e.g. 80, 100)"
	case sfSendDelay:
		return "grace period to take back a send with ctrl+z; 0 = send immediately"
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
		return "← → or space to pick model"
	case sfClaudeModel:
		return "← → or space to pick model"
	case sfGeminiModel:
		return "← → or space to pick model"
	case sfOllamaModel:
		return "← → or space to pick model"
	case sfSavePath:
		return "Directory for exported markdown summaries."
	case sfRetroBg, sfRetroFg, sfRetroAccent:
		return "leave blank to use the built-in palette for this theme"
	case sfDisplayDensity:
		return "comfortable adds vertical spacing in lists; compact fits more rows on small terminals"
	case sfPaneCorners:
		return "square or round corners for the pane borders"
	case sfActionableLinks:
		return "enable ctrl+n / ctrl+p to select links in article content; o opens selected link"
	case sfFilterLinks:
		return "strip bare URLs from the article body text"
	case sfFocusLine:
		return "highlight the current readable line in the content pane"
	case sfShowSender:
		return "show the sender's name in a column before the subject in the message list"
	case sfThreadedConversations:
		return "group related replies into one optional conversation row"
	case sfNotifications:
		return "show a desktop notification when new mail arrives during background sync"
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
	closingBlock := s.renderAboutClosingNote(bodyW, chrome)
	closingStart := addBlock(ind.Render(closingBlock))
	heartLine := closingStart + max(0, lipgloss.Height(closingBlock)-1)
	lines = append(lines, blank)
	linksBlock := ind.Render(s.renderAboutLinks(bodyW, chrome))
	linksLines := strings.Split(linksBlock, "\n")
	// The About body gets a two-line back-link prefix after this section is
	// rendered, so reserve that space and pin the link row to the pane bottom.
	if targetHeight := s.detailHeight - 2; targetHeight > 0 {
		for len(lines)+len(linksLines) < targetHeight {
			lines = append(lines, blank)
		}
	}
	linksStart := len(lines)
	lines = append(lines, linksLines...)
	return settingsSectionBody{
		lines: lines,
		anchors: map[settingsField]int{
			sfAboutHeart:  heartLine,
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

	titleCentered := aboutCenterText(titleTxt, contentW)
	taglineCentered := aboutCenterText(tagline, contentW)

	helixRows := 5
	if contentW < 32 || (s.detailHeight > 0 && s.detailHeight <= 22) {
		helixRows = 3
	}
	lines := []string{s.renderAboutHeroTextLine(titleCentered, contentW, 0, true)}
	lines = append(lines, renderPrideDNA(contentW, helixRows, s.aboutGradientFrame, s.aboutRevealActive, s.aboutRevealFrame, chrome.plainUI)...)
	lines = append(lines, s.renderAboutHeroTextLine(taglineCentered, contentW, 1, false))

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

var aboutPrideColors = [...]lipgloss.Color{
	"#E40303", "#FF8C00", "#FFED00", "#008026", "#004DFF", "#750787",
}

var aboutDNARestColors = [...]lipgloss.Color{
	"#24163A", // rungs
	"#6D4AA2", // strand A
	"#B79AF4", // strand B
	"#E2D6FF", // crossings
}

const aboutDNAPulseColor lipgloss.Color = "#F7F1FF"

type aboutDNAColorRole int

const (
	aboutDNAColorRung aboutDNAColorRole = iota
	aboutDNAColorStrandA
	aboutDNAColorStrandB
	aboutDNAColorCross
)

type aboutHelixCell struct {
	ch       rune
	fg       lipgloss.Color
	priority int
}

func renderPrideDNA(width, rows, frame int, revealActive bool, revealFrame int, plainUI bool) []string {
	width = max(1, width)
	if rows != 3 {
		rows = 5
	}
	grid := make([][]aboutHelixCell, rows)
	for row := range grid {
		grid[row] = make([]aboutHelixCell, width)
	}

	intensity := aboutRevealIntensity(revealActive, revealFrame)
	hold := revealActive && revealFrame >= settingsAboutRevealStart && revealFrame < settingsAboutRevealEnd
	center := float64(rows-1) / 2
	amplitude := center
	for col := 0; col < width; col++ {
		theta := 2 * math.Pi * float64((col+frame)%16) / 16
		normalA := center + math.Sin(theta)*amplitude
		normalB := center - math.Sin(theta)*amplitude
		yA := clamp(int(math.Round(lerp(normalA, 0, intensity))), 0, rows-1)
		yB := clamp(int(math.Round(lerp(normalB, float64(rows-1), intensity))), 0, rows-1)
		rungColor := aboutDNAColor(aboutDNAColorRung, width, col, frame, hold, intensity)
		strandAColor := aboutDNAColor(aboutDNAColorStrandA, width, col, frame, hold, intensity)
		strandBColor := aboutDNAColor(aboutDNAColorStrandB, width, col, frame, hold, intensity)
		crossColor := aboutDNAColor(aboutDNAColorCross, width, col, frame, hold, intensity)
		rung := (col+frame/2)%4 == 0
		if rung && !hold {
			for row := min(yA, yB) + 1; row < max(yA, yB); row++ {
				setAboutHelixCell(grid, row, col, aboutDNAChars(plainUI).rung, rungColor, 1)
			}
		}
		chars := aboutDNAChars(plainUI)
		strandA := chars.rise
		strandB := chars.fall
		if math.Cos(theta) >= 0 {
			strandA, strandB = chars.fall, chars.rise
		}
		if rung {
			strandA, strandB = chars.base, chars.base
		}
		if hold {
			strandA, strandB = chars.hold, chars.hold
		}
		if yA == yB {
			crossing := (col+frame)%8 == 0
			if crossing {
				setAboutHelixCell(grid, yA, col, chars.cross, crossColor, 3)
			} else {
				setAboutHelixCell(grid, yA, col, strandA, strandAColor, 2)
			}
		} else {
			setAboutHelixCell(grid, yA, col, strandA, strandAColor, 2)
			setAboutHelixCell(grid, yB, col, strandB, strandBColor, 2)
		}
	}
	if hold {
		overlayAboutReveal(grid, "LOVE IS LOVE")
	}

	bg := lipgloss.Color("#000000")
	lines := make([]string, rows)
	for row := range grid {
		var line strings.Builder
		for _, cell := range grid[row] {
			ch := cell.ch
			if ch == 0 {
				ch = ' '
			}
			if plainUI {
				line.WriteRune(ch)
				continue
			}
			fg := cell.fg
			if fg == "" {
				fg = bg
			}
			line.WriteString(lipgloss.NewStyle().Background(bg).Foreground(fg).Bold(cell.priority >= 3).Render(string(ch)))
		}
		lines[row] = line.String()
	}
	return lines
}

type aboutHelixChars struct {
	rise, fall, rung, base, cross, hold rune
}

func aboutDNAChars(plainUI bool) aboutHelixChars {
	if plainUI {
		return aboutHelixChars{rise: '/', fall: '\\', rung: '|', base: 'o', cross: 'X', hold: '-'}
	}
	return aboutHelixChars{rise: '╱', fall: '╲', rung: '│', base: '●', cross: '◆', hold: '━'}
}

func setAboutHelixCell(grid [][]aboutHelixCell, row, col int, ch rune, fg lipgloss.Color, priority int) {
	if row < 0 || row >= len(grid) || col < 0 || col >= len(grid[row]) || priority < grid[row][col].priority {
		return
	}
	grid[row][col] = aboutHelixCell{ch: ch, fg: fg, priority: priority}
}

func overlayAboutReveal(grid [][]aboutHelixCell, message string) {
	if len(grid) == 0 || len(grid[0]) == 0 {
		return
	}
	width := len(grid[0])
	runes := []rune(message)
	if len(runes) > width {
		runes = runes[:width]
	}
	start := max(0, (width-len(runes))/2)
	row := len(grid) / 2
	colorIdx := 0
	for i, ch := range runes {
		color := lipgloss.Color("#ffffff")
		if ch != ' ' {
			color = aboutPrideColors[colorIdx%len(aboutPrideColors)]
			colorIdx++
		}
		setAboutHelixCell(grid, row, start+i, ch, color, 4)
	}
}

func aboutRevealIntensity(active bool, frame int) float64 {
	if !active {
		return 0
	}
	switch {
	case frame < settingsAboutRevealStart:
		return clamp01(float64(frame+1) / settingsAboutRevealStart)
	case frame < settingsAboutRevealEnd:
		return 1
	default:
		return clamp01(float64(settingsAboutRevealTotal-frame) / (settingsAboutRevealTotal - settingsAboutRevealEnd))
	}
}

func prideDNAColor(width, col, frame int) lipgloss.Color {
	if width <= 0 {
		return aboutPrideColors[0]
	}
	bandWidth := max(1, (width+len(aboutPrideColors)-1)/len(aboutPrideColors))
	base := aboutPrideColors[((col+frame/2)/bandWidth)%len(aboutPrideColors)]
	pulse := (frame * 2) % width
	distance := col - pulse
	if distance < 0 {
		distance = -distance
	}
	distance = min(distance, width-distance)
	if distance <= 3 {
		return adjustLightness(base, 0.24*(1-float64(distance)/4))
	}
	return base
}

func aboutDNAColor(role aboutDNAColorRole, width, col, frame int, prideReveal bool, revealIntensity float64) lipgloss.Color {
	if prideReveal {
		return prideDNAColor(width, col, frame)
	}
	role = aboutDNAColorRole(clamp(int(role), 0, len(aboutDNARestColors)-1))
	base := aboutDNARestColors[role]
	if width <= 0 {
		return base
	}
	pulse := (frame * 2) % width
	distance := col - pulse
	if distance < 0 {
		distance = -distance
	}
	distance = min(distance, width-distance)
	if distance == 0 || revealIntensity > 0.7 && distance <= 1 {
		return aboutDNAPulseColor
	}
	if distance <= 3 {
		return adjustLightness(base, (0.18+0.12*revealIntensity)*(1-float64(distance)/4))
	}
	return base
}

func lerp(from, to, amount float64) float64 {
	return from + (to-from)*clamp01(amount)
}

func (s Settings) renderAboutLinks(width int, chrome managerChrome) string {
	aboutBg := lipgloss.Color("#000000")
	renderBtn := func(label string, focused bool) string {
		bg := aboutBg
		fg := chrome.accent
		borderFg := chrome.accent
		if focused {
			bg = chrome.accent
			fg = readableText(chrome.accent, chrome.accent, 4.5)
			borderFg = chrome.accent
		}
		return lipgloss.NewStyle().
			Background(bg).
			Foreground(fg).
			Border(lipgloss.RoundedBorder()).
			BorderBackground(aboutBg).
			BorderForeground(borderFg).
			Bold(focused).
			Width(14).
			Align(lipgloss.Center).
			Render(label)
	}
	repoBtn := renderBtn("Repository", s.focusedField == sfAboutRepo)
	issuesBtn := renderBtn("Issues", s.focusedField == sfAboutIssues)
	gap := lipgloss.NewStyle().
		Background(aboutBg).
		Width(8).
		Height(lipgloss.Height(repoBtn)).
		Render("")
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
		Foreground(lipgloss.Color("#ffffff")).
		Italic(true).
		Width(max(1, width)).
		Align(lipgloss.Center).
		Render("Thanks for taking a look -allie")

	focused := s.focusedField == sfAboutHeart
	heartText := "  ❤  "
	if chrome.plainUI {
		heartText = " <3 "
	}
	if focused {
		if chrome.plainUI {
			heartText = ".<3."
		} else {
			heartText = "· ❤ ·"
		}
	}
	if s.aboutRevealActive {
		if chrome.plainUI {
			heartText = "*<3*"
		} else {
			heartText = "✦ ❤ ✦"
		}
	}
	heartColor := lipgloss.Color("#e64553")
	if s.aboutRevealActive && s.aboutRevealFrame >= settingsAboutRevealStart && s.aboutRevealFrame < settingsAboutRevealEnd {
		heartColor = prideDNAColor(max(1, width), width/2, s.aboutGradientFrame)
	} else if focused || s.aboutRevealActive {
		heartColor = aboutDNAColor(aboutDNAColorCross, max(1, width), width/2, s.aboutGradientFrame, false,
			aboutRevealIntensity(s.aboutRevealActive, s.aboutRevealFrame))
	}
	heart := lipgloss.NewStyle().Background(aboutBg).Foreground(heartColor).Bold(focused).
		Width(max(1, width)).Align(lipgloss.Center).Render(heartText)

	heartBlock := lipgloss.NewStyle().
		Background(aboutBg).
		Render(heart)
	gap := lipgloss.NewStyle().
		Background(aboutBg).
		Width(max(1, width)).
		Render("")

	return lipgloss.NewStyle().
		Background(aboutBg).
		PaddingTop(1).
		Render(lipgloss.JoinVertical(lipgloss.Left, signoff, gap, heartBlock))
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
	return lipgloss.Color("#000000")
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
