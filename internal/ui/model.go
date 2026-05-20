package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"time"
	"unicode"

	md "github.com/JohannesKaufmann/html-to-markdown"
	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/ai"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/allisonhere/tide/internal/feed"
	"github.com/allisonhere/tide/internal/greader"
	"github.com/allisonhere/tide/internal/update"
)

// ── Enums ────────────────────────────────────────────────────────────────────

type pane int

const (
	paneFeeds pane = iota
	paneArticles
	paneContent
)

type sidebarRowKind int

const (
	rowKindFolder sidebarRowKind = iota
	rowKindFeed
)

type sidebarRow struct {
	kind     sidebarRowKind
	folderID int64
	feedID   int64
}

type overlayMode int

const (
	overlayNone overlayMode = iota
	overlayQuitConfirm
	overlaySearch
	overlayThemePicker
	overlayFeedManager
	overlayHelp
	overlayFetchError // fetch-error details for a single feed
	overlaySettings
	overlayUpdateConfirm
	overlayContentSearch
	overlaySummary
)

type updateState int

const (
	updateStateIdle updateState = iota
	updateStateChecking
	updateStateAvailable
	updateStateDownloading
	updateStateInstalling
	updateStateInstalled
	updateStateNeedsElevation
	updateStateError
)

// ── Model ────────────────────────────────────────────────────────────────────

type Model struct {
	db  *db.DB
	cfg config.Config

	// Runtime services are kept on the model so commands can snapshot what they need before running async. -allie
	currentVersion string
	updater        *update.Updater

	// Layout and focus are the root of every render decision; pane state should stay independent of overlays. -allie
	width, height int
	focused       pane

	// Feed pane state owns the sidebar projection, while feeds/folders remain the source data from DB + remote source. -allie
	// Feed pane
	feeds            []db.Feed
	folders          []db.Folder
	sidebarRows      []sidebarRow
	sidebarCursor    int
	collapsedFolders map[int64]bool

	// Article pane keeps the unfiltered cache and current filtered projection so search/unread toggles do not re-query. -allie
	// Article pane
	articles         []db.Article
	filteredArticles []db.Article
	articleCursor    int
	listOffset       int
	searchQuery      string
	showUnreadOnly   bool

	// Content pane
	viewport             viewport.Model
	contentLinks         []string
	contentLinkIdx       int
	contentArticleID     int64
	contentFocusLine     int
	contentLineCount     int
	contentFocusable     []bool
	contentSearchInput   textinput.Model
	contentSearchQuery   string
	contentSearchMatches []int
	contentSearchIdx     int

	// Help overlay
	helpVP viewport.Model

	// Overlays / inputs
	overlay     overlayMode
	searchInput textinput.Model

	// Theme
	confirmedTheme int
	activeTheme    int
	styles         Styles
	themeCursor    int

	// Feed manager (delegate)
	feedManager FeedManager

	// Status
	statusMsg string
	statusErr bool

	// Fetch error details overlay
	lastFetchError *feed.FetchResult

	// Async
	refreshing  map[int64]bool
	spinner     spinner.Model
	mdConverter *md.Converter

	// Startup selection is deferred until FeedsLoadedMsg so feed-manager saves can select rows after reload. -allie
	firstLoad           bool  // true until the initial FeedsLoadedMsg is processed
	pendingSelectFeedID int64 // select this feed when FeedsLoadedMsg arrives
	keys                KeyMap

	// Settings overlay
	settings Settings

	// Optional Google Reader-compatible source
	greaderClient  *greader.Client
	greaderStreams map[int64]string

	// Update fields mirror Settings state so background checks, prompts, and modal actions share one lifecycle. -allie
	// Update flow
	updateState          updateState
	updateInfo           update.ReleaseInfo
	updateInfoFresh      bool
	downloadedUpdate     *update.DownloadedAsset
	updateInstall        update.InstallResult
	updateErr            string
	updateDismissed      bool
	pendingUpdateInstall bool

	// Dev-only: launch with Settings > Updates showing the manual-install preview; avoids persisting demo update state.
	previewManualUpdateUI bool

	// AI summary overlay
	summarizer        ai.Summarizer // nil when not configured
	summaryArticle    db.Article
	summaryGenerating bool
	summaryErr        string
}

func NewModel(database *db.DB, cfg config.Config, currentVersion string, previewManualUpdate bool) Model {
	merged, themeIdx := MergedThemeFromConfig(cfg)

	si := textinput.New()
	si.Placeholder = "search articles..."
	si.CharLimit = 100

	csi := textinput.New()
	csi.Placeholder = "find in article..."
	csi.CharLimit = 100

	sp := spinner.New()
	if ThemeUsesASCII(merged.Name) {
		sp.Spinner = spinner.Line
	} else {
		sp.Spinner = spinner.Dot
	}
	// Leave Style empty so frames are plain text; foreground-only styling caused holes
	// when composed with StatusSpinner.Render on the status bar. Sidebar/summary wrap rows
	// with their own styles.
	sp.Style = lipgloss.NewStyle()

	summarizer, _ := ai.New(cfg.AI)

	m := Model{
		db:                    database,
		cfg:                   cfg,
		currentVersion:        currentVersion,
		previewManualUpdateUI: previewManualUpdate,
		updater:               update.New(),
		focused:               paneFeeds,
		confirmedTheme:        themeIdx,
		activeTheme:           themeIdx,
		styles:                BuildStyles(merged, cfg.Display.Density),
		feedManager:           NewFeedManager(database),
		searchInput:           si,
		spinner:               sp,
		refreshing:            make(map[int64]bool),
		collapsedFolders:      map[int64]bool{},
		mdConverter:           md.NewConverter("", true, nil),
		greaderStreams:        map[int64]string{},
		firstLoad:             true,
		keys:                  DefaultKeys,
		summarizer:            summarizer,
		showUnreadOnly:        cfg.Display.DefaultUnreadOnly,
		contentLinkIdx:        -1,
		contentSearchInput:    csi,
		contentSearchIdx:      -1,
	}
	m.resetSourceClient()
	m.restoreCachedUpdateState()
	if previewManualUpdate {
		m.applyManualUpdatePreview()
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadFeedsCmd(), m.spinner.Tick}
	if !m.previewManualUpdateUI {
		if cmd := m.maybeCheckForUpdatesCmd(false); cmd != nil {
			cmds = append(cmds, cmd)
		}
	}
	return tea.Batch(cmds...)
}

// ── Update ───────────────────────────────────────────────────────────────────

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Update handles async results before key routing so completed commands cannot be swallowed by modal focus. -allie
	switch msg := msg.(type) {

	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
		m.viewport = viewport.New(m.contentBodyWidth(), m.contentBodyHeight())
		m.viewport.Style = lipgloss.NewStyle()
		if len(m.filteredArticles) > 0 {
			m.setViewportArticle(m.filteredArticles[m.articleCursor])
			m.ensureContentFocusVisible()
		}
		if m.overlay == overlayHelp {
			m.resetHelpVP()
		}
		if m.overlay == overlayFeedManager {
			fm := m.feedManager
			fm.syncTextInputWidthsForRightPane(feedManagerRightPaneWidth(m.width))
			m.feedManager = fm
		}
		return m, nil

	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd

	case StatusClearMsg:
		m.statusMsg = ""
		m.statusErr = false
		return m, nil

	case UpdateCheckedMsg:
		m.updateErr = ""
		m.cfg.Updates.LastCheckedUnix = time.Now().Unix()
		if msg.Err != nil {
			m.pendingUpdateInstall = false
			m.updateState = updateStateError
			m.updateErr = msg.Err.Error()
			m.syncSettingsUpdateState()
			config.Save(m.cfg) //nolint:errcheck
			if msg.Manual {
				m.setStatus("update check failed: "+msg.Err.Error(), true)
				return m, m.clearStatusCmd()
			}
			return m, nil
		}

		m.updateInfo = msg.Result.Latest
		m.updateInfoFresh = true
		m.updateDismissed = msg.Result.Latest.Version != "" && msg.Result.Latest.Version == m.cfg.Updates.DismissedVersion
		if msg.Result.Available {
			m.updateState = updateStateAvailable
			m.cfg.Updates.AvailableVersion = msg.Result.Latest.Version
			m.cfg.Updates.AvailableSummary = msg.Result.Latest.Summary
			m.cfg.Updates.AvailablePublished = msg.Result.Latest.PublishedAt.Unix()
			m.syncSettingsUpdateState()
			config.Save(m.cfg) //nolint:errcheck
			if m.pendingUpdateInstall {
				m.pendingUpdateInstall = false
				if !m.updateDismissed {
					m.updateState = updateStateDownloading
					m.syncSettingsUpdateState()
					return m, m.downloadUpdateCmd(m.updateInfo)
				}
			}
			if !m.updateDismissed && (msg.Manual || update.IsStableVersion(m.currentVersion)) {
				return m, nil
			}
			return m, nil
		}

		m.updateState = updateStateIdle
		m.pendingUpdateInstall = false
		m.updateDismissed = false
		m.cfg.Updates.DismissedVersion = ""
		m.clearCachedAvailableUpdate()
		config.Save(m.cfg) //nolint:errcheck
		m.syncSettingsUpdateState()
		if msg.Manual {
			m.setStatus("Tide is up to date", false)
			return m, m.clearStatusCmd()
		}
		return m, nil

	case UpdateDownloadedMsg:
		if msg.Err != nil {
			m.updateState = updateStateError
			m.updateErr = msg.Err.Error()
			m.syncSettingsUpdateState()
			m.setStatus("update download failed: "+msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		m.downloadedUpdate = &msg.Asset
		m.updateState = updateStateInstalling
		m.syncSettingsUpdateState()
		return m, m.installUpdateCmd(msg.Asset)

	case UpdateInstalledMsg:
		if msg.Err != nil {
			m.updateState = updateStateError
			m.updateErr = msg.Err.Error()
			m.syncSettingsUpdateState()
			m.setStatus("update failed: "+msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		m.updateInstall = msg.Result
		m.downloadedUpdate = nil
		if msg.Result.RequiresManual {
			m.updateState = updateStateNeedsElevation
			m.syncSettingsUpdateState()
			m.setStatus("update downloaded; admin permission required", true)
			return m, m.clearStatusCmd()
		}
		m.updateState = updateStateInstalled
		m.updateDismissed = false
		m.cfg.Updates.DismissedVersion = ""
		m.clearCachedAvailableUpdate()
		config.Save(m.cfg) //nolint:errcheck
		m.syncSettingsUpdateState()
		m.setStatus("Tide updated to "+msg.Result.Version+m.styles.InlineMidDot()+"restart when ready", false)
		return m, m.clearStatusCmd()

	case RestartedMsg:
		if msg.Err != nil {
			m.setStatus(msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		return m, tea.Quit

	case FeedsLoadedMsg:
		// Reloads preserve the user's sidebar intent, unless a save flow requested a specific feed selection. -allie
		if msg.Err != nil && len(msg.Feeds) == 0 && len(msg.Folders) == 0 && len(msg.RemoteStreams) == 0 {
			m.greaderStreams = map[int64]string{}
			m.feeds = nil
			m.folders = nil
			m.rebuildSidebar()
			m.clearArticles()
			m.setStatus(msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		prevKind, prevID := m.currentSidebarSelection()
		m.feeds = msg.Feeds
		m.folders = msg.Folders
		m.greaderStreams = msg.RemoteStreams
		if m.greaderStreams == nil {
			m.greaderStreams = map[int64]string{}
		}
		statusCmd := tea.Cmd(nil)
		if msg.Err != nil {
			m.setStatus(msg.Err.Error(), true)
			statusCmd = m.clearStatusCmd()
		}
		m.rebuildSidebar()
		isFirstLoad := m.firstLoad
		m.firstLoad = false
		if m.pendingSelectFeedID != 0 {
			for i, row := range m.sidebarRows {
				if row.kind == rowKindFeed && row.feedID == m.pendingSelectFeedID {
					m.sidebarCursor = i
					break
				}
			}
			m.pendingSelectFeedID = 0
		} else if prevID != 0 {
			for i, row := range m.sidebarRows {
				if row.kind == prevKind {
					if row.kind == rowKindFeed && row.feedID == prevID {
						m.sidebarCursor = i
						break
					}
					if row.kind == rowKindFolder && row.folderID == prevID {
						m.sidebarCursor = i
						break
					}
				}
			}
		} else {
			for i, row := range m.sidebarRows {
				if row.kind == rowKindFeed {
					m.sidebarCursor = i
					break
				}
			}
		}
		m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))
		if m.overlay == overlayFeedManager && m.feedManager.mode == fmList {
			m.feedManager.setData(m.feeds, m.folders)
			if feed := m.selectedFeed(); feed != nil {
				m.feedManager.selectFeed(feed.ID)
			} else if folderID, ok := m.selectedFolderID(); ok && folderID >= 0 {
				m.feedManager.selectFolder(folderID)
			}
		}
		if len(m.feeds) == 0 {
			m.clearArticles()
			return m, nil
		}
		cmds := []tea.Cmd{}
		if selected := m.selectedFeed(); selected != nil {
			cmds = append(cmds, m.loadUnreadArticlesCmd(selected.ID))
		} else {
			m.clearArticles()
		}
		// Only auto-refresh on startup — manual refresh uses f/F keys.
		if isFirstLoad {
			for _, f := range m.feeds {
				if m.isRemoteFeed(f.ID) {
					continue
				}
				cmds = append(cmds, m.refreshFeedCmd(f.ID, f.URL, false))
			}
		}
		if statusCmd != nil {
			cmds = append(cmds, statusCmd)
		}
		return m, tea.Batch(cmds...)

	case ArticlesLoadedMsg:
		// Ignore stale article loads from a previously selected feed; async loads can race with sidebar movement. -allie
		if msg.Err != nil {
			if selected := m.selectedFeed(); selected != nil && msg.FeedID == selected.ID {
				m.clearArticles()
			}
			m.setStatus(msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		if selected := m.selectedFeed(); selected != nil && msg.FeedID == selected.ID {
			m.articles = msg.Articles
			m.applyFilter()
			m.articleCursor = clamp(m.articleCursor, 0, max(0, len(m.filteredArticles)-1))
			m.listOffset = 0
			var cmd tea.Cmd
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
				cmd = m.maybeFetchArticleContentCmd(m.filteredArticles[m.articleCursor])
			}
			return m, cmd
		}
		return m, nil

	case FeedRefreshedMsg:
		delete(m.refreshing, msg.FeedID)
		if msg.Err != nil {
			r := msg.Result
			if r != nil {
				friendly := r.FriendlyMessage()
				if r.HasDetails() && msg.Manual {
					// Show error details overlay for manually triggered single-feed refresh.
					m.lastFetchError = r
					m.overlay = overlayFetchError
					m.setStatus(fmt.Sprintf("refresh failed: %s", friendly), true)
				} else {
					m.setStatus(fmt.Sprintf("refresh failed: %s", friendly), true)
					return m, m.clearStatusCmd()
				}
			} else {
				m.setStatus(fmt.Sprintf("refresh failed: %v", msg.Err), true)
				return m, m.clearStatusCmd()
			}
			return m, nil
		}
		cmds := []tea.Cmd{}
		// Successful refreshes update storage first; the visible pane reloads from DB afterward to keep filters consistent. -allie
		for _, a := range msg.Articles {
			if err := m.db.UpsertArticle(a); err != nil {
				continue
			}
		}
		now := time.Now()
		currentFeed, err := m.db.GetFeed(msg.FeedID)
		if err == nil {
			title := strings.TrimSpace(msg.Title)
			if title == "" {
				title = currentFeed.Title
			}
			description := strings.TrimSpace(msg.Description)
			if description == "" {
				description = currentFeed.Description
			}
			faviconURL := strings.TrimSpace(msg.FaviconURL)
			if faviconURL == "" {
				faviconURL = currentFeed.FaviconURL
			}
			if err := m.db.UpdateFeedMeta(msg.FeedID, title, description, faviconURL, now); err != nil {
				fmt.Fprintf(os.Stderr, "feed meta update failed (feed %d): %v\n", msg.FeedID, err)
			}
		} else {
			if err := m.db.TouchFeedFetched(msg.FeedID, now); err != nil {
				fmt.Fprintf(os.Stderr, "feed touch failed (feed %d): %v\n", msg.FeedID, err)
			}
		}
		if r := msg.Result; r != nil && r.SuggestURLUpdate {
			if err := m.db.UpdateFeedURL(msg.FeedID, r.SuggestedURL); err != nil {
				m.setStatus(fmt.Sprintf("URL update failed: %v", err), true)
			} else {
				m.setStatus(fmt.Sprintf("feed URL updated to %s", r.SuggestedURL), false)
			}
		}
		if selected := m.selectedFeed(); selected != nil && msg.FeedID == selected.ID {
			cmds = append(cmds, m.loadUnreadArticlesCmd(msg.FeedID))
		}
		cmds = append(cmds, m.loadFeedsCmd())
		cmds = append(cmds, m.clearStatusCmd())
		return m, tea.Batch(cmds...)

	case FeedSavedMsg:
		m.feedManager.busy = false
		m.feedManager.busyMsg = ""
		if msg.Err != nil {
			m.feedManager.statusMsg = fmt.Sprintf("SAVE FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("save failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.feedManager = m.newFeedManager()
		m.feedManager.mode = fmList
		m.feedManager.selectFeed(msg.Feed.ID)
		m.feedManager.statusMsg = fmt.Sprintf("SAVED: %s", strings.ToUpper(msg.Feed.Title))
		m.setStatus(fmt.Sprintf("saved: %s", msg.Feed.Title), false)
		m.pendingSelectFeedID = msg.Feed.ID
		return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())

	case RemoteFeedAddedMsg:
		m.feedManager.busy = false
		m.feedManager.busyMsg = ""
		if msg.Err != nil {
			m.feedManager.statusMsg = fmt.Sprintf("SAVE FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("save failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.cfg.Source.GReaderURL = msg.Source.GReaderURL
		m.cfg.Source.GReaderLogin = msg.Source.GReaderLogin
		m.cfg.Source.GReaderPassword = msg.Source.GReaderPassword
		saveErr := config.Save(m.cfg)
		m.resetSourceClient()
		m.feedManager = m.newFeedManager()
		m.feedManager.mode = fmList
		if saveErr != nil {
			m.feedManager.statusMsg = fmt.Sprintf("GREADER CONFIG SAVE FAILED: %v", saveErr)
			m.setStatus(fmt.Sprintf("greader config save failed: %v", saveErr), true)
			return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())
		}
		if msg.SettingsOnly {
			m.feedManager.statusMsg = "SAVED GREADER SETTINGS"
			m.setStatus("saved greader settings", false)
			return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())
		}
		if msg.StreamID == "" {
			m.feedManager.statusMsg = fmt.Sprintf("CONNECTED GREADER%s%d FEEDS", m.styles.InlineMidDot(), msg.FeedCount)
			m.setStatus(fmt.Sprintf("connected greader: %d feeds", msg.FeedCount), false)
			return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())
		}
		title := strings.TrimSpace(msg.Title)
		if title == "" {
			title = "Google Reader feed"
		}
		m.feedManager.statusMsg = fmt.Sprintf("ADDED: %s", strings.ToUpper(title))
		m.setStatus(fmt.Sprintf("added: %s", title), false)
		if msg.StreamID != "" {
			m.pendingSelectFeedID = remoteStableID("feed", msg.StreamID)
		}
		return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())

	case FeedDeletedMsg:
		m.feedManager.busy = false
		m.feedManager.busyMsg = ""
		if msg.Err != nil {
			m.feedManager.statusMsg = fmt.Sprintf("DELETE FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("delete failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.sidebarCursor = 0
		m.articleCursor = 0
		m.clearArticles()
		m.feedManager = m.newFeedManager()
		m.feedManager.mode = fmList
		m.feedManager.statusMsg = "DELETED FEED"
		return m, m.loadFeedsCmd()

	case FolderSavedMsg:
		m.feedManager.busy = false
		m.feedManager.busyMsg = ""
		if msg.Err != nil {
			m.feedManager.statusMsg = fmt.Sprintf("SAVE FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("save failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.setStatus(fmt.Sprintf("saved folder: %s", msg.Folder.Name), false)
		m.feedManager = m.newFeedManager()
		m.feedManager.mode = fmList
		m.feedManager.selectFolder(msg.Folder.ID)
		m.feedManager.statusMsg = fmt.Sprintf("SAVED FOLDER: %s", strings.ToUpper(msg.Folder.Name))
		return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())

	case FolderDeletedMsg:
		m.feedManager.busy = false
		m.feedManager.busyMsg = ""
		if msg.Err != nil {
			m.feedManager.statusMsg = fmt.Sprintf("DELETE FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("delete failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.feedManager = m.newFeedManager()
		m.feedManager.mode = fmList
		m.feedManager.statusMsg = "DELETED FOLDER"
		return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())

	case OPMLImportedMsg:
		m.feedManager.busy = false
		m.feedManager.busyMsg = ""
		if msg.Err != nil {
			m.feedManager.statusMsg = fmt.Sprintf("IMPORT FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("import failed: %v", msg.Err), true)
		}
		m.feedManager = m.newFeedManager()
		m.feedManager.mode = fmList
		if msg.Err == nil {
			m.setStatus(fmt.Sprintf("imported %d feeds", msg.Count), false)
			m.feedManager.statusMsg = fmt.Sprintf("IMPORTED %d FEEDS", msg.Count)
		}
		return m, tea.Batch(m.loadFeedsCmd(), m.clearStatusCmd())

	case OPMLExportedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("export failed: %v", msg.Err), true)
		} else {
			m.setStatus(fmt.Sprintf("exported to %s", msg.Path), false)
		}
		return m, m.clearStatusCmd()

	case ArticleReadUpdatedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("mark read failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}

		for i := range m.articles {
			if m.articles[i].ID == msg.ArticleID {
				m.articles[i].Read = msg.Read
				break
			}
		}
		if msg.FeedID != 0 && msg.WasRead != msg.Read {
			delta := int64(1)
			if msg.Read {
				delta = -1
			}
			m.adjustFeedUnreadCount(msg.FeedID, delta)
		}
		m.applyFilter()

		if len(m.filteredArticles) == 0 {
			m.articleCursor = 0
			m.listOffset = 0
			m.clearViewportArticle()
			return m, nil
		}

		if idx := m.indexOfFilteredArticle(msg.ArticleID); msg.Advance && idx >= 0 && idx == m.articleCursor && idx < len(m.filteredArticles)-1 {
			m.articleCursor = idx + 1
			visible := max(1, m.articleRowsVisible())
			if m.articleCursor >= m.listOffset+visible {
				m.listOffset = m.articleCursor - visible + 1
			}
		} else {
			m.articleCursor = clamp(m.articleCursor, 0, max(0, len(m.filteredArticles)-1))
			maxOffset := max(0, len(m.filteredArticles)-1)
			m.listOffset = clamp(m.listOffset, 0, maxOffset)
		}

		current := m.filteredArticles[m.articleCursor]
		m.setViewportArticle(current)

		if current.ID != msg.ArticleID || !msg.Read {
			return m, m.maybeFetchArticleContentCmd(current)
		}
		return m, nil

	case FeedsReadUpdatedMsg:
		if len(msg.FeedIDs) > 0 {
			m.markFeedsReadInMemory(msg.FeedIDs)
		}
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("mark read failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		return m, nil

	case ArticleContentFetchedMsg:
		if msg.Err != nil {
			return m, nil
		}
		if !m.articleIsRemote(msg.ArticleID) {
			if err := m.db.UpdateArticleContent(msg.ArticleID, msg.Content); err != nil {
				return m, nil
			}
		}
		for i := range m.articles {
			if m.articles[i].ID == msg.ArticleID {
				m.articles[i].Content = msg.Content
			}
		}
		m.applyFilter()
		for i := range m.filteredArticles {
			if m.filteredArticles[i].ID == msg.ArticleID && i == m.articleCursor {
				m.setViewportArticle(m.filteredArticles[i])
			}
		}
		return m, nil

	case AISummaryFetchedMsg:
		m.summaryGenerating = false
		if msg.Err != nil {
			m.summaryErr = msg.Err.Error()
			return m, nil
		}
		if !m.articleIsRemote(msg.ArticleID) {
			if err := m.db.SaveSummary(msg.ArticleID, msg.Summary); err != nil {
				fmt.Fprintf(os.Stderr, "save summary failed (article %d): %v\n", msg.ArticleID, err)
				m.setStatus(fmt.Sprintf("summary not saved: %v", err), true)
			}
		}
		var markReadArticle *db.Article
		for i := range m.articles {
			if m.articles[i].ID == msg.ArticleID {
				m.articles[i].Summary = msg.Summary
				if m.cfg.AI.MarkReadOnSummarize && !m.articles[i].Read {
					a := m.articles[i]
					markReadArticle = &a
				}
			}
		}
		m.applyFilter()
		if m.summaryArticle.ID == msg.ArticleID {
			m.summaryArticle.Summary = msg.Summary
		}
		if markReadArticle != nil {
			return m, m.setArticleReadCmd(*markReadArticle, true, false)
		}
		return m, nil

	case SummarySavedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("save failed: %v", msg.Err), true)
		} else {
			m.setStatus("saved → "+msg.Path, false)
		}
		return m, m.clearStatusCmd()

	case ClipboardCopiedMsg:
		if msg.Err != nil {
			m.setStatus("copy failed: "+msg.Err.Error(), true)
		} else {
			m.setStatus("copied to clipboard", false)
		}
		return m, m.clearStatusCmd()

	case ErrMsg:
		m.setStatus(msg.Err.Error(), true)
		return m, m.clearStatusCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		if m.overlay == overlayFeedManager {
			return m.handleFeedManager(msg)
		}
		if m.overlay == overlaySettings {
			return m.handleSettings(msg)
		}
	}

	return m, nil
}

func (m Model) handleKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// Modal overlays get first claim on keys so main-pane shortcuts cannot leak through dialogs. -allie
	// Overlay / window takes priority
	if m.overlay != overlayNone {
		return m.handleOverlayKey(msg)
	}
	return m.handleMainKey(msg)
}

func (m Model) handleMainKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Quit):
		if !m.cfg.Display.ConfirmQuit {
			return m, tea.Quit
		}
		m.overlay = overlayQuitConfirm
		return m, nil

	case keyMatches(msg, m.keys.Help):
		m.overlay = overlayHelp
		m.resetHelpVP()
		return m, nil

	case keyMatches(msg, m.keys.FeedManager):
		m.overlay = overlayFeedManager
		m.feedManager = m.newFeedManager()
		return m, nil

	case keyMatches(msg, m.keys.Add):
		return m.openFeedAddDialog(), nil

	case keyMatches(msg, m.keys.ThemePicker):
		m.overlay = overlayThemePicker
		m.themeCursor = m.confirmedTheme
		return m, nil

	case keyMatches(msg, m.keys.Search):
		m.overlay = overlaySearch
		m.searchInput.Reset()
		m.searchInput.Focus()
		return m, nil

	case keyMatches(msg, m.keys.UnreadOnly):
		if m.focused == paneFeeds {
			return m, nil
		}
		// Preserve the current article when the filter toggles, falling back only if that article disappears. -allie
		m.showUnreadOnly = !m.showUnreadOnly
		var currentID int64
		if len(m.filteredArticles) > 0 {
			currentID = m.filteredArticles[m.articleCursor].ID
		}
		m.applyFilter()
		if idx := m.indexOfFilteredArticle(currentID); idx >= 0 {
			m.articleCursor = idx
		} else {
			m.articleCursor = clamp(m.articleCursor, 0, max(0, len(m.filteredArticles)-1))
		}
		if m.showUnreadOnly {
			m.setStatus("showing unread only", false)
		} else {
			m.setStatus("showing all articles", false)
		}
		if len(m.filteredArticles) > 0 {
			m.setViewportArticle(m.filteredArticles[m.articleCursor])
		} else {
			m.clearViewportArticle()
		}
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.NextPane):
		return m.focusPane(pane((int(m.focused) + 1) % 3))

	case keyMatches(msg, m.keys.PrevPane):
		return m.focusPane(pane((int(m.focused) + 2) % 3))

	case keyMatches(msg, m.keys.Left):
		if m.focused > paneFeeds {
			return m.focusPane(m.focused - 1)
		}
		return m, nil

	case keyMatches(msg, m.keys.Right):
		if m.focused < paneContent {
			return m.focusPane(m.focused + 1)
		}
		return m, nil

	case keyMatches(msg, m.keys.Up):
		return m.handleUp()

	case keyMatches(msg, m.keys.Down):
		return m.handleDown()

	case keyMatches(msg, m.keys.Enter):
		if m.focused == paneFeeds && m.toggleSelectedFolder() {
			return m, nil
		}
		if m.focused == paneArticles && len(m.filteredArticles) > 0 {
			m.focused = paneContent
			current := m.filteredArticles[m.articleCursor]
			if m.cfg.Display.MarkReadOnOpen && !current.Read {
				return m, m.setArticleReadCmd(current, true, false)
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.Back):
		if m.focused == paneContent {
			m.focused = paneArticles
		}
		return m, nil

	case keyMatches(msg, m.keys.Refresh):
		if selected := m.selectedFeed(); selected != nil {
			if m.isRemoteFeed(selected.ID) {
				return m, tea.Batch(m.loadFeedsCmd(), m.loadUnreadArticlesCmd(selected.ID))
			}
			return m, m.refreshFeedCmd(selected.ID, selected.URL, true)
		}
		return m, nil

	case keyMatches(msg, m.keys.RefreshAll):
		var cmds []tea.Cmd
		for _, f := range m.feeds {
			if m.isRemoteFeed(f.ID) {
				continue
			}
			cmds = append(cmds, m.refreshFeedCmd(f.ID, f.URL, false))
		}
		if m.greaderClient != nil {
			cmds = append(cmds, m.loadFeedsCmd())
		}
		return m, tea.Batch(cmds...)

	case keyMatches(msg, m.keys.MarkRead):
		if len(m.filteredArticles) > 0 {
			a := m.filteredArticles[m.articleCursor]
			read := !a.Read
			advance := !a.Read
			return m, m.setArticleReadCmd(a, read, advance)
		}
		return m, nil

	case keyMatches(msg, m.keys.MarkAllRead):
		if feed := m.selectedFeed(); feed != nil {
			return m, m.markAllReadCmd(feed.ID)
		}
		if folderID, ok := m.selectedFolderID(); ok {
			return m, m.markFolderReadCmd(folderID)
		}
		return m, nil

	case keyMatches(msg, m.keys.OpenBrowser):
		if len(m.filteredArticles) > 0 {
			if m.focused == paneContent {
				if link, ok := m.currentContentLink(); ok {
					return m, m.openBrowserCmd(link)
				}
			}
			return m, m.openBrowserCmd(m.filteredArticles[m.articleCursor].Link)
		}
		return m, nil

	case keyMatches(msg, m.keys.NextLink):
		if m.focused == paneContent && m.actionableLinksEnabled() {
			m.stepContentLink(1)
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.PrevLink):
		if m.focused == paneContent && m.actionableLinksEnabled() {
			m.stepContentLink(-1)
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.Summary):
		if m.focused != paneFeeds && len(m.filteredArticles) > 0 {
			return m.openSummary()
		}
		return m, nil

	case keyMatches(msg, m.keys.ContentSearch):
		if m.focused == paneContent && m.contentArticleID != 0 {
			m.overlay = overlayContentSearch
			m.contentSearchInput.Reset()
			m.contentSearchInput.Focus()
		}
		return m, nil

	case keyMatches(msg, m.keys.Settings):
		m.settings = newSettings(m.cfg, m.settingsUpdateState())
		m.overlay = overlaySettings
		return m, nil

	case msg.String() == "U":
		if m.showAvailableUpdatePrompt() && strings.TrimSpace(m.effectiveManualCommand()) == "" {
			m.overlay = overlayUpdateConfirm
			return m, nil
		}
		return m, nil

	case msg.String() == "i":
		if m.showAvailableUpdatePrompt() {
			return m, m.dismissAvailableUpdate()
		}
		return m, nil

	case keyMatches(msg, m.keys.Space):
		if m.focused == paneFeeds && m.toggleSelectedFolder() {
			return m, nil
		}
		return m, nil
	}

	// Forward scroll keys to viewport when content is focused
	if m.focused == paneContent {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) focusPane(next pane) (tea.Model, tea.Cmd) {
	wasArticles := m.focused == paneArticles
	m.focused = next
	if !wasArticles && next == paneArticles && len(m.filteredArticles) > 0 {
		return m, m.focusedArticleChangedCmd(m.filteredArticles[m.articleCursor])
	}
	return m, nil
}

func (m Model) handleUp() (tea.Model, tea.Cmd) {
	switch m.focused {
	case paneFeeds:
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
			if selected := m.selectedFeed(); selected != nil {
				return m, m.loadUnreadArticlesCmd(selected.ID)
			}
			m.clearArticles()
		}
	case paneArticles:
		if m.articleCursor > 0 {
			m.articleCursor--
			if m.articleCursor < m.listOffset {
				m.listOffset = m.articleCursor
			}
			if len(m.filteredArticles) > 0 {
				article := m.filteredArticles[m.articleCursor]
				m.setViewportArticle(article)
				return m, m.focusedArticleChangedCmd(article)
			}
		}
	case paneContent:
		if !m.cfg.Display.FocusLine {
			m.viewport.ScrollUp(1)
			return m, nil
		}
		m.moveContentFocusLine(-1)
	}
	return m, nil
}

func (m Model) handleDown() (tea.Model, tea.Cmd) {
	switch m.focused {
	case paneFeeds:
		if m.sidebarCursor < len(m.sidebarRows)-1 {
			m.sidebarCursor++
			if selected := m.selectedFeed(); selected != nil {
				return m, m.loadUnreadArticlesCmd(selected.ID)
			}
			m.clearArticles()
		}
	case paneArticles:
		if m.articleCursor < len(m.filteredArticles)-1 {
			m.articleCursor++
			visible := m.articleRowsVisible()
			if m.articleCursor >= m.listOffset+visible {
				m.listOffset = m.articleCursor - visible + 1
			}
			if len(m.filteredArticles) > 0 {
				article := m.filteredArticles[m.articleCursor]
				m.setViewportArticle(article)
				return m, m.focusedArticleChangedCmd(article)
			}
		}
	case paneContent:
		if !m.cfg.Display.FocusLine {
			m.viewport.ScrollDown(1)
			return m, nil
		}
		m.moveContentFocusLine(1)
	}
	return m, nil
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayQuitConfirm:
		switch {
		case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
			return m, tea.Quit
		case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
			m.overlay = overlayNone
		}
		return m, nil

	case overlaySearch:
		switch {
		case keyMatches(msg, m.keys.Cancel):
			m.overlay = overlayNone
			m.searchQuery = ""
			m.applyFilter()
			m.articleCursor = 0
			m.listOffset = 0
		case keyMatches(msg, m.keys.Confirm):
			m.overlay = overlayNone
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.searchQuery = m.searchInput.Value()
			m.applyFilter()
			m.articleCursor = 0
			m.listOffset = 0
			return m, cmd
		}
		return m, nil

	case overlayThemePicker:
		prevTheme := m.activeTheme
		switch {
		case keyMatches(msg, m.keys.Up):
			if m.themeCursor > 0 {
				m.themeCursor--
				m.activeTheme = m.themeCursor
				m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
				if len(m.filteredArticles) > 0 {
					m.setViewportArticle(m.filteredArticles[m.articleCursor])
				}
			}
		case keyMatches(msg, m.keys.Down):
			if m.themeCursor < len(BuiltinThemes)-1 {
				m.themeCursor++
				m.activeTheme = m.themeCursor
				m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
				if len(m.filteredArticles) > 0 {
					m.setViewportArticle(m.filteredArticles[m.articleCursor])
				}
			}
		case keyMatches(msg, m.keys.Confirm):
			m.confirmedTheme = m.themeCursor
			m.overlay = overlayNone
			m.cfg.Theme = BuiltinThemes[m.confirmedTheme].Name
			config.Save(m.cfg)
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
			}
		case keyMatches(msg, m.keys.Cancel):
			m.activeTheme = m.confirmedTheme
			m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
			m.overlay = overlayNone
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
			}
		}
		if m.activeTheme != prevTheme {
			return m, setTermBgCmd(m.styles.Theme.Bg)
		}
		return m, nil

	case overlayFeedManager:
		return m.handleFeedManager(msg)

	case overlayHelp:
		if keyMatches(msg, m.keys.Back, m.keys.Help, m.keys.Quit) {
			m.overlay = overlayNone
			return m, nil
		}
		var cmd tea.Cmd
		m.helpVP, cmd = m.helpVP.Update(msg)
		return m, cmd

	case overlayFetchError:
		switch {
		case keyMatches(msg, m.keys.Cancel), keyMatches(msg, m.keys.Quit), keyMatches(msg, m.keys.Confirm):
			m.overlay = overlayNone
			m.lastFetchError = nil
			return m, m.clearStatusCmd()
		}
		return m, nil

	case overlaySettings:
		return m.handleSettings(msg)

	case overlayUpdateConfirm:
		switch {
		case keyMatches(msg, m.keys.Confirm):
			m.overlay = overlayNone
			if m.updateInfo.DownloadURL == "" {
				m.pendingUpdateInstall = true
				m.updateState = updateStateChecking
				m.syncSettingsUpdateState()
				return m, m.checkForUpdatesCmd(true)
			}
			m.updateState = updateStateDownloading
			m.syncSettingsUpdateState()
			return m, m.downloadUpdateCmd(m.updateInfo)
		case keyMatches(msg, m.keys.Cancel):
			m.overlay = overlayNone
			return m, nil
		}
		return m, nil

	case overlaySummary:
		return m.handleSummaryKey(msg)

	case overlayContentSearch:
		switch {
		case keyMatches(msg, m.keys.Cancel):
			m.clearContentSearch()
			return m, nil
		case keyMatches(msg, m.keys.Confirm):
			m.cycleContentSearchMatch(1)
			return m, nil
		case keyMatches(msg, m.keys.Up):
			m.cycleContentSearchMatch(-1)
			return m, nil
		case keyMatches(msg, m.keys.Down):
			m.cycleContentSearchMatch(1)
			return m, nil
		default:
			var cmd tea.Cmd
			m.contentSearchInput, cmd = m.contentSearchInput.Update(msg)
			m.applyContentSearch()
			return m, cmd
		}
	}

	return m, nil
}

func (m Model) handleSettings(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Settings previews theme changes live, but only commits them to config when the overlay finishes with Save. -allie
	prevThemeIdx := m.settings.themeIdx
	newS, cmd, done := m.settings.Update(msg, m.keys)
	m.settings = newS
	tickChanged := m.settings.themeIdx != prevThemeIdx
	_, cfgThemeIdx := ThemeByName(m.cfg.Theme)
	previewingTheme := m.settings.themeIdx != cfgThemeIdx
	if tickChanged && !done {
		m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.settings.themeIdx), m.cfg.Display.Density)
		if len(m.filteredArticles) > 0 {
			m.setViewportArticle(m.filteredArticles[m.articleCursor])
		}
		cmd = tea.Batch(cmd, setTermBgCmd(m.styles.Theme.Bg))
	}
	action := m.settings.takeAction()
	switch action {
	case settingsActionCheckUpdates:
		if m.previewManualUpdateUI {
			m.setStatus("preview: check updates disabled", false)
			return m, m.clearStatusCmd()
		}
		m.pendingUpdateInstall = false
		m.updateState = updateStateChecking
		m.updateErr = ""
		m.syncSettingsUpdateState()
		return m, m.checkForUpdatesCmd(true)
	case settingsActionInstallUpdate:
		if m.previewManualUpdateUI {
			m.setStatus("preview: install disabled", false)
			return m, m.clearStatusCmd()
		}
		m.pendingUpdateInstall = true
		m.updateState = updateStateChecking
		m.updateErr = ""
		m.syncSettingsUpdateState()
		return m, m.checkForUpdatesCmd(true)
	case settingsActionDismissVersion:
		return m, m.dismissAvailableUpdate()
	case settingsActionRestartAfterUpdate:
		if m.updateInstall.Restartable {
			return m, restartProcessCmd(m.updateInstall.ExecutablePath)
		}
		return m, nil
	case settingsActionOpenRepo, settingsActionOpenIssues:
		if url := settingsActionURL(action); url != "" {
			return m, m.openBrowserCmd(url)
		}
		return m, nil
	case settingsActionCopyManualInstall:
		cmd := strings.TrimSpace(m.settings.update.manualCommand)
		if cmd == "" {
			return m, nil
		}
		return m, copyToClipboardCmd(cmd)
	}
	if done {
		if m.settings.shouldSave {
			m.cfg = m.settings.ApplyTo(m.cfg)
			m.showUnreadOnly = m.cfg.Display.DefaultUnreadOnly
			merged, _ := MergedThemeFromConfig(m.cfg)
			m.styles = BuildStyles(merged, m.cfg.Display.Density)
			if ThemeUsesASCII(merged.Name) {
				m.spinner.Spinner = spinner.Line
			} else {
				m.spinner.Spinner = spinner.Dot
			}
			feed.SetMaxFeedBodyBytes(m.cfg.Feed.MaxBodyMiB << 20)
			config.Save(m.cfg)
			summarizer, _ := ai.New(m.cfg.AI)
			m.summarizer = summarizer
			m.resetSourceClient()
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
			} else {
				m.clearViewportArticle()
			}
			m.overlay = overlayNone
			m.sidebarCursor = 0
			m.articleCursor = 0
			m.clearArticles()
			return m, m.loadFeedsCmd()
		}
		m.overlay = overlayNone
		if previewingTheme {
			merged, _ := MergedThemeFromConfig(m.cfg)
			m.styles = BuildStyles(merged, m.cfg.Display.Density)
			if len(m.filteredArticles) > 0 {
				m.setViewportArticle(m.filteredArticles[m.articleCursor])
			}
			return m, setTermBgCmd(m.styles.Theme.Bg)
		}
		return m, nil
	}
	if aiMsg, ok := msg.(AIValidateDoneMsg); ok && aiMsg.Err == nil {
		m.setStatus("AI provider connection OK", false)
		cmd = tea.Batch(cmd, m.clearStatusCmd())
	}
	return m, cmd
}

func (m Model) handleSummaryKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Back), keyMatches(msg, m.keys.Summary):
		m.overlay = overlayNone
		return m, nil
	case keyMatches(msg, m.keys.CopyText):
		if !m.summaryGenerating && m.summaryErr == "" && m.summaryArticle.Summary != "" {
			return m, copyToClipboardCmd(m.summaryArticle.Summary)
		}
	case keyMatches(msg, m.keys.SaveMD):
		if !m.summaryGenerating && m.summaryErr == "" && m.summaryArticle.Summary != "" {
			return m, saveSummaryMDCmd(m.summaryArticle, m.summaryArticle.Summary, m.cfg.AI.SavePath)
		}
	}
	return m, nil
}

func (m Model) handleFeedManager(msg tea.Msg) (tea.Model, tea.Cmd) {
	// The manager owns edit-mode details; the root model only syncs source data and final selection on exit. -allie
	fm := m.feedManager
	fm.syncTextInputWidthsForRightPane(feedManagerRightPaneWidth(m.width))
	newFM, cmd, exit := fm.Update(msg, m.keys)
	m.feedManager = newFM
	if exit {
		browseFeedID := m.feedManager.browseFeedID
		editable := m.feedManager.editable()
		m.overlay = overlayNone
		if browseFeedID != 0 && m.selectSidebarFeed(browseFeedID) {
			m.clearArticles()
			return m, m.loadUnreadArticlesCmd(browseFeedID)
		}
		if editable {
			return m, m.loadFeedsCmd()
		}
		return m, nil
	}
	return m, cmd
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	right := lipgloss.JoinVertical(lipgloss.Left,
		m.renderArticlesPane(),
		m.renderContentPane(),
	)
	main := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderFeedsPane(),
		right,
	)
	view := lipgloss.JoinVertical(lipgloss.Left, main, m.renderStatusBar())

	if m.overlay != overlayNone {
		view = m.renderOverlay(view)
	}
	view = clampView(view, m.width, m.height, m.styles.Theme.Bg)
	return lipgloss.NewStyle().
		Width(m.width).
		Height(m.height).
		Background(m.styles.Theme.Bg).
		Render(view)
}

// ── Pane renderers ────────────────────────────────────────────────────────────

func (m Model) renderFeedsPane() string {
	w := m.feedsPaneWidth()
	innerW := w - 1 // account for right border
	focused := m.focused == paneFeeds
	title := m.renderPaneHeader(paneFeeds, "Feeds", focused, innerW)
	rows := []string{title}

	for i, row := range m.sidebarRows {
		selected := i == m.sidebarCursor
		switch row.kind {
		case rowKindFolder:
			rows = append(rows, m.renderFolderHeader(row.folderID, selected, innerW))
		case rowKindFeed:
			if feed := m.feedByID(row.feedID); feed != nil {
				rows = append(rows, m.renderSidebarFeedRow(*feed, selected, innerW))
			}
		}
	}

	if len(m.sidebarRows) == 0 {
		rows = append(rows, m.styles.FeedItem.Foreground(
			lipgloss.Color(m.styles.Theme.Dimmed),
		).Render(m.emptyFeedsHint()))
	}
	footer := fmt.Sprintf("  %d feeds", len(m.feeds))
	if len(m.folders) > 0 {
		footer = fmt.Sprintf("  %d folders%s%d feeds", len(m.folders), m.styles.InlineMidDot(), len(m.feeds))
	}
	footer = m.styles.ArticleRead.Width(innerW).Render(footer)
	bodyHeight := max(0, m.mainHeight()-1)
	for viewLineCount(rows) < bodyHeight {
		rows = append(rows, m.styles.FeedItem.Width(innerW).Render(""))
	}
	rows = append(rows, footer)

	border := m.styles.FeedsPane
	if focused {
		border = border.BorderForeground(m.styles.Theme.BorderFocus)
	}

	content := strings.Join(rows, "\n")
	return border.Width(innerW).Height(m.mainHeight()).Render(content)
}

func (m Model) renderArticlesPane() string {
	w := m.articlesPaneWidth()
	h := m.articlesPaneContentHeight()
	articleUnread, articleRead, articleSelected, headerActive, borderColor, borderFocus := m.articleRowStyles()

	rows := []string{}
	visible := m.filteredArticles
	end := min(m.listOffset+m.articleRowsVisible(), len(visible))
	for i := m.listOffset; i < end; i++ {
		a := visible[i]
		age := m.formatTime(a.PublishedAt)

		dot := m.articleRowPrefix(a.Read)
		style := articleRead
		if !a.Read {
			style = articleUnread
		}
		if i == m.articleCursor {
			style = articleSelected
		}

		rows = append(rows, style.Width(w-2).Render(renderArticleRow(dot, unescapeDisplayText(a.Title), age, w-2)))
	}

	if len(m.filteredArticles) == 0 {
		if m.searchQuery != "" {
			rows = append(rows, articleRead.Render("  no results"))
		} else {
			rows = append(rows, articleRead.Render("  no articles"))
		}
	}

	focused := m.focused == paneArticles
	border := m.styles.ArticlesPane
	if focused {
		border = border.BorderForeground(borderFocus)
	}
	title := "Articles"
	if m.searchQuery != "" {
		title = fmt.Sprintf("Articles [/%s]", m.searchQuery)
	}
	if m.showUnreadOnly {
		title += " (unread)"
	}

	contentRows := append([]string{m.renderPaneHeaderWithAccent(paneArticles, title, focused, w, headerActive)}, rows...)
	for viewLineCount(contentRows) < h {
		contentRows = append(contentRows, articleRead.Width(w-2).Render(""))
	}

	bg := m.styles.Theme.Bg
	return lipgloss.NewStyle().
		Background(bg).
		Border(lipPaneBorder(m.styles.PlainUI), false, false, true, false).
		BorderForeground(lipgloss.Color(func() string {
			if focused {
				return string(borderFocus)
			}
			return string(borderColor)
		}())).
		BorderBackground(bg).
		Width(w).Height(h).
		Render(strings.Join(contentRows, "\n"))
}

func (m Model) renderContentPane() string {
	w := m.articlesPaneWidth()
	paneH := m.contentPaneOuterHeight()
	bodyH := m.contentBodyHeight()
	bg := m.styles.Theme.Bg

	focused := m.focused == paneContent
	searching := m.overlay == overlayContentSearch

	vpH := bodyH
	if searching {
		vpH = max(1, bodyH-1)
	}

	vp := m.viewport
	vp.Width = w
	vp.Height = vpH
	vp.Style = lipgloss.NewStyle().Background(bg)
	body := m.renderContentFocusLine(vp.View(), w, vpH, focused)
	body = clampView(body, w, vpH, bg)

	header := m.renderPaneHeader(paneContent, "Content", focused, w)
	content := header + "\n" + body

	if searching {
		matchInfo := ""
		if len(m.contentSearchMatches) > 0 {
			matchInfo = fmt.Sprintf("  [%d/%d]", m.contentSearchIdx+1, len(m.contentSearchMatches))
		} else if m.contentSearchQuery != "" {
			matchInfo = "  [no matches]"
		}
		searchBar := m.styles.ContentBody.Width(w).Render(m.contentSearchInput.View() + matchInfo)
		content = header + "\n" + searchBar + "\n" + body
	}

	inner := m.styles.ContentPane.
		Width(w).
		Height(paneH).
		Render(content)

	return lipgloss.NewStyle().
		Background(bg).
		Width(w).Height(paneH).
		Render(inner)
}

func (m Model) renderPaneHeader(p pane, label string, focused bool, width int) string {
	return m.renderPaneHeaderWithAccent(p, label, focused, width, m.styles.PaneHeaderActive)
}

func (m Model) renderPaneHeaderWithAccent(p pane, label string, focused bool, width int, activeStyle lipgloss.Style) string {
	style := m.styles.PaneHeaderInactive
	prefix := "    "
	title := m.headerLabel(label)
	if focused {
		style = activeStyle
		prefix = "> "
	}
	hint := m.renderPaneHint(p)
	return style.Width(width).Render(renderPaneHeaderRow(prefix, title, hint, width))
}

func (m Model) renderPaneHint(p pane) string {
	var hint string
	switch p {
	case paneFeeds:
		hint = m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " move  " +
			m.keyHint(m.keys.Enter) + " toggle  " + m.keyHint(m.keys.Refresh) + " refresh"
	case paneArticles:
		hint = m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " move  " +
			m.keyHint(m.keys.MarkRead) + " read  " + m.keyHint(m.keys.Search) + " search"
	case paneContent:
		progress := ""
		if m.contentLineCount > 0 {
			pct := min(100, (m.viewport.YOffset+m.viewport.Height)*100/m.contentLineCount)
			progress = fmt.Sprintf("%d%%  ", pct)
		}
		hint = progress + m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " line  " +
			m.keyHint(m.keys.OpenBrowser) + " open  " + m.keyHint(m.keys.ContentSearch) + " find  " +
			m.keyHint(m.keys.Back) + " back"
		if m.actionableLinksEnabled() && len(m.contentLinks) > 0 {
			hint += "  " + m.keyHint(m.keys.PrevLink) + "/" + m.keyHint(m.keys.NextLink) + " links"
		}
	}
	if hint == "" {
		return ""
	}
	return hint
}

func (m Model) keyHint(binding key.Binding) string {
	k := strings.TrimSpace(binding.Help().Key)
	if k == "" {
		return "?"
	}
	return k
}

// statusBarJoin concatenates status bar segments with a styled separator so gaps
// keep the status bar background (raw spaces between lipgloss blocks would not).
func (m Model) statusBarJoin(parts ...string) string {
	sep := m.styles.StatusBarJoiner.Render(m.styles.StatusBarSepText())
	var nonEmpty []string
	for _, p := range parts {
		if p != "" {
			nonEmpty = append(nonEmpty, p)
		}
	}
	if len(nonEmpty) == 0 {
		return ""
	}
	return strings.Join(nonEmpty, sep)
}

// statusBarInlineText wraps plain segments with a status bar style (no padding) so they share
// the same background as lipgloss-rendered parts when joined; raw text would otherwise show gaps.
func (m Model) statusBarInlineText(style lipgloss.Style, s string) string {
	if s == "" {
		return ""
	}
	return style.Copy().UnsetPadding().Render(s)
}

// statusBarKeyHintStrip is always shown on the status bar: main shortcuts ending with ? help.
func (m Model) statusBarKeyHintStrip() string {
	k := m.keys
	seg := func(b key.Binding) string {
		help := b.Help()
		keyStr := strings.TrimSpace(help.Key)
		desc := strings.TrimSpace(help.Desc)
		if keyStr == "" {
			keyStr = "?"
		}
		return m.styles.StatusHint.Render(keyStr + " " + desc)
	}
	return m.statusBarJoin(
		seg(k.FeedManager),
		seg(k.Settings),
		seg(k.Search),
		seg(k.Help),
	)
}

func (m Model) renderArticleContent(a db.Article) string {
	contentWidth := m.contentBodyWidth()
	bodyWidth := m.contentBodyWidth()
	titleWidth := max(1, contentWidth-m.styles.ContentTitle.GetHorizontalFrameSize())
	metaWidth := max(1, contentWidth-m.styles.ContentMeta.GetHorizontalFrameSize())
	title := m.styles.ContentTitle.Width(contentWidth + 2).Render(truncate(unescapeDisplayText(a.Title), titleWidth+2))
	meta := " " + m.styles.ContentMeta.Width(contentWidth).Render(truncate(a.PublishedAt.Format("Mon, 02 Jan 2006 15:04")+"  "+a.Link, metaWidth))

	content := a.Content
	if content == "" {
		content = "No content available. Press o to open in browser."
	}
	if m.cfg.Display.FilterLinks {
		content = filterLinksFromContent(content)
	}
	body := indentBlock(m.styles.ContentBody.Width(bodyWidth).Render(formatArticleBody(content, bodyWidth, m.styles.PlainUI)), 1)

	if m.actionableLinksEnabled() && len(m.contentLinks) > 0 {
		body += "\n\n" + m.renderContentLinks(bodyWidth)
	}

	return fillViewWidth(title+"\n"+meta+"\n\n"+body, m.articlesPaneWidth(), m.styles.Theme.Bg)
}

func (m Model) renderContentLinks(width int) string {
	lines := make([]string, 0, len(m.contentLinks)+1)
	lines = append(lines, strings.ToUpper("Links"))
	activeStyle := lipgloss.NewStyle().
		Background(m.styles.Theme.BorderFocus).
		Foreground(contrastFg(m.styles.Theme.BorderFocus)).
		Bold(true)
	for i, link := range m.contentLinks {
		prefix := "  "
		if i == m.contentLinkIdx {
			prefix = "> "
		}
		line := truncate(prefix+link, max(8, width))
		if i == m.contentLinkIdx {
			line = activeStyle.Render(line)
		}
		lines = append(lines, line)
	}
	return indentBlock(m.styles.ContentBody.Width(width).Render(strings.Join(lines, "\n")), 1)
}

func (m Model) actionableLinksEnabled() bool {
	return m.cfg.Display.ActionableLinks
}

func (m *Model) setViewportArticle(a db.Article) {
	sameArticle := m.contentArticleID == a.ID && m.contentLineCount > 0
	m.syncContentLinks(a)
	content := m.renderArticleContent(a)
	m.contentSearchMatches = collectSearchMatches(content, m.contentSearchQuery)
	m.viewport.SetContent(content)
	m.contentArticleID = a.ID
	m.contentLineCount = strings.Count(content, "\n") + 1
	m.contentFocusable = articleFocusableLines(content)
	m.contentFocusLine = clamp(m.contentFocusLine, 0, max(0, m.contentLineCount-1))
	if !sameArticle {
		m.contentFocusLine = firstFocusableLine(m.contentFocusable)
		m.viewport.GotoTop()
	}
	m.ensureContentFocusVisible()
}

func (m *Model) clearViewportArticle() {
	m.viewport.SetContent("")
	m.contentLinks = nil
	m.contentLinkIdx = -1
	m.contentArticleID = 0
	m.contentFocusLine = 0
	m.contentLineCount = 0
	m.contentFocusable = nil
	m.clearContentSearch()
	m.viewport.GotoTop()
}

func (m *Model) clearContentSearch() {
	m.overlay = overlayNone
	m.contentSearchQuery = ""
	m.contentSearchMatches = nil
	m.contentSearchIdx = -1
	m.contentSearchInput.Blur()
	if a := m.currentContentArticle(); a != nil {
		m.setViewportArticle(*a)
	}
}

func (m *Model) currentContentArticle() *db.Article {
	for i := range m.filteredArticles {
		if m.filteredArticles[i].ID == m.contentArticleID {
			return &m.filteredArticles[i]
		}
	}
	return nil
}

func (m *Model) applyContentSearch() {
	q := strings.ToLower(strings.TrimSpace(m.contentSearchInput.Value()))
	m.contentSearchQuery = q
	if a := m.currentContentArticle(); a != nil {
		m.setViewportArticle(*a)
	}
	if len(m.contentSearchMatches) > 0 {
		m.contentSearchIdx = 0
		m.scrollToContentMatch(0)
	} else {
		m.contentSearchIdx = -1
	}
}

func (m *Model) cycleContentSearchMatch(delta int) {
	if len(m.contentSearchMatches) == 0 {
		return
	}
	n := len(m.contentSearchMatches)
	m.contentSearchIdx = ((m.contentSearchIdx+delta)%n + n) % n
	m.scrollToContentMatch(m.contentSearchIdx)
}

func (m *Model) scrollToContentMatch(idx int) {
	if idx < 0 || idx >= len(m.contentSearchMatches) {
		return
	}
	line := m.contentSearchMatches[idx]
	m.viewport.SetYOffset(max(0, line-m.viewport.Height/2))
}

func (m *Model) moveContentFocusLine(delta int) {
	if m.contentLineCount <= 0 {
		return
	}
	m.contentFocusLine = nextContentFocusLine(m.contentFocusLine, delta, m.contentFocusable, m.contentLineCount)
	m.ensureContentFocusVisible()
}

func (m *Model) ensureContentFocusVisible() {
	if m.contentLineCount <= 0 {
		return
	}
	bodyH := max(1, m.contentBodyHeight())
	top := m.viewport.YOffset
	bottom := top + bodyH - 1
	switch {
	case m.contentFocusLine < top:
		m.viewport.SetYOffset(m.contentFocusLine)
	case m.contentFocusLine > bottom:
		m.viewport.SetYOffset(m.contentFocusLine - bodyH + 1)
	}
}

func (m Model) renderContentFocusLine(body string, width, height int, focused bool) string {
	hasSearch := len(m.contentSearchMatches) > 0
	hasFocus := m.cfg.Display.FocusLine && focused && m.contentLineCount > 0

	if !hasSearch && !hasFocus {
		return body
	}
	if width <= 0 || height <= 0 {
		return body
	}

	lines := strings.Split(body, "\n")

	styleLine := func(lineIdx int, style lipgloss.Style) {
		viewIdx := lineIdx - m.viewport.YOffset
		if viewIdx < 0 || viewIdx >= height || viewIdx >= len(lines) {
			return
		}
		l := ansi.Truncate(ansi.Strip(lines[viewIdx]), width, "")
		if pad := width - lipgloss.Width(l); pad > 0 {
			l += strings.Repeat(" ", pad)
		}
		lines[viewIdx] = style.Width(width).Render(l)
	}

	if hasSearch {
		for _, matchLine := range m.contentSearchMatches {
			styleLine(matchLine, m.styles.SearchMatch)
		}
		if m.contentSearchIdx >= 0 && m.contentSearchIdx < len(m.contentSearchMatches) {
			styleLine(m.contentSearchMatches[m.contentSearchIdx], m.styles.ContentFocusLine)
		}
	}

	if hasFocus {
		styleLine(m.contentFocusLine, m.styles.ContentFocusLine)
	}

	return strings.Join(lines, "\n")
}

func collectSearchMatches(content, query string) []int {
	if query == "" {
		return nil
	}
	lines := strings.Split(content, "\n")
	matches := make([]int, 0)
	for i, line := range lines {
		if strings.Contains(strings.ToLower(ansi.Strip(line)), query) {
			matches = append(matches, i)
		}
	}
	return matches
}

func articleFocusableLines(content string) []bool {
	lines := strings.Split(ansi.Strip(content), "\n")
	focusable := make([]bool, len(lines))
	nonEmpty := 0
	pastHeader := false
	for i, line := range lines {
		if strings.TrimSpace(line) == "" {
			if nonEmpty >= 2 {
				pastHeader = true
			}
			continue
		}
		if pastHeader {
			focusable[i] = true
		}
		nonEmpty++
	}
	return focusable
}

func firstFocusableLine(focusable []bool) int {
	for i, ok := range focusable {
		if ok {
			return i
		}
	}
	return 0
}

func nextContentFocusLine(current, delta int, focusable []bool, lineCount int) int {
	if delta == 0 || lineCount <= 0 {
		return clamp(current, 0, max(0, lineCount-1))
	}
	current = clamp(current, 0, max(0, lineCount-1))
	for next := current + delta; next >= 0 && next < lineCount; next += delta {
		if next < len(focusable) && focusable[next] {
			return next
		}
	}
	return current
}

func (m *Model) syncContentLinks(a db.Article) {
	if !m.actionableLinksEnabled() {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}

	links := extractActionableLinks(a.Content, a.Link)
	if len(links) == 0 {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}

	if cur, ok := m.currentContentLink(); ok {
		for i, link := range links {
			if link == cur {
				m.contentLinks = links
				m.contentLinkIdx = i
				return
			}
		}
	}

	m.contentLinks = links
	m.contentLinkIdx = 0
}

func (m *Model) stepContentLink(delta int) {
	if len(m.contentLinks) == 0 {
		m.contentLinkIdx = -1
		return
	}
	if m.contentLinkIdx < 0 {
		m.contentLinkIdx = 0
	}
	m.contentLinkIdx = (m.contentLinkIdx + delta + len(m.contentLinks)) % len(m.contentLinks)
}

func (m Model) currentContentLink() (string, bool) {
	if len(m.contentLinks) == 0 || m.contentLinkIdx < 0 || m.contentLinkIdx >= len(m.contentLinks) {
		return "", false
	}
	return m.contentLinks[m.contentLinkIdx], true
}

func (m Model) renderStatusBar() string {
	w := m.width
	updateInfoPart := m.statusUpdateInfoPart()
	updateActionPart := m.statusUpdateActionPart()
	linkPart := ""
	if m.actionableLinksEnabled() && len(m.contentLinks) > 0 {
		idx := clamp(m.contentLinkIdx, 0, len(m.contentLinks)-1) + 1
		linkPart = m.statusBarInlineText(m.styles.StatusBar, fmt.Sprintf("link %d/%d", idx, len(m.contentLinks)))
	}

	if m.statusMsg != "" {
		style := m.styles.StatusBar
		if m.statusErr {
			style = m.styles.StatusError
		}
		parts := []string{m.statusBarInlineText(style, m.statusMsg)}
		if updateInfoPart != "" && !m.statusMsgCoversUpdateState() {
			parts = append(parts, updateInfoPart)
		}
		if linkPart != "" {
			parts = append(parts, linkPart)
		}
		parts = append(parts, m.statusBarKeyHintStrip())
		return style.Width(w).Render(m.statusLine(m.statusBarJoin(parts...), updateActionPart))
	}

	// Build status from current state
	sb := m.styles.StatusBar
	parts := []string{}

	if updateInfoPart != "" {
		parts = append(parts, updateInfoPart)
	}
	if linkPart != "" {
		parts = append(parts, linkPart)
	}

	if len(m.feeds) > 0 {
		f := m.selectedFeed()
		if f != nil {
			parts = append(parts, m.statusBarInlineText(sb, unescapeDisplayText(f.Title)))
			if f.UnreadCount > 0 {
				parts = append(parts, m.statusBarInlineText(sb, fmt.Sprintf("%d unread", f.UnreadCount)))
			}
			if !f.LastFetched.IsZero() && f.LastFetched.Unix() > 0 {
				parts = append(parts, m.statusBarInlineText(sb, "fetched "+m.formatTime(f.LastFetched)))
			}
		} else if folderID, ok := m.selectedFolderID(); ok {
			parts = append(parts, m.statusBarInlineText(sb, m.folderName(folderID)))
			if unread := m.folderUnreadCount(folderID); unread > 0 {
				parts = append(parts, m.statusBarInlineText(sb, fmt.Sprintf("%d unread", unread)))
			}
		}
	}

	if len(m.refreshing) > 0 {
		parts = append(parts, m.styles.StatusSpinner.Render(
			m.spinner.View()+" refreshing..."),
		)
	}

	parts = append(parts, m.statusBarKeyHintStrip())

	return m.styles.StatusBar.Width(w).Render(m.statusLine(m.statusBarJoin(parts...), updateActionPart))
}

func (m Model) statusUpdateInfoPart() string {
	switch m.updateState {
	case updateStateChecking:
		return m.styles.StatusSpinner.Render(m.spinner.View() + " checking Tide updates...")
	case updateStateAvailable:
		return ""
	case updateStateInstalled:
		if m.updateInstall.Version != "" {
			msg := "restart to use Tide " + m.updateInstall.Version
			return m.statusBarInlineText(m.styles.StatusBar, msg)
		}
	}
	return ""
}

func (m Model) statusUpdateActionPart() string {
	if !m.showAvailableUpdatePrompt() {
		return ""
	}
	if strings.TrimSpace(m.effectiveManualCommand()) != "" {
		return m.styles.StatusNotice.Render("App update available  i ignore")
	}
	return m.styles.StatusNotice.Render("App update available  U install  i ignore")
}

func (m Model) statusMsgCoversUpdateState() bool {
	msg := strings.ToLower(m.statusMsg)
	switch m.updateState {
	case updateStateChecking:
		return strings.Contains(msg, "checking update")
	case updateStateAvailable:
		return strings.Contains(msg, "app update")
	case updateStateInstalled:
		return strings.Contains(msg, "restart") || strings.Contains(msg, "tide updated to")
	default:
		return false
	}
}

func (m Model) showAvailableUpdatePrompt() bool {
	return m.updateState == updateStateAvailable && m.updateInfo.Version != "" && !m.updateDismissed
}

func (m Model) renderOverlay(base string) string {
	var box string

	switch m.overlay {
	case overlayQuitConfirm:
		quitW := 40
		qt := m.styles.Theme
		chrome := newManagerChrome(quitW, qt, m.styles.PlainUI)
		header := renderManagerHeader("QUIT TIDE?", quitW, chrome)
		body := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.text).
			Width(quitW).
			Padding(1, 2).
			Render("Exit Tide now?")
		actions := renderManagerActions(quitW, chrome,
			"y", "quit",
			"esc", "cancel",
		)
		inner := lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
		inner = clampView(inner, quitW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, quitW, chrome, chrome.accent)

	case overlaySearch:
		winW := min(m.width-4, 52)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSearchOverlay(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayThemePicker:
		winW := min(m.width-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderThemePicker(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayFeedManager:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		m.feedManager.collapsedFolders = m.collapsedFolders
		inner := m.feedManager.View(winW, winH, m.styles, m.iconsEnabled())
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayHelp:
		winW := min(m.width-6, 90)
		winH := min(m.height-4, 38)
		t := m.styles.Theme
		surface := modalSurface(t)
		border := t.OverlayBorder
		if border == "" {
			border = t.BorderFocus
		}
		m.helpVP.Style = lipgloss.NewStyle().Background(surface)
		footer := m.styles.OverlayHint.
			MarginTop(1).
			Width(max(1, winW-1)).
			Padding(0, 1, 0, 4).
			Render("[esc/?/q] close  [j/k/↑↓] scroll")
		box = lipgloss.NewStyle().
			Background(surface).
			Border(lipPaneBorder(m.styles.PlainUI)).
			BorderForeground(border).
			Width(winW).Height(winH).
			Render(m.helpVP.View() + "\n" + footer)

	case overlayFetchError:
		if m.lastFetchError != nil {
			winW := min(m.width-4, 70)
			et := m.styles.Theme
			chrome := newManagerChrome(winW, et, m.styles.PlainUI)
			inner := m.renderFetchErrorOverlay(winW, chrome)
			inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
			box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)
		}

	case overlaySettings:
		winW := min(m.width-4, 62)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.settings.View(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayUpdateConfirm:
		winW := min(m.width-8, 72)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderUpdateConfirmOverlay(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlaySummary:
		winW := min(m.width-8, 76)
		winH := min(m.height-6, 20)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSummaryOverlay(winW, winH, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)
	}

	return overlayOnBase(base, box, m.width, m.height, m.styles.Theme.Bg)
}

func renderChromeOverlayBox(inner string, width int, chrome managerChrome, border lipgloss.Color) string {
	return lipgloss.NewStyle().
		Background(chrome.baseBg).
		Border(lipPaneBorder(chrome.plainUI)).
		BorderForeground(border).
		BorderBackground(chrome.baseBg).
		Width(width).
		Render(inner)
}

func (m Model) renderSearchOverlay(width int, chrome managerChrome) string {
	header := renderManagerHeader("SEARCH ARTICLES", width, chrome)
	input := m.searchInput
	inputW := max(1, width-4)
	input.Width = inputW
	input.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	input.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(input.View())
	actions := renderManagerActions(width, chrome, "enter", "apply", "esc", "clear")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
}

func (m Model) renderSummaryOverlay(width, height int, chrome managerChrome) string {
	header := renderManagerHeader("AI SUMMARY", width, chrome)

	var bodyText string
	switch {
	case m.summaryGenerating:
		bodyText = m.spinner.View() + " Generating summary…"
	case m.summaryErr != "":
		bodyText = "Error: " + m.summaryErr
	default:
		bodyText = formatSummaryBody(m.summaryArticle.Summary, width-4, m.styles.PlainUI)
	}

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(bodyText)

	var hints string
	if !m.summaryGenerating && m.summaryErr == "" {
		provider := ""
		if m.summarizer != nil {
			prefix := "  ·  "
			if m.styles.PlainUI {
				prefix = " | "
			}
			provider = prefix + m.summarizer.ProviderName()
		}
		providerLine := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Foreground(chrome.muted).
			Width(width).
			Padding(0, 2).
			Render(provider)
		hints = lipgloss.JoinVertical(lipgloss.Left,
			providerLine,
			renderManagerActions(width, chrome, "c", "copy", "M", "save .md", "esc", "close"),
		)
	} else {
		hints = renderManagerActions(width, chrome, "esc", "close")
	}

	bodyH := max(1, height-lipgloss.Height(header)-lipgloss.Height(hints))
	body = clampView(body, width, bodyH, chrome.baseBg)
	return lipgloss.JoinVertical(lipgloss.Left, header, body, hints)
}

func (m Model) renderUpdateConfirmOverlay(width int, chrome managerChrome) string {
	header := renderManagerHeader("INSTALL TIDE UPDATE?", width, chrome)

	target, _ := os.Executable()
	bodyLines := []string{
		"Install Tide " + m.updateInfo.Version + "?",
	}
	if summary := strings.TrimSpace(m.updateInfo.Summary); summary != "" {
		bodyLines = append(bodyLines, "", "What's new: "+summary)
	}
	bodyLines = append(bodyLines,
		"",
		"Asset: "+m.updateInfo.AssetName+".tar.gz",
		"Target: "+target,
		"",
		"The update will download first, then replace the current binary if the install path is writable.",
	)
	bodyText := strings.Join(bodyLines, "\n")

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2, 0, 2).
		Render(bodyText)

	note := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2, 1, 2).
		Render("Also available in Settings > Updates")

	actions := renderManagerActions(width, chrome, "enter", "install", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, note, actions)
}

func overlayOnBase(base, box string, width, height int, bg lipgloss.Color) string {
	base = clampView(base, width, height, bg)

	boxLines := strings.Split(box, "\n")
	boxH := len(boxLines)
	boxW := 0
	for _, l := range boxLines {
		if w := lipgloss.Width(l); w > boxW {
			boxW = w
		}
	}

	// Center position — matches lipgloss.Center, lipgloss.Center
	overlayX := (width - boxW) / 2
	overlayY := (height - boxH) / 2
	if overlayX < 0 {
		overlayX = 0
	}
	if overlayY < 0 {
		overlayY = 0
	}
	rightStart := overlayX + boxW

	baseLines := strings.Split(base, "\n")
	for len(baseLines) < height {
		baseLines = append(baseLines, "")
	}

	result := make([]string, height)
	for y := 0; y < height; y++ {
		baseLine := baseLines[y]
		boxRow := y - overlayY
		if boxRow < 0 || boxRow >= boxH {
			result[y] = baseLine
			continue
		}
		left := ansi.Cut(baseLine, 0, overlayX)
		right := ansi.Cut(baseLine, rightStart, width)
		result[y] = left + boxLines[boxRow] + right
	}
	return strings.Join(result, "\n")
}

func (m Model) renderThemePicker(width int, chrome managerChrome) string {
	header := renderManagerHeader("THEME", width, chrome)
	rows := make([]string, 0, len(BuiltinThemes))
	for i, t := range BuiltinThemes {
		if i == m.themeCursor {
			rows = append(rows, renderManagerSelectedRow(width, m.styles.ThemePickerCursor()+t.Name, chrome, m.styles))
		} else {
			rows = append(rows, clampView(
				lipgloss.NewStyle().
					Background(chrome.baseBg).
					Foreground(chrome.text).
					Padding(0, 1).
					Render("  "+t.Name),
				width,
				1,
				chrome.baseBg,
			))
		}
	}
	body := clampView(lipgloss.JoinVertical(lipgloss.Left, rows...), width, len(rows), chrome.baseBg)
	hints := renderManagerActions(width, chrome, "enter", "confirm", "esc", "revert")
	return lipgloss.JoinVertical(lipgloss.Left, header, body, hints)
}

// ── Commands ─────────────────────────────────────────────────────────────────

func (m *Model) loadFeedsCmd() tea.Cmd {
	db := m.db
	client := m.greaderClient
	return func() tea.Msg {
		feeds, err := db.ListFeeds()
		if err != nil {
			return FeedsLoadedMsg{Err: err}
		}
		folders, err := db.ListFolders()
		if err != nil {
			return FeedsLoadedMsg{Err: err}
		}
		streams := map[int64]string{}
		if client != nil {
			remoteFeeds, remoteStreams, remoteErr := m.loadGReaderFeeds(context.Background())
			feeds = append(feeds, remoteFeeds...)
			if remoteStreams != nil {
				streams = remoteStreams
			}
			return FeedsLoadedMsg{Feeds: feeds, Folders: folders, RemoteStreams: streams, Err: remoteErr}
		}
		return FeedsLoadedMsg{Feeds: feeds, Folders: folders, RemoteStreams: streams}
	}
}

func (m *Model) loadArticlesCmd(feedID int64) tea.Cmd {
	if m.isRemoteFeed(feedID) {
		return func() tea.Msg {
			articles, err := m.loadGReaderArticles(context.Background(), feedID)
			return ArticlesLoadedMsg{FeedID: feedID, Articles: articles, Err: err}
		}
	}
	return func() tea.Msg {
		articles, err := m.db.ListArticles(feedID)
		if err != nil {
			return ArticlesLoadedMsg{FeedID: feedID, Err: err}
		}
		return ArticlesLoadedMsg{FeedID: feedID, Articles: articles}
	}
}

func (m *Model) loadUnreadArticlesCmd(feedID int64) tea.Cmd {
	if m.isRemoteFeed(feedID) {
		return m.loadArticlesCmd(feedID)
	}
	return func() tea.Msg {
		articles, err := m.db.ListUnreadArticles(feedID)
		if err != nil {
			return ArticlesLoadedMsg{FeedID: feedID, Err: err}
		}
		return ArticlesLoadedMsg{FeedID: feedID, Articles: articles}
	}
}

func (m *Model) maybeCheckForUpdatesCmd(manual bool) tea.Cmd {
	if manual {
		return m.checkForUpdatesCmd(true)
	}
	if !m.cfg.Updates.CheckOnStartup {
		return nil
	}
	// Always run the startup check when enabled: the banner is check-first and
	// does not trust cached results, so skipping here would leave the user
	// without any update signal for up to CheckIntervalHours. -allie
	return m.checkForUpdatesCmd(false)
}

func (m *Model) checkForUpdatesCmd(manual bool) tea.Cmd {
	m.updateState = updateStateChecking
	updater := m.updater
	currentVersion := m.currentVersion
	return func() tea.Msg {
		result, err := updater.Check(currentVersion)
		return UpdateCheckedMsg{Result: result, Manual: manual, Err: err}
	}
}

func (m *Model) downloadUpdateCmd(info update.ReleaseInfo) tea.Cmd {
	updater := m.updater
	return func() tea.Msg {
		asset, err := updater.Download(info)
		return UpdateDownloadedMsg{Asset: asset, Err: err}
	}
}

func (m *Model) installUpdateCmd(asset update.DownloadedAsset) tea.Cmd {
	updater := m.updater
	currentExec, _ := os.Executable()
	return func() tea.Msg {
		result, err := updater.Install(asset, currentExec)
		return UpdateInstalledMsg{Result: result, Err: err}
	}
}

func (m *Model) refreshFeedCmd(feedID int64, feedURL string, manual bool) tea.Cmd {
	m.refreshing[feedID] = true
	conv := m.mdConverter
	return func() tea.Msg {
		result := feed.FetchFeed(feedURL)
		if !result.IsSuccess() {
			return FeedRefreshedMsg{FeedID: feedID, Err: result.Err, Result: result, Manual: manual}
		}

		parsed := result.Feed
		articles := make([]db.Article, 0, len(parsed.Items))
		for _, item := range parsed.Items {
			content, _ := conv.ConvertString(item.Content)
			articles = append(articles, db.Article{
				FeedID:      feedID,
				GUID:        item.GUID,
				Title:       item.Title,
				Link:        item.Link,
				Content:     content,
				PublishedAt: item.PublishedAt,
			})
		}
		return FeedRefreshedMsg{
			FeedID:      feedID,
			Title:       parsed.Title,
			Description: parsed.Description,
			FaviconURL:  parsed.FaviconURL,
			Articles:    articles,
			Result:      result,
			Manual:      manual,
		}
	}
}

func (m *Model) setArticleReadCmd(article db.Article, read, advance bool) tea.Cmd {
	database := m.db
	client := m.greaderClient
	isRemote := m.isRemoteFeed(article.FeedID)
	return func() tea.Msg {
		if isRemote {
			if client == nil {
				return ArticleReadUpdatedMsg{
					ArticleID: article.ID,
					FeedID:    article.FeedID,
					WasRead:   article.Read,
					Read:      read,
					Advance:   advance,
					Err:       fmt.Errorf("google reader source not configured"),
				}
			}
			if err := client.MarkEntryRead(context.Background(), article.GUID, read); err != nil {
				return ArticleReadUpdatedMsg{
					ArticleID: article.ID,
					FeedID:    article.FeedID,
					WasRead:   article.Read,
					Read:      read,
					Advance:   advance,
					Err:       err,
				}
			}
		} else if err := database.MarkRead(article.ID, read); err != nil {
			return ArticleReadUpdatedMsg{
				ArticleID: article.ID,
				FeedID:    article.FeedID,
				WasRead:   article.Read,
				Read:      read,
				Advance:   advance,
				Err:       err,
			}
		}
		return ArticleReadUpdatedMsg{
			ArticleID: article.ID,
			FeedID:    article.FeedID,
			WasRead:   article.Read,
			Read:      read,
			Advance:   advance,
		}
	}
}

func (m *Model) focusedArticleChangedCmd(article db.Article) tea.Cmd {
	fetchCmd := m.maybeFetchArticleContentCmd(article)
	if !m.cfg.Display.MarkReadOnFocus || article.Read {
		return fetchCmd
	}
	readCmd := m.setArticleReadCmd(article, true, false)
	if fetchCmd == nil {
		return readCmd
	}
	return tea.Batch(fetchCmd, readCmd)
}

func (m *Model) maybeFetchArticleContentCmd(a db.Article) tea.Cmd {
	if !shouldFetchArticleContent(a) {
		return nil
	}
	return func() tea.Msg {
		content, err := feed.FetchArticleText(a.Link)
		if err != nil {
			return ArticleContentFetchedMsg{ArticleID: a.ID, Err: err}
		}
		return ArticleContentFetchedMsg{ArticleID: a.ID, Content: content}
	}
}

func (m *Model) markAllReadCmd(feedID int64) tea.Cmd {
	database := m.db
	client := m.greaderClient
	streamID := strings.TrimSpace(m.greaderStreams[feedID])
	isRemote := m.isRemoteFeed(feedID)
	return func() tea.Msg {
		if isRemote {
			if client == nil {
				return FeedsReadUpdatedMsg{Err: fmt.Errorf("google reader source not configured")}
			}
			if err := client.MarkAllRead(context.Background(), streamID); err != nil {
				return FeedsReadUpdatedMsg{Err: err}
			}
		} else if err := database.MarkAllRead(feedID); err != nil {
			return FeedsReadUpdatedMsg{Err: err}
		}
		return FeedsReadUpdatedMsg{FeedIDs: []int64{feedID}}
	}
}

func (m *Model) markFolderReadCmd(folderID int64) tea.Cmd {
	feedIDs := make([]int64, 0)
	streamIDs := make(map[int64]string)
	for _, feed := range m.feeds {
		if feed.FolderID == folderID {
			feedIDs = append(feedIDs, feed.ID)
			if streamID, ok := m.greaderStreams[feed.ID]; ok {
				streamIDs[feed.ID] = streamID
			}
		}
	}
	database := m.db
	client := m.greaderClient
	return func() tea.Msg {
		applied := make([]int64, 0, len(feedIDs))
		for _, feedID := range feedIDs {
			if streamID, ok := streamIDs[feedID]; ok {
				if client == nil {
					return FeedsReadUpdatedMsg{FeedIDs: applied, Err: fmt.Errorf("google reader source not configured")}
				}
				if err := client.MarkAllRead(context.Background(), streamID); err != nil {
					return FeedsReadUpdatedMsg{FeedIDs: applied, Err: err}
				}
			} else if err := database.MarkAllRead(feedID); err != nil {
				return FeedsReadUpdatedMsg{FeedIDs: applied, Err: err}
			}
			applied = append(applied, feedID)
		}
		return FeedsReadUpdatedMsg{FeedIDs: applied}
	}
}

func (m *Model) clearStatusCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return StatusClearMsg{}
	})
}

func restartProcessCmd(executablePath string) tea.Cmd {
	return func() tea.Msg {
		cmd := exec.Command(executablePath, os.Args[1:]...)
		cmd.Stdin = os.Stdin
		cmd.Stdout = os.Stdout
		cmd.Stderr = os.Stderr
		if err := cmd.Start(); err != nil {
			return RestartedMsg{Err: fmt.Errorf("restart Tide: %w", err)}
		}
		return RestartedMsg{}
	}
}

func (m Model) openBrowserCmd(url string) tea.Cmd {
	browser := m.cfg.Display.Browser
	return func() tea.Msg {
		var cmd *exec.Cmd
		if browser != "" {
			cmd = exec.Command(browser, url)
		} else {
			switch runtime.GOOS {
			case "darwin":
				cmd = exec.Command("open", url)
			default:
				cmd = exec.Command("xdg-open", url)
			}
		}
		_ = cmd.Start()
		return nil
	}
}

func (m Model) openSummary() (tea.Model, tea.Cmd) {
	if len(m.filteredArticles) == 0 {
		return m, nil
	}
	a := m.filteredArticles[m.articleCursor]

	// If we already have a cached summary, show it immediately.
	if a.Summary != "" {
		m.summaryArticle = a
		m.summaryGenerating = false
		m.summaryErr = ""
		m.overlay = overlaySummary
		return m, nil
	}

	// No AI provider configured — prompt the user to set one up.
	if m.summarizer == nil {
		m.setStatus("AI not configured — press S to open settings", false)
		return m, m.clearStatusCmd()
	}

	m.summaryArticle = a
	m.summaryGenerating = true
	m.summaryErr = ""
	m.overlay = overlaySummary
	return m, m.aiSummarizeCmd(a)
}

func (m *Model) aiSummarizeCmd(a db.Article) tea.Cmd {
	summarizer := m.summarizer
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		summary, err := summarizer.Summarize(ctx, a.Title, a.Content)
		return AISummaryFetchedMsg{ArticleID: a.ID, Summary: summary, Err: err}
	}
}

func copyToClipboardCmd(text string) tea.Cmd {
	return func() tea.Msg {
		candidates := [][]string{
			{"wl-copy"},
			{"xclip", "-selection", "clipboard"},
			{"xsel", "--clipboard", "--input"},
			{"pbcopy"},
		}
		for _, args := range candidates {
			path, err := exec.LookPath(args[0])
			if err != nil {
				continue
			}
			cmd := exec.Command(path, args[1:]...)
			cmd.Stdin = strings.NewReader(text)
			if err := cmd.Run(); err == nil {
				return ClipboardCopiedMsg{}
			}
		}
		return ClipboardCopiedMsg{Err: fmt.Errorf("no clipboard tool found (wl-copy/xclip/xsel/pbcopy)")}
	}
}

func saveSummaryMDCmd(a db.Article, summary, savePath string) tea.Cmd {
	return func() tea.Msg {
		if savePath == "" {
			savePath = "~/"
		}
		if strings.HasPrefix(savePath, "~/") {
			home, _ := os.UserHomeDir()
			savePath = filepath.Join(home, savePath[2:])
		}
		if err := os.MkdirAll(savePath, 0o755); err != nil {
			return SummarySavedMsg{Err: err}
		}

		filename := summaryFilename(a.Title)
		fullPath := filepath.Join(savePath, filename)

		content := fmt.Sprintf("# %s\n\n**Source:** %s\n**Published:** %s\n\n---\n\n%s\n",
			a.Title,
			a.Link,
			a.PublishedAt.Format("Mon, 02 Jan 2006"),
			summary,
		)
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return SummarySavedMsg{Err: err}
		}
		return SummarySavedMsg{Path: fullPath}
	}
}

func summaryFilename(title string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(title) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9'):
			b.WriteRune(r)
		case unicode.IsSpace(r) || r == '-':
			if b.Len() > 0 {
				s := b.String()
				if s[len(s)-1] != '-' {
					b.WriteByte('-')
				}
			}
		}
	}
	s := strings.Trim(b.String(), "-")
	if s == "" {
		s = "summary"
	}
	if len(s) > 50 {
		s = s[:50]
	}
	return s + ".md"
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Model) rebuildSidebar() {
	m.sidebarRows = buildSidebarRows(m.feeds, m.folders, m.collapsedFolders, true)
	m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))
}

func buildSidebarRows(feeds []db.Feed, folders []db.Folder, collapsedFolders map[int64]bool, showUncategorized bool) []sidebarRow {
	if len(folders) == 0 {
		rows := make([]sidebarRow, 0, len(feeds))
		for _, feed := range feeds {
			rows = append(rows, sidebarRow{kind: rowKindFeed, feedID: feed.ID})
		}
		return rows
	}

	byFolder := make(map[int64][]int64)
	for _, feed := range feeds {
		byFolder[feed.FolderID] = append(byFolder[feed.FolderID], feed.ID)
	}

	rows := make([]sidebarRow, 0, len(feeds)+len(folders)+1)
	for _, folder := range folders {
		rows = append(rows, sidebarRow{kind: rowKindFolder, folderID: folder.ID})
		if collapsedFolders[folder.ID] {
			continue
		}
		for _, feedID := range byFolder[folder.ID] {
			rows = append(rows, sidebarRow{kind: rowKindFeed, feedID: feedID})
		}
	}
	if uncategorized := byFolder[0]; len(uncategorized) > 0 {
		if showUncategorized {
			rows = append(rows, sidebarRow{kind: rowKindFolder, folderID: 0})
			if collapsedFolders[0] {
				return rows
			}
		}
		for _, feedID := range uncategorized {
			rows = append(rows, sidebarRow{kind: rowKindFeed, feedID: feedID})
		}
	}
	return rows
}

func (m Model) newFeedManager() FeedManager {
	fm := NewFeedManagerWithSource(m.db, m.cfg.Source)
	fm.setData(m.feeds, m.folders)
	if feed := m.selectedFeed(); feed != nil {
		fm.selectFeed(feed.ID)
	} else if folderID, ok := m.selectedFolderID(); ok {
		if folderID >= 0 {
			fm.selectFolder(folderID)
		}
	}
	fm.mode = fmList
	fm.paneFocus = fmPaneList
	fm.remoteSettingsEdit = false
	fm.blurEditInputs()
	return fm
}

func (m *Model) selectSidebarFeed(feedID int64) bool {
	for i, row := range m.sidebarRows {
		if row.kind == rowKindFeed && row.feedID == feedID {
			m.sidebarCursor = i
			return true
		}
	}
	return false
}

func (m Model) selectedFolderHasRemoteFeeds() bool {
	folderID, ok := m.selectedFolderID()
	if !ok {
		return false
	}
	for _, feed := range m.feeds {
		if feed.FolderID == folderID && m.isRemoteFeed(feed.ID) {
			return true
		}
	}
	return false
}

func (m *Model) clearArticles() {
	m.articles = nil
	m.filteredArticles = nil
	m.articleCursor = 0
	m.listOffset = 0
	m.clearViewportArticle()
}

// effectiveManualCommand is the command shown in Settings (real install result, or suggested script when an update is available but the install path is not writable).
func (m Model) effectiveManualCommand() string {
	if s := strings.TrimSpace(m.updateInstall.ManualCommand); s != "" {
		return s
	}
	if m.updateDismissed {
		return ""
	}
	v := strings.TrimSpace(m.updateInfo.Version)
	if v == "" {
		return ""
	}
	if !update.IsNewerVersion(v, m.currentVersion) {
		return ""
	}
	ok, err := update.InstallDestinationWritable()
	if err != nil || ok {
		return ""
	}
	return update.SuggestedManualInstallScript
}

func (m Model) settingsUpdateState() settingsUpdateState {
	lastChecked := time.Time{}
	if m.cfg.Updates.LastCheckedUnix > 0 {
		lastChecked = time.Unix(m.cfg.Updates.LastCheckedUnix, 0)
	}
	return settingsUpdateState{
		currentVersion:   m.currentVersion,
		state:            m.updateState,
		latestVersion:    m.updateInfo.Version,
		latestIsFresh:    m.updateInfoFresh,
		publishedAt:      m.updateInfo.PublishedAt,
		summary:          m.updateInfo.Summary,
		lastChecked:      lastChecked,
		err:              m.updateErr,
		dismissed:        m.updateDismissed,
		manualCommand:    m.effectiveManualCommand(),
		restartable:      m.updateInstall.Restartable,
		installedVersion: m.updateInstall.Version,
	}
}

func (m *Model) syncSettingsUpdateState() {
	m.settings.setUpdateState(m.settingsUpdateState())
}

func (m *Model) applyManualUpdatePreview() {
	pub := time.Date(2026, 4, 12, 0, 0, 0, 0, time.UTC)
	now := time.Now()
	m.currentVersion = "v0.0.38"
	m.updateState = updateStateNeedsElevation
	m.updateInfo = update.ReleaseInfo{
		Version:     "v0.0.39",
		PublishedAt: pub,
		Summary:     "## Tide v0.0.39",
	}
	m.updateInfoFresh = true
	m.updateErr = ""
	m.updateDismissed = false
	m.pendingUpdateInstall = false
	m.downloadedUpdate = nil
	m.updateInstall = update.InstallResult{
		RequiresManual: true,
		ManualCommand:  update.SuggestedManualInstallScript,
	}
	m.cfg.Updates.LastCheckedUnix = now.Unix()
	m.settings = newSettings(m.cfg, m.settingsUpdateState())
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setActiveSection(ssUpdates)
	m.settings.setFocusedField(sfUpdateManualCommand)
	m.overlay = overlaySettings
}

func (m *Model) dismissAvailableUpdate() tea.Cmd {
	if m.previewManualUpdateUI {
		m.setStatus("preview: dismiss ignored (not saved)", false)
		return m.clearStatusCmd()
	}
	if m.updateInfo.Version == "" {
		return nil
	}
	m.cfg.Updates.DismissedVersion = m.updateInfo.Version
	config.Save(m.cfg) //nolint:errcheck
	m.updateDismissed = true
	m.syncSettingsUpdateState()
	m.setStatus("Tide update "+m.updateInfo.Version+" dismissed", false)
	return m.clearStatusCmd()
}

func (m *Model) restoreCachedUpdateState() {
	// Banner is check-first: we never surface an update banner from cached config
	// alone, only after a live GitHub check in this session. Clear any stale
	// cached "available_*" values so they cannot resurface. -allie
	m.clearCachedAvailableUpdate()
}

func (m *Model) clearCachedAvailableUpdate() {
	m.cfg.Updates.AvailableVersion = ""
	m.cfg.Updates.AvailableSummary = ""
	m.cfg.Updates.AvailablePublished = 0
}

func (m Model) currentSidebarSelection() (sidebarRowKind, int64) {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return rowKindFeed, 0
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind == rowKindFolder {
		return rowKindFolder, row.folderID
	}
	return rowKindFeed, row.feedID
}

func (m Model) selectedFeed() *db.Feed {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return nil
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindFeed {
		return nil
	}
	return m.feedByID(row.feedID)
}

func (m Model) selectedFolderID() (int64, bool) {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return 0, false
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindFolder {
		return 0, false
	}
	return row.folderID, true
}

func (m Model) feedByID(feedID int64) *db.Feed {
	for i := range m.feeds {
		if m.feeds[i].ID == feedID {
			return &m.feeds[i]
		}
	}
	return nil
}

func (m *Model) adjustFeedUnreadCount(feedID int64, delta int64) {
	for i := range m.feeds {
		if m.feeds[i].ID == feedID {
			m.feeds[i].UnreadCount = max(0, m.feeds[i].UnreadCount+delta)
			return
		}
	}
}

func (m *Model) markFeedsReadInMemory(feedIDs []int64) {
	if len(feedIDs) == 0 {
		return
	}

	feedSet := make(map[int64]struct{}, len(feedIDs))
	for _, feedID := range feedIDs {
		feedSet[feedID] = struct{}{}
		for i := range m.feeds {
			if m.feeds[i].ID == feedID {
				m.feeds[i].UnreadCount = 0
				break
			}
		}
	}

	changedArticles := false
	for i := range m.articles {
		if _, ok := feedSet[m.articles[i].FeedID]; ok && !m.articles[i].Read {
			m.articles[i].Read = true
			changedArticles = true
		}
	}
	if !changedArticles {
		return
	}

	m.applyFilter()
	if len(m.filteredArticles) == 0 {
		m.articleCursor = 0
		m.listOffset = 0
		m.clearViewportArticle()
		return
	}
	m.articleCursor = clamp(m.articleCursor, 0, max(0, len(m.filteredArticles)-1))
	maxOffset := max(0, len(m.filteredArticles)-1)
	m.listOffset = clamp(m.listOffset, 0, maxOffset)
	m.setViewportArticle(m.filteredArticles[m.articleCursor])
}

func (m Model) folderName(folderID int64) string {
	if folderID == 0 {
		return "Uncategorized"
	}
	for _, folder := range m.folders {
		if folder.ID == folderID {
			return unescapeDisplayText(folder.Name)
		}
	}
	return "Folder"
}

func (m Model) folderColor(folderID int64) lipgloss.Color {
	if folderID == 0 {
		return ""
	}
	if config.IsRetroTerminalTheme(string(m.styles.Theme.Name)) {
		return ""
	}
	for _, folder := range m.folders {
		if folder.ID == folderID && folder.Color != "" {
			return lipgloss.Color(folder.Color)
		}
	}
	return ""
}

func (m Model) selectedFeedFolderColor() lipgloss.Color {
	if feed := m.selectedFeed(); feed != nil {
		return m.folderColor(feed.FolderID)
	}
	return ""
}

func (m Model) folderUnreadCount(folderID int64) int64 {
	var total int64
	for _, feed := range m.feeds {
		if feed.FolderID == folderID {
			total += feed.UnreadCount
		}
	}
	return total
}

func (m Model) folderHeaderStyle(folderID int64, selected bool) lipgloss.Style {
	accent := m.folderColor(folderID)
	style := m.styles.FeedItem.Copy().Foreground(lipgloss.Color(m.styles.Theme.Dimmed)).Bold(true)
	if accent != "" {
		style = style.Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
	}
	if selected {
		style = m.sidebarSelectedStyle(accent).Copy().Bold(true)
	}
	return style
}

func (m Model) folderBadgeStyle(folderID int64, selected bool) lipgloss.Style {
	accent := m.folderColor(folderID)
	if selected {
		return m.sidebarSelectedBadgeStyle(accent)
	}
	if accent == "" {
		return m.styles.UnreadBadge
	}
	return m.styles.UnreadBadge.Copy().Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
}

func (m Model) feedAccentStyle(feed db.Feed, selected bool) lipgloss.Style {
	style := m.styles.FeedItem
	accent := m.folderColor(feed.FolderID)
	if accent != "" {
		style = style.Copy().Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
	}
	if selected {
		style = m.sidebarSelectedStyle(accent)
	}
	return style
}

func (m Model) feedBadgeStyle(feed db.Feed, selected bool) lipgloss.Style {
	accent := m.folderColor(feed.FolderID)
	if selected {
		return m.sidebarSelectedBadgeStyle(accent)
	}
	if accent == "" {
		return m.styles.UnreadBadge
	}
	return m.styles.UnreadBadge.Copy().Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
}

func (m Model) sidebarSelectedStyle(accent lipgloss.Color) lipgloss.Style {
	if m.focused == paneFeeds {
		if accent != "" {
			return m.styles.FeedItemSelectedFocused.Copy().
				Background(accent).
				Foreground(readableText(m.styles.Theme.Fg, accent, 4.5))
		}
		return m.styles.FeedItemSelectedFocused
	}

	style := m.styles.FeedItemSelectedUnfocused
	if accent != "" {
		bg := terminalColorAsColor(style.GetBackground())
		style = style.Copy().Foreground(accentReadableOn(accent, bg, 3))
	}
	return style
}

func (m Model) sidebarSelectedBadgeStyle(accent lipgloss.Color) lipgloss.Style {
	style := m.sidebarSelectedStyle(accent)
	return m.styles.UnreadBadge.Copy().Foreground(terminalColorAsColor(style.GetForeground()))
}

func terminalColorAsColor(c lipgloss.TerminalColor) lipgloss.Color {
	if c == nil {
		return ""
	}
	return lipgloss.Color(fmt.Sprint(c))
}

func (m Model) articleRowStyles() (lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Color, lipgloss.Color) {
	accent := m.selectedFeedFolderColor()
	unread := m.styles.ArticleUnread
	read := m.styles.ArticleRead
	selected := m.styles.ArticleSelected
	headerActive := m.styles.PaneHeaderActive
	border := m.styles.Theme.Border
	borderFocus := m.styles.Theme.BorderFocus
	if accent != "" {
		leg := accentReadableOn(accent, m.styles.Theme.Bg, 3)
		unread = unread.Copy().Foreground(leg)
		selected = selected.Copy().Foreground(leg)
		headerActive = headerActive.Copy().
			Background(accent).
			Foreground(readableText(m.styles.Theme.Fg, accent, 4.5))
		borderFocus = accent
	}
	return unread, read, selected, headerActive, border, borderFocus
}

func (m *Model) toggleSelectedFolder() bool {
	folderID, ok := m.selectedFolderID()
	if !ok {
		return false
	}
	m.collapsedFolders[folderID] = !m.collapsedFolders[folderID]
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindFolder && row.folderID == folderID {
			m.sidebarCursor = i
			break
		}
	}
	m.clearArticles()
	return true
}

func (m *Model) applyFilter() {
	q := strings.ToLower(m.searchQuery)
	if q == "" && !m.showUnreadOnly {
		m.filteredArticles = m.articles
		return
	}
	filtered := make([]db.Article, 0, len(m.articles))
	for _, a := range m.articles {
		if m.showUnreadOnly && a.Read {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(a.Title), q) {
			continue
		}
		filtered = append(filtered, a)
	}
	m.filteredArticles = filtered
}

func (m Model) indexOfFilteredArticle(articleID int64) int {
	for i := range m.filteredArticles {
		if m.filteredArticles[i].ID == articleID {
			return i
		}
	}
	return -1
}

func (m *Model) setStatus(msg string, isErr bool) {
	m.statusMsg = msg
	m.statusErr = isErr
}

func (m Model) renderFetchErrorOverlay(w int, chrome managerChrome) string {
	r := m.lastFetchError
	if r == nil {
		return ""
	}

	textW := max(1, w-4)
	bg := chrome.baseBg
	surf := chrome.surfaceBg
	accent := chrome.accent
	muted := chrome.muted
	text := chrome.text

	label := func(s string) string {
		return lipgloss.NewStyle().Background(bg).Foreground(muted).Width(14).Render(s)
	}
	val := func(s string) string {
		return lipgloss.NewStyle().Background(bg).Foreground(text).Render(s)
	}
	accentLine := func(s string) string {
		return lipgloss.NewStyle().Background(bg).Foreground(accent).Bold(true).Render(s)
	}
	row := func(k, v string) string {
		return lipgloss.NewStyle().Background(bg).Width(textW).
			Render(label(k) + val(v))
	}

	header := renderManagerHeader("FETCH ERROR", w, chrome)

	// Title line
	title := accentLine(r.FriendlyMessage())

	// Detail rows
	rows := []string{""}
	if r.StatusCode != 0 {
		rows = append(rows, row("Status:", fmt.Sprintf("%d", r.StatusCode)))
	}
	if r.ContentType != "" {
		ct := r.ContentType
		if len(ct) > textW-16 {
			ct = ct[:textW-16]
		}
		rows = append(rows, row("Content-Type:", ct))
	}
	origURL := r.OriginalURL
	if len(origURL) > textW-16 {
		origURL = "…" + origURL[len(origURL)-(textW-17):]
	}
	rows = append(rows, row("Original URL:", origURL))
	if r.FinalURL != r.OriginalURL {
		finalURL := r.FinalURL
		if len(finalURL) > textW-16 {
			finalURL = "…" + finalURL[len(finalURL)-(textW-17):]
		}
		rows = append(rows, row("Final URL:", finalURL))
	}

	// Redirect chain
	if len(r.RedirectChain) > 1 {
		rows = append(rows, "")
		rows = append(rows, lipgloss.NewStyle().Background(bg).Foreground(muted).
			Render(fmt.Sprintf("Redirects (%d):", len(r.RedirectChain)-1)))
		for _, u := range r.RedirectChain {
			display := u
			if len(display) > textW-4 {
				display = "…" + display[len(display)-(textW-5):]
			}
			rows = append(rows, lipgloss.NewStyle().Background(bg).Foreground(text).
				Render("  → "+display))
		}
	}

	// Snippet
	if r.Snippet != "" {
		rows = append(rows, "")
		rows = append(rows, lipgloss.NewStyle().Background(bg).Foreground(muted).Render("Preview:"))
		snip := strings.ReplaceAll(r.Snippet, "\n", " ")
		snip = strings.Join(strings.Fields(snip), " ")
		if len(snip) > textW-2 {
			snip = snip[:textW-2] + "…"
		}
		rows = append(rows, lipgloss.NewStyle().Background(surf).Foreground(text).
			Width(textW).Padding(0, 1).Render(snip))
	}

	if r.SuggestURLUpdate {
		rows = append(rows, "")
		rows = append(rows, lipgloss.NewStyle().Background(bg).Foreground(accent).
			Render("↳ Feed permanently moved to new URL"))
	}
	actions := renderManagerActions(w, chrome, "esc", "dismiss")

	body := lipgloss.NewStyle().Background(bg).Width(w).Padding(0, 2).
		Render(lipgloss.JoinVertical(lipgloss.Left, title, strings.Join(rows, "\n")))

	return lipgloss.JoinVertical(lipgloss.Left, header, body, actions)
}

func shouldFetchArticleContent(a db.Article) bool {
	content := strings.TrimSpace(a.Content)
	if a.Link == "" {
		return false
	}
	if len(content) >= 500 && strings.Count(content, "\n") >= 3 {
		return false
	}
	return true
}

func keyMatches(msg tea.KeyMsg, bindings ...key.Binding) bool {
	for _, b := range bindings {
		if key.Matches(msg, b) {
			return true
		}
	}
	return false
}

func truncate(s string, maxW int) string {
	if maxW <= 0 {
		return ""
	}
	if lipgloss.Width(s) <= maxW {
		return s
	}
	if maxW <= 1 {
		return ansi.Truncate(s, maxW, "")
	}
	return ansi.Truncate(s, maxW, "…")
}

func (m Model) formatTime(t time.Time) string {
	switch m.cfg.Display.DateFormat {
	case "absolute":
		return t.Format("Jan 2, 2006")
	case "none":
		return ""
	default:
		return relativeTime(t)
	}
}

func relativeTime(t time.Time) string {
	d := time.Since(t)
	switch {
	case d < time.Minute:
		return "now"
	case d < time.Hour:
		return fmt.Sprintf("%dm", int(d.Minutes()))
	case d < 24*time.Hour:
		return fmt.Sprintf("%dh", int(d.Hours()))
	case d < 7*24*time.Hour:
		return fmt.Sprintf("%dd", int(d.Hours()/24))
	case d < 4*7*24*time.Hour:
		return fmt.Sprintf("%dw", int(d.Hours()/(24*7)))
	default:
		return t.Format("Jan 2")
	}
}

func clamp(v, lo, hi int) int {
	if v < lo {
		return lo
	}
	if v > hi {
		return hi
	}
	return v
}

// statusBarSpaceFill renders spaces with the status bar background for pads and gaps
// (raw spaces would show the terminal default behind lipgloss segments).
func (m Model) statusBarSpaceFill(n int) string {
	if n <= 0 {
		return ""
	}
	return m.styles.StatusBarJoiner.Render(strings.Repeat(" ", n))
}

func (m Model) padRightWithStatusBarBG(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) >= width {
		return s
	}
	return s + m.statusBarSpaceFill(width-lipgloss.Width(s))
}

func (m Model) statusLine(left, right string) string {
	maxW := max(0, m.width-4) // leave room for status bar padding
	left = strings.ReplaceAll(left, "\n", " ")
	right = strings.ReplaceAll(right, "\n", " ")
	if right == "" {
		return truncate(left, maxW)
	}

	right = truncate(right, maxW)
	rightW := lipgloss.Width(right)
	if rightW >= maxW {
		return right
	}
	if left == "" {
		return m.statusBarSpaceFill(maxW-rightW) + right
	}

	const gap = 2
	leftW := maxW - rightW - gap
	if leftW <= 0 {
		return right
	}

	left = truncate(left, leftW)
	return m.padRightWithStatusBarBG(left, leftW) + m.statusBarSpaceFill(gap) + right
}

func renderFeedRow(prefix, title, badge string, width int) string {
	prefixW := lipgloss.Width(prefix)
	badgeW := lipgloss.Width(badge)
	gapW := 0
	if badge != "" {
		gapW = 1
	}
	nameW := max(0, width-prefixW-badgeW-gapW)
	name := truncate(title, nameW)
	row := prefix + padRight(name, nameW)
	if badge != "" {
		row += " " + badge
	}
	return padRight(row, width)
}

func renderPaneHeaderRow(prefix, title, hint string, width int) string {
	base := prefix + title
	baseW := lipgloss.Width(base)
	if width <= 0 {
		return ""
	}
	if hint == "" || baseW >= width {
		return padRight(truncate(base, width), width)
	}

	hintMax := max(0, width-baseW-1)
	if hintMax == 0 {
		return padRight(truncate(base, width), width)
	}
	hint = truncate(hint, hintMax)
	spaceW := max(1, width-baseW-lipgloss.Width(hint))
	return base + strings.Repeat(" ", spaceW) + hint
}

func folderDisplayLabel(name string, collapsed, icons bool) string {
	if !icons {
		return name
	}
	if collapsed {
		return "󰉖 " + name
	}
	return "󰉋 " + name
}

func feedDisplayLabel(name string, icons bool) string {
	if !icons {
		return name
	}
	return "\U000f046b " + name
}

func (m Model) renderFolderHeader(folderID int64, selected bool, width int) string {
	icon := "v "
	label := m.folderName(folderID)
	if m.iconsEnabled() {
		icon = "▾ "
		label = folderDisplayLabel(label, false, true)
	}
	if m.collapsedFolders[folderID] {
		icon = "> "
		if m.iconsEnabled() {
			icon = "▸ "
			label = folderDisplayLabel(m.folderName(folderID), true, true)
		}
	}
	badge := ""
	if unread := m.folderUnreadCount(folderID); unread > 0 {
		badge = m.folderBadgeStyle(folderID, selected).Render(fmt.Sprintf("(%d)", unread))
	}
	row := renderFeedRow(icon, label, badge, width)
	style := m.folderHeaderStyle(folderID, selected)
	return style.Width(width).Render(row)
}

func (m Model) renderSidebarFeedRow(feed db.Feed, selected bool, width int) string {
	badge := ""
	if feed.UnreadCount > 0 {
		badge = m.feedBadgeStyle(feed, selected).Render(fmt.Sprintf("(%d)", feed.UnreadCount))
	}

	prefix := "    "
	title := unescapeDisplayText(feed.Title)
	if m.iconsEnabled() {
		title = feedDisplayLabel(title, true)
	} else {
		prefix = "    " + m.feedRowPrefix(selected)
	}
	if m.refreshing[feed.ID] {
		prefix = "    " + m.spinner.View() + " "
	}
	row := renderFeedRow(prefix, title, badge, width)
	style := m.feedAccentStyle(feed, selected)
	return style.Width(width).Render(row)
}

func renderArticleRow(prefix, title, age string, width int) string {
	prefixW := lipgloss.Width(prefix)
	ageW := lipgloss.Width(age)
	gapW := 2
	if age == "" {
		gapW = 0
	}
	titleW := max(0, width-prefixW-ageW-gapW)
	row := prefix + padRight(truncate(title, titleW), titleW) + strings.Repeat(" ", gapW) + age
	return padRight(row, width)
}

func (m Model) iconsEnabled() bool {
	return m.cfg.Display.Icons
}

func (m Model) headerLabel(label string) string {
	if !m.iconsEnabled() {
		return label
	}
	switch label {
	case "Feeds":
		return "◉ Feeds"
	case "Content":
		return "▣ Content"
	}
	if strings.HasPrefix(label, "Articles") {
		return strings.Replace(label, "Articles", "≣ Articles", 1)
	}
	return label
}

func (m Model) feedRowPrefix(selected bool) string {
	if m.styles.PlainUI {
		if selected {
			return "> "
		}
		return "  "
	}
	if !m.iconsEnabled() {
		if selected {
			return "> "
		}
		return "  "
	}
	if selected {
		return "▸ "
	}
	return "◦ "
}

func (m Model) articleRowPrefix(read bool) string {
	if m.styles.PlainUI {
		if read {
			return "- "
		}
		return "* "
	}
	if !m.iconsEnabled() {
		if read {
			return "  "
		}
		return "o "
	}
	if read {
		return "· "
	}
	return "● "
}

func (m Model) emptyFeedsHint() string {
	if m.styles.PlainUI {
		if m.iconsEnabled() {
			return "  + press a or m to add feeds"
		}
		return "  press a or m to add feeds"
	}
	if m.iconsEnabled() {
		return "  ＋ press a or m to add feeds"
	}
	return "  press a or m to add feeds"
}

func (m Model) openFeedAddDialog() Model {
	m.overlay = overlayFeedManager
	m.feedManager = m.newFeedManager()
	m.feedManager.focusAdd()
	return m
}

func padRight(s string, width int) string {
	if width <= 0 {
		return ""
	}
	if lipgloss.Width(s) >= width {
		return s
	}
	return s + strings.Repeat(" ", width-lipgloss.Width(s))
}

func viewLineCount(parts []string) int {
	n := 0
	for _, p := range parts {
		if p == "" {
			n++
			continue
		}
		n += strings.Count(p, "\n") + 1
	}
	return n
}

func clampView(view string, width, height int, bg lipgloss.Color) string {
	if width <= 0 || height <= 0 {
		return ""
	}

	bgStyle := lipgloss.NewStyle().Background(bg)
	lines := strings.Split(view, "\n")
	if len(lines) > height {
		lines = lines[:height]
	}
	for i, line := range lines {
		line = ansi.Truncate(line, width, "")
		if !strings.HasSuffix(line, ansi.ResetStyle) {
			line += ansi.ResetStyle
		}
		pad := width - lipgloss.Width(line)
		if pad > 0 {
			line += bgStyle.Render(strings.Repeat(" ", pad))
		}
		lines[i] = line
	}
	for len(lines) < height {
		lines = append(lines, bgStyle.Render(strings.Repeat(" ", width)))
	}
	return strings.Join(lines, "\n")
}

func fillViewWidth(view string, width int, bg lipgloss.Color) string {
	if width <= 0 || view == "" {
		return view
	}
	return clampView(view, width, strings.Count(view, "\n")+1, bg)
}

func indentBlock(view string, pad int) string {
	if view == "" || pad <= 0 {
		return view
	}
	prefix := strings.Repeat(" ", pad)
	lines := strings.Split(view, "\n")
	for i := range lines {
		lines[i] = prefix + lines[i]
	}
	return strings.Join(lines, "\n")
}

func (m *Model) resetHelpVP() {
	winW := min(m.width-6, 90)
	winH := min(m.height-4, 38)
	vpW := max(1, winW-1)
	vpH := winH - 3 // inside border, minus footer row
	m.helpVP = viewport.New(vpW, vpH)
	m.helpVP.SetContent(renderHelp(vpW, m.styles, m.keys))
}

// ── Dimension helpers ─────────────────────────────────────────────────────────

func (m Model) feedsPaneWidth() int    { return int(float64(m.width) * 0.28) }
func (m Model) articlesPaneWidth() int { return m.width - m.feedsPaneWidth() }
func (m Model) mainHeight() int        { return m.height - 1 }
func (m Model) articlesPaneOuterHeight() int {
	return max(3, int(float64(m.mainHeight())*0.40))
}
func (m Model) articlesPaneContentHeight() int {
	return max(2, m.articlesPaneOuterHeight()-1)
}
func (m Model) articleRowsVisible() int {
	stride := m.styles.ListItemLineStride()
	bodyLines := max(0, m.articlesPaneContentHeight()-1)
	return bodyLines / stride
}
func (m Model) contentPaneOuterHeight() int {
	return max(3, m.mainHeight()-m.articlesPaneOuterHeight())
}
func (m Model) contentViewportHeight() int {
	return max(1, m.contentPaneOuterHeight())
}
func (m Model) contentBodyHeight() int {
	return max(1, m.contentPaneOuterHeight()-1)
}
func (m Model) contentBodyWidth() int {
	w := max(1, m.articlesPaneWidth()-2)
	if cap := m.cfg.Display.ReadingWidth; cap > 0 && cap < w {
		return cap
	}
	return w
}
