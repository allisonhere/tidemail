package ui

import (
	"bytes"
	"io"
	"net/http"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/feed"
	"github.com/allisonhere/tide/internal/update"
)

func TestFeedsLoadedPopulatesPane(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)

	// Simulate WindowSizeMsg so width/height are set
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = m2.(Model)

	// Simulate FeedsLoadedMsg with one feed
	feed := db.Feed{ID: 1, Title: "XDA", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)

	if len(m.feeds) != 1 {
		t.Errorf("expected 1 feed after FeedsLoadedMsg, got %d", len(m.feeds))
	}
	if m.overlay != overlayNone {
		t.Errorf("expected overlay=None, got %v", m.overlay)
	}

	// Verify the feed pane renders with the feed name
	view := m.View()
	if view == "" {
		t.Fatal("View() returned empty string")
	}
	if !containsString(view, "XDA") {
		t.Errorf("feeds pane does not contain 'XDA'")
		// Print a truncated view for debugging
		if len(view) > 500 {
			t.Logf("view (first 500 chars): %q", view[:500])
		} else {
			t.Logf("view: %q", view)
		}
	}
	if m.firstLoad {
		t.Error("firstLoad should be false after FeedsLoadedMsg")
	}
}

func TestFirstLoadEmptyDoesNotOpenOverlay(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = m2.(Model)

	// Simulate empty FeedsLoadedMsg on first load
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: nil})
	m = m2.(Model)

	if m.overlay != overlayNone {
		t.Errorf("expected overlay=None for empty first load, got %v", m.overlay)
	}
}

func TestEnterDoesNotOpenAddDialogWhenNoFeedsExist(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	m = m2.(Model)
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: nil})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)

	if m.overlay != overlayNone {
		t.Fatalf("expected enter on empty main screen to leave overlay closed, got %v", m.overlay)
	}
}

func TestAddKeyOpensAddDialogWhenNoFeedsExist(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	m = m2.(Model)
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: nil})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = m2.(Model)

	if m.overlay != overlayFeedManager {
		t.Fatalf("expected a on empty main screen to open feed manager overlay, got %v", m.overlay)
	}
	if m.feedManager.mode != fmEdit {
		t.Fatalf("expected a on empty main screen to open add dialog, got mode %v", m.feedManager.mode)
	}
	if m.feedManager.listPaneFocused() {
		t.Fatal("expected a on empty main screen to focus the add dialog form")
	}
}

func TestFeedPaneKeepsCurrentFeedVisibleWhenUnfocused(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)

	m2, _ := m.Update(tea.WindowSizeMsg{Width: 200, Height: 50})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Visible Feed", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyTab})
	m = m2.(Model)

	view := m.View()
	if !containsString(view, "Visible Feed") {
		t.Fatalf("feed pane lost current feed when unfocused: %q", view)
	}
}

func TestLongStatusMessageDoesNotChangeViewHeight(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 20})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Stable Feed", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)

	baseHeight := strings.Count(m.View(), "\n")

	m.setStatus("this is a very long error message that should stay on one line and not push the whole layout up or make the feed pane disappear when it renders at the bottom of the screen", true)
	view := m.View()

	if strings.Count(view, "\n") != baseHeight {
		t.Fatalf("view height changed when status message was shown: before=%d after=%d", baseHeight, strings.Count(view, "\n"))
	}
	if !containsString(view, "Stable Feed") {
		t.Fatalf("feed pane lost feed title when status message was shown: %q", view)
	}
}

func TestStatusBarShowsUpdateCheckActivity(t *testing.T) {
	m := Model{
		width:       80,
		styles:      BuildStyles(CatppuccinMocha, "comfortable"),
		updateState: updateStateChecking,
		spinner:     spinner.New(),
	}

	bar := m.renderStatusBar()
	if !containsString(bar, "checking Tide updates") {
		t.Fatalf("expected status bar to show update check activity, got %q", bar)
	}
}

func TestStatusBarOmitsUInstallWhenManualInstallRequired(t *testing.T) {
	m := Model{
		width:           80,
		styles:          BuildStyles(CatppuccinMocha, "comfortable"),
		updateState:     updateStateAvailable,
		updateInfo:      update.ReleaseInfo{Version: "v1.1.0"},
		updateInstall:   update.InstallResult{ManualCommand: "sudo true"},
		updateDismissed: false,
	}

	bar := m.renderStatusBar()
	if !containsString(bar, "App update available") || !containsString(bar, "i ignore") {
		t.Fatalf("expected status bar to show update + ignore, got %q", bar)
	}
	if containsString(bar, "U install") {
		t.Fatalf("did not expect U install when manual command is set, got %q", bar)
	}
}

func TestStatusBarKeepsUpdateAvailableVisibleWithLongFeedTitle(t *testing.T) {
	m := Model{
		width:       48,
		styles:      BuildStyles(CatppuccinMocha, "comfortable"),
		updateState: updateStateAvailable,
		updateInfo:  update.ReleaseInfo{Version: "v1.1.0"},
		feeds: []db.Feed{
			{ID: 1, Title: "A Very Long Feed Title That Would Normally Push Status Details Off Screen"},
		},
		sidebarRows:   []sidebarRow{{kind: rowKindFeed, feedID: 1}},
		sidebarCursor: 0,
	}

	bar := m.renderStatusBar()
	if !containsString(bar, "App update available") || !containsString(bar, "U install") || !containsString(bar, "i ignore") {
		t.Fatalf("expected status bar to keep update actions visible, got %q", bar)
	}
	if !strings.HasSuffix(strings.TrimRight(ansi.Strip(bar), " "), "App update available  U install  i ignore") {
		t.Fatalf("expected full update prompt to be right-aligned at the end of the bar, got %q", ansi.Strip(bar))
	}
}

func TestStatusMessageStillIncludesAvailableUpdateHint(t *testing.T) {
	m := Model{
		width:       80,
		styles:      BuildStyles(CatppuccinMocha, "comfortable"),
		updateState: updateStateAvailable,
		updateInfo:  update.ReleaseInfo{Version: "v1.1.0", Summary: "Faster update flow."},
		statusMsg:   "saved settings",
	}

	bar := m.renderStatusBar()
	if !containsString(bar, "App update available") || !containsString(bar, "U install") || !containsString(bar, "i ignore") {
		t.Fatalf("expected transient status bar to retain update actions, got %q", bar)
	}
	if !containsString(bar, "saved settings") {
		t.Fatalf("expected transient status message to remain visible, got %q", bar)
	}
}

func TestStatusBarShowsUpdateSummaryWhenAvailable(t *testing.T) {
	m := Model{
		width:       96,
		styles:      BuildStyles(CatppuccinMocha, "comfortable"),
		updateState: updateStateAvailable,
		updateInfo:  update.ReleaseInfo{Version: "v1.1.0", Summary: "Faster update flow."},
	}

	bar := m.renderStatusBar()
	if !containsString(bar, "App update available") || !containsString(bar, "U install") || !containsString(bar, "i ignore") {
		t.Fatalf("expected combined app update prompt in status bar, got %q", bar)
	}
	if containsString(ansi.Strip(bar), "Faster update flow.") {
		t.Fatalf("expected status bar to keep update summary out of the main prompt, got %q", ansi.Strip(bar))
	}
}

func TestStatusBarAlwaysShowsGlobalKeyHints(t *testing.T) {
	m := Model{
		width:         120,
		styles:        BuildStyles(CatppuccinMocha, "comfortable"),
		keys:          DefaultKeys,
		feeds:         []db.Feed{{ID: 1, Title: "Example", URL: "https://example.com/feed", UnreadCount: 1}},
		sidebarRows:   []sidebarRow{{kind: rowKindFeed, feedID: 1}},
		sidebarCursor: 0,
	}
	plain := ansi.Strip(m.renderStatusBar())
	for _, want := range []string{"feed manager", "settings", "search", "? help"} {
		if !containsString(plain, want) {
			t.Fatalf("expected status bar to include %q, got %q", want, plain)
		}
	}

	m.statusMsg = "notice"
	m.statusErr = false
	withMsg := ansi.Strip(m.renderStatusBar())
	if !containsString(withMsg, "notice") || !containsString(withMsg, "? help") {
		t.Fatalf("expected transient status to retain global hints, got %q", withMsg)
	}
}

func TestUppercaseUOpensAvailableUpdateConfirm(t *testing.T) {
	m := Model{
		updateState: updateStateAvailable,
		updateInfo:  update.ReleaseInfo{Version: "v1.1.0"},
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	got := next.(Model)

	if got.overlay != overlayUpdateConfirm {
		t.Fatalf("expected U to open update confirm, got overlay %v", got.overlay)
	}
}

func TestUppercaseUDoesNotOpenConfirmWhenManualInstallRequired(t *testing.T) {
	m := Model{
		updateState:   updateStateAvailable,
		updateInfo:    update.ReleaseInfo{Version: "v1.1.0"},
		updateInstall: update.InstallResult{ManualCommand: "sudo cp a b"},
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'U'}})
	got := next.(Model)

	if got.overlay != overlayNone {
		t.Fatalf("expected U to not open install confirm when manual install is required, got overlay %v", got.overlay)
	}
}

func TestEffectiveManualUsesSuggestedWhenInstallDirNotWritable(t *testing.T) {
	ok, err := update.InstallDestinationWritable()
	if err != nil {
		t.Fatalf("InstallDestinationWritable: %v", err)
	}
	if ok {
		t.Skip("install destination is writable; cannot assert suggested script path")
	}
	m := Model{
		currentVersion:  "v1.0.0",
		updateInfo:      update.ReleaseInfo{Version: "v2.0.0"},
		updateDismissed: false,
		updateInstall:   update.InstallResult{},
	}
	if got := m.effectiveManualCommand(); got != update.SuggestedManualInstallScript {
		t.Fatalf("effectiveManualCommand = %q want %q", got, update.SuggestedManualInstallScript)
	}
}

func TestUpdateConfirmEscReturnsToApp(t *testing.T) {
	m := Model{
		overlay: overlayUpdateConfirm,
		keys:    DefaultKeys,
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	got := next.(Model)

	if got.overlay != overlayNone {
		t.Fatalf("expected esc from update confirm to return to app, got overlay %v", got.overlay)
	}
}

func TestUpdateConfirmEnterStartsDownloadAndReturnsToApp(t *testing.T) {
	m := Model{
		overlay:    overlayUpdateConfirm,
		keys:       DefaultKeys,
		updateInfo: update.ReleaseInfo{Version: "v1.1.0", DownloadURL: "https://example.com/tide.tar.gz"},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.overlay != overlayNone {
		t.Fatalf("expected enter from update confirm to return to app, got overlay %v", got.overlay)
	}
	if got.updateState != updateStateDownloading {
		t.Fatalf("expected enter from update confirm to start downloading, got state %v", got.updateState)
	}
	if cmd == nil {
		t.Fatal("expected enter from update confirm to start a download command")
	}
}

func TestUpdateConfirmEnterWithNoURLChecksFirst(t *testing.T) {
	m := Model{
		overlay:    overlayUpdateConfirm,
		keys:       DefaultKeys,
		updateInfo: update.ReleaseInfo{Version: "v1.1.0"},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if got.overlay != overlayNone {
		t.Fatalf("expected overlay dismissed, got %v", got.overlay)
	}
	if got.updateState != updateStateChecking {
		t.Fatalf("expected state checking when DownloadURL empty, got %v", got.updateState)
	}
	if !got.pendingUpdateInstall {
		t.Fatal("expected pendingUpdateInstall to be set")
	}
	if cmd == nil {
		t.Fatal("expected a check command to be returned")
	}
}

func TestUpdateConfirmOverlayMentionsSettingsAvailability(t *testing.T) {
	m := Model{
		updateInfo: update.ReleaseInfo{
			Version:   "v1.1.0",
			AssetName: "tide-darwin-aarch64",
			Summary:   "Faster update flow.",
		},
	}

	view := m.renderUpdateConfirmOverlay(72, newManagerChrome(72, CatppuccinMocha, false))
	if !containsString(view, "INSTALL TIDE UPDATE?") {
		t.Fatalf("expected Tide-specific update header, got %q", view)
	}
	if !containsString(view, "What's new: Faster update flow.") {
		t.Fatalf("expected update confirm overlay to include release summary, got %q", view)
	}
	if !containsString(view, "Settings > Updates") {
		t.Fatalf("expected update confirm overlay to mention settings availability, got %q", view)
	}
}

func TestRefreshAllBindingReturnsCommand(t *testing.T) {
	m := Model{
		keys:       DefaultKeys,
		refreshing: map[int64]bool{},
		feeds: []db.Feed{
			{ID: 7, URL: "https://example.com/feed.xml"},
		},
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'F'}})
	_ = next.(Model)
	if cmd == nil {
		t.Fatal("expected refresh-all binding to return a command")
	}
}

func TestFeedRefreshAutoAppliesPermanentRedirectURL(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	feedID, err := database.AddFeed("https://example.com/old.xml", "Redirected Feed", "")
	if err != nil {
		t.Fatalf("AddFeed returned error: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)

	next, cmd := m.Update(FeedRefreshedMsg{
		FeedID: feedID,
		Result: &feed.FetchResult{
			Kind:             feed.KindSuccess,
			SuggestURLUpdate: true,
			SuggestedURL:     "https://example.com/new.xml",
		},
	})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected refresh handling to return follow-up commands")
	}
	gotFeed, err := database.GetFeed(feedID)
	if err != nil {
		t.Fatalf("GetFeed returned error: %v", err)
	}
	if gotFeed.URL != "https://example.com/new.xml" {
		t.Fatalf("expected feed URL to auto-update, got %q", gotFeed.URL)
	}
	if !strings.Contains(m.statusMsg, "feed URL updated to https://example.com/new.xml") {
		t.Fatalf("expected status message about auto-updated URL, got %q", m.statusMsg)
	}
}

func TestFeedRefreshPersistsFeedMetadata(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	feedID, err := database.AddFeed("https://example.com/feed.xml", "Old Title", "Old Description")
	if err != nil {
		t.Fatalf("AddFeed returned error: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)

	next, cmd := m.Update(FeedRefreshedMsg{
		FeedID:      feedID,
		Title:       "New Title",
		Description: "New Description",
		FaviconURL:  "https://example.com/favicon.png",
		Result: &feed.FetchResult{
			Kind: feed.KindSuccess,
		},
	})
	_ = next.(Model)

	if cmd == nil {
		t.Fatal("expected refresh handling to return follow-up commands")
	}

	gotFeed, err := database.GetFeed(feedID)
	if err != nil {
		t.Fatalf("GetFeed returned error: %v", err)
	}
	if gotFeed.Title != "New Title" {
		t.Fatalf("expected title to update, got %q", gotFeed.Title)
	}
	if gotFeed.Description != "New Description" {
		t.Fatalf("expected description to update, got %q", gotFeed.Description)
	}
	if gotFeed.FaviconURL != "https://example.com/favicon.png" {
		t.Fatalf("expected favicon URL to update, got %q", gotFeed.FaviconURL)
	}
}

func TestLocalReadArticlesDisappearAfterRefreshReload(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	feedID, err := database.AddFeed("https://example.com/feed.xml", "Local Feed", "desc")
	if err != nil {
		t.Fatalf("AddFeed returned error: %v", err)
	}

	for _, article := range []db.Article{
		{FeedID: feedID, GUID: "readme", Title: "Read Me", Link: "https://example.com/read", Content: "one", PublishedAt: unixTestTime(1710000100)},
		{FeedID: feedID, GUID: "keepme", Title: "Keep Me", Link: "https://example.com/keep", Content: "two", PublishedAt: unixTestTime(1710000000)},
	} {
		if err := database.UpsertArticle(article); err != nil {
			t.Fatalf("UpsertArticle returned error: %v", err)
		}
	}
	if err := database.MarkRead(1, true); err != nil {
		t.Fatalf("MarkRead returned error: %v", err)
	}

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{{ID: feedID, Title: "Local Feed", URL: "https://example.com/feed.xml"}}})
	m = m2.(Model)
	m.sidebarCursor = 0
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: feedID, Articles: []db.Article{
		{ID: 1, FeedID: feedID, GUID: "readme", Title: "Read Me", Link: "https://example.com/read", Content: "one", PublishedAt: unixTestTime(1710000100), Read: true},
		{ID: 2, FeedID: feedID, GUID: "keepme", Title: "Keep Me", Link: "https://example.com/keep", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}})
	m = m2.(Model)

	next, cmd := m.Update(FeedRefreshedMsg{
		FeedID: feedID,
		Result: &feed.FetchResult{Kind: feed.KindSuccess},
	})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected refresh handling to return follow-up commands")
	}

	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok {
		t.Fatalf("expected batch command, got %T", msg)
	}

	var reloadMsg ArticlesLoadedMsg
	foundReload := false
	for _, sub := range batch {
		if sub == nil {
			continue
		}
		if got, ok := sub().(ArticlesLoadedMsg); ok {
			reloadMsg = got
			foundReload = true
			break
		}
	}
	if !foundReload {
		t.Fatal("expected refresh batch to include article reload")
	}
	if len(reloadMsg.Articles) != 1 {
		t.Fatalf("expected unread-only reload after refresh, got %d articles", len(reloadMsg.Articles))
	}
	if reloadMsg.Articles[0].GUID != "keepme" {
		t.Fatalf("expected only unread article after refresh reload, got %q", reloadMsg.Articles[0].GUID)
	}
}

func TestFetchErrorOverlayOmitsURLUpdateAction(t *testing.T) {
	m := Model{
		lastFetchError: &feed.FetchResult{
			Kind:             feed.KindHttpError,
			Err:              io.EOF,
			OriginalURL:      "https://example.com/old.xml",
			FinalURL:         "https://example.com/new.xml",
			RedirectChain:    []string{"https://example.com/old.xml", "https://example.com/new.xml"},
			SuggestURLUpdate: true,
			SuggestedURL:     "https://example.com/new.xml",
		},
	}

	view := m.renderFetchErrorOverlay(72, newManagerChrome(72, CatppuccinMocha, false))
	if !containsString(view, "Feed permanently moved to new URL") {
		t.Fatalf("expected redirect note in fetch error overlay, got %q", view)
	}
	if containsString(view, "update URL") {
		t.Fatalf("expected fetch error overlay to omit URL update action, got %q", view)
	}
}

func TestLowercaseIDismissesAvailableUpdate(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	m := Model{
		cfg:         config.DefaultConfig(),
		updateState: updateStateAvailable,
		updateInfo:  update.ReleaseInfo{Version: "v1.1.0"},
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'i'}})
	got := next.(Model)

	if !got.updateDismissed {
		t.Fatal("expected i to dismiss the available update")
	}
	if got.cfg.Updates.DismissedVersion != "v1.1.0" {
		t.Fatalf("expected dismissed version to be persisted, got %q", got.cfg.Updates.DismissedVersion)
	}
	if !strings.Contains(got.statusMsg, "Tide update v1.1.0 dismissed") {
		t.Fatalf("expected Tide-specific dismiss status, got %q", got.statusMsg)
	}
}

func TestFeedPaneDoesNotStartWithBlankLines(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 20})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Top Feed", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)

	pane := m.renderFeedsPane()
	if strings.HasPrefix(pane, "\n\n") {
		t.Fatalf("feed pane starts with unexpected blank lines: %q", pane[:min(40, len(pane))])
	}
	if !containsString(pane, "Top Feed") {
		t.Fatalf("feed pane missing feed title: %q", pane)
	}
}

func TestCursorMoveDoesNotChangeRenderedLineCount(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{
		{ID: 1, Title: "Linux Magazine News (path: lmi_news)", URL: "https://example.com/1"},
		{ID: 2, Title: "XDA", URL: "https://example.com/2"},
		{ID: 3, Title: "XDA Dev", URL: "https://example.com/3"},
	}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds})
	m = m2.(Model)

	before := m.View()
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	after := m.View()

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("line count changed on cursor move: before=%d after=%d", len(beforeLines), len(afterLines))
	}

	for i := range beforeLines {
		beforeWidth := lipgloss.Width(beforeLines[i])
		afterWidth := lipgloss.Width(afterLines[i])
		if beforeWidth != afterWidth {
			t.Fatalf("line %d width changed on cursor move: before=%d after=%d\nbefore=%q\nafter=%q", i+1, beforeWidth, afterWidth, beforeLines[i], afterLines[i])
		}
	}
}

func TestFeedSelectionChangeWithArticlesKeepsFrameStable(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{
		{ID: 1, Title: "Feed One", URL: "https://example.com/1"},
		{ID: 2, Title: "Feed Two", URL: "https://example.com/2"},
	}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds})
	m = m2.(Model)

	articlesOne := []db.Article{
		{ID: 1, FeedID: 1, Title: "Short", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000000)},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articlesOne})
	m = m2.(Model)
	before := m.View()

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	articlesTwo := []db.Article{
		{ID: 2, FeedID: 2, Title: "A much longer article title for wrapping checks", Link: "https://example.com/b", Content: strings.Repeat("content line\n", 40), PublishedAt: unixTestTime(1710000100)},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 2, Articles: articlesTwo})
	m = m2.(Model)
	after := m.View()

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("frame height changed after feed selection update: before=%d after=%d", len(beforeLines), len(afterLines))
	}
	for i := range beforeLines {
		if lipgloss.Width(beforeLines[i]) != lipgloss.Width(afterLines[i]) {
			t.Fatalf("frame width changed at line %d after feed selection update", i+1)
		}
	}
}

func TestBuildStylesUsesThemeOverlayColors(t *testing.T) {
	styles := BuildStyles(CatppuccinMocha, "comfortable")
	wantBg := adjustLightness(CatppuccinMocha.Bg, 0.06)

	if got := styles.Overlay.GetBackground(); got != wantBg {
		t.Fatalf("expected overlay background %q, got %q", wantBg, got)
	}
	if got := styles.Overlay.GetBorderTopForeground(); got != CatppuccinMocha.OverlayBorder {
		t.Fatalf("expected overlay border color %q, got %q", CatppuccinMocha.OverlayBorder, got)
	}
	if got := styles.OverlayTitle.GetBackground(); got != CatppuccinMocha.BorderFocus {
		t.Fatalf("expected overlay title accent %q, got %q", CatppuccinMocha.BorderFocus, got)
	}
}

func TestBuildStylesListStrideReflectsDensity(t *testing.T) {
	comfort := BuildStyles(CatppuccinMocha, "comfortable")
	compact := BuildStyles(CatppuccinMocha, "compact")
	if comfort.ListItemLineStride() <= compact.ListItemLineStride() {
		t.Fatalf("expected comfortable list stride > compact, got %d vs %d",
			comfort.ListItemLineStride(), compact.ListItemLineStride())
	}
	hComfort := lipgloss.Height(comfort.FeedItem.Width(20).Render("x"))
	hCompact := lipgloss.Height(compact.FeedItem.Width(20).Render("x"))
	if hComfort <= hCompact {
		t.Fatalf("expected comfortable feed row height > compact, got %d vs %d", hComfort, hCompact)
	}
}

func TestBuildStylesUsesDistinctHelpSectionSurface(t *testing.T) {
	styles := BuildStyles(CatppuccinMocha, "comfortable")
	overlayBg := terminalColorAsColor(styles.Overlay.GetBackground())
	sectionBg := terminalColorAsColor(styles.HelpSection.GetBackground())

	if overlayBg == "" || sectionBg == "" {
		t.Fatalf("expected explicit help section and overlay backgrounds, got overlay=%q section=%q", overlayBg, sectionBg)
	}
	if sectionBg == overlayBg {
		t.Fatalf("expected help section background to differ from overlay background, got %q", sectionBg)
	}
	if contrastRatio(sectionBg, overlayBg) > 1.35 {
		t.Fatalf("expected help section background to stay close to overlay background, got contrast %.2f", contrastRatio(sectionBg, overlayBg))
	}
}

func TestHelpStylesShareSectionSurfaceBackground(t *testing.T) {
	styles := BuildStyles(CatppuccinMocha, "comfortable")
	sectionBg := string(terminalColorAsColor(styles.HelpSection.GetBackground()))

	if sectionBg == "" {
		t.Fatal("expected help section background to be set")
	}
	for _, got := range []string{
		string(terminalColorAsColor(styles.HelpSectionBody.GetBackground())),
		string(terminalColorAsColor(styles.HelpKey.GetBackground())),
		string(terminalColorAsColor(styles.HelpDesc.GetBackground())),
	} {
		if got != sectionBg {
			t.Fatalf("expected help styles to share section surface %q, got %q", sectionBg, got)
		}
	}
}

func TestRenderHelpRowsFitViewportWidth(t *testing.T) {
	width := 100
	view := renderHelp(width, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys)
	for i, line := range strings.Split(view, "\n") {
		if got := lipgloss.Width(line); got > width {
			t.Fatalf("line %d exceeds help width: got %d want <= %d in %q", i+1, got, width, ansi.Strip(line))
		}
	}
}

func TestRenderHelpDocumentsDisplayFocusLine(t *testing.T) {
	view := ansi.Strip(renderHelp(100, BuildStyles(CatppuccinMocha, "comfortable"), DefaultKeys))
	if !strings.Contains(view, "focus line") {
		t.Fatalf("expected help to document Display focus line setting, got %q", view)
	}
	if !strings.Contains(view, "move content focus line") {
		t.Fatalf("expected help to document content focus line navigation, got %q", view)
	}
}

func TestResetHelpViewportUsesFullOverlayWidth(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m.width = 120
	m.height = 30
	m.styles = BuildStyles(CatppuccinMocha, "comfortable")

	m.resetHelpVP()

	wantW := max(1, min(m.width-6, 90)-1)
	if m.helpVP.Width != wantW {
		t.Fatalf("expected help viewport width %d, got %d", wantW, m.helpVP.Width)
	}
}

func TestIconsToggleChangesRenderedPaneMarkers(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Display.Icons = true
	m := NewModel(database, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{{ID: 1, Title: "Feed One", URL: "https://example.com/feed", UnreadCount: 2}}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds})
	m = m2.(Model)
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: []db.Article{
		{ID: 1, FeedID: 1, Title: "Unread Article", PublishedAt: unixTestTime(1710000000), Read: false},
		{ID: 2, FeedID: 1, Title: "Read Article", PublishedAt: unixTestTime(1710000100), Read: true},
	}})
	m = m2.(Model)

	view := m.View()
	if !containsString(view, "◉ Feeds") {
		t.Fatalf("expected feeds header icon when icons are enabled: %q", view)
	}
	if !containsString(view, "≣ Articles") {
		t.Fatalf("expected articles header icon when icons are enabled: %q", view)
	}
	if !containsString(view, "● Unread Article") {
		t.Fatalf("expected unread article icon when icons are enabled: %q", view)
	}
	if !containsString(view, "· Read Article") {
		t.Fatalf("expected read article marker when icons are enabled: %q", view)
	}
}

func TestSettingsCanReopenAfterSave(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = m2.(Model)
	if m.overlay != overlaySettings {
		t.Fatalf("expected settings overlay to open, got %v", m.overlay)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyCtrlS})
	m = m2.(Model)
	if m.overlay != overlayNone {
		t.Fatalf("expected settings overlay to close on save, got %v", m.overlay)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = m2.(Model)
	if m.overlay != overlaySettings {
		t.Fatalf("expected settings overlay to reopen after save, got %v", m.overlay)
	}
}

func TestSettingsSaveAppliesDefaultUnreadOnlyImmediately(t *testing.T) {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Display.DefaultUnreadOnly = false
	m := NewModel(database, cfg, "v1.0.0", false)
	m.settings = newSettings(m.cfg, m.settingsUpdateState())
	m.settings.defaultUnreadOnly = true
	m.settings.setFocusedPane(settingsPaneSidebar)
	m.overlay = overlaySettings

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if m.overlay != overlayNone {
		t.Fatalf("expected settings overlay to close on save, got %v", m.overlay)
	}
	if !m.cfg.Display.DefaultUnreadOnly {
		t.Fatal("expected settings save to update config default unread-only")
	}
	if !m.showUnreadOnly {
		t.Fatal("expected settings save to apply unread-only filter state immediately")
	}
}

func TestSettingsOverlayOpensWithSidebarFocus(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m.keys = DefaultKeys

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'S'}})
	m = next.(Model)

	if m.overlay != overlaySettings {
		t.Fatalf("expected settings overlay to open, got %v", m.overlay)
	}
	if m.settings.focusedPane != settingsPaneSidebar {
		t.Fatalf("expected settings overlay to open with sidebar focus, got %v", m.settings.focusedPane)
	}
	if m.settings.activeSection != ssDisplay {
		t.Fatalf("expected settings overlay to open on DISPLAY section, got %v", m.settings.activeSection)
	}
}

func TestSettingsViewShowsFeedMaxSizeField(t *testing.T) {
	s := newSettings(config.DefaultConfig(), settingsUpdateState{})
	s.setActiveSection(ssFeeds)

	view := s.View(100, 30, newManagerChrome(100, CatppuccinMocha, false))

	if !containsString(view, "Feed max size (MiB)") {
		t.Fatalf("expected feed max size field in settings view: %q", view)
	}
}

func TestLoadFeedsCmdCombinesLocalAndGReaderFeeds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	if _, err := database.AddFeed("https://local.example.com/feed.xml", "Local Feed", ""); err != nil {
		t.Fatalf("AddFeed returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/subscription/list?output=json":
			return uiResponseWithJSON(http.StatusOK, `{
				"subscriptions": [
					{"id":"feed/http://example.com/feed.xml","title":"Remote Feed","url":"http://example.com/feed.xml","htmlUrl":"http://example.com/"}
				]
			}`), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/unread-count?output=json":
			return uiResponseWithJSON(http.StatusOK, `{"unreadcounts":[{"id":"feed/http://example.com/feed.xml","count":4}]}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}
	msg := m.loadFeedsCmd()()
	m2, _ := m.Update(msg)
	m = m2.(Model)

	if len(m.feeds) != 2 {
		t.Fatalf("expected local + remote feeds, got %d", len(m.feeds))
	}
	var foundLocal, foundRemote bool
	for _, feed := range m.feeds {
		switch feed.Title {
		case "Local Feed":
			foundLocal = true
		case "Remote Feed":
			foundRemote = true
			if feed.UnreadCount != 4 {
				t.Fatalf("expected unread count 4, got %d", feed.UnreadCount)
			}
		}
	}
	if !foundLocal || !foundRemote {
		t.Fatalf("expected both local and remote feeds, got %#v", m.feeds)
	}
	if len(m.greaderStreams) != 1 {
		t.Fatalf("expected remote stream map to be populated, got %d entries", len(m.greaderStreams))
	}
}

func TestLoadFeedsCmdAppliesLocalFolderPrefsToGReaderFeeds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	folderID, err := database.AddFolder("Remote", "#7aa2f7")
	if err != nil {
		t.Fatalf("AddFolder returned error: %v", err)
	}
	if err := database.SetRemoteFeedFolder(remoteStableID("feed", "feed/http://example.com/feed.xml"), folderID); err != nil {
		t.Fatalf("SetRemoteFeedFolder returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/subscription/list?output=json":
			return uiResponseWithJSON(http.StatusOK, `{
				"subscriptions": [
					{"id":"feed/http://example.com/feed.xml","title":"Remote Feed","url":"http://example.com/feed.xml","htmlUrl":"http://example.com/"}
				]
			}`), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/unread-count?output=json":
			return uiResponseWithJSON(http.StatusOK, `{"unreadcounts":[{"id":"feed/http://example.com/feed.xml","count":4}]}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	msg := m.loadFeedsCmd()()
	m2, _ := m.Update(msg)
	m = m2.(Model)

	for _, feed := range m.feeds {
		if feed.Title == "Remote Feed" {
			if feed.FolderID != folderID {
				t.Fatalf("expected remote feed folder %d, got %d", folderID, feed.FolderID)
			}
			return
		}
	}
	t.Fatalf("expected remote feed to be present, got %#v", m.feeds)
}

func TestLoadFeedsCmdIgnoresLocalTitleOverrideForGReaderFeeds(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	remoteFeedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	if err := database.SetRemoteFeedTitle(remoteFeedID, "Custom Remote"); err != nil {
		t.Fatalf("SetRemoteFeedTitle returned error: %v", err)
	}

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/subscription/list?output=json":
			return uiResponseWithJSON(http.StatusOK, `{
				"subscriptions": [
					{"id":"feed/http://example.com/feed.xml","title":"Server Title","url":"http://example.com/feed.xml","htmlUrl":"http://example.com/"}
				]
			}`), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/unread-count?output=json":
			return uiResponseWithJSON(http.StatusOK, `{"unreadcounts":[{"id":"feed/http://example.com/feed.xml","count":4}]}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	msg := m.loadFeedsCmd()()
	m2, _ := m.Update(msg)
	m = m2.(Model)

	for _, feed := range m.feeds {
		if feed.ID == remoteFeedID {
			if feed.Title != "Server Title" {
				t.Fatalf("expected server title despite local override, got %q", feed.Title)
			}
			return
		}
	}
	t.Fatalf("expected remote feed to be present, got %#v", m.feeds)
}

func TestLoadArticlesCmdUsesGReaderFeedWhenSelected(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/subscription/list?output=json":
			return uiResponseWithJSON(http.StatusOK, `{
				"subscriptions": [
					{"id":"feed/http://example.com/feed.xml","title":"Remote Feed","url":"http://example.com/feed.xml","htmlUrl":"http://example.com/"}
				]
			}`), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/unread-count?output=json":
			return uiResponseWithJSON(http.StatusOK, `{"unreadcounts":[{"id":"feed/http://example.com/feed.xml","count":1}]}`), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/stream/contents/feed%2Fhttp:%2F%2Fexample.com%2Ffeed.xml?n=100&output=json&xt=user%2F-%2Fstate%2Fcom.google%2Fread":
			return uiResponseWithJSON(http.StatusOK, `{
				"items": [
					{
						"id":"tag:google.com,2005:reader/item/abc123",
						"title":"Remote Article",
						"published":1710000000,
						"alternate":[{"href":"https://example.com/articles/1"}],
						"summary":{"content":"<p>Hello remote world</p>"},
						"origin":{"streamId":"feed/http://example.com/feed.xml"}
					}
				]
			}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}
	msg := m.loadFeedsCmd()()
	m2, _ := m.Update(msg)
	m = m2.(Model)

	if len(m.feeds) != 1 {
		t.Fatalf("expected 1 remote feed, got %d", len(m.feeds))
	}

	msg = m.loadArticlesCmd(m.feeds[0].ID)()
	m2, _ = m.Update(msg)
	m = m2.(Model)

	if len(m.articles) != 1 {
		t.Fatalf("expected 1 remote article, got %d", len(m.articles))
	}
	if m.articles[0].Link != "https://example.com/articles/1" {
		t.Fatalf("unexpected remote article link %q", m.articles[0].Link)
	}
	if !strings.Contains(m.articles[0].Content, "Hello remote world") {
		t.Fatalf("expected remote content to be normalized into article body, got %q", m.articles[0].Content)
	}
}

func TestRemoteMarkReadUsesGReaderAndUpdatesUnreadCount(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(nil, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:          feedID,
			Title:       "Remote Feed",
			URL:         "https://example.com/feed.xml",
			UnreadCount: 2,
		}},
		RemoteStreams: map[int64]string{feedID: "feed/http://example.com/feed.xml"},
	})
	m = m2.(Model)

	articleOneID := remoteStableID("article", "tag:google.com,2005:reader/item/abc123")
	articleTwoID := remoteStableID("article", "tag:google.com,2005:reader/item/abc124")
	articles := []db.Article{
		{ID: articleOneID, FeedID: feedID, GUID: "tag:google.com,2005:reader/item/abc123", Title: "Remote Article", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000100), Read: false},
		{ID: articleTwoID, FeedID: feedID, GUID: "tag:google.com,2005:reader/item/abc124", Title: "Remote Article 2", Link: "https://example.com/b", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: feedID, Articles: articles})
	m = m2.(Model)
	m.focused = paneArticles

	editTagCalled := false
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			return uiResponseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/edit-tag":
			editTagCalled = true
			body, _ := io.ReadAll(req.Body)
			if got := string(body); got != "T=csrf-token&a=user%2F-%2Fstate%2Fcom.google%2Fread&i=tag%3Agoogle.com%2C2005%3Areader%2Fitem%2Fabc123&r=user%2F-%2Fstate%2Fcom.google%2Fkept-unread" {
				t.Fatalf("unexpected edit-tag body %q", got)
			}
			return uiResponseWithBody(http.StatusOK, "OK"), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("expected remote mark-read to return a command")
	}

	m2, _ = m.Update(cmd())
	m = m2.(Model)

	if !editTagCalled {
		t.Fatal("expected remote mark-read to call edit-tag")
	}
	if !m.articles[0].Read {
		t.Fatal("expected first remote article to be marked read")
	}
	if got := m.feeds[0].UnreadCount; got != 1 {
		t.Fatalf("expected remote unread count to decrement to 1, got %d", got)
	}
	if m.articleCursor != 1 {
		t.Fatalf("expected remote mark-read to advance to next article, got %d", m.articleCursor)
	}
}

func TestRemoteMarkAllReadUsesGReaderAndClearsUnreadState(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(nil, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:          feedID,
			Title:       "Remote Feed",
			URL:         "https://example.com/feed.xml",
			UnreadCount: 3,
		}},
		RemoteStreams: map[int64]string{feedID: "feed/http://example.com/feed.xml"},
	})
	m = m2.(Model)

	articles := []db.Article{
		{ID: remoteStableID("article", "tag:google.com,2005:reader/item/abc123"), FeedID: feedID, GUID: "tag:google.com,2005:reader/item/abc123", Title: "Remote Article", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000100), Read: false},
		{ID: remoteStableID("article", "tag:google.com,2005:reader/item/abc124"), FeedID: feedID, GUID: "tag:google.com,2005:reader/item/abc124", Title: "Remote Article 2", Link: "https://example.com/b", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: feedID, Articles: articles})
	m = m2.(Model)

	markAllCalled := false
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			return uiResponseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/mark-all-as-read":
			markAllCalled = true
			body, _ := io.ReadAll(req.Body)
			if got := string(body); got != "T=csrf-token&s=feed%2Fhttp%3A%2F%2Fexample.com%2Ffeed.xml" {
				t.Fatalf("unexpected mark-all-as-read body %q", got)
			}
			return uiResponseWithBody(http.StatusOK, "OK"), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'R'}})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("expected remote mark-all-read to return a command")
	}

	m2, _ = m.Update(cmd())
	m = m2.(Model)

	if !markAllCalled {
		t.Fatal("expected remote mark-all-read to call mark-all-as-read")
	}
	if got := m.feeds[0].UnreadCount; got != 0 {
		t.Fatalf("expected remote unread count to clear, got %d", got)
	}
	for i, article := range m.articles {
		if !article.Read {
			t.Fatalf("expected remote article %d to be marked read", i)
		}
	}
}

func TestRemoteMarkReadOnOpenUsesGReader(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnOpen = true
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(nil, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:          feedID,
			Title:       "Remote Feed",
			URL:         "https://example.com/feed.xml",
			UnreadCount: 1,
		}},
		RemoteStreams: map[int64]string{feedID: "feed/http://example.com/feed.xml"},
	})
	m = m2.(Model)

	articleID := remoteStableID("article", "tag:google.com,2005:reader/item/abc123")
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: feedID, Articles: []db.Article{{
		ID:          articleID,
		FeedID:      feedID,
		GUID:        "tag:google.com,2005:reader/item/abc123",
		Title:       "Remote Article",
		Link:        "https://example.com/a",
		Content:     "one",
		PublishedAt: unixTestTime(1710000100),
		Read:        false,
	}}})
	m = m2.(Model)
	m.focused = paneArticles

	editTagCalled := false
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			return uiResponseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/edit-tag":
			editTagCalled = true
			return uiResponseWithBody(http.StatusOK, "OK"), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if m.focused != paneContent {
		t.Fatalf("expected enter to move focus to content pane, got %v", m.focused)
	}
	if cmd == nil {
		t.Fatal("expected remote mark-read-on-open to return a command")
	}

	m2, _ = m.Update(cmd())
	m = m2.(Model)

	if !editTagCalled {
		t.Fatal("expected mark-read-on-open to call edit-tag")
	}
	if !m.articles[0].Read {
		t.Fatal("expected remote article to be marked read on open")
	}
	if got := m.feeds[0].UnreadCount; got != 0 {
		t.Fatalf("expected unread count to drop to 0, got %d", got)
	}
}

func TestRemoteMarkReadFailureLeavesStateUnchanged(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(nil, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:          feedID,
			Title:       "Remote Feed",
			URL:         "https://example.com/feed.xml",
			UnreadCount: 1,
		}},
		RemoteStreams: map[int64]string{feedID: "feed/http://example.com/feed.xml"},
	})
	m = m2.(Model)

	articleID := remoteStableID("article", "tag:google.com,2005:reader/item/abc123")
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: feedID, Articles: []db.Article{{
		ID:          articleID,
		FeedID:      feedID,
		GUID:        "tag:google.com,2005:reader/item/abc123",
		Title:       "Remote Article",
		Link:        "https://example.com/a",
		Content:     "one",
		PublishedAt: unixTestTime(1710000100),
		Read:        false,
	}}})
	m = m2.(Model)
	m.focused = paneArticles

	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			return uiResponseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/edit-tag":
			return uiResponseWithBody(http.StatusUnauthorized, "denied"), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = m2.(Model)
	if cmd == nil {
		t.Fatal("expected remote mark-read to return a command")
	}

	m2, clearCmd := m.Update(cmd())
	m = m2.(Model)

	if m.articles[0].Read {
		t.Fatal("expected failed remote mark-read to leave article unread")
	}
	if got := m.feeds[0].UnreadCount; got != 1 {
		t.Fatalf("expected failed remote mark-read to leave unread count at 1, got %d", got)
	}
	if !strings.Contains(m.statusMsg, "mark read failed") {
		t.Fatalf("expected status error after failed remote mark-read, got %q", m.statusMsg)
	}
	if clearCmd == nil {
		t.Fatal("expected failed remote mark-read to schedule status clear")
	}
}

func TestRemoteReadArticleDoesNotReturnAfterReload(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(nil, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:          feedID,
			Title:       "Remote Feed",
			URL:         "https://example.com/feed.xml",
			UnreadCount: 2,
		}},
		RemoteStreams: map[int64]string{feedID: "feed/http://example.com/feed.xml"},
	})
	m = m2.(Model)

	readArticleID := remoteStableID("article", "tag:google.com,2005:reader/item/readme")
	unreadArticleID := remoteStableID("article", "tag:google.com,2005:reader/item/keepme")
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: feedID, Articles: []db.Article{
		{ID: readArticleID, FeedID: feedID, GUID: "tag:google.com,2005:reader/item/readme", Title: "Read Me", Link: "https://example.com/read", Content: "one", PublishedAt: unixTestTime(1710000100), Read: false},
		{ID: unreadArticleID, FeedID: feedID, GUID: "tag:google.com,2005:reader/item/keepme", Title: "Keep Me", Link: "https://example.com/keep", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}})
	m = m2.(Model)
	m.focused = paneArticles

	state := "mark"
	m.greaderClient.HTTPClient = &http.Client{Transport: uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.String() {
		case "https://rss.example.com/api/greader.php/accounts/ClientLogin":
			return uiResponseWithBody(http.StatusOK, "Auth=test-token\n"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/token":
			return uiResponseWithBody(http.StatusOK, "csrf-token"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/edit-tag":
			if state != "mark" {
				t.Fatalf("unexpected edit-tag during state %q", state)
			}
			return uiResponseWithBody(http.StatusOK, "OK"), nil
		case "https://rss.example.com/api/greader.php/reader/api/0/stream/contents/feed%2Fhttp:%2F%2Fexample.com%2Ffeed.xml?n=100&output=json&xt=user%2F-%2Fstate%2Fcom.google%2Fread":
			if state != "reload" {
				t.Fatalf("unexpected stream reload during state %q", state)
			}
			return uiResponseWithJSON(http.StatusOK, `{
				"items": [
					{
						"id":"tag:google.com,2005:reader/item/keepme",
						"title":"Keep Me",
						"published":1710000000,
						"alternate":[{"href":"https://example.com/keep"}],
						"summary":{"content":"<p>two</p>"},
						"origin":{"streamId":"feed/http://example.com/feed.xml"}
					}
				]
			}`), nil
		default:
			t.Fatalf("unexpected request %s", req.URL.String())
			return nil, nil
		}
	})}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)

	state = "reload"
	m2, _ = m.Update(m.loadArticlesCmd(feedID)())
	m = m2.(Model)

	if len(m.articles) != 1 {
		t.Fatalf("expected only unread remote article after reload, got %d", len(m.articles))
	}
	if m.articles[0].ID != unreadArticleID {
		t.Fatalf("expected only unread article to remain after reload, got %d", m.articles[0].ID)
	}
}

func TestLocalReadArticleDisappearsAfterReload(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	feedID, err := database.AddFeed("https://example.com/feed.xml", "Local Feed", "desc")
	if err != nil {
		t.Fatal(err)
	}

	seed := []db.Article{
		{
			FeedID:      feedID,
			GUID:        "readme",
			Title:       "Read Me",
			Link:        "https://example.com/read",
			Content:     "one",
			PublishedAt: unixTestTime(1710000100),
		},
		{
			FeedID:      feedID,
			GUID:        "keepme",
			Title:       "Keep Me",
			Link:        "https://example.com/keep",
			Content:     "two",
			PublishedAt: unixTestTime(1710000000),
		},
	}
	for _, article := range seed {
		if err := database.UpsertArticle(article); err != nil {
			t.Fatal(err)
		}
	}

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:          feedID,
			Title:       "Local Feed",
			URL:         "https://example.com/feed.xml",
			UnreadCount: 2,
		}},
	})
	m = m2.(Model)

	m2, _ = m.Update(m.loadArticlesCmd(feedID)())
	m = m2.(Model)
	m.focused = paneArticles

	if len(m.articles) != 2 {
		t.Fatalf("expected 2 local articles before mark-read, got %d", len(m.articles))
	}

	m2, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = m2.(Model)
	m2, _ = m.Update(cmd())
	m = m2.(Model)

	if !m.articles[0].Read {
		t.Fatal("expected selected local article to be marked read in memory")
	}

	m2, _ = m.Update(m.loadArticlesCmd(feedID)())
	m = m2.(Model)

	if len(m.articles) != 2 {
		t.Fatalf("expected both local articles after reload, got %d", len(m.articles))
	}
	if m.articles[0].GUID != "readme" {
		t.Fatalf("expected read local article to remain first after reload, got %q", m.articles[0].GUID)
	}
	if !m.articles[0].Read {
		t.Fatal("expected first local article to remain marked read after reload")
	}
	if m.articles[1].GUID != "keepme" {
		t.Fatalf("expected unread local article to remain second after reload, got %q", m.articles[1].GUID)
	}
}

func TestFeedManagerKeyStillOpensEditableOverlayWithRemoteFeedsLoaded(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m2, _ := m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{ID: -1, Title: "Remote Feed", URL: "https://example.com/feed"}},
		RemoteStreams: map[int64]string{
			-1: "feed/http://example.com/feed",
		},
	})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = m2.(Model)

	if m.overlay != overlayFeedManager {
		t.Fatalf("expected feed manager overlay, got %v", m.overlay)
	}
	if !m.feedManager.editable() {
		t.Fatal("expected feed manager to stay editable for the local add/manage flow")
	}
	if got := len(m.feedManager.feeds); got != 1 {
		t.Fatalf("expected remote feed to be present in manager data, got %d feeds", got)
	}
	if selected := m.feedManager.selectedFeedRow(); selected == nil || selected.ID != -1 {
		t.Fatalf("expected remote feed to be selected in manager, got %#v", selected)
	}
}

func TestAddKeyOpensAddDialogWithSourceToggle(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m2, _ := m.Update(FeedsLoadedMsg{})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = m2.(Model)

	if m.overlay != overlayFeedManager {
		t.Fatalf("expected add key to open feed manager overlay, got %v", m.overlay)
	}
	if m.feedManager.mode != fmEdit {
		t.Fatalf("expected add key to enter add dialog, got mode %v", m.feedManager.mode)
	}
	if m.feedManager.listPaneFocused() {
		t.Fatal("expected add dialog to start focused on the right pane")
	}
	if m.feedManager.focusedField != fmFieldBack {
		t.Fatalf("expected add dialog to focus back link, got field %d", m.feedManager.focusedField)
	}
}

func TestFeedManagerOverlayShowsAddActionAndAOpensAddDialog(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	m = m2.(Model)
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{ID: -1, Title: "Remote Feed", URL: "https://example.com/feed"}},
		RemoteStreams: map[int64]string{
			-1: "feed/http://example.com/feed",
		},
	})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = m2.(Model)

	view := ansi.Strip(m.View())
	if !strings.Contains(view, "ADD FEED") {
		t.Fatalf("expected manager overlay footer to advertise add feed, got %q", view)
	}
	if strings.Contains(view, "BROWSE-ONLY") {
		t.Fatalf("expected editable manager overlay not to render browse-only footer, got %q", view)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'a'}})
	m = m2.(Model)

	if m.overlay != overlayFeedManager {
		t.Fatalf("expected manager overlay to remain open after a, got %v", m.overlay)
	}
	if m.feedManager.mode != fmEdit {
		t.Fatalf("expected a in manager list view to open add dialog, got mode %v", m.feedManager.mode)
	}
	if m.feedManager.listPaneFocused() {
		t.Fatal("expected add dialog from manager list view to start focused on the right pane")
	}
}

func TestFeedManagerOpensInListModeWithLeftPaneFocus(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	feedID, err := database.AddFeed("https://example.com/feed.xml", "Example Feed", "")
	if err != nil {
		t.Fatalf("save feed: %v", err)
	}
	feed := db.Feed{ID: feedID, Title: "Example Feed", URL: "https://example.com/feed.xml"}

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 96, Height: 24})
	m = m2.(Model)
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'m'}})
	m = m2.(Model)

	if m.overlay != overlayFeedManager {
		t.Fatalf("expected manager overlay after m, got %v", m.overlay)
	}
	if m.feedManager.mode != fmList {
		t.Fatalf("expected manager to open in list mode, got %v", m.feedManager.mode)
	}
	if !m.feedManager.listPaneFocused() {
		t.Fatal("expected manager to open focused on the left pane")
	}
}

func TestRemoteFeedAddedMsgPersistsGReaderConfigAndTargetsStream(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(RemoteFeedAddedMsg{
		Source: config.SourceConfig{
			GReaderURL:      "https://rss.example.com/api/greader.php",
			GReaderLogin:    "alice",
			GReaderPassword: "secret",
		},
		StreamID: "feed/http://example.com/feed",
		Title:    "Remote Feed",
	})
	m = m2.(Model)

	if m.cfg.Source.GReaderURL != "https://rss.example.com/api/greader.php" {
		t.Fatalf("expected greader URL to be stored, got %q", m.cfg.Source.GReaderURL)
	}
	if m.cfg.Source.GReaderLogin != "alice" {
		t.Fatalf("expected greader login to be stored, got %q", m.cfg.Source.GReaderLogin)
	}
	if m.cfg.Source.GReaderPassword != "secret" {
		t.Fatalf("expected greader password to be stored, got %q", m.cfg.Source.GReaderPassword)
	}
	if m.greaderClient == nil {
		t.Fatal("expected greader client to be initialized after remote add")
	}
	if m.pendingSelectFeedID != remoteStableID("feed", "feed/http://example.com/feed") {
		t.Fatalf("expected remote add flow to target the added feed, got %d", m.pendingSelectFeedID)
	}

	saved, err := config.Load()
	if err != nil {
		t.Fatalf("load config from disk: %v", err)
	}
	if saved.Source.GReaderURL != "https://rss.example.com/api/greader.php" {
		t.Fatalf("expected greader URL to persist to disk, got %q", saved.Source.GReaderURL)
	}
	if saved.Source.GReaderLogin != "alice" {
		t.Fatalf("expected greader login to persist to disk, got %q", saved.Source.GReaderLogin)
	}
	if saved.Source.GReaderPassword != "secret" {
		t.Fatalf("expected greader password to persist to disk, got %q", saved.Source.GReaderPassword)
	}
}

func TestRemoteFeedAddedMsgWithoutStreamShowsConnectedStatus(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(RemoteFeedAddedMsg{
		Source: config.SourceConfig{
			GReaderURL:      "https://rss.example.com/api/greader.php",
			GReaderLogin:    "alice",
			GReaderPassword: "secret",
		},
		FeedCount: 7,
	})
	m = m2.(Model)

	if m.pendingSelectFeedID != 0 {
		t.Fatalf("expected connect-only greader load not to target a specific feed, got %d", m.pendingSelectFeedID)
	}
	if m.statusMsg != "connected greader: 7 feeds" {
		t.Fatalf("expected connected status, got %q", m.statusMsg)
	}
}

func TestRemoteFeedAddedMsgSettingsOnlyShowsSavedStatus(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(RemoteFeedAddedMsg{
		SettingsOnly: true,
		Source: config.SourceConfig{
			GReaderURL:      "https://rss.example.com/api/greader.php",
			GReaderLogin:    "alice",
			GReaderPassword: "secret",
		},
	})
	m = m2.(Model)

	if m.statusMsg != "saved greader settings" {
		t.Fatalf("expected saved settings status, got %q", m.statusMsg)
	}
	if m.pendingSelectFeedID != 0 {
		t.Fatalf("expected settings-only save not to target a feed, got %d", m.pendingSelectFeedID)
	}
}

func TestRemoteFeedAddedMsgSaveFailureSurfacesStatus(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	blocked := t.TempDir() + "/config-blocker"
	if err := os.WriteFile(blocked, []byte("not a dir"), 0o644); err != nil {
		t.Fatalf("write blocker file: %v", err)
	}
	t.Setenv("XDG_CONFIG_HOME", blocked)

	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, cmd := m.Update(RemoteFeedAddedMsg{
		Source: config.SourceConfig{
			GReaderURL:      "https://rss.example.com/api/greader.php",
			GReaderLogin:    "alice",
			GReaderPassword: "secret",
		},
		FeedCount: 7,
	})
	m = m2.(Model)

	if !strings.Contains(m.statusMsg, "greader config save failed") {
		t.Fatalf("expected save failure status, got %q", m.statusMsg)
	}
	if m.greaderClient == nil {
		t.Fatal("expected greader client to remain initialized in-memory after save failure")
	}
	if cmd == nil {
		t.Fatal("expected save failure path to return follow-up commands")
	}
}

func TestFeedManagerGReaderSaveCmdQuickAddsRemoteFeed(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	loginHit := false
	quickAddHit := false

	origTransport := http.DefaultTransport
	http.DefaultTransport = uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/greader.php/accounts/ClientLogin":
			loginHit = true
			return uiResponseWithBody(http.StatusOK, "SID=ignored\nAuth=test-token\n"), nil
		case "/api/greader.php/reader/api/0/subscription/quickadd":
			quickAddHit = true
			if got := req.Header.Get("Authorization"); got != "GoogleLogin auth=test-token" {
				t.Fatalf("expected auth header on quickadd request, got %q", got)
			}
			body, err := io.ReadAll(req.Body)
			if err != nil {
				t.Fatalf("read quickadd body: %v", err)
			}
			if got := string(body); got != "quickadd=https%3A%2F%2Fexample.com%2Ffeed.xml" {
				t.Fatalf("unexpected quickadd body %q", got)
			}
			return uiResponseWithJSON(http.StatusOK, `{"numResults":1,"query":"https://example.com/feed.xml","streamId":"feed/http://example.com/feed.xml","streamName":"Example Feed"}`), nil
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = origTransport }()

	fm := NewFeedManagerWithSource(database, config.SourceConfig{})
	fm.focusAdd()
	fm.addSourceIdx = fmAddSourceGReader
	fm.urlInput.SetValue("https://example.com/feed.xml")
	fm.greaderURLInput.SetValue("https://rss.example.com/api/greader.php")
	fm.greaderLoginInput.SetValue("alice")
	fm.greaderPasswordInput.SetValue("secret")

	msg := fm.saveCmd()()
	got, ok := msg.(RemoteFeedAddedMsg)
	if !ok {
		t.Fatalf("expected RemoteFeedAddedMsg, got %T", msg)
	}
	if got.Err != nil {
		t.Fatalf("expected successful remote add, got error %v", got.Err)
	}
	if !loginHit || !quickAddHit {
		t.Fatalf("expected login and quickadd requests, login=%v quickadd=%v", loginHit, quickAddHit)
	}
	if got.StreamID != "feed/http://example.com/feed.xml" {
		t.Fatalf("expected stream id from quickadd, got %q", got.StreamID)
	}
	if got.Title != "Example Feed" {
		t.Fatalf("expected server title from quickadd, got %q", got.Title)
	}
	if got.Source.GReaderURL != "https://rss.example.com/api/greader.php" || got.Source.GReaderLogin != "alice" || got.Source.GReaderPassword != "secret" {
		t.Fatalf("unexpected persisted source config: %#v", got.Source)
	}
	prefs, err := database.ListRemoteFeedPrefs()
	if err != nil {
		t.Fatalf("ListRemoteFeedPrefs returned error: %v", err)
	}
	if pref, ok := prefs[remoteStableID("feed", "feed/http://example.com/feed.xml")]; ok && pref.Title != "" {
		t.Fatalf("expected no local title override to be stored, got %q", pref.Title)
	}
}

func TestFeedManagerPrefilledRemoteQuickAddDoesNotReuseSelectedFeedTitle(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	loginHit := false
	quickAddHit := false

	origTransport := http.DefaultTransport
	http.DefaultTransport = uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/greader.php/accounts/ClientLogin":
			loginHit = true
			return uiResponseWithBody(http.StatusOK, "SID=ignored\nAuth=test-token\n"), nil
		case "/api/greader.php/reader/api/0/subscription/quickadd":
			quickAddHit = true
			return uiResponseWithJSON(http.StatusOK, `{"numResults":1,"query":"https://example.com/new.xml","streamId":"feed/http://example.com/new.xml","streamName":"Fresh Feed"}`), nil
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = origTransport }()

	fm := NewFeedManagerWithSource(database, config.SourceConfig{})
	fm.setData([]db.Feed{{
		ID:    remoteStableID("feed", "feed/http://example.com/old.xml"),
		Title: "Existing Remote Feed",
		URL:   "https://example.com/new.xml",
	}}, nil)
	fm.selectFeed(remoteStableID("feed", "feed/http://example.com/old.xml"))
	fm.focusAdd()
	fm.prefillAddFormFromSelectedRemoteFeed()
	fm.greaderURLInput.SetValue("https://rss.example.com/api/greader.php")
	fm.greaderLoginInput.SetValue("alice")
	fm.greaderPasswordInput.SetValue("secret")

	if got := fm.titleInput.Value(); got != "" {
		t.Fatalf("expected prefilled quick-add title to stay blank, got %q", got)
	}

	msg := fm.saveCmd()()
	got, ok := msg.(RemoteFeedAddedMsg)
	if !ok {
		t.Fatalf("expected RemoteFeedAddedMsg, got %T", msg)
	}
	if got.Err != nil {
		t.Fatalf("expected successful remote add, got error %v", got.Err)
	}
	if !loginHit || !quickAddHit {
		t.Fatalf("expected login and quickadd requests, login=%v quickadd=%v", loginHit, quickAddHit)
	}
	if got.Title != "Fresh Feed" {
		t.Fatalf("expected server stream name without stale title override, got %q", got.Title)
	}

	prefs, err := database.ListRemoteFeedPrefs()
	if err != nil {
		t.Fatalf("ListRemoteFeedPrefs returned error: %v", err)
	}
	if pref, ok := prefs[remoteStableID("feed", "feed/http://example.com/new.xml")]; ok && pref.Title != "" {
		t.Fatalf("expected no local title override for new remote feed, got %q", pref.Title)
	}
}

// TestFeedManagerEditRemoteFeedSaveClearsTitleOverride checks that saving
// GReader remote settings clears any local title override so the name always
// follows the feed from the API.
func TestFeedManagerEditRemoteFeedSaveClearsTitleOverride(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	const streamID = "feed/http://example.com/feed.xml"
	remoteFeedID := remoteStableID("feed", streamID)

	sourceCfg := config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	}

	if err := database.SetRemoteFeedTitle(remoteFeedID, "Stale Override"); err != nil {
		t.Fatalf("SetRemoteFeedTitle: %v", err)
	}

	fm := NewFeedManagerWithSource(database, sourceCfg)
	fm.setData([]db.Feed{{
		ID:    remoteFeedID,
		Title: "Server Name",
		URL:   "https://example.com/",
	}}, nil)
	fm.selectFeed(remoteFeedID)
	fm.focusRemoteSettingsEdit(db.Feed{
		ID:    remoteFeedID,
		Title: "Server Name",
		URL:   "https://example.com/",
	})
	if !fm.remoteSettingsEdit || fm.editTarget != remoteFeedID {
		t.Fatalf("expected remoteSettingsEdit for remote feed, got edit=%v target=%d", fm.remoteSettingsEdit, fm.editTarget)
	}

	msg := fm.saveCmd()()
	got, ok := msg.(RemoteFeedAddedMsg)
	if !ok {
		t.Fatalf("expected RemoteFeedAddedMsg, got %T", msg)
	}
	if got.Err != nil {
		t.Fatalf("save returned error: %v", got.Err)
	}
	if !got.SettingsOnly {
		t.Fatalf("expected SettingsOnly=true, got %#v", got)
	}

	prefs, err := database.ListRemoteFeedPrefs()
	if err != nil {
		t.Fatalf("ListRemoteFeedPrefs: %v", err)
	}
	if pref, exists := prefs[remoteFeedID]; exists && pref.Title != "" {
		t.Fatalf("expected title override cleared on save, got %q", pref.Title)
	}
}

func TestFeedManagerGReaderSaveCmdAllowsBlankFeedURL(t *testing.T) {
	loginHit := false
	listHit := false
	quickAddHit := false

	origTransport := http.DefaultTransport
	http.DefaultTransport = uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/greader.php/accounts/ClientLogin":
			loginHit = true
			return uiResponseWithBody(http.StatusOK, "SID=ignored\nAuth=test-token\n"), nil
		case "/api/greader.php/reader/api/0/subscription/list":
			listHit = true
			if req.URL.RawQuery != "output=json" {
				t.Fatalf("unexpected list query %q", req.URL.RawQuery)
			}
			if got := req.Header.Get("Authorization"); got != "GoogleLogin auth=test-token" {
				t.Fatalf("expected auth header on list request, got %q", got)
			}
			return uiResponseWithJSON(http.StatusOK, `{"subscriptions":[{"id":"feed/http://example.com/feed.xml","title":"Example Feed"}]}`), nil
		case "/api/greader.php/reader/api/0/subscription/quickadd":
			quickAddHit = true
			t.Fatal("did not expect quickadd when feed URL is blank")
			return nil, nil
		default:
			t.Fatalf("unexpected request path %s", req.URL.Path)
			return nil, nil
		}
	})
	defer func() { http.DefaultTransport = origTransport }()

	fm := NewFeedManagerWithSource(nil, config.SourceConfig{})
	fm.focusAdd()
	fm.addSourceIdx = fmAddSourceGReader
	fm.greaderURLInput.SetValue("https://rss.example.com/api/greader.php")
	fm.greaderLoginInput.SetValue("alice")
	fm.greaderPasswordInput.SetValue("secret")

	msg := fm.saveCmd()()
	got, ok := msg.(RemoteFeedAddedMsg)
	if !ok {
		t.Fatalf("expected RemoteFeedAddedMsg, got %T", msg)
	}
	if got.Err != nil {
		t.Fatalf("expected successful greader connect, got error %v", got.Err)
	}
	if !loginHit || !listHit || quickAddHit {
		t.Fatalf("expected login and subscription list only, login=%v list=%v quickadd=%v", loginHit, listHit, quickAddHit)
	}
	if got.StreamID != "" {
		t.Fatalf("expected no target stream for blank feed URL, got %q", got.StreamID)
	}
	if got.FeedCount != 1 {
		t.Fatalf("expected feed count from subscription list, got %d", got.FeedCount)
	}
}

func TestFeedManagerGReaderSettingsEditSaveCmdDoesNotHitNetwork(t *testing.T) {
	loginHit := false
	listHit := false
	quickAddHit := false

	origTransport := http.DefaultTransport
	http.DefaultTransport = uiRoundTripFunc(func(req *http.Request) (*http.Response, error) {
		switch req.URL.Path {
		case "/api/greader.php/accounts/ClientLogin":
			loginHit = true
		case "/api/greader.php/reader/api/0/subscription/list":
			listHit = true
		case "/api/greader.php/reader/api/0/subscription/quickadd":
			quickAddHit = true
		}
		t.Fatalf("unexpected network request in settings-only save: %s", req.URL.Path)
		return nil, nil
	})
	defer func() { http.DefaultTransport = origTransport }()

	fm := NewFeedManagerWithSource(nil, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.focusRemoteSettingsEdit(db.Feed{
		ID:    -1,
		Title: "Remote Feed",
		URL:   "https://example.com/feed.xml",
	})
	fm.greaderURLInput.SetValue("https://rss.example.com/api/greader.php")
	fm.greaderLoginInput.SetValue("alice")
	fm.greaderPasswordInput.SetValue("secret")

	msg := fm.saveCmd()()
	got, ok := msg.(RemoteFeedAddedMsg)
	if !ok {
		t.Fatalf("expected RemoteFeedAddedMsg, got %T", msg)
	}
	if got.Err != nil {
		t.Fatalf("expected successful settings save, got error %v", got.Err)
	}
	if !got.SettingsOnly {
		t.Fatal("expected settings-only result")
	}
	if got.StreamID != "" {
		t.Fatalf("expected settings-only save not to return a stream id, got %q", got.StreamID)
	}
	if loginHit || listHit || quickAddHit {
		t.Fatalf("expected settings-only save not to hit network, login=%v list=%v quickAdd=%v", loginHit, listHit, quickAddHit)
	}
}

func TestFeedManagerGReaderSettingsEditSaveCmdPreservesFolderPreference(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	remoteID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	folderID, err := database.AddFolder("Existing", "#7aa2f7")
	if err != nil {
		t.Fatalf("AddFolder: %v", err)
	}
	if err := database.SetRemoteFeedFolder(remoteID, folderID); err != nil {
		t.Fatalf("SetRemoteFeedFolder: %v", err)
	}

	fm := NewFeedManagerWithSource(database, config.SourceConfig{
		GReaderURL:      "https://rss.example.com/api/greader.php",
		GReaderLogin:    "alice",
		GReaderPassword: "secret",
	})
	fm.focusRemoteSettingsEdit(db.Feed{
		ID:       remoteID,
		Title:    "Remote Feed",
		URL:      "https://example.com/feed.xml",
		FolderID: folderID,
	})

	msg := fm.saveCmd()()
	got, ok := msg.(RemoteFeedAddedMsg)
	if !ok {
		t.Fatalf("expected RemoteFeedAddedMsg, got %T", msg)
	}
	if got.Err != nil {
		t.Fatalf("expected successful settings save, got error %v", got.Err)
	}
	if !got.SettingsOnly {
		t.Fatal("expected settings-only result")
	}

	assignments, err := database.ListRemoteFeedFolders()
	if err != nil {
		t.Fatalf("ListRemoteFeedFolders returned error: %v", err)
	}
	if gotID := assignments[remoteID]; gotID != folderID {
		t.Fatalf("expected folder preference unchanged after save, want folder %d got %+v", folderID, assignments)
	}
}

type uiRoundTripFunc func(*http.Request) (*http.Response, error)

func (fn uiRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return fn(req)
}

func uiResponseWithBody(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"text/plain"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func uiResponseWithJSON(status int, body string) *http.Response {
	return &http.Response{
		StatusCode: status,
		Header:     http.Header{"Content-Type": []string{"application/json"}},
		Body:       io.NopCloser(bytes.NewBufferString(body)),
	}
}

func TestSettingsAboutActionReturnsBrowserCommand(t *testing.T) {
	m := Model{
		cfg:      config.DefaultConfig(),
		keys:     DefaultKeys,
		settings: newSettings(config.DefaultConfig(), settingsUpdateState{}),
	}
	m.settings.setActiveSection(ssAbout)
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setFocusedField(sfAboutRepo)

	next, cmd := m.handleSettings(tea.KeyMsg{Type: tea.KeyEnter})
	got := next.(Model)

	if cmd == nil {
		t.Fatal("expected ABOUT repository action to return a browser command")
	}
	if got.settings.action != settingsActionNone {
		t.Fatalf("expected ABOUT repository action to be consumed, got %v", got.settings.action)
	}
}

func TestRightKeyReachesContentPane(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = m2.(Model)
	if m.focused != paneArticles {
		t.Fatalf("expected first l to focus articles, got %v", m.focused)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'l'}})
	m = m2.(Model)
	if m.focused != paneContent {
		t.Fatalf("expected second l to focus content, got %v", m.focused)
	}
}

func TestQuitOverlayDoesNotCloseOnQ(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = m2.(Model)
	if m.overlay != overlayQuitConfirm {
		t.Fatalf("expected quit confirm overlay, got %v", m.overlay)
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'q'}})
	m = m2.(Model)
	if m.overlay != overlayQuitConfirm {
		t.Fatalf("expected q to leave quit confirm open, got %v", m.overlay)
	}
}

func TestSummaryUnavailableFromFeedsPane(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{{ID: 1, Title: "Feed One", URL: "https://example.com/feed"}}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds})
	m = m2.(Model)

	articles := []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Content: "Body", PublishedAt: unixTestTime(1710000000)},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articles})
	m = m2.(Model)

	m.focused = paneFeeds
	m2, _ = m.handleMainKey(tea.KeyMsg{Runes: []rune{'s'}})
	m = m2.(Model)

	if m.overlay == overlaySummary {
		t.Fatal("summary overlay should not open from feeds pane")
	}
}

func TestFormatSummaryBodyPreservesParagraphsAndLists(t *testing.T) {
	body := "First paragraph with extra   spacing.\n\n- first bullet item\n- second bullet item\n\n1. first numbered item\n2. second numbered item"

	got := formatSummaryBody(body, 24, false)

	if !strings.Contains(got, "First paragraph with") {
		t.Fatalf("expected formatted paragraph, got %q", got)
	}
	if !strings.Contains(got, "• first bullet item") {
		t.Fatalf("expected bullet formatting, got %q", got)
	}
	if !strings.Contains(got, "1. first numbered item") {
		t.Fatalf("expected numbered list formatting, got %q", got)
	}
	if !strings.Contains(got, "\n\n• first bullet item") {
		t.Fatalf("expected paragraph break before list, got %q", got)
	}
}

func TestFormatSummaryBodySplitsDenseSingleParagraph(t *testing.T) {
	body := "Sentence one explains the setup. Sentence two adds context. Sentence three gives the key point. Sentence four closes it out."

	got := formatSummaryBody(body, 48, false)

	if !strings.Contains(got, "\n\nSentence three gives the key point.") {
		t.Fatalf("expected dense summary to split into short paragraphs, got %q", got)
	}
}

func TestFormatSummaryBodyUsesASCIIBulletsInPlainUI(t *testing.T) {
	body := "- first bullet item\n- second bullet item"

	got := formatSummaryBody(body, 24, true)

	if !strings.Contains(got, "* first bullet item") {
		t.Fatalf("expected ASCII bullet formatting, got %q", got)
	}
	if strings.Contains(got, "• first bullet item") {
		t.Fatalf("expected plain UI to avoid Unicode bullets, got %q", got)
	}
}

func TestArticleCursorMoveKeepsFrameStable(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{{ID: 1, Title: "Feed One", URL: "https://example.com/1"}}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds})
	m = m2.(Model)
	m.focused = paneArticles

	articles := []db.Article{
		{ID: 1, FeedID: 1, Title: "Short title", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000000), Read: false},
		{ID: 2, FeedID: 1, Title: "A much longer article title for width stability testing", Link: "https://example.com/b", Content: "two", PublishedAt: unixTestTime(1710000100), Read: true},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articles})
	m = m2.(Model)

	before := m.View()
	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = m2.(Model)
	after := m.View()

	beforeLines := strings.Split(before, "\n")
	afterLines := strings.Split(after, "\n")
	if len(beforeLines) != len(afterLines) {
		t.Fatalf("frame height changed after article cursor move: before=%d after=%d", len(beforeLines), len(afterLines))
	}
	for i := range beforeLines {
		if lipgloss.Width(beforeLines[i]) != lipgloss.Width(afterLines[i]) {
			t.Fatalf("frame width changed at line %d after article cursor move", i+1)
		}
	}
}

func newArticleFocusTestModel(t *testing.T, cfg config.Config, articles []db.Article) (Model, *db.DB) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}

	m := NewModel(database, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Feed One", URL: "https://example.com/feed", UnreadCount: int64(len(articles))}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)
	m.sidebarCursor = 0
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articles})
	m = m2.(Model)
	m.focused = paneArticles
	return m, database
}

func articleFocusContent() string {
	return strings.Repeat("body line\n", 80)
}

func TestArticleFocusMarksTopArticleReadWhenEnteringArticlesPane(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = true
	m, database := newArticleFocusTestModel(t, cfg, []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000200), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000100), Read: false},
	})
	defer database.Close()

	m.focused = paneFeeds
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected entering articles pane to return a mark-read command")
	}
	if m.focused != paneArticles {
		t.Fatalf("expected focus to move to articles pane, got %v", m.focused)
	}
	if m.articleCursor != 0 {
		t.Fatalf("expected top article to stay focused, got cursor %d", m.articleCursor)
	}

	msg, ok := cmd().(ArticleReadUpdatedMsg)
	if !ok {
		t.Fatalf("expected ArticleReadUpdatedMsg, got %T", msg)
	}
	if msg.ArticleID != 1 {
		t.Fatalf("expected top article 1 to be marked read, got article %d", msg.ArticleID)
	}
	if !msg.Read {
		t.Fatal("expected entering articles pane to mark article read")
	}
	if msg.Advance {
		t.Fatal("expected read-on-focus command not to request cursor advance")
	}
}

func TestArticleLoadDoesNotMarkTopArticleReadBeforeEnteringArticlesPane(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = true
	m, database := newArticleFocusTestModel(t, cfg, []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000200), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000100), Read: false},
	})
	defer database.Close()

	m.focused = paneFeeds
	m2, cmd := m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: m.articles})
	m = m2.(Model)
	if cmd != nil {
		t.Fatal("expected article load outside articles pane not to mark read")
	}
	if m.filteredArticles[0].Read {
		t.Fatal("expected top article to remain unread after passive load")
	}
}

func TestArticleFocusMarksUnreadArticleReadWhenEnabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = true
	m, database := newArticleFocusTestModel(t, cfg, []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000200), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000100), Read: false},
	})
	defer database.Close()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected read-on-focus to return a mark-read command")
	}
	if m.articleCursor != 1 {
		t.Fatalf("expected focus to move to second article, got cursor %d", m.articleCursor)
	}

	msg, ok := cmd().(ArticleReadUpdatedMsg)
	if !ok {
		t.Fatalf("expected ArticleReadUpdatedMsg, got %T", msg)
	}
	if msg.ArticleID != 2 {
		t.Fatalf("expected focused article 2 to be marked read, got article %d", msg.ArticleID)
	}
	if !msg.Read {
		t.Fatal("expected read-on-focus command to mark article read")
	}
	if msg.Advance {
		t.Fatal("expected read-on-focus command not to request cursor advance")
	}
}

func TestArticleFocusDoesNotMarkReadWhenDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = false
	m, database := newArticleFocusTestModel(t, cfg, []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000200), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000100), Read: false},
	})
	defer database.Close()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("expected disabled read-on-focus not to return a mark-read command")
	}
	if m.articleCursor != 1 {
		t.Fatalf("expected focus to still move to second article, got cursor %d", m.articleCursor)
	}
}

func TestArticleFocusDoesNotMarkAlreadyReadArticle(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = true
	m, database := newArticleFocusTestModel(t, cfg, []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000200), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000100), Read: true},
	})
	defer database.Close()

	_, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	if cmd != nil {
		t.Fatal("expected already-read focused article not to dispatch mark-read")
	}
}

func TestArticleFocusReadInUnreadOnlySettlesOnNextArticle(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.MarkReadOnFocus = true
	cfg.Display.DefaultUnreadOnly = true
	m, database := newArticleFocusTestModel(t, cfg, []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000300), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000200), Read: false},
		{ID: 3, FeedID: 1, Title: "Article Three", Link: "https://example.com/c", Content: articleFocusContent(), PublishedAt: unixTestTime(1710000100), Read: false},
	})
	defer database.Close()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected read-on-focus to mark focused article")
	}
	msg, ok := cmd().(ArticleReadUpdatedMsg)
	if !ok {
		t.Fatalf("expected ArticleReadUpdatedMsg, got %T", msg)
	}
	next, _ = m.Update(msg)
	m = next.(Model)

	if len(m.filteredArticles) != 2 {
		t.Fatalf("expected unread-only filter to remove focused read article, got %d articles", len(m.filteredArticles))
	}
	if m.articleCursor != 1 {
		t.Fatalf("expected cursor to settle at same list index, got %d", m.articleCursor)
	}
	if got := m.filteredArticles[m.articleCursor].ID; got != 3 {
		t.Fatalf("expected next unread article to be focused, got article %d", got)
	}
}

func TestArticleReadUpdatedAdvancesToNextArticleInArticlesPane(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Feed One", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)
	m.sidebarCursor = 0

	articles := []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000100), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articles})
	m = m2.(Model)
	m.focused = paneArticles

	m2, cmd := m.Update(ArticleReadUpdatedMsg{ArticleID: 1, Read: true, Advance: true})
	m = m2.(Model)

	if !m.articles[0].Read {
		t.Fatal("expected first article to be marked read")
	}
	if m.articleCursor != 1 {
		t.Fatalf("expected article cursor to advance to next article, got %d", m.articleCursor)
	}
	if m.filteredArticles[m.articleCursor].ID != 2 {
		t.Fatalf("expected second article to become selected, got article %d", m.filteredArticles[m.articleCursor].ID)
	}
	if cmd == nil {
		t.Fatal("expected mark-read update to trigger follow-up commands")
	}
}

func TestArticleReadUpdatedDoesNotAdvanceOutsideArticlesPane(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Feed One", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)
	m.sidebarCursor = 0

	articles := []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000100), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articles})
	m = m2.(Model)
	m.focused = paneContent

	m2, _ = m.Update(ArticleReadUpdatedMsg{ArticleID: 1, Read: true, Advance: false})
	m = m2.(Model)

	if !m.articles[0].Read {
		t.Fatal("expected first article to be marked read")
	}
	if m.articleCursor != 0 {
		t.Fatalf("expected article cursor to stay put outside articles pane, got %d", m.articleCursor)
	}
	if m.filteredArticles[m.articleCursor].ID != 1 {
		t.Fatalf("expected first article to remain selected, got article %d", m.filteredArticles[m.articleCursor].ID)
	}
}

func TestMarkReadKeyTogglesArticleBackToUnread(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Feed One", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)
	m.sidebarCursor = 0
	m.focused = paneContent
	m.articles = []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000100), Read: true},
	}
	m.applyFilter()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	_ = next.(Model)
	if cmd == nil {
		t.Fatal("expected toggle command for read article")
	}

	msg := cmd()
	updateMsg, ok := msg.(ArticleReadUpdatedMsg)
	if !ok {
		t.Fatalf("expected ArticleReadUpdatedMsg, got %T", msg)
	}
	if updateMsg.Read {
		t.Fatal("expected toggle to mark article unread")
	}
	if updateMsg.Advance {
		t.Fatal("expected content-pane toggle to avoid advancing")
	}
}

func TestUnreadOnlyToggleClearsContentWhenNoArticlesRemain(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Feed One", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)
	m.focused = paneArticles
	m.articles = []db.Article{
		{ID: 1, FeedID: 1, Title: "Read Article", Link: "https://example.com/a", Content: "stale content", PublishedAt: unixTestTime(1710000100), Read: true},
	}
	m.applyFilter()
	m.viewport.SetContent(m.renderArticleContent(m.filteredArticles[0]))

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'u'}})
	m = next.(Model)

	if len(m.filteredArticles) != 0 {
		t.Fatalf("expected unread-only filter to hide read article, got %d articles", len(m.filteredArticles))
	}
	if got := m.viewport.View(); strings.Contains(got, "stale content") || strings.Contains(got, "Read Article") {
		t.Fatalf("expected empty content pane after unread-only filter removes all articles, got %q", got)
	}
}

func TestMarkReadKeyAdvancesToNextArticleFromContentPane(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feed := db.Feed{ID: 1, Title: "Feed One", URL: "https://example.com/feed"}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: []db.Feed{feed}})
	m = m2.(Model)
	m.sidebarCursor = 0
	m.focused = paneContent
	m.articles = []db.Article{
		{ID: 1, FeedID: 1, Title: "Article One", Link: "https://example.com/a", Content: "one", PublishedAt: unixTestTime(1710000100), Read: false},
		{ID: 2, FeedID: 1, Title: "Article Two", Link: "https://example.com/b", Content: "two", PublishedAt: unixTestTime(1710000000), Read: false},
	}
	m.applyFilter()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'r'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected mark-read command from content pane")
	}

	msg := cmd()
	updateMsg, ok := msg.(ArticleReadUpdatedMsg)
	if !ok {
		t.Fatalf("expected ArticleReadUpdatedMsg, got %T", msg)
	}
	if !updateMsg.Read {
		t.Fatal("expected mark-read command to mark article read")
	}
	if !updateMsg.Advance {
		t.Fatal("expected content-pane mark-read to advance to the next article")
	}
}

func TestContentPaneClampsViewportOutputToPaneSize(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{{ID: 1, Title: "Feed One", URL: "https://example.com/1"}}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds})
	m = m2.(Model)

	articles := []db.Article{
		{ID: 1, FeedID: 1, Title: "Short title", Link: "https://example.com/a", Content: "one short line", PublishedAt: unixTestTime(1710000000)},
	}
	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: articles})
	m = m2.(Model)

	w := m.articlesPaneWidth()
	bodyH := m.contentBodyHeight()
	bg := m.styles.Theme.Bg

	vp := m.viewport
	vp.Width = w
	vp.Height = bodyH
	vp.Style = lipgloss.NewStyle().Background(bg)
	wantBody := clampView(vp.View(), w, bodyH, bg)

	got := m.renderContentPane()
	if !strings.Contains(got, wantBody) {
		t.Fatalf("expected content pane to include clamped viewport body")
	}
}

func TestContentPaneUsesFullAllocatedHeight(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	got := m.renderContentPane()
	lines := strings.Split(got, "\n")

	if gotH := len(lines); gotH != m.contentPaneOuterHeight() {
		t.Fatalf("expected content pane height %d, got %d", m.contentPaneOuterHeight(), gotH)
	}
	if got := m.contentBodyHeight(); got != m.contentPaneOuterHeight()-1 {
		t.Fatalf("expected content body height to fill pane below header, got %d want %d", got, m.contentPaneOuterHeight()-1)
	}
}

func TestContentDownMovesFocusLineWithoutScrollingWhenVisible(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.focused = paneContent
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     strings.Join([]string{"one", "two", "three", "four", "five", "six"}, "\n\n"),
		PublishedAt: unixTestTime(1710000000),
	})
	start := m.contentFocusLine

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)

	if m.contentFocusLine <= start {
		t.Fatalf("expected content focus line to move down by one, got %d from start %d", m.contentFocusLine, start)
	}
	if m.viewport.YOffset != 0 {
		t.Fatalf("expected viewport not to scroll while focus line is visible, got offset %d", m.viewport.YOffset)
	}
}

func TestContentDownSkipsBlankSpacerLines(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.focused = paneContent
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     "first body line\n\nsecond body line",
		PublishedAt: unixTestTime(1710000000),
	})

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)

	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	if !strings.Contains(lines[m.contentFocusLine], "second body line") {
		t.Fatalf("expected j to move to next readable body line, got line %d: %q in %#v", m.contentFocusLine, lines[m.contentFocusLine], lines)
	}
}

func TestContentFocusLineStartsAtFirstBodyLine(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     "first body line\n\nsecond body line",
		PublishedAt: unixTestTime(1710000000),
	})

	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	if m.contentFocusLine < 0 || m.contentFocusLine >= len(lines) {
		t.Fatalf("content focus line %d outside rendered lines %#v", m.contentFocusLine, lines)
	}
	if !strings.Contains(lines[m.contentFocusLine], "first body line") {
		t.Fatalf("expected focus line to start on first body line, got line %d: %q in %#v", m.contentFocusLine, lines[m.contentFocusLine], lines)
	}
}

func TestContentDownScrollsOneLineOnlyWhenFocusMovesPastBottom(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.focused = paneContent
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     strings.Join([]string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}, "\n\n"),
		PublishedAt: unixTestTime(1710000000),
	})
	m.contentFocusLine = m.contentBodyHeight() - 1 // "four", the bottom visible readable line.
	m.viewport.SetYOffset(0)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'j'}})
	m = next.(Model)

	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	if !strings.Contains(lines[m.contentFocusLine-m.viewport.YOffset], "five") {
		t.Fatalf("expected focus to move to next readable line, got focus=%d offset=%d lines=%#v", m.contentFocusLine, m.viewport.YOffset, lines)
	}
	if m.viewport.YOffset != m.contentFocusLine-m.contentBodyHeight()+1 {
		t.Fatalf("expected viewport to scroll only enough to reveal focus, got offset %d focus %d bodyH %d", m.viewport.YOffset, m.contentFocusLine, m.contentBodyHeight())
	}
}

func TestContentUpScrollsOneLineOnlyWhenFocusMovesPastTop(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.focused = paneContent
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     strings.Join([]string{"one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten"}, "\n\n"),
		PublishedAt: unixTestTime(1710000000),
	})
	m.contentFocusLine = 5 // "two", the top visible readable line.
	m.viewport.SetYOffset(5)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'k'}})
	m = next.(Model)

	lines := strings.Split(ansi.Strip(m.viewport.View()), "\n")
	if !strings.Contains(lines[m.contentFocusLine-m.viewport.YOffset], "one") {
		t.Fatalf("expected focus to move to previous readable line, got focus=%d offset=%d lines=%#v", m.contentFocusLine, m.viewport.YOffset, lines)
	}
	if m.viewport.YOffset != m.contentFocusLine {
		t.Fatalf("expected viewport to scroll only enough to reveal focus, got offset %d focus %d", m.viewport.YOffset, m.contentFocusLine)
	}
}

func TestContentFocusLineUsesVisibleHighlightBackground(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.focused = paneContent
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     strings.Join([]string{"one", "two", "three"}, "\n\n"),
		PublishedAt: unixTestTime(1710000000),
	})
	m.contentFocusLine = 4
	m.ensureContentFocusVisible()

	got := m.renderContentPane()
	want := m.styles.ContentFocusLine.Width(m.articlesPaneWidth()).Render(" one")
	if !strings.Contains(got, want[:min(len(want), 12)]) {
		t.Fatalf("expected content focus line to render with highlight style prefix from %q, got %q", want, got)
	}
}

func TestContentFocusLineCanBeDisabled(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.FocusLine = false
	m := NewModel(nil, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 90, Height: 18})
	m = m2.(Model)
	m.focused = paneContent
	m.setViewportArticle(db.Article{
		Title:       "Readable article",
		Link:        "https://example.com/a",
		Content:     strings.Join([]string{"one", "two", "three"}, "\n\n"),
		PublishedAt: unixTestTime(1710000000),
	})

	body := m.viewport.View()
	got := m.renderContentFocusLine(body, m.articlesPaneWidth(), m.contentBodyHeight(), true)
	if got != body {
		t.Fatalf("expected disabled focus line to leave content body unchanged, got %q want %q", got, body)
	}
}

func TestRenderArticleContentFillsPaneWidth(t *testing.T) {
	m := Model{
		width:  120,
		height: 30,
		styles: BuildStyles(GruvboxLight, "comfortable"),
	}

	got := m.renderArticleContent(db.Article{
		Title:       "Short title",
		Link:        "https://example.com/a",
		Content:     "one short line",
		PublishedAt: unixTestTime(1710000000),
	})

	for i, line := range strings.Split(got, "\n") {
		if lipgloss.Width(line) != m.articlesPaneWidth() {
			t.Fatalf("expected article content line %d to fill pane width %d, got %d", i+1, m.articlesPaneWidth(), lipgloss.Width(line))
		}
	}
}

func TestRenderArticleContentUsesOneCharacterLeftMargin(t *testing.T) {
	m := Model{
		width:  120,
		height: 30,
		styles: BuildStyles(GruvboxLight, "comfortable"),
	}

	got := m.renderArticleContent(db.Article{
		Title:       "Short title",
		Link:        "https://example.com/a",
		Content:     "one short line",
		PublishedAt: unixTestTime(1710000000),
	})

	lines := strings.Split(ansi.Strip(got), "\n")
	for i, line := range lines {
		if line == "" {
			continue
		}
		if !strings.HasPrefix(line, " ") {
			t.Fatalf("expected article content line %d to start with a one-character left margin, got %q", i+1, line)
		}
	}
}

func TestRenderArticleContentKeepsHeaderSingleLineWithinMargins(t *testing.T) {
	m := Model{
		width:  70,
		height: 30,
		styles: BuildStyles(GruvboxLight, "comfortable"),
	}

	publishedAt := unixTestTime(1710000000)
	got := m.renderArticleContent(db.Article{
		Title:       strings.Repeat("Long title ", 12),
		Link:        "https://example.com/this/is/a/very/long/link/that/should/not/wrap/in/the/header",
		Content:     "one short line",
		PublishedAt: publishedAt,
	})

	var nonEmpty []string
	for _, line := range strings.Split(ansi.Strip(got), "\n") {
		if strings.TrimSpace(line) == "" {
			continue
		}
		nonEmpty = append(nonEmpty, line)
	}

	if len(nonEmpty) != 3 {
		t.Fatalf("expected title, meta, and one body line without header wrapping; got %d non-empty lines: %#v", len(nonEmpty), nonEmpty)
	}
	if !strings.HasPrefix(nonEmpty[0], " ") {
		t.Fatalf("expected title line to keep one-character left margin, got %q", nonEmpty[0])
	}
	if !strings.Contains(nonEmpty[0], "…") {
		t.Fatalf("expected long title to truncate instead of wrap, got %q", nonEmpty[0])
	}

	wantMetaPrefix := " " + publishedAt.Format("Mon, 02 Jan 2006 15:04")
	if !strings.HasPrefix(nonEmpty[1], wantMetaPrefix) {
		t.Fatalf("expected second non-empty line to be meta, got %q", nonEmpty[1])
	}
}

func TestThemePickerUsesFullWidthChromeRows(t *testing.T) {
	m := Model{
		width:       120,
		height:      30,
		themeCursor: 7,
		styles:      BuildStyles(GruvboxLight, "comfortable"),
	}

	chrome := newManagerChrome(40, m.styles.Theme, false)
	got := m.renderThemePicker(40, chrome)
	lines := strings.Split(got, "\n")

	if !containsString(got, "THEME") {
		t.Fatalf("expected theme picker header, got %q", got)
	}
	if !containsString(got, "gruvbox-light") {
		t.Fatalf("expected theme picker to include current selection row, got %q", got)
	}
	for i, line := range lines {
		if lipgloss.Width(line) != 40 {
			t.Fatalf("expected theme picker line %d to fill width 40, got %d", i+1, lipgloss.Width(line))
		}
	}
}

func TestFeedsPaneRendersFoldersAndUncategorized(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{
		{ID: 1, Title: "Alpha", URL: "https://example.com/1", FolderID: 10, UnreadCount: 2},
		{ID: 2, Title: "Beta", URL: "https://example.com/2"},
	}
	folders := []db.Folder{{ID: 10, Name: "Tech", Position: 0}}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds, Folders: folders})
	m = m2.(Model)

	pane := m.renderFeedsPane()
	if !containsString(pane, "Tech") {
		t.Fatalf("expected folder name in feed pane: %q", pane)
	}
	if !containsString(pane, "Uncategorized") {
		t.Fatalf("expected uncategorized group in feed pane: %q", pane)
	}
	if !containsString(pane, "1 folders · 2 feeds") {
		t.Fatalf("expected folder/feed footer in pane: %q", pane)
	}
}

func TestFeedsLoadedSelectsFirstFeedInsteadOfFolderByDefault(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Source.GReaderURL = "https://rss.example.com/api/greader.php"
	cfg.Source.GReaderLogin = "alice"
	cfg.Source.GReaderPassword = "secret"

	m := NewModel(database, cfg, "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	remoteFeedID := remoteStableID("feed", "feed/http://example.com/feed.xml")
	m2, _ = m.Update(FeedsLoadedMsg{
		Feeds: []db.Feed{{
			ID:    remoteFeedID,
			Title: "Remote Feed",
			URL:   "https://example.com/feed.xml",
		}},
		Folders: []db.Folder{{ID: 10, Name: "Tech", Position: 0}},
		RemoteStreams: map[int64]string{
			remoteFeedID: "feed/http://example.com/feed.xml",
		},
	})
	m = m2.(Model)

	selected := m.selectedFeed()
	if selected == nil {
		t.Fatal("expected default selection to land on a feed row, got folder selection")
	}
	if selected.ID != remoteFeedID {
		t.Fatalf("expected remote feed %d to be selected, got %d", remoteFeedID, selected.ID)
	}
	if _, ok := m.selectedFolderID(); ok {
		t.Fatal("expected no folder header to be selected by default")
	}
}

func TestFolderSelectionClearsArticlesAndToggleCollapses(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 30})
	m = m2.(Model)

	feeds := []db.Feed{
		{ID: 1, Title: "Alpha", URL: "https://example.com/1", FolderID: 10},
		{ID: 2, Title: "Beta", URL: "https://example.com/2", FolderID: 10},
	}
	folders := []db.Folder{{ID: 10, Name: "Tech", Position: 0}}
	m2, _ = m.Update(FeedsLoadedMsg{Feeds: feeds, Folders: folders})
	m = m2.(Model)

	m2, _ = m.Update(ArticlesLoadedMsg{FeedID: 1, Articles: []db.Article{
		{ID: 1, FeedID: 1, Title: "Article", PublishedAt: unixTestTime(1710000000)},
	}})
	m = m2.(Model)

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = m2.(Model)
	if len(m.filteredArticles) != 0 {
		t.Fatalf("expected folder selection to clear articles, got %d", len(m.filteredArticles))
	}

	m2, _ = m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = m2.(Model)
	if len(m.sidebarRows) != 1 {
		t.Fatalf("expected collapsed folder to hide feed rows, got %d sidebar rows", len(m.sidebarRows))
	}
}

func TestFolderAccentStylesPropagateToFeedsAndArticles(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m.folders = []db.Folder{{ID: 10, Name: "Tech", Color: "#f7768e"}}
	m.feeds = []db.Feed{{ID: 1, Title: "Feed One", URL: "https://example.com/1", FolderID: 10, UnreadCount: 3}}
	m.sidebarRows = []sidebarRow{{kind: rowKindFolder, folderID: 10}, {kind: rowKindFeed, feedID: 1}}
	m.sidebarCursor = 1

	feedStyle := m.feedAccentStyle(m.feeds[0], false)
	if got := feedStyle.GetForeground(); got != lipgloss.Color("#f7768e") {
		t.Fatalf("expected feed accent foreground, got %q", got)
	}
	badgeStyle := m.feedBadgeStyle(m.feeds[0], false)
	if got := badgeStyle.GetForeground(); got != lipgloss.Color("#f7768e") {
		t.Fatalf("expected feed badge accent, got %q", got)
	}

	articleUnread, _, articleSelected, headerActive, _, borderFocus := m.articleRowStyles()
	if got := articleUnread.GetForeground(); got != lipgloss.Color("#f7768e") {
		t.Fatalf("expected article unread accent, got %q", got)
	}
	if got := articleSelected.GetForeground(); got != lipgloss.Color("#f7768e") {
		t.Fatalf("expected article selected accent, got %q", got)
	}
	if got := headerActive.GetBackground(); got != lipgloss.Color("#f7768e") {
		t.Fatalf("expected article header accent background, got %q", got)
	}
	if borderFocus != lipgloss.Color("#f7768e") {
		t.Fatalf("expected article border focus accent, got %q", borderFocus)
	}
}

func TestRetroTerminalThemesIgnoreFolderAccents(t *testing.T) {
	cases := []struct {
		name  string
		theme Theme
	}{
		{"vt100", VT100},
		{"vt52", VT52},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			m := Model{
				styles:  BuildStyles(tc.theme, "comfortable"),
				focused: paneFeeds,
				folders: []db.Folder{{ID: 10, Name: "Tech", Color: "#f7768e"}},
				feeds:   []db.Feed{{ID: 1, Title: "Feed One", URL: "https://example.com/1", FolderID: 10, UnreadCount: 3}},
				sidebarRows: []sidebarRow{
					{kind: rowKindFolder, folderID: 10},
					{kind: rowKindFeed, feedID: 1},
				},
				sidebarCursor: 1,
			}
			if got := m.folderColor(10); got != "" {
				t.Fatalf("folderColor: got %q, want empty", got)
			}
			pink := lipgloss.Color("#f7768e")
			if got := m.feedAccentStyle(m.feeds[0], false).GetForeground(); got == pink {
				t.Fatalf("feed accent foreground still folder pink, got %q", got)
			}
			if got := m.feedBadgeStyle(m.feeds[0], false).GetForeground(); got == pink {
				t.Fatalf("feed badge foreground still folder pink, got %q", got)
			}
			articleUnread, _, articleSelected, headerActive, _, borderFocus := m.articleRowStyles()
			if got := articleUnread.GetForeground(); got == pink {
				t.Fatalf("article unread foreground still folder pink, got %q", got)
			}
			if got := articleSelected.GetForeground(); got == pink {
				t.Fatalf("article selected foreground still folder pink, got %q", got)
			}
			if got := headerActive.GetBackground(); got == pink {
				t.Fatalf("article header active background still folder pink, got %q", got)
			}
			if borderFocus != lipgloss.Color(m.styles.Theme.BorderFocus) {
				t.Fatalf("borderFocus %q, want theme BorderFocus %q", borderFocus, m.styles.Theme.BorderFocus)
			}
		})
	}
}

func TestSidebarSelectedStyleUsesFilledBackground(t *testing.T) {
	m := Model{styles: BuildStyles(CatppuccinMocha, "comfortable"), focused: paneFeeds}

	selected := m.sidebarSelectedStyle("")
	if got := selected.GetBackground(); got == CatppuccinMocha.Bg {
		t.Fatalf("expected selected feed background to differ from theme bg, got %q", got)
	}
	if got := selected.GetBackground(); got != m.styles.FeedItemSelectedFocused.GetBackground() {
		t.Fatalf("expected focused sidebar selection background, got %q", got)
	}
}

func TestSidebarSelectedStyleSoftensWhenFeedsPaneUnfocused(t *testing.T) {
	m := Model{styles: BuildStyles(CatppuccinMocha, "comfortable"), focused: paneArticles}

	selected := m.sidebarSelectedStyle("")
	if got := selected.GetBackground(); got != m.styles.FeedItemSelectedUnfocused.GetBackground() {
		t.Fatalf("expected unfocused sidebar selection background, got %q", got)
	}
	if got := selected.GetBackground(); got == m.styles.FeedItemSelectedFocused.GetBackground() {
		t.Fatalf("expected unfocused selection to differ from focused selection background, got %q", got)
	}
}

func TestSidebarSelectedStyleUsesFolderAccentAsFocusedBackground(t *testing.T) {
	m := Model{styles: BuildStyles(CatppuccinMocha, "comfortable"), focused: paneFeeds}

	selected := m.sidebarSelectedStyle(lipgloss.Color("#f7768e"))
	if got := selected.GetBackground(); got != lipgloss.Color("#f7768e") {
		t.Fatalf("expected focused folder accent background, got %q", got)
	}
}

func TestUpdateCheckedMsgMarksAvailableRelease(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "v1.0.0", false)
	m.settings = newSettings(m.cfg, m.settingsUpdateState())

	next, _ := m.Update(UpdateCheckedMsg{
		Result: update.CheckResult{
			CurrentVersion: "v1.0.0",
			Latest: update.ReleaseInfo{
				Version:     "v1.1.0",
				Summary:     "Faster update flow.",
				PublishedAt: unixTestTime(1710000000),
			},
			Available: true,
		},
		Manual: true,
	})
	m = next.(Model)

	if m.updateState != updateStateAvailable {
		t.Fatalf("expected updateStateAvailable, got %v", m.updateState)
	}
	if m.updateInfo.Version != "v1.1.0" {
		t.Fatalf("expected latest version v1.1.0, got %q", m.updateInfo.Version)
	}
	if !m.updateInfoFresh {
		t.Fatal("expected fresh update check to mark release info as fresh")
	}
	if m.settings.update.latestVersion != "v1.1.0" {
		t.Fatalf("expected settings update state to sync latest version, got %q", m.settings.update.latestVersion)
	}
	if !m.settings.update.latestIsFresh {
		t.Fatal("expected settings update state to mark latest version as fresh")
	}
	if m.cfg.Updates.AvailableVersion != "v1.1.0" {
		t.Fatalf("expected available update to persist in config, got %q", m.cfg.Updates.AvailableVersion)
	}
}

func TestNewModelDoesNotShowBannerFromCachedUpdate(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Skip("cannot open DB:", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Updates.AvailableVersion = "v1.1.0"
	cfg.Updates.AvailableSummary = "Faster update flow."
	cfg.Updates.AvailablePublished = 1710000000

	m := NewModel(database, cfg, "v1.0.0", false)

	if m.updateState == updateStateAvailable {
		t.Fatalf("expected cached update NOT to surface banner; got updateStateAvailable")
	}
	if m.updateInfo.Version != "" {
		t.Fatalf("expected no cached update info to populate; got %q", m.updateInfo.Version)
	}
	if m.cfg.Updates.AvailableVersion != "" {
		t.Fatalf("expected cached available_version to be cleared, got %q", m.cfg.Updates.AvailableVersion)
	}
}

func TestSettingsViewRendersUpdateActions(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{
		currentVersion: "v1.0.0",
		state:          updateStateAvailable,
		latestVersion:  "v1.1.0",
		summary:        "Faster update flow.",
	})
	s.setActiveSection(ssUpdates)
	chrome := newManagerChrome(62, CatppuccinMocha, false)
	view := s.View(62, 40, chrome)

	if !containsString(view, "CATEGORIES") {
		t.Fatalf("expected categories pane in settings view: %q", view)
	}
	if !containsString(view, "Current version") {
		t.Fatalf("expected current version row in settings view: %q", view)
	}
	if !containsString(view, "Update now") {
		t.Fatalf("expected update action in settings view: %q", view)
	}
}

func TestSettingsViewHidesUpdateNowWhenManualInstallRequired(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{
		currentVersion: "v1.0.0",
		state:          updateStateNeedsElevation,
		latestVersion:  "v1.1.0",
		manualCommand:  "sudo cp /tmp/tide /usr/local/bin/tide",
		summary:        "Requires elevation.",
	})
	s.setActiveSection(ssUpdates)
	chrome := newManagerChrome(62, CatppuccinMocha, false)
	view := ansi.Strip(s.View(62, 40, chrome))

	if !strings.Contains(view, "Ignore") {
		t.Fatalf("expected Ignore when newer release needs manual install, got %q", view)
	}
	if !strings.Contains(view, "sudo cp") {
		t.Fatalf("expected manual command in view, got %q", view)
	}
	if strings.Contains(view, "Update now") {
		t.Fatalf("did not expect Update now when manual install is required, got %q", view)
	}
}

func TestSettingsViewHidesInstallWhenAlreadyOnLatest(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{
		currentVersion: "v1.1.0",
		state:          updateStateIdle,
		latestVersion:  "v1.1.0",
		summary:        "Nothing new.",
	})
	s.setActiveSection(ssUpdates)
	chrome := newManagerChrome(62, CatppuccinMocha, false)
	view := ansi.Strip(s.View(62, 40, chrome))

	if strings.Contains(view, "Update now") {
		t.Fatalf("did not expect Update now when already on latest: %q", view)
	}
	if strings.Contains(view, "Ignore") {
		t.Fatalf("did not expect Ignore action when already on latest: %q", view)
	}
}

func TestSettingsViewShowsLatestVersionLabelForRestoredRelease(t *testing.T) {
	cfg := config.DefaultConfig()
	s := newSettings(cfg, settingsUpdateState{
		currentVersion: "v1.0.0",
		state:          updateStateAvailable,
		latestVersion:  "v1.1.0",
		latestIsFresh:  false,
		summary:        "Faster update flow.",
	})
	s.setActiveSection(ssUpdates)
	chrome := newManagerChrome(62, CatppuccinMocha, false)
	view := s.View(62, 40, chrome)

	if !containsString(view, "Latest version") {
		t.Fatalf("expected latest version label in settings view: %q", view)
	}
}

func TestPreviewManualUpdateOpensSettingsWithManualBlock(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "v1.0.0", true)
	if !m.previewManualUpdateUI {
		t.Fatal("expected preview flag set")
	}
	m2, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 40})
	m = m2.(Model)
	if m.overlay != overlaySettings {
		t.Fatalf("expected settings overlay, got %v", m.overlay)
	}
	view := ansi.Strip(m.View())
	if !strings.Contains(view, "Copy Command") {
		t.Fatalf("expected Copy Command in view, got %q", view)
	}
	if !strings.Contains(view, "curl -fsSL") {
		t.Fatalf("expected preview install command in view, got %q", view)
	}
}

func containsString(s, sub string) bool {
	return len(s) >= len(sub) && (s == sub || len(s) > 0 && containsSubstring(s, sub))
}

func containsSubstring(s, sub string) bool {
	for i := 0; i <= len(s)-len(sub); i++ {
		if s[i:i+len(sub)] == sub {
			return true
		}
	}
	return false
}

func unixTestTime(ts int64) time.Time {
	return time.Unix(ts, 0)
}

func TestFormatArticleBodyWrapsParagraphsAndBullets(t *testing.T) {
	body := "This is a long paragraph that should wrap cleanly across multiple lines without turning into one unreadable run-on sentence.\n\n- first bullet point with a bit more detail\n- second bullet point\n\n> quoted line here"
	got := formatArticleBody(body, 24, false)

	if !containsString(got, "\n") {
		t.Fatalf("expected wrapped output, got %q", got)
	}
	if !containsString(got, "• first bullet point") {
		t.Fatalf("expected bullet formatting, got %q", got)
	}
	if !containsString(got, "│ quoted line here") {
		t.Fatalf("expected quote formatting, got %q", got)
	}
}
