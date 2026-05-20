package ui

import (
	"strings"
	"testing"

	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestFeedManagerListViewGeometry(t *testing.T) {
	fm := FeedManager{
		feeds: []db.Feed{
			{ID: 1, Title: "How-To Geek - Linux", URL: "https://www.howtogeek.com/feed/category/linux/"},
			{ID: 2, Title: "The Verge - Tech Policy (path: verge_policy)", URL: "https://www.theverge.com/rss/index.xml"},
			{ID: 3, Title: "Hacker News - Frontpage (path: hn_top)", URL: "https://news.ycombinator.com/rss"},
			{ID: 4, Title: "XDA Dev", URL: "https://www.xda-developers.com/feed/"},
		},
		cursor: 0,
		mode:   fmList,
	}

	view := fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true)
	lines := strings.Split(ansi.Strip(view), "\n")

	if got := len(lines); got > 24 {
		t.Fatalf("expected at most 24 lines, got %d", got)
	}
	if !strings.Contains(lines[len(lines)-1], "BACK") {
		t.Fatalf("expected bottom line to contain action bar, got %q", lines[len(lines)-1])
	}
	if strings.Contains(lines[4], "Linux\n") {
		t.Fatalf("selected row wrapped unexpectedly")
	}
	if !strings.Contains(strings.Join(lines, "\n"), "HTTPS://WWW.HOWTOGEEK.COM/FEED/CATEGORY/LINUX/") {
		t.Fatalf("source URL missing from rendered view")
	}
}

func TestManagerChromeUsesThemeOverlaySurface(t *testing.T) {
	chrome := newManagerChrome(80, CatppuccinMocha, false)
	wantBase := adjustLightness(CatppuccinMocha.Bg, 0.06)

	if chrome.baseBg != wantBase {
		t.Fatalf("expected modal base to be raised from theme bg, got %q want %q", chrome.baseBg, wantBase)
	}
	if chrome.border != CatppuccinMocha.OverlayBorder {
		t.Fatalf("expected modal border to use theme overlay border, got %q want %q", chrome.border, CatppuccinMocha.OverlayBorder)
	}
	if chrome.accent != CatppuccinMocha.BorderFocus {
		t.Fatalf("expected modal accent to use theme focus border, got %q want %q", chrome.accent, CatppuccinMocha.BorderFocus)
	}
	if chrome.baseBg == lipgloss.Color("#0c0e14") || chrome.surfaceBg == lipgloss.Color("#111319") || chrome.fieldBg == lipgloss.Color("#1e2235") {
		t.Fatal("manager chrome still uses hard-coded dark modal colors")
	}
	if contrastRatio(chrome.text, chrome.baseBg) < 4.5 {
		t.Fatalf("expected accessible text contrast on modal base, got %.2f", contrastRatio(chrome.text, chrome.baseBg))
	}
}

func TestManagerChromeKeepsReadableLightThemeContrast(t *testing.T) {
	chrome := newManagerChrome(80, CatppuccinLatte, false)
	wantBase := adjustLightness(CatppuccinLatte.Bg, -0.06)

	if chrome.baseBg != wantBase {
		t.Fatalf("expected light modal base to be lowered from theme bg, got %q want %q", chrome.baseBg, wantBase)
	}
	if chrome.baseBg == CatppuccinLatte.Bg {
		t.Fatal("expected modal base to remain distinct from main background")
	}
	if contrastRatio(chrome.text, chrome.baseBg) < 4.5 {
		t.Fatalf("expected readable text on light modal base, got %.2f", contrastRatio(chrome.text, chrome.baseBg))
	}
	if contrastRatio(chrome.muted, chrome.baseBg) < 3 {
		t.Fatalf("expected readable muted text on light modal base, got %.2f", contrastRatio(chrome.muted, chrome.baseBg))
	}
}

func TestFeedManagerListModeStartsWithLeftPaneFocus(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	if !fm.listPaneFocused() {
		t.Fatal("expected manager list mode to start focused on the left pane")
	}

	fm.focusAdd()
	if fm.listPaneFocused() {
		t.Fatal("expected add dialog to start focused on the right pane")
	}
}

func TestRenderTextInputCompactsUnfocusedSecretPreview(t *testing.T) {
	input := textinput.New()
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '●'
	input.SetValue("sk-abcdefghijklmnopqrstuvwxyz123456")

	rendered := ansi.Strip(renderTextInput(input, 30, false, true, newManagerChrome(60, CatppuccinMocha, false)))

	if strings.Contains(rendered, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret preview leaked raw value: %q", rendered)
	}
	if !strings.Contains(rendered, "●●●●●●●●●●●●●●●●●●●●…") {
		t.Fatalf("expected compact masked preview, got %q", rendered)
	}
}

func TestRenderSecretSummaryUsesFingerprintInsteadOfRawValue(t *testing.T) {
	rendered := ansi.Strip(renderSecretSummary("sk-abcdefghijklmnopqrstuvwxyz123456", 40, newManagerChrome(60, CatppuccinMocha, false)))

	if strings.Contains(rendered, "abcdefghijklmnopqrstuvwxyz") {
		t.Fatalf("secret summary leaked raw value: %q", rendered)
	}
	if !strings.Contains(rendered, "saved") {
		t.Fatalf("expected saved status, got %q", rendered)
	}
	if !strings.Contains(rendered, "chars") || !strings.Contains(rendered, "id ") {
		t.Fatalf("expected fingerprint summary, got %q", rendered)
	}
}

func TestRenderSecretEditorKeepsExpectedGeometry(t *testing.T) {
	input := textinput.New()
	input.EchoMode = textinput.EchoPassword
	input.EchoCharacter = '●'
	input.Focus()
	input.SetValue("sk-abcdefghijklmnopqrstuvwxyz1234567890abcdefghijklmnopqrstuvwxyz1234567890")

	rendered := renderSecretEditor(input, 40, newManagerChrome(60, CatppuccinMocha, false))
	lines := strings.Split(rendered, "\n")

	if got := len(lines); got != 3 {
		t.Fatalf("expected 3-line secret editor, got %d", got)
	}
	for i, line := range lines {
		if got := lipgloss.Width(line); got != 40 {
			t.Fatalf("expected secret editor line %d width 40, got %d", i+1, got)
		}
	}
}

func TestFeedManagerEditViewShowsBusyStatus(t *testing.T) {
	fm := FeedManager{
		mode:      fmEdit,
		paneFocus: fmPaneDetail,
		statusMsg: "ADDING FEED...",
		busy:      true,
		busyMsg:   "ADDING FEED...",
	}

	view := ansi.Strip(fm.View(80, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))

	if !strings.Contains(view, "ADDING FEED...") {
		t.Fatalf("expected busy status in edit view, got %q", view)
	}
	if !strings.Contains(view, "WORKING") {
		t.Fatalf("expected busy action hint in edit view, got %q", view)
	}
}

func TestFeedManagerDeleteRequiresY(t *testing.T) {
	fm := FeedManager{
		feeds:  []db.Feed{{ID: 1, Title: "Test Feed", URL: "https://example.com/feed"}},
		mode:   fmConfirmDelete,
		cursor: 0,
	}

	next, cmd := fm.updateConfirmDelete(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if cmd != nil {
		t.Fatal("expected enter not to trigger delete command")
	}
	if next.mode != fmConfirmDelete {
		t.Fatalf("expected delete confirm to remain open on enter, got %v", next.mode)
	}

	next, cmd = fm.updateConfirmDelete(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}}, DefaultKeys)
	if cmd == nil {
		t.Fatal("expected y to trigger delete command")
	}
	if next.mode != fmList {
		t.Fatalf("expected delete confirm to close on y, got %v", next.mode)
	}
}

func TestFeedManagerEditViewShowsFolderPickerAndNewField(t *testing.T) {
	fm := FeedManager{
		mode:          fmEdit,
		paneFocus:     fmPaneDetail,
		editTarget:    1,
		folders:       []db.Folder{{ID: 1, Name: "Tech"}},
		folderCursor:  2,
		showNewFolder: true,
		focusedField:  4,
		colorCursor:   1,
	}
	fm.newFolderInput = textinput.New()
	fm.newFolderInput.SetValue("Infra")

	view := ansi.Strip(fm.View(80, 20, BuildStyles(CatppuccinMocha, "comfortable"), true))

	if !strings.Contains(view, "Folder") {
		t.Fatalf("expected folder picker label, got %q", view)
	}
	if !strings.Contains(view, "+ New folder") {
		t.Fatalf("expected new folder option, got %q", view)
	}
	if !strings.Contains(view, "New") || !strings.Contains(view, "Infra") {
		t.Fatalf("expected new folder input row, got %q", view)
	}
	if !strings.Contains(view, "Color") {
		t.Fatalf("expected color picker row, got %q", view)
	}
}

func TestFeedManagerEditUsesPickedFolderColorInsteadOfSelectedRow(t *testing.T) {
	fm := FeedManager{
		mode:         fmEdit,
		folders:      []db.Folder{{ID: 1, Name: "One", Color: "#7aa2f7"}, {ID: 2, Name: "Two", Color: "#f7768e"}},
		rows:         []fmRow{{kind: fmRowFolder, folderID: 1}, {kind: fmRowFolder, folderID: 2}},
		cursor:       0,
		folderCursor: 2, // picks folder ID 2
	}

	fm.syncFolderPicker()

	if got := string(fm.currentColorOption().Color); got != "#f7768e" {
		t.Fatalf("expected picked folder color #f7768e, got %q", got)
	}
}

func TestFeedManagerDisplayedColorTracksPickedFolderWhileBrowsing(t *testing.T) {
	fm := FeedManager{
		mode:         fmEdit,
		folders:      []db.Folder{{ID: 1, Name: "One", Color: "#7aa2f7"}, {ID: 2, Name: "Two", Color: "#f7768e"}},
		folderCursor: 1,
		focusedField: 2,
		colorCursor:  0,
	}

	fm.syncFolderPicker()
	if got := string(fm.displayedColorOption().Color); got != "#7aa2f7" {
		t.Fatalf("expected first picked folder color #7aa2f7, got %q", got)
	}

	fm.folderCursor = 2
	fm.syncFolderPicker()
	if got := string(fm.displayedColorOption().Color); got != "#f7768e" {
		t.Fatalf("expected second picked folder color #f7768e, got %q", got)
	}
}

func TestFeedManagerExistingFolderColorIsReadOnlyInFeedEdit(t *testing.T) {
	fm := FeedManager{
		mode:         fmEdit,
		folders:      []db.Folder{{ID: 1, Name: "One", Color: "#7aa2f7"}, {ID: 2, Name: "Two", Color: "#f7768e"}},
		folderCursor: 1,
		focusedField: 4,
		colorCursor:  0,
	}

	fm.syncFolderPicker()
	before := string(fm.displayedColorOption().Color)
	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	after := string(next.displayedColorOption().Color)

	if before != "#7aa2f7" || after != "#7aa2f7" {
		t.Fatalf("expected existing folder color to stay #7aa2f7, got before=%q after=%q", before, after)
	}
}

func TestFeedManagerListShowsFoldersBeforeFeeds(t *testing.T) {
	fm := FeedManager{
		folders: []db.Folder{{ID: 1, Name: "Tech", Color: "#7aa2f7"}},
		feeds:   []db.Feed{{ID: 2, Title: "Feed One", URL: "https://example.com/feed"}},
		cursor:  0,
		mode:    fmList,
	}

	view := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(view, "FOLDERS + FEEDS") {
		t.Fatalf("expected combined manager section, got %q", view)
	}
	if !strings.Contains(view, "󰉋 TECH") {
		t.Fatalf("expected folder row in manager list, got %q", view)
	}
	if !strings.Contains(view, "\U000f046b FEED ONE") {
		t.Fatalf("expected feed icon in manager list, got %q", view)
	}
	if !strings.Contains(view, "EDIT") || !strings.Contains(view, "DELETE") {
		t.Fatalf("expected generic actions in footer, got %q", view)
	}
}

func TestFeedManagerListShowsCollapsedFolderIcon(t *testing.T) {
	fm := FeedManager{
		folders:          []db.Folder{{ID: 1, Name: "Tech", Color: "#7aa2f7"}},
		collapsedFolders: map[int64]bool{1: true},
		cursor:           0,
		mode:             fmList,
	}

	view := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(view, "󰉖 TECH") {
		t.Fatalf("expected collapsed folder icon in manager list, got %q", view)
	}
}

func TestFeedManagerListOmitsIconsWhenDisabled(t *testing.T) {
	fm := FeedManager{
		folders: []db.Folder{{ID: 1, Name: "Tech", Color: "#7aa2f7"}},
		feeds:   []db.Feed{{ID: 2, Title: "Feed One", URL: "https://example.com/feed"}},
		cursor:  0,
		mode:    fmList,
	}

	view := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), false))
	if strings.Contains(view, "󰉋 TECH") || strings.Contains(view, "󰉖 TECH") {
		t.Fatalf("expected no folder glyphs when icons disabled, got %q", view)
	}
	if strings.Contains(view, "\U000f046b FEED ONE") {
		t.Fatalf("expected no feed glyph when icons disabled, got %q", view)
	}
}

func TestRemoteFeedManagerViewShowsBrowseOnlyActions(t *testing.T) {
	fm := NewRemoteFeedManager("Google Reader", []db.Feed{{
		ID:          -1,
		Title:       "Remote Feed",
		URL:         "https://example.com/feed",
		Description: "Tech",
	}}, nil)

	view := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))

	if !strings.Contains(view, "SUBSCRIPTIONS") {
		t.Fatalf("expected remote manager list title, got %q", view)
	}
	if strings.Contains(view, "ADD FEED") || strings.Contains(view, "DELETE") {
		t.Fatalf("expected remote manager to omit local CRUD actions, got %q", view)
	}
	if !strings.Contains(view, "BROWSE-ONLY") || !strings.Contains(view, "GOOGLE READER") {
		t.Fatalf("expected remote browse-only footer, got %q", view)
	}
}

func TestRemoteFeedManagerReadOnlyKeysStayInListMode(t *testing.T) {
	fm := NewRemoteFeedManager("Google Reader", []db.Feed{{
		ID:    -1,
		Title: "Remote Feed",
		URL:   "https://example.com/feed",
	}}, nil)

	next, _ := fm.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}}, DefaultKeys)
	if next.mode != fmList {
		t.Fatalf("expected browse-only manager to stay in list mode, got %v", next.mode)
	}
	if !strings.Contains(next.statusMsg, "BROWSE-ONLY") {
		t.Fatalf("expected browse-only status after add key, got %q", next.statusMsg)
	}

	next, _ = next.updateList(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if !next.shouldExit {
		t.Fatal("expected enter to exit remote manager into browse flow")
	}
	if next.browseFeedID != -1 {
		t.Fatalf("expected enter to target selected remote feed, got %d", next.browseFeedID)
	}
}

func TestFeedManagerAddDialogCanSwitchToGReaderFields(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.focusAdd()

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if next.addSourceIdx != fmAddSourceGReader {
		t.Fatalf("expected enter on source toggle to switch to greader, got %d", next.addSourceIdx)
	}

	view := ansi.Strip(next.View(96, 32, BuildStyles(CatppuccinMocha, "comfortable"), true))
	for _, want := range []string{"ADD FEED", "Source", "Name", "PULLED FROM THE FEED WHEN ADDED.", "URL (optional)", "API URL", "Login", "Password", "alice", "https://rss.example.com/api/greader.php"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected greader add dialog to contain %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "secret") {
		t.Fatalf("expected greader password to stay masked, got %q", view)
	}
	if !strings.Contains(view, "●") {
		t.Fatalf("expected masked password preview in greader add dialog, got %q", view)
	}
	for _, unwanted := range []string{"Folder", "Color"} {
		if strings.Contains(view, unwanted) {
			t.Fatalf("expected greader add dialog to hide %q, got %q", unwanted, view)
		}
	}
}

func TestFeedManagerSourceToggleShowsToggleHint(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)

	view := ansi.Strip(next.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(view, "TOGGLE SOURCE") {
		t.Fatalf("expected add dialog source toggle hint, got %q", view)
	}
}

func TestFeedManagerAddDialogStartsRightPaneFocused(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()

	if fm.listPaneFocused() {
		t.Fatal("expected add dialog to start on the right pane")
	}
}

func TestFeedManagerImportStartsRightPaneFocused(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusImport()

	if fm.listPaneFocused() {
		t.Fatal("expected import mode to start on the right pane")
	}
}

func TestFeedManagerAddDialogAResetsFreshAdd(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()

	if fm.listPaneFocused() {
		t.Fatal("expected a-driven add dialog to stay on the right pane")
	}
	if fm.focusedField != fmFieldBack {
		t.Fatalf("expected add dialog to focus back link, got %d", fm.focusedField)
	}
}

func TestFeedManagerBackLinkReturnsFocusToListPane(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.setData([]db.Feed{
		{ID: 1, Title: "Alpha", URL: "https://example.com/alpha.xml"},
		{ID: 2, Title: "Beta", URL: "https://example.com/beta.xml"},
	}, nil)
	fm.focusAdd()
	fm.cursor = 1

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	if !next.listPaneFocused() {
		t.Fatal("expected back link activation to return focus to the list pane")
	}
	if next.mode != fmList {
		t.Fatalf("expected back link activation to restore list mode, got %v", next.mode)
	}
	if next.cursor != 1 {
		t.Fatalf("expected back link to preserve selected left row, got cursor %d", next.cursor)
	}
}

func TestFeedManagerEditCancelRestoresMainActions(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.setData([]db.Feed{
		{ID: 1, Title: "Alpha", URL: "https://example.com/alpha.xml"},
	}, nil)
	fm.mode = fmEdit
	fm.paneFocus = fmPaneDetail
	fm.editTarget = 1
	fm.focusedField = fmFieldBack

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)

	if next.mode != fmList {
		t.Fatalf("expected esc from local edit to restore list mode, got %v", next.mode)
	}
	view := strings.ToLower(next.View(74, 22, BuildStyles(CatppuccinMocha, "compact"), false))
	for _, want := range []string{"move", "delete", "import", "export"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected main manager action %q after esc, view:\n%s", want, view)
		}
	}
}

func TestFeedManagerTextInputLeftArrowMovesToBackLinkAtCursorStart(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if next.focusedField != 1 {
		t.Fatalf("expected tab from source to land on URL for Local add, got %d", next.focusedField)
	}
	next.focusCurrentEditField()
	next.urlInput.SetValue("https://example.com/feed.xml")
	next.urlInput.CursorStart()

	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedField != fmFieldBack {
		t.Fatalf("expected left arrow at cursor start to move focus to back link, got %d", next.focusedField)
	}
	if next.mode != fmEdit {
		t.Fatalf("expected add dialog to remain in edit mode, got %v", next.mode)
	}
}

func TestFeedManagerEditCancelReturnsToListPaneAndClearsRemoteSettings(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.focusRemoteSettingsEdit(db.Feed{
		ID:    -1,
		Title: "Remote Feed",
		URL:   "https://example.com/feed",
	})

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)

	if next.mode != fmEdit {
		t.Fatalf("expected first esc to stay in edit mode, got %v", next.mode)
	}
	if !next.listPaneFocused() {
		t.Fatal("expected first esc to return focus to the left pane")
	}
	if !next.remoteSettingsEdit {
		t.Fatal("expected first esc to keep remote settings mode until overlay closes")
	}

	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if !next.shouldExit {
		t.Fatal("expected second esc from list pane to exit the manager overlay")
	}
}

func TestFeedManagerTextInputLeftArrowStaysInDetailWhenCursorCanMove(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if next.focusedField != 1 {
		t.Fatalf("expected tab from source to land on URL for Local add, got %d", next.focusedField)
	}
	next.focusCurrentEditField()
	next.urlInput.SetValue("abc")
	next.urlInput.CursorEnd()

	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.listPaneFocused() {
		t.Fatal("expected left arrow to stay in the detail pane while the cursor can still move left")
	}
	if got := next.urlInput.Position(); got != 2 {
		t.Fatalf("expected URL cursor to move left to position 2, got %d", got)
	}
}

func TestFeedManagerGReaderInputLeftArrowMovesToLeftPaneAtCursorStart(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next.focusedField = fmFieldGReaderURL
	next.focusCurrentEditField()
	next.greaderURLInput.SetValue("https://rss.example.com/api/greader.php")
	next.greaderURLInput.CursorStart()

	next, _ = next.updateEdit(tea.KeyMsg{Type: tea.KeyLeft}, DefaultKeys)

	if next.focusedField != fmFieldBack {
		t.Fatalf("expected greader API URL left arrow at cursor start to move focus to back link, got %d", next.focusedField)
	}
	if next.addSourceIdx != fmAddSourceGReader {
		t.Fatalf("expected greader source to remain selected, got %d", next.addSourceIdx)
	}
}

func TestFeedManagerLeftPaneNavigationUpdatesRightPaneDetails(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.setData([]db.Feed{
		{ID: 1, Title: "Alpha", URL: "https://example.com/alpha.xml"},
		{ID: 2, Title: "Beta", URL: "https://example.com/beta.xml"},
	}, nil)
	fm.focusAdd()
	fm.paneFocus = fmPaneList

	firstView := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(firstView, "ADD FEED") {
		t.Fatalf("expected left-pane add state to show add workspace title, got %q", firstView)
	}
	if !strings.Contains(firstView, "HTTPS://EXAMPLE.COM/ALPHA.XML") {
		t.Fatalf("expected initial details pane to show first feed info, got %q", firstView)
	}
	if strings.Contains(firstView, "HTTPS://EXAMPLE.COM/BETA.XML") {
		t.Fatalf("expected initial details pane not to show second feed info, got %q", firstView)
	}

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	secondView := ansi.Strip(next.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(secondView, "HTTPS://EXAMPLE.COM/BETA.XML") {
		t.Fatalf("expected moving down in left pane to update details for second feed, got %q", secondView)
	}
	if strings.Contains(secondView, "HTTPS://EXAMPLE.COM/ALPHA.XML") {
		t.Fatalf("expected second details pane not to keep first feed info, got %q", secondView)
	}
}

func TestFeedManagerRemoteFeedDetailsShowGReaderConfig(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.setData([]db.Feed{{
		ID:          -1,
		Title:       "Remote Feed",
		URL:         "https://example.com/feed",
		Description: "Tech",
		FolderID:    1,
	}}, []db.Folder{{ID: 1, Name: "Remote", Color: "#7aa2f7"}})
	fm.selectFeed(-1)

	view := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))

	for _, want := range []string{
		"SOURCE: GOOGLE READER",
		"FOLDER: REMOTE",
		"API URL: HTTPS://RSS.EXAMPLE.COM/API/GREADER.PHP",
		"LOGIN: ALICE",
		"PASSWORD: ●●●●●●",
		"CATEGORY: TECH",
	} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected remote feed details to contain %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "secret") {
		t.Fatalf("expected remote feed details to mask the stored password, got %q", view)
	}
}

func TestFeedManagerEnteringRightPaneFromRemoteRowPrefillsGReaderForm(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.setData([]db.Feed{{
		ID:    -1,
		Title: "Remote Feed",
		URL:   "https://example.com/feed",
	}}, nil)
	fm.focusAdd()
	fm.paneFocus = fmPaneList

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	if next.listPaneFocused() {
		t.Fatal("expected enter on a selected remote row to move focus to the right pane")
	}
	if next.addSourceIdx != fmAddSourceGReader {
		t.Fatalf("expected remote row to switch add form to greader, got %d", next.addSourceIdx)
	}
	if got := next.titleInput.Value(); got != "" {
		t.Fatalf("expected remote row to leave title blank to avoid stale overrides, got %q", got)
	}
	if got := next.urlInput.Value(); got != "https://example.com/feed" {
		t.Fatalf("expected remote row to prefill feed URL, got %q", got)
	}
	if got := next.greaderURLInput.Value(); got != "https://rss.example.com/api/greader.php" {
		t.Fatalf("expected greader API URL to stay populated, got %q", got)
	}
	if got := next.greaderLoginInput.Value(); got != "alice" {
		t.Fatalf("expected greader login to stay populated, got %q", got)
	}
	if got := next.greaderPasswordInput.Value(); got != "secret" {
		t.Fatalf("expected greader password to stay populated internally, got %q", got)
	}

	view := ansi.Strip(next.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	for _, want := range []string{"ADD FEED", "API URL", "LOGIN", "REMOTE FEED", "HTTPS://EXAMPLE.COM/FEED"} {
		if !strings.Contains(strings.ToUpper(view), want) {
			t.Fatalf("expected prefilled greader form to contain %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "secret") {
		t.Fatalf("expected greader password to remain masked in the prefilled form, got %q", view)
	}
}

func TestEditableFeedManagerEnterBrowsesRemoteFeed(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.setData([]db.Feed{{
		ID:          -1,
		Title:       "Remote Feed",
		URL:         "https://example.com/feed",
		Description: "Tech",
	}}, nil)

	next, _ := fm.updateList(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if !next.shouldExit {
		t.Fatal("expected enter on remote feed to exit into browse flow")
	}
	if next.browseFeedID != -1 {
		t.Fatalf("expected remote browse target -1, got %d", next.browseFeedID)
	}
}

func TestEditableFeedManagerEditRemoteFeedOpensGReaderSettings(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.setData([]db.Feed{{
		ID:       -1,
		Title:    "Remote Feed",
		URL:      "https://example.com/feed",
		FolderID: 1,
	}}, []db.Folder{{ID: 1, Name: "Remote", Color: "#7aa2f7"}})
	fm.selectFeed(-1)

	next, _ := fm.updateList(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'e'}}, DefaultKeys)
	if next.mode != fmEdit {
		t.Fatalf("expected remote edit attempt to open settings edit mode, got %v", next.mode)
	}
	if next.listPaneFocused() {
		t.Fatal("expected remote edit to focus the right pane")
	}
	if !next.remoteSettingsEdit {
		t.Fatal("expected remote edit to enter greader settings mode")
	}
	if next.addSourceIdx != fmAddSourceGReader {
		t.Fatalf("expected remote edit to stay on greader source, got %d", next.addSourceIdx)
	}
	if next.focusedField != fmFieldBack {
		t.Fatalf("expected remote edit to focus back link, got %d", next.focusedField)
	}
	if got := next.titleInput.Value(); got != "Remote Feed" {
		t.Fatalf("expected remote edit to keep selected feed title, got %q", got)
	}
	if got := next.urlInput.Value(); got != "https://example.com/feed" {
		t.Fatalf("expected remote edit to keep selected feed URL, got %q", got)
	}
	if got := next.greaderURLInput.Value(); got != "https://rss.example.com/api/greader.php" {
		t.Fatalf("expected remote edit to prefill API URL, got %q", got)
	}
	view := ansi.Strip(next.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	for _, want := range []string{"GREADER SETTINGS", "BACK TO SECTIONS", "NAME", "REMOTE FEED", "FEED URL", "API URL"} {
		if !strings.Contains(strings.ToUpper(view), want) {
			t.Fatalf("expected remote edit view to contain %q, got %q", want, view)
		}
	}
	if strings.Contains(view, "secret") {
		t.Fatalf("expected remote edit to keep password masked, got %q", view)
	}
}

func TestFeedManagerRowsDoNotInsertUncategorizedFolderRow(t *testing.T) {
	fm := FeedManager{
		folders: []db.Folder{{ID: 1, Name: "Tech", Color: "#7aa2f7"}},
		feeds: []db.Feed{
			{ID: 2, Title: "Folder Feed", URL: "https://example.com/tech", FolderID: 1},
			{ID: 3, Title: "Loose Feed", URL: "https://example.com/loose"},
		},
		mode: fmList,
	}
	fm.rebuildRows()

	rows := fm.managerRows()
	if got := len(rows); got != 3 {
		t.Fatalf("expected folder plus two feed rows, got %d", got)
	}
	if rows[0].kind != fmRowFolder || rows[1].kind != fmRowFeed || rows[2].kind != fmRowFeed {
		t.Fatalf("unexpected manager row order: %#v", rows)
	}

	view := ansi.Strip(fm.View(96, 24, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(view, "LOOSE FEED") {
		t.Fatalf("expected uncategorized feed to remain visible in manager list, got %q", view)
	}
}

func TestFeedManagerFolderEditViewShowsNameAndColor(t *testing.T) {
	fm := FeedManager{
		mode:             fmFolderEdit,
		folderEditTarget: 1,
		focusedField:     4,
		colorCursor:      2,
	}
	fm.titleInput = textinput.New()
	fm.titleInput.SetValue("Tech")

	view := ansi.Strip(fm.View(80, 20, BuildStyles(CatppuccinMocha, "comfortable"), true))
	if !strings.Contains(view, "EDIT FOLDER") {
		t.Fatalf("expected folder edit header, got %q", view)
	}
	if !strings.Contains(view, "Name") || !strings.Contains(view, "Color") {
		t.Fatalf("expected folder edit fields, got %q", view)
	}
}

func TestFeedManagerFolderEditCancelReturnsToListPane(t *testing.T) {
	fm := FeedManager{
		mode:         fmFolderEdit,
		paneFocus:    fmPaneDetail,
		titleInput:   textinput.New(),
		focusedField: 0,
	}
	fm.setData([]db.Feed{{ID: 1, Title: "Alpha", URL: "https://example.com/alpha.xml"}}, []db.Folder{{ID: 1, Name: "Tech"}})
	fm.focusCurrentEditField()

	next, _ := fm.updateFolderEdit(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)

	if next.mode != fmList {
		t.Fatalf("expected esc from folder edit to restore list mode, got %v", next.mode)
	}
	if !next.listPaneFocused() {
		t.Fatal("expected esc to return focus to the left pane")
	}
	view := strings.ToLower(next.View(74, 22, BuildStyles(CatppuccinMocha, "compact"), false))
	for _, want := range []string{"move", "delete", "import", "export"} {
		if !strings.Contains(view, want) {
			t.Fatalf("expected main manager action %q after esc, view:\n%s", want, view)
		}
	}
}

func TestFeedManagerImportCancelReturnsToListPane(t *testing.T) {
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusImport()

	next, _ := fm.updateImport(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)

	if next.mode != fmImport {
		t.Fatalf("expected first esc to stay in import mode, got %v", next.mode)
	}
	if !next.listPaneFocused() {
		t.Fatal("expected first esc to return focus to the left pane")
	}

	next, _ = next.updateImport(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if !next.shouldExit {
		t.Fatal("expected second esc to exit the manager overlay")
	}
}

func TestFeedManagerViewsClampToNarrowWidth(t *testing.T) {
	styles := BuildStyles(CatppuccinMocha, "comfortable")
	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.setData([]db.Feed{{
		ID:          -1,
		Title:       "Remote Feed",
		URL:         "https://example.com/feed",
		Description: "Tech",
		FolderID:    1,
	}}, []db.Folder{{ID: 1, Name: "Remote", Color: "#7aa2f7"}})

	cases := []FeedManager{
		func() FeedManager {
			next := fm
			next.mode = fmList
			return next
		}(),
		func() FeedManager {
			next := fm
			next.focusAdd()
			return next
		}(),
		func() FeedManager {
			next := fm
			next.focusAdd()
			next.addSourceIdx = fmAddSourceGReader
			next.focusCurrentEditField()
			return next
		}(),
		func() FeedManager {
			next := fm
			next.focusRemoteSettingsEdit(db.Feed{
				ID:       -1,
				Title:    "Remote Feed",
				URL:      "https://example.com/feed",
				FolderID: 1,
			})
			return next
		}(),
		func() FeedManager {
			next := fm
			next.focusAddFolder()
			return next
		}(),
		func() FeedManager {
			next := fm
			next.focusImport()
			return next
		}(),
	}

	for i, tc := range cases {
		view := tc.View(52, 18, styles, true)
		lines := strings.Split(view, "\n")
		if got := len(lines); got > 18 {
			t.Fatalf("case %d: expected at most 18 lines, got %d", i, got)
		}
		for j, line := range lines {
			if got := lipgloss.Width(line); got > 52 {
				t.Fatalf("case %d line %d: expected width <= 52, got %d", i, j+1, got)
			}
		}
	}
}

func TestFeedManagerAddFolderCanFocusColorField(t *testing.T) {
	fm := FeedManager{}
	fm.titleInput = textinput.New()

	fm.focusAddFolder()
	if fm.focusedField != fmFieldBack {
		t.Fatalf("expected add-folder flow to start on back link, got %d", fm.focusedField)
	}

	next, _ := fm.updateFolderEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	next, _ = next.updateFolderEdit(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if next.focusedField != 4 {
		t.Fatalf("expected tab to move focus to color field, got %d", next.focusedField)
	}

	next, _ = next.updateFolderEdit(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	if next.focusedField != fmFieldBack {
		t.Fatalf("expected down from color field to wrap to back link, got %d", next.focusedField)
	}
}

func TestFeedManagerEditTextInputsAcceptMovementRunes(t *testing.T) {
	tests := []struct {
		name          string
		focusedField  int
		showNewFolder bool
		addSourceIdx  int
		key           rune
		value         func(FeedManager) string
	}{
		{
			name:         "url",
			focusedField: 1,
			key:          'j',
			value: func(fm FeedManager) string {
				return fm.urlInput.Value()
			},
		},
		{
			name:         "url h at cursor start",
			focusedField: 1,
			key:          'h',
			value: func(fm FeedManager) string {
				return fm.urlInput.Value()
			},
		},
		{
			name:          "new folder",
			focusedField:  3,
			showNewFolder: true,
			key:           'k',
			value: func(fm FeedManager) string {
				return fm.newFolderInput.Value()
			},
		},
		{
			name:         "greader api url h at cursor start",
			focusedField: fmFieldGReaderURL,
			addSourceIdx: fmAddSourceGReader,
			key:          'h',
			value: func(fm FeedManager) string {
				return fm.greaderURLInput.Value()
			},
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			fm := FeedManager{
				mode:            fmEdit,
				paneFocus:       fmPaneDetail,
				titleInput:      textinput.New(),
				urlInput:        textinput.New(),
				newFolderInput:  textinput.New(),
				greaderURLInput: textinput.New(),
				focusedField:    tc.focusedField,
				showNewFolder:   tc.showNewFolder,
				addSourceIdx:    tc.addSourceIdx,
			}
			fm.focusCurrentEditField()

			next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{tc.key}}, DefaultKeys)

			if next.focusedField != tc.focusedField {
				t.Fatalf("expected focus to stay on field %d, got %d", tc.focusedField, next.focusedField)
			}
			if next.listPaneFocused() {
				t.Fatal("expected typed movement rune to stay in the detail pane")
			}
			if got := tc.value(next); got != string(tc.key) {
				t.Fatalf("expected typed rune %q to stay in the field, got %q", string(tc.key), got)
			}
		})
	}
}

func TestFeedManagerEditURLRightArrowMovesCursor(t *testing.T) {
	fm := FeedManager{
		mode:         fmEdit,
		paneFocus:    fmPaneDetail,
		editTarget:   1,
		focusedField: 2,
		titleInput:   textinput.New(),
		urlInput:     textinput.New(),
	}
	fm.urlInput.SetValue("abc")
	fm.urlInput.CursorStart()
	fm.focusCurrentEditField()

	next, _ := fm.updateEdit(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
	if got := next.urlInput.Position(); got != 1 {
		t.Fatalf("expected cursor at position 1 after right arrow, got %d", got)
	}
}

func TestFeedManagerSyncedWidthAllowsRightMovementInLongField(t *testing.T) {
	fm := FeedManager{
		mode:         fmEdit,
		paneFocus:    fmPaneDetail,
		editTarget:   1,
		focusedField: 2,
		titleInput:   textinput.New(),
		urlInput:     textinput.New(),
	}
	fm.urlInput.SetValue(strings.Repeat("a", 80))
	fm.urlInput.CursorStart()
	fm.focusCurrentEditField()
	fm.syncTextInputWidthsForRightPane(32)

	next := fm
	for range 5 {
		var cmd tea.Cmd
		next, cmd = next.updateEdit(tea.KeyMsg{Type: tea.KeyRight}, DefaultKeys)
		_ = cmd
	}
	if got := next.urlInput.Position(); got != 5 {
		t.Fatalf("expected cursor at 5 after five right arrows, got %d", got)
	}
}

func TestFeedManagerFolderEditNameAcceptsMovementRunes(t *testing.T) {
	fm := FeedManager{
		mode:         fmFolderEdit,
		titleInput:   textinput.New(),
		focusedField: 0,
	}
	fm.focusCurrentEditField()

	next, _ := fm.updateFolderEdit(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}}, DefaultKeys)

	if next.focusedField != 0 {
		t.Fatalf("expected folder name focus to stay on the text input, got %d", next.focusedField)
	}
	if got := next.titleInput.Value(); got != "k" {
		t.Fatalf("expected folder name input to receive typed rune, got %q", got)
	}
}
