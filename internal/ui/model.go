package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/key"
	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	"github.com/charmbracelet/bubbles/viewport"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"

	"github.com/allisonhere/tide/internal/ai"
	"github.com/allisonhere/tide/internal/auth"
	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	imapClient "github.com/allisonhere/tide/internal/imap"
	"github.com/allisonhere/tide/internal/update"
)

// ── Enums ────────────────────────────────────────────────────────────────────

type pane int

const (
	paneAccounts pane = iota
	paneMessages
	paneContent
)

type sidebarRowKind int

const (
	rowKindUnified sidebarRowKind = iota
	rowKindAccount
	rowKindSysFolderHeader
	rowKindPersonalFolderHeader
	rowKindMailbox
)

type sidebarRow struct {
	kind      sidebarRowKind
	accountID int64
	mailboxID int64
	label     string // section header label (e.g. "System", "Labels")
	count     int    // item count for section headers
}

type overlayMode int

const (
	overlayNone overlayMode = iota
	overlayQuitConfirm
	overlaySearch
	overlayThemePicker
	overlayAccountManager
	overlayContactManager
	overlayHelp
	overlaySettings
	overlayUpdateConfirm
	overlayContentSearch
	overlaySummary
	overlayCompose
	overlayCommandPalette
	overlaySaveAttach
	overlayMoveMessage
	overlayGrammarPreview
	overlayLogViewer
	overlayFilterManager
	overlayDraftCloseConfirm
	overlayBulkDeleteConfirm
)

type commandPaletteContext int

const (
	commandPaletteMain commandPaletteContext = iota
	commandPaletteCompose
	commandPaletteSummary
	commandPaletteSaveAttach
)

type updateState int

type logEntry struct {
	Time    time.Time
	Message string
	IsError bool
}

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
	db *db.DB
	// sessions pools one live IMAP connection per account; all background
	// commands go through it instead of dialing, so bursts of operations
	// can't exceed per-user server connection caps. Pointer-typed: shared
	// across the value copies Bubble Tea makes of the model.
	sessions *imapClient.SessionPool
	cfg      config.Config

	currentVersion string
	updater        *update.Updater

	width, height int
	focused       pane

	accounts          []db.Account
	mailboxes         []db.Mailbox
	sidebarRows       []sidebarRow
	sidebarCursor     int
	sidebarOffset     int
	collapsedAccounts map[int64]bool
	collapsedSections map[string]bool // key: "system:<id>" or "personal:<id>"

	messages         []db.Message
	filteredMessages []db.Message
	messageThreads   []messageThread
	drafts           []db.Draft
	messageCursor    int
	listOffset       int
	searchQuery      string
	searchMode       bool
	searchEditing    bool
	showUnreadOnly   bool
	selectedMessages map[int64]bool

	viewport               viewport.Model
	contentLinks           []string
	contentLinkIdx         int
	contentMessageID       int64
	contentFocusLine       int
	contentLineCount       int
	contentFocusable       []bool
	contentLines           []string
	contentSearchInput     textinput.Model
	contentSearchQuery     string
	contentSearchMatches   []int
	contentSearchIdx       int
	contentSelectionActive bool
	contentSelectionAnchor int
	contentSelectionAll    bool

	contentAttachments     []db.Attachment
	contentQuotesCollapsed bool
	contentShowHeaders     bool

	saveAttachPicker filePicker
	movePicker       movePicker
	filterManager    filterManager

	grammarOriginal    string
	grammarCorrected   string
	grammarQuoteSuffix string // quoted/forwarded text held aside during a grammar check

	logBuffer []logEntry

	helpVP                viewport.Model
	helpSearchInput       textinput.Model
	helpSearchActive      bool
	overlay               overlayMode
	searchInput           textinput.Model
	commandInput          textinput.Model
	commandCursor         int
	commandPaletteOrigin  overlayMode
	commandPaletteContext commandPaletteContext

	confirmedTheme int
	activeTheme    int
	styles         Styles
	themeCursor    int

	accountManager AccountManager
	contactManager ContactManager
	compose        ComposeModel
	addressBook    []string

	statusMsg string
	statusErr bool

	syncing map[int64]bool
	spinner spinner.Model
	// spinnerRunning tracks whether the spinner's self-rescheduling tick loop is
	// currently live. The loop only runs while something is loading (see
	// spinnerActive); this flag prevents starting a second parallel loop, which
	// would double the frame rate. See ensureSpinner and the spinner.TickMsg case.
	spinnerRunning bool

	// lastFolderRefresh throttles the per-account server folder LIST so it runs
	// far less often than message sync. Keyed by account ID; zero value means
	// "never refreshed", so the first auto-sync after launch picks up folders.
	lastFolderRefresh map[int64]time.Time

	firstLoad              bool
	pendingSelectMailboxID int64
	pendingSelectMessageID int64
	keys                   KeyMap

	settings Settings

	pendingBulkDelete []db.Message

	updateState          updateState
	updateInfo           update.ReleaseInfo
	updateInfoFresh      bool
	downloadedUpdate     *update.DownloadedAsset
	updateInstall        update.InstallResult
	updateErr            string
	updateDismissed      bool
	pendingUpdateInstall bool
	// restartExecPath, when non-empty, asks main to replace this process with
	// the binary at that path AFTER the program exits — so BubbleTea has fully
	// restored the terminal before the new version takes over (see
	// settingsActionRestartAfterUpdate). Restarting from inside the running TUI
	// would leave two processes fighting over the terminal.
	restartExecPath string

	previewManualUpdateUI bool

	summarizer        ai.Summarizer
	summaryMessage    db.Message
	summaryGenerating bool
	summaryErr        string
}

// CloseSessions logs out all pooled IMAP connections; called on shutdown.
func (m Model) CloseSessions() {
	if m.sessions != nil {
		m.sessions.Close()
	}
}

// RestartExecPath returns the binary main should exec after the program exits
// to complete an in-app update restart, or "" when no restart was requested.
// Exec-ing only after exit guarantees the terminal is fully restored first.
func (m Model) RestartExecPath() string { return m.restartExecPath }

func NewModel(database *db.DB, cfg config.Config, currentVersion string, previewManualUpdate bool) Model {
	merged, themeIdx := MergedThemeFromConfig(cfg)

	// Compose body editors pick up vim mode from config via this package var
	// (read in newEditorArea), the same way the clipboard is injected.
	setEditorVimMode(cfg.Display.ComposeVim)

	si := textinput.New()
	si.Placeholder = "search messages..."
	si.CharLimit = 100

	ci := textinput.New()
	ci.Placeholder = "type a command..."
	ci.CharLimit = 100

	csi := textinput.New()
	csi.Placeholder = "find in message..."
	csi.CharLimit = 100

	hsi := textinput.New()
	hsi.Placeholder = "search help..."
	hsi.CharLimit = 100

	sp := spinner.New()
	if ThemeUsesASCII(merged.Name) {
		sp.Spinner = spinner.Line
	} else {
		sp.Spinner = spinner.Dot
	}
	sp.Style = lipgloss.NewStyle()

	summarizer, _ := ai.New(cfg.AI)

	m := Model{
		db:                    database,
		sessions:              imapClient.NewSessionPool(),
		cfg:                   cfg,
		currentVersion:        currentVersion,
		previewManualUpdateUI: previewManualUpdate,
		updater:               update.New(),
		focused:               paneAccounts,
		confirmedTheme:        themeIdx,
		activeTheme:           themeIdx,
		styles:                BuildStyles(merged, cfg.Display.Density),
		accountManager:        NewAccountManager(database),
		searchInput:           si,
		helpSearchInput:       hsi,
		commandInput:          ci,
		spinner:               sp,
		syncing:               make(map[int64]bool),
		lastFolderRefresh:     make(map[int64]time.Time),
		collapsedAccounts:     map[int64]bool{},
		collapsedSections:     map[string]bool{},
		firstLoad:             true,
		keys:                  DefaultKeys,
		summarizer:            summarizer,
		showUnreadOnly:        cfg.Display.DefaultUnreadOnly,
		contentLinkIdx:        -1,
		contentShowHeaders:    true,
		contentSearchInput:    csi,
		contentSearchIdx:      -1,
		selectedMessages:      make(map[int64]bool),
	}
	m.restoreCachedUpdateState()
	if previewManualUpdate {
		m.applyManualUpdatePreview()
	}
	return m
}

func (m Model) Init() tea.Cmd {
	// The spinner tick loop is started lazily by ensureSpinner when a loading
	// state begins — not here. Kicking it at startup would re-render the idle TUI
	// ~10×/sec forever (the spinner self-reschedules on every tick).
	cmds := []tea.Cmd{m.loadAccountsCmd()}
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
		if m.activeMessageRowCount() > 0 {
			m.setViewportForCurrentRow()
			m.ensureContentFocusVisible()
		}
		if m.overlay == overlayHelp {
			m.resetHelpVP()
		}
		return m, nil

	case spinner.TickMsg:
		// The spinner self-perpetuates: Update returns a fresh Tick on every
		// TickMsg. Only keep the loop alive while something is actually animating,
		// otherwise an idle TUI would re-render ~10×/sec forever. Once nothing is
		// loading, drop the tick and mark the loop stopped so ensureSpinner can
		// restart it next time.
		if !m.spinnerActive() {
			m.spinnerRunning = false
			return m, nil
		}
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
			m.saveConfig()
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
			m.saveConfig()
			if m.pendingUpdateInstall {
				m.pendingUpdateInstall = false
				// A manual check from Settings overrides any previous dismiss.
				m.updateState = updateStateDownloading
				m.syncSettingsUpdateState()
				return m, m.downloadUpdateCmd(m.updateInfo)
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
		m.saveConfig()
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
			// Clear dismiss — the user clearly wants this update.
			m.updateDismissed = false
			m.cfg.Updates.DismissedVersion = ""
			m.saveConfig()
			m.setStatus("update downloaded; admin permission required", true)
			return m, m.clearStatusCmd()
		}
		m.updateState = updateStateInstalled
		m.updateDismissed = false
		m.cfg.Updates.DismissedVersion = ""
		m.clearCachedAvailableUpdate()
		m.saveConfig()
		m.syncSettingsUpdateState()
		m.setStatus("Tide updated to "+msg.Result.Version+m.styles.InlineMidDot()+"restart when ready", false)
		return m, m.clearStatusCmd()

	case AccountsLoadedMsg:
		if msg.Err != nil && len(msg.Accounts) == 0 && len(msg.Mailboxes) == 0 {
			m.accounts = nil
			m.mailboxes = nil
			m.rebuildSidebar()
			m.clearMessages()
			m.setStatus(msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		prevKind, prevID := m.currentSidebarSelection()
		m.accounts = msg.Accounts
		m.mailboxes = msg.Mailboxes
		statusCmd := tea.Cmd(nil)
		if msg.Err != nil {
			m.setStatus(msg.Err.Error(), true)
			statusCmd = m.clearStatusCmd()
		}
		m.rebuildSidebar()
		m.accountManager.setData(m.accounts, m.mailboxes, m.cfg.Accounts)
		openAccountManagerOnEmptyFirstLoad := m.firstLoad && msg.Err == nil && len(m.accounts) == 0
		if m.firstLoad {
			m.loadCollapseState()
			m.rebuildSidebar()
			statusCmd = tea.Batch(statusCmd, m.startSyncTimers(), m.syncInboxesNowCmd(), m.loadAddressBookCmd())
		}
		m.firstLoad = false
		if prevID == 0 && prevKind == rowKindMailbox {
			m.sidebarCursor = 0
		}
		if m.pendingSelectMailboxID != 0 {
			for i, row := range m.sidebarRows {
				if row.kind == rowKindMailbox && row.mailboxID == m.pendingSelectMailboxID {
					m.sidebarCursor = i
					break
				}
			}
			m.pendingSelectMailboxID = 0
		} else if prevID != 0 {
			for i, row := range m.sidebarRows {
				if row.kind == prevKind {
					if row.kind == rowKindUnified {
						m.sidebarCursor = i
						break
					}
					if row.kind == rowKindMailbox && row.mailboxID == prevID {
						m.sidebarCursor = i
						break
					}
					if row.kind == rowKindAccount && row.accountID == prevID {
						m.sidebarCursor = i
						break
					}
					if row.kind == rowKindSysFolderHeader && row.accountID == prevID {
						m.sidebarCursor = i
						break
					}
					if row.kind == rowKindPersonalFolderHeader && row.accountID == prevID {
						m.sidebarCursor = i
						break
					}
				}
			}
		}
		m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))
		m.clampSidebarOffset()
		if len(m.mailboxes) == 0 {
			m.clearMessages()
			if openAccountManagerOnEmptyFirstLoad {
				m.overlay = overlayAccountManager
				m.accountManager = m.newAccountManager()
			}
			if statusCmd != nil {
				return m, statusCmd
			}
			return m, nil
		}
		cmds := []tea.Cmd{}
		if m.selectedUnifiedInbox() {
			cmds = append(cmds, m.loadUnifiedInboxCmd())
		} else if selected := m.selectedMailbox(); selected != nil {
			cmds = append(cmds, m.loadMailboxMessagesCmd(selected.ID))
			if m.selectedDraftsMailbox() {
				cmds = append(cmds, m.loadDraftsCmd(selected.ID))
			}
		} else {
			m.clearMessages()
		}
		if statusCmd != nil {
			cmds = append(cmds, statusCmd)
		}
		return m, tea.Batch(cmds...)

	case MessagesLoadedMsg:
		if msg.Search {
			if strings.TrimSpace(m.searchQuery) != strings.TrimSpace(msg.Query) {
				return m, nil
			}
			if msg.Err != nil {
				m.clearMessages()
				m.setStatus(msg.Err.Error(), true)
				return m, m.clearStatusCmd()
			}
			currentID := int64(0)
			if current := m.currentRowMessage(); current != nil {
				currentID = current.ID
			}
			m.messages = msg.Messages
			m.applyFilter()
			if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
				m.messageCursor = idx
			} else {
				m.messageCursor = clamp(m.messageCursor, 0, max(0, m.activeMessageRowCount()-1))
			}
			m.listOffset = clamp(m.listOffset, 0, max(0, m.activeMessageRowCount()-1))
			if current := m.currentRowMessage(); current != nil {
				m.setViewportForCurrentRow()
			} else {
				m.clearViewportMessage()
			}
			return m, nil
		}
		if m.searchActive() {
			return m, nil
		}
		if msg.Err != nil {
			if selected := m.selectedMailbox(); selected != nil && msg.MailboxID == selected.ID {
				m.clearMessages()
			}
			m.setStatus(msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		if (msg.MailboxID == 0 && m.selectedUnifiedInbox()) || (func() bool {
			selected := m.selectedMailbox()
			return selected != nil && msg.MailboxID == selected.ID
		}()) {
			m.messages = msg.Messages
			m.applyFilter()
			if m.pendingSelectMessageID != 0 {
				if idx := m.indexOfFilteredMessage(m.pendingSelectMessageID); idx >= 0 {
					m.messageCursor = idx
				}
				m.pendingSelectMessageID = 0
			}
			m.messageCursor = clamp(m.messageCursor, 0, max(0, m.activeMessageRowCount()-1))
			m.listOffset = 0
			if msg := m.currentRowMessage(); msg != nil {
				m.setViewportForCurrentRow()
			}
		}
		return m, nil

	case DraftsLoadedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("load drafts failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		if selected := m.selectedMailbox(); selected != nil && selected.ID == msg.MailboxID {
			m.drafts = msg.Drafts
			m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.drafts)-1))
			m.listOffset = clamp(m.listOffset, 0, max(0, len(m.drafts)-1))
		}
		return m, nil

	case MailboxSyncedMsg:
		delete(m.syncing, msg.MailboxID)
		if msg.Err != nil {
			if auth.IsAuthFailure(msg.Err) {
				name := "account"
				if mb := m.mailboxByID(msg.MailboxID); mb != nil {
					name = m.accountName(mb.AccountID)
				}
				// Sticky (no clearStatusCmd): an expired sign-in needs the user to
				// re-authenticate, so a 2-second flash would just be missed.
				m.setStatus(name+" sign-in expired — press M to re-authenticate", true)
				return m, nil
			}
			m.setStatus(fmt.Sprintf("sync failed: %v (%v)", msg.Err, msg.Total.Round(time.Millisecond)), true)
			return m, m.clearStatusCmd()
		}
		cmds := []tea.Cmd{m.loadAccountsCmd()}
		if m.selectedUnifiedInbox() {
			cmds = append(cmds, m.loadUnifiedInboxCmd())
		} else if selected := m.selectedMailbox(); selected != nil && msg.MailboxID == selected.ID {
			cmds = append(cmds, m.loadMailboxMessagesCmd(msg.MailboxID))
		}
		if mb := m.mailboxByID(msg.MailboxID); mb != nil && m.isDraftsMailbox(*mb) {
			cmds = append(cmds, m.importRemoteDraftsCmd(msg.MailboxID))
		}
		if msg.Manual && msg.NewCount > 0 {
			m.setStatus(fmt.Sprintf("synced: %d new", msg.NewCount), false)
			cmds = append(cmds, m.clearStatusCmd())
		} else if msg.Manual {
			m.setStatus("up to date", false)
			cmds = append(cmds, m.clearStatusCmd())
		} else {
			// Auto-sync: log to buffer silently (no status bar spam).
			m.addToLog(fmt.Sprintf("auto-synced %d new (%v)", msg.NewCount, msg.Total.Round(time.Millisecond)), false)
		}
		if len(msg.NewMessages) > 0 && !msg.Manual && m.cfg.Display.Notifications {
			cmds = append(cmds, m.notifyCmd(msg.MailboxID, msg.NewMessages))
		}
		cmds = append(cmds, m.loadAddressBookCmd())
		return m, tea.Batch(cmds...)

	case AccountSavedMsg:
		m.accountManager.busy = false
		m.accountManager.busyMsg = ""
		m.accountManager.statusMsg = ""
		if msg.Err != nil {
			detail := m.accountManager.redactSensitiveWithAccounts(msg.Err.Error(), []config.AccountConfig{msg.AccountCfg})
			m.accountManager.statusMsg = fmt.Sprintf("SAVE FAILED: %s", detail)
			m.setStatus(fmt.Sprintf("save failed: %s", detail), true)
			return m, m.clearStatusCmd()
		}
		// Update config with new account
		found := false
		for i, a := range m.cfg.Accounts {
			if a.Name == msg.AccountCfg.Name {
				m.cfg.Accounts[i] = msg.AccountCfg
				found = true
				break
			}
		}
		if !found {
			m.cfg.Accounts = append(m.cfg.Accounts, msg.AccountCfg)
		}
		// Also update in-memory accounts so scheduleNextSync can find it.
		foundAcct := false
		for i, a := range m.accounts {
			if a.ID == msg.Account.ID {
				m.accounts[i] = msg.Account
				foundAcct = true
				break
			}
		}
		if !foundAcct {
			m.accounts = append(m.accounts, msg.Account)
		}
		m.saveConfig()
		m.accountManager = m.newAccountManager()
		m.accountManager.mode = amList
		m.accountManager.statusMsg = fmt.Sprintf("SAVED: %s", strings.ToUpper(msg.Account.Name))
		m.setStatus(fmt.Sprintf("saved: %s", msg.Account.Name), false)
		if len(msg.Mailboxes) > 0 {
			m.pendingSelectMailboxID = msg.Mailboxes[0].ID
		}
		return m, tea.Batch(m.loadAccountsCmd(), m.clearStatusCmd(), m.scheduleNextSync(msg.Account.ID))

	case AutoSyncMsg:
		var cmds []tea.Cmd
		for _, mb := range m.mailboxes {
			if mb.AccountID == msg.AccountID && isInboxMailbox(mb) {
				cmds = append(cmds, m.syncMailboxCmd(mb.ID, false))
			}
		}
		// Piggyback an account-level folder refresh on the auto-sync tick, but
		// throttled far below the message sync cadence — the folder tree changes
		// rarely and a LIST per minute would be wasteful. The first tick after
		// launch always passes (zero-value timestamp).
		if last := m.lastFolderRefresh[msg.AccountID]; time.Since(last) >= folderRefreshInterval {
			if cmd := m.refreshMailboxesCmd(msg.AccountID); cmd != nil {
				m.lastFolderRefresh[msg.AccountID] = time.Now()
				cmds = append(cmds, cmd)
			}
		}
		// Reschedule next auto-sync for this account — only one pending
		// timer per account, avoiding the accumulation that causes
		// "Too many simultaneous connections" errors.
		if cmd := m.scheduleNextSync(msg.AccountID); cmd != nil {
			cmds = append(cmds, cmd)
		}
		if len(cmds) > 0 {
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case MailboxesRefreshedMsg:
		if msg.Err != nil {
			// Non-fatal: log to the fetch log and move on. Folder refresh is a
			// convenience, so don't intrude on the status line during routine
			// auto-sync.
			logFetch(fmt.Sprintf("account %d", msg.AccountID), "(folders)", 0, 0, 0, 0, msg.Err)
			return m, nil
		}
		if len(msg.Mailboxes) == 0 && len(msg.Removed) == 0 {
			return m, nil
		}
		// Capture the active folder before mutating so we can detect whether it
		// was just pruned out from under the user.
		var activeID int64
		if sel := m.selectedMailbox(); sel != nil {
			activeID = sel.ID
		}
		if len(msg.Mailboxes) > 0 {
			m.mailboxes = append(m.mailboxes, msg.Mailboxes...)
		}
		if len(msg.Removed) > 0 {
			gone := make(map[int64]bool, len(msg.Removed))
			for _, id := range msg.Removed {
				gone[id] = true
				// Drop any in-flight spinner state for the pruned mailbox.
				// syncing is only cleared on MailboxSyncedMsg, which is keyed by
				// mailbox ID — once the row is gone that message can never land,
				// so the entry (and its status-line spinner) would leak forever.
				delete(m.syncing, id)
			}
			kept := m.mailboxes[:0]
			for _, mb := range m.mailboxes {
				if !gone[mb.ID] {
					kept = append(kept, mb)
				}
			}
			m.mailboxes = kept
			// If the folder being viewed was pruned, drop its now-orphaned
			// message list so the content pane doesn't show stale rows.
			if gone[activeID] {
				m.messages = nil
				m.filteredMessages = nil
			}
		}
		m.rebuildSidebar()
		return m, nil

	case AddressBookLoadedMsg:
		if msg.Err == nil {
			m.addressBook = msg.Addresses
		}
		return m, nil

	case AccountTestedMsg:
		m.accountManager.busy = false
		m.accountManager.busyMsg = ""
		if msg.Err != nil {
			detail := m.accountManager.redactSensitive(msg.Err.Error())
			m.accountManager.statusMsg = fmt.Sprintf("TEST FAILED: %s", detail)
			return m, nil
		}
		m.accountManager.statusMsg = fmt.Sprintf("CONNECTED: %d MAILBOXES", msg.MailboxCount)
		return m, nil

	case AccountDeletedMsg:
		m.accountManager.busy = false
		m.accountManager.busyMsg = ""
		m.accountManager.statusMsg = ""
		if msg.Err != nil {
			m.accountManager.statusMsg = fmt.Sprintf("DELETE FAILED: %v", msg.Err)
			m.setStatus(fmt.Sprintf("delete failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.sidebarCursor = 0
		m.sidebarOffset = 0
		m.messageCursor = 0
		m.clearMessages()
		m.accountManager = m.newAccountManager()
		m.accountManager.mode = amList
		m.accountManager.statusMsg = "DELETED ACCOUNT"
		return m, m.loadAccountsCmd()

	case MessageReadUpdatedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("mark read failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		for i := range m.messages {
			if m.messages[i].ID == msg.MessageID {
				m.messages[i].Read = msg.Read
				break
			}
		}
		if msg.MailboxID != 0 && msg.WasRead != msg.Read {
			delta := int64(1)
			if msg.Read {
				delta = -1
			}
			m.adjustMailboxUnreadCount(msg.MailboxID, delta)
		}
		m.applyFilter()
		if len(m.filteredMessages) == 0 {
			m.messageCursor = 0
			m.listOffset = 0
			m.clearViewportMessage()
			return m, nil
		}
		rowCount := m.activeMessageRowCount()
		if idx := m.indexOfFilteredMessage(msg.MessageID); msg.Advance && idx >= 0 && idx == m.messageCursor && idx < rowCount-1 {
			m.messageCursor = idx + 1
			visible := max(1, m.articleRowsVisible())
			if m.messageCursor >= m.listOffset+visible {
				m.listOffset = m.messageCursor - visible + 1
			}
		} else {
			m.messageCursor = clamp(m.messageCursor, 0, max(0, rowCount-1))
			m.listOffset = clamp(m.listOffset, 0, max(0, rowCount-1))
		}
		m.setViewportForCurrentRow()
		return m, nil

	case FolderCreatedMsg:
		if msg.Err != nil {
			m.setStatus("create folder failed: "+msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		if m.mailboxByID(msg.MailboxID) == nil {
			m.mailboxes = append(m.mailboxes, db.Mailbox{ID: msg.MailboxID, AccountID: msg.AccountID, Name: msg.Name, Delimiter: msg.Delimiter})
		}
		m.rebuildSidebar()
		if m.overlay == overlayMoveMessage {
			m.movePicker.creating = false
			m.movePicker.currentPath = msg.Name
			m.movePicker.cursor = 0
			m.refreshMovePickerEntries()
		}
		m.setStatus("folder created: "+msg.Name, false)
		return m, m.clearStatusCmd()

	case FilterGeneratedMsg:
		if m.overlay == overlayFilterManager {
			if msg.Err != nil {
				m.filterManager.mode = fmInput
				m.filterManager.input.Focus()
				m.filterManager.status = "generate failed: " + msg.Err.Error()
				return m, nil
			}
			m.filterManager.draft = msg.Rule
			m.filterManager.mode = fmReview
			m.filterManager.status = ""
		}
		return m, nil

	case FilterRunMsg:
		if msg.Err != nil {
			m.filterManager.status = "error: " + msg.Err.Error()
			m.setStatus("filter run error: "+msg.Err.Error(), true)
		} else if msg.DryRun {
			m.filterManager.status = fmt.Sprintf("test: %d message(s) would match", msg.Matched)
		} else {
			m.filterManager.status = fmt.Sprintf("applied to %d of %d matched", msg.Applied, msg.Matched)
		}
		cmds := []tea.Cmd{m.clearStatusCmd()}
		if !msg.DryRun && msg.Applied > 0 {
			cmds = append(cmds, m.loadAccountsCmd())
		}
		return m, tea.Batch(cmds...)

	case MessageMovedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("%s failed: %v", msg.Action, msg.Err), true)
			return m, m.clearStatusCmd()
		}
		if m.removeMessageFromMemory(msg.MessageID) {
			m.adjustMailboxUnreadCount(msg.FromMailboxID, -1)
		}
		if msg.Action == "" {
			msg.Action = "move"
		}
		m.setStatus(msg.Action+"d", false)
		return m, m.clearStatusCmd()

	case MessagesDeletedMsg:
		for _, ref := range msg.Deleted {
			if m.removeMessageFromMemory(ref.ID) {
				m.adjustMailboxUnreadCount(ref.MailboxID, -1)
			}
		}
		switch {
		case msg.Failed > 0 && len(msg.Deleted) == 0:
			m.setStatus(fmt.Sprintf("delete failed: %v", msg.Err), true)
		case msg.Failed > 0:
			m.setStatus(fmt.Sprintf("deleted %d; %d failed and kept: %v", len(msg.Deleted), msg.Failed, msg.Err), true)
		case len(msg.Deleted) > 1:
			m.setStatus(fmt.Sprintf("deleted %d", len(msg.Deleted)), false)
		default:
			m.setStatus("deleted", false)
		}
		return m, m.clearStatusCmd()

	case GrammarCheckedMsg:
		m.compose.busy = false
		if msg.Err != nil {
			m.compose.statusMsg = "grammar check failed"
			m.compose.isErr = true
		} else {
			m.grammarOriginal = m.compose.bodyInput.Value()
			m.grammarCorrected = msg.Corrected
			m.overlay = overlayGrammarPreview
		}
		return m, nil

	case MailboxReadUpdatedMsg:
		if len(msg.MailboxIDs) > 0 {
			m.markMailboxesReadInMemory(msg.MailboxIDs)
		}
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("mark read failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		return m, nil

	case MessageSentMsg:
		draftID := m.compose.draftID
		m.compose = ComposeModel{}
		m.overlay = overlayNone
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("send failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		m.setStatus("message sent", false)
		if draftID != 0 {
			return m, tea.Batch(m.deleteDraftCmd(draftID), m.clearStatusCmd())
		}
		return m, m.clearStatusCmd()

	case DraftSavedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("draft save failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		if m.overlay == overlayCompose && msg.DraftID != 0 {
			m.compose.draftID = msg.DraftID
			m.compose.dirty = false
			m.compose.lastAutosaved = time.Now()
		}
		return m, nil

	case DraftDeletedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("draft delete failed: %v", msg.Err), true)
			// The list row was removed optimistically; the draft still exists, so
			// reload to bring it back rather than hiding it until the next refresh.
			cmds := []tea.Cmd{m.clearStatusCmd()}
			if mb := m.selectedMailbox(); mb != nil && m.isDraftsMailbox(*mb) {
				cmds = append(cmds, m.loadDraftsCmd(mb.ID))
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case AISummaryFetchedMsg:
		m.summaryGenerating = false
		if msg.Err != nil {
			m.summaryErr = msg.Err.Error()
			return m, nil
		}
		if err := m.db.SaveSummary(msg.MessageID, msg.Summary); err != nil {
			fmt.Fprintf(os.Stderr, "save summary failed (message %d): %v\n", msg.MessageID, err)
			m.setStatus(fmt.Sprintf("summary not saved: %v", err), true)
		}
		var markReadMsg *db.Message
		for i := range m.messages {
			if m.messages[i].ID == msg.MessageID {
				m.messages[i].Summary = msg.Summary
				if m.cfg.AI.MarkReadOnSummarize && !m.messages[i].Read {
					cp := m.messages[i]
					markReadMsg = &cp
				}
			}
		}
		m.applyFilter()
		if m.summaryMessage.ID == msg.MessageID {
			m.summaryMessage.Summary = msg.Summary
		}
		if markReadMsg != nil {
			return m, m.setMessageReadCmd(*markReadMsg, true, false)
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

	case ClipboardReadMsg:
		if msg.Err != nil {
			m.setStatus("paste failed: "+msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		if m.overlay == overlayCompose && msg.Text != "" {
			sanitized := sanitizePaste(msg.Text)
			m.setStatus(fmt.Sprintf("pasted %d chars", len(sanitized)), false)
			return m.handleCompose(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(sanitized), Paste: true})
		}
		return m, nil

	case AttachmentsSavedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("save attachments failed: %v", msg.Err), true)
		} else {
			m.setStatus(fmt.Sprintf("saved %d attachment(s) to %s", msg.Count, msg.Path), false)
		}
		return m, m.clearStatusCmd()

	case ErrMsg:
		m.setStatus(msg.Err.Error(), true)
		return m, m.clearStatusCmd()

	case tea.KeyMsg:
		return m.handleKey(msg)

	default:
		if m.overlay == overlayAccountManager {
			return m.handleAccountManager(msg)
		}
		if m.overlay == overlayContactManager {
			return m.handleContactManager(msg)
		}
		if m.overlay == overlaySettings {
			return m.handleSettings(msg)
		}
		if m.overlay == overlayCompose {
			return m.handleCompose(msg)
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
	// Search mode editing intercept — capture typed characters for the query.
	if m.searchMode && m.searchEditing {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			return m.exitSearchMode()
		case keyMatches(msg, m.keys.Confirm):
			m.searchEditing = false
			m.searchInput.Blur()
			return m, nil
		case keyMatches(msg, m.keys.Backspace):
			return m.handleSearchCharacter(deleteLastRune(m.searchQuery))
		default:
			if msg.String() != "" && len(msg.Runes) > 0 {
				return m.handleSearchCharacter(m.searchQuery + string(msg.Runes))
			}
		}
		return m, nil
	}

	// Esc exits search mode when not editing.
	if m.searchMode && keyMatches(msg, m.keys.Cancel) {
		return m.exitSearchMode()
	}

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

	case keyMatches(msg, m.keys.AccountManager):
		m.overlay = overlayAccountManager
		m.accountManager = m.newAccountManager()
		return m, nil

	case keyMatches(msg, m.keys.ContactManager):
		m.overlay = overlayContactManager
		m.contactManager = NewContactManager(m.db)
		return m, nil

	case keyMatches(msg, m.keys.ThemePicker):
		m.overlay = overlayThemePicker
		m.themeCursor = m.confirmedTheme
		return m, nil

	case keyMatches(msg, m.keys.Search):
		if m.focused == paneContent && m.contentMessageID != 0 {
			m.overlay = overlayContentSearch
			m.contentSearchInput.Reset()
			m.contentSearchInput.Focus()
			return m, nil
		}
		if m.searchMode {
			// Already in search mode — re-enter editing.
			m.searchEditing = true
			m.searchInput.SetValue(m.searchQuery)
			m.searchInput.Focus()
			return m, nil
		}
		// Enter persistent search mode.
		m.searchMode = true
		m.searchEditing = true
		m.searchInput.SetValue(m.searchQuery)
		m.searchInput.Focus()
		return m, nil

	case keyMatches(msg, m.keys.Command):
		m.openCommandPalette(overlayNone, commandPaletteMain)
		return m, nil

	case keyMatches(msg, m.keys.UnreadOnly):
		if m.focused == paneAccounts {
			return m, nil
		}
		m.showUnreadOnly = !m.showUnreadOnly
		var currentID int64
		if msg := m.currentRowMessage(); msg != nil {
			currentID = msg.ID
		}
		m.applyFilter()
		if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
			m.messageCursor = idx
		} else {
			m.messageCursor = clamp(m.messageCursor, 0, max(0, m.activeMessageRowCount()-1))
		}
		if m.showUnreadOnly {
			m.setStatus("showing unread only", false)
		} else {
			m.setStatus("showing all messages", false)
		}
		if m.activeMessageRowCount() > 0 {
			m.setViewportForCurrentRow()
		} else {
			m.clearViewportMessage()
		}
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.ToggleThreads):
		if m.focused == paneAccounts || m.selectedDraftsMailbox() {
			return m, nil
		}
		var currentID int64
		if msg := m.currentRowMessage(); msg != nil {
			currentID = msg.ID
		}
		m.cfg.Display.ThreadedConversations = !m.cfg.Display.ThreadedConversations
		m.applyFilter()
		if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
			m.messageCursor = idx
		} else {
			m.messageCursor = clamp(m.messageCursor, 0, max(0, m.activeMessageRowCount()-1))
		}
		if m.activeMessageRowCount() > 0 {
			m.setViewportForCurrentRow()
		} else {
			m.clearViewportMessage()
		}
		m.saveConfig()
		if m.cfg.Display.ThreadedConversations {
			m.setStatus("threaded conversations on", false)
		} else {
			m.setStatus("threaded conversations off", false)
		}
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.NextPane):
		return m.focusPane(pane((int(m.focused) + 1) % 3))

	case keyMatches(msg, m.keys.PrevPane):
		return m.focusPane(pane((int(m.focused) + 2) % 3))

	case keyMatches(msg, m.keys.ResizeAccountsNarrow):
		m.resizeAccountsPane(-accountsPaneStepPercent)
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.ResizeAccountsWide):
		m.resizeAccountsPane(accountsPaneStepPercent)
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.ResizeMessagesShorter):
		m.resizeMessagesPane(-messagesPaneStepPercent)
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.ResizeMessagesTaller):
		m.resizeMessagesPane(messagesPaneStepPercent)
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.Left):
		if m.focused > paneAccounts {
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

	case keyMatches(msg, m.keys.VisualSelect):
		if m.focused == paneContent && m.contentMessageID != 0 {
			m.startContentSelection()
		}
		return m, nil

	case keyMatches(msg, m.keys.VisualLine):
		if m.focused == paneContent && m.contentMessageID != 0 {
			m.selectAllContentLines()
		}
		return m, nil

	case msg.String() == "y" || msg.String() == "ctrl+c":
		if m.focused == paneContent && m.contentMessageID != 0 {
			text := m.contentSelectionText(msg.String() == "ctrl+c")
			if text == "" {
				m.setStatus("nothing to copy", false)
				return m, m.clearStatusCmd()
			}
			m.clearContentSelection()
			return m, clipboardWriteCmd(text)
		}
		return m, nil

	case keyMatches(msg, m.keys.Enter):
		if m.focused == paneAccounts {
			if m.toggleSelectedAccount() {
				return m, nil
			}
			if m.toggleSelectedSection() {
				return m, nil
			}
		}
		if m.focused == paneMessages && (m.activeMessageRowCount() > 0 || (m.selectedDraftsMailbox() && len(m.drafts) > 0)) {
			return m.focusPane(paneContent)
		}
		return m, nil

	case keyMatches(msg, m.keys.Back):
		if m.focused == paneContent {
			if m.contentSelectionActive {
				m.clearContentSelection()
				return m, nil
			}
			m.focused = paneMessages
			return m, nil
		}
		if m.focused == paneMessages && m.hasSelection() {
			m.clearSelection()
			return m, nil
		}
		return m, nil

	case keyMatches(msg, m.keys.Sync):
		if m.selectedUnifiedInbox() {
			// Sync all inboxes when 'f' is pressed on Unified Inbox
			var cmds []tea.Cmd
			for _, mb := range m.mailboxes {
				if isInboxMailbox(mb) {
					cmds = append(cmds, m.syncMailboxCmd(mb.ID, true))
				}
			}
			if len(cmds) > 0 {
				return m, tea.Batch(cmds...)
			}
		}
		if selected := m.selectedMailbox(); selected != nil {
			return m, m.syncMailboxCmd(selected.ID, true)
		}
		return m, nil

	case keyMatches(msg, m.keys.SyncAll):
		var cmds []tea.Cmd
		for _, mb := range m.mailboxes {
			cmds = append(cmds, m.syncMailboxCmd(mb.ID, false))
		}
		return m, tea.Batch(cmds...)

	case keyMatches(msg, m.keys.MarkRead):
		if m.focused == paneMessages && m.activeMessageRowCount() > 0 {
			msgs := m.selectedActionMessages()
			if len(msgs) == 0 {
				return m, nil
			}
			var cmds []tea.Cmd
			if !m.hasSelection() {
				read := false
				for _, msg2 := range msgs {
					if !msg2.Read {
						read = true
						break
					}
				}
				for i, msg2 := range msgs {
					advance := !msg2.Read && read && len(msgs) == 1 && i == 0
					cmds = append(cmds, m.setMessageReadCmd(msg2, read, advance))
				}
				return m, tea.Batch(cmds...)
			}
			for _, msg2 := range msgs {
				read := !msg2.Read
				advance := false
				cmds = append(cmds, m.setMessageReadCmd(msg2, read, advance))
			}
			m.clearSelection()
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case keyMatches(msg, m.keys.SelectAll):
		if m.focused == paneMessages && m.activeMessageRowCount() > 0 {
			for _, msg2 := range m.filteredMessages {
				m.selectedMessages[msg2.ID] = true
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.Archive):
		if m.focused != paneAccounts && m.activeMessageRowCount() > 0 {
			msgs := m.selectedActionMessages()
			var cmds []tea.Cmd
			for _, msg2 := range msgs {
				cmds = append(cmds, m.archiveMessageCmd(msg2))
			}
			if m.hasSelection() {
				m.clearSelection()
			}
			return m, tea.Batch(cmds...)
		}
		return m, nil

	case keyMatches(msg, m.keys.Move):
		if m.focused != paneAccounts && m.activeMessageRowCount() > 0 {
			m.openMovePicker(m.movePickerMessages())
		}
		return m, nil

	case keyMatches(msg, m.keys.Delete):
		if m.focused == paneMessages && m.selectedDraftsMailbox() && len(m.drafts) > 0 {
			idx := clamp(m.messageCursor, 0, len(m.drafts)-1)
			id := m.drafts[idx].ID
			m.drafts = append(m.drafts[:idx], m.drafts[idx+1:]...)
			m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.drafts)-1))
			return m, m.deleteDraftCmd(id)
		}
		if m.focused != paneAccounts && m.activeMessageRowCount() > 0 {
			selected := m.selectedActionMessages()
			if len(selected) > 0 {
				if len(selected) > 1 && m.hasSelection() {
					m.pendingBulkDelete = append([]db.Message(nil), selected...)
					m.overlay = overlayBulkDeleteConfirm
					return m, nil
				}
				m.pendingBulkDelete = nil
				m.clearSelection()
				return m, m.deleteMessagesCmd(selected)
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.Reply):
		var cur *db.Message
		if m.focused == paneContent && m.contentMessageID != 0 {
			cur = m.currentContentMessage()
		} else if m.focused == paneMessages {
			cur = m.currentRowMessage()
		}
		if cur != nil {
			acfg := m.accountCfgForMailbox(cur.MailboxID)
			m.compose = NewReply(*cur, acfg, m.cfg.Accounts)
			m.overlay = overlayCompose
		}
		return m, nil

	case keyMatches(msg, m.keys.Forward):
		var cur *db.Message
		if m.focused == paneContent && m.contentMessageID != 0 {
			cur = m.currentContentMessage()
		} else if m.focused == paneMessages {
			cur = m.currentRowMessage()
		}
		if cur != nil {
			acfg := m.accountCfgForMailbox(cur.MailboxID)
			m.compose = NewForward(*cur, acfg, m.cfg.Accounts)
			m.overlay = overlayCompose
		}
		return m, nil

	case keyMatches(msg, m.keys.Compose):
		var acfg config.AccountConfig
		if len(m.cfg.Accounts) > 0 {
			acfg = m.cfg.Accounts[0]
		}
		m.compose = NewCompose(acfg, m.cfg.Accounts, m.addressBook)
		m.overlay = overlayCompose
		return m, nil

	case keyMatches(msg, m.keys.MarkAllRead):
		if mailbox := m.selectedMailbox(); mailbox != nil {
			return m, m.markMailboxReadCmd(mailbox.ID)
		}
		if accountID, ok := m.selectedAccountID(); ok {
			return m, m.markAccountReadCmd(accountID)
		}
		return m, nil

	case keyMatches(msg, m.keys.OpenBrowser):
		if len(m.filteredMessages) > 0 && m.focused == paneContent {
			if link, ok := m.focusedLineLink(); ok {
				return m, m.openBrowserCmd(link)
			}
			if link, ok := m.currentContentLink(); ok {
				return m, m.openBrowserCmd(link)
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.NextLink):
		if m.focused == paneContent && m.actionableLinksEnabled() {
			m.stepContentLink(1)
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.PrevLink):
		if m.focused == paneContent && m.actionableLinksEnabled() {
			m.stepContentLink(-1)
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.SaveAttach):
		if m.focused == paneContent && m.contentMessageID != 0 && len(m.contentAttachments) > 0 {
			home, err := os.UserHomeDir()
			if err != nil {
				return m, saveAttachmentsCmd(m.contentAttachments)
			}
			m.openSaveAttachPicker(filepath.Join(home, "Downloads"))
			m.overlay = overlaySaveAttach
			return m, nil
		}
		return m, nil

	case keyMatches(msg, m.keys.Summary):
		if m.focused != paneAccounts && m.activeMessageRowCount() > 0 {
			return m.openSummary()
		}
		return m, nil

	case keyMatches(msg, m.keys.ContentSearch):
		if m.focused == paneContent && m.contentMessageID != 0 {
			m.overlay = overlayContentSearch
			m.contentSearchInput.Reset()
			m.contentSearchInput.Focus()
		}
		return m, nil

	case keyMatches(msg, m.keys.ToggleHeaders):
		if m.focused == paneContent && m.contentMessageID != 0 {
			m.contentShowHeaders = !m.contentShowHeaders
			m.setViewportForCurrentRow()
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
		if m.focused == paneAccounts {
			if m.toggleSelectedAccount() {
				return m, nil
			}
			if m.toggleSelectedSection() {
				return m, nil
			}
		}
		if m.focused == paneMessages && m.activeMessageRowCount() > 0 {
			m.toggleCurrentRowSelection()
			// Auto-advance cursor for rapid multi-select
			if m.messageCursor < m.activeMessageRowCount()-1 {
				m.messageCursor++
				visible := m.articleRowsVisible()
				if m.messageCursor >= m.listOffset+visible {
					m.listOffset = m.messageCursor - visible + 1
				}
			}
			// Preview the now-focused message, just like cursor navigation, so the
			// user sees what they're advancing through while multi-selecting.
			if !m.selectedDraftsMailbox() {
				if msg2 := m.currentRowMessage(); msg2 != nil {
					m.setViewportForCurrentRow()
					return m, m.focusedMessageChangedCmd(*msg2)
				}
			}
			return m, nil
		}
		return m, nil
	}

	if m.focused == paneContent {
		var cmd tea.Cmd
		m.viewport, cmd = m.viewport.Update(msg)
		return m, cmd
	}

	return m, nil
}

func (m Model) focusPane(next pane) (tea.Model, tea.Cmd) {
	wasMessages := m.focused == paneMessages
	if m.focused == paneContent && next != paneContent {
		m.clearContentSelection()
	}
	m.focused = next
	if wasMessages && next == paneContent && m.selectedDraftsMailbox() && len(m.drafts) > 0 {
		idx := clamp(m.messageCursor, 0, len(m.drafts)-1)
		m.compose = NewComposeFromDraft(m.drafts[idx], m.cfg.Accounts, m.addressBook)
		m.overlay = overlayCompose
		m.focused = paneMessages
		return m, nil
	}
	if wasMessages && next == paneContent {
		if msg := m.currentRowMessage(); msg != nil {
			return m, m.openedMessageCmd(*msg)
		}
	}
	if !wasMessages && next == paneMessages {
		if msg := m.currentRowMessage(); msg != nil {
			return m, m.focusedMessageChangedCmd(*msg)
		}
	}
	return m, nil
}

func (m Model) handleUp() (tea.Model, tea.Cmd) {
	switch m.focused {
	case paneAccounts:
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
			m.clampSidebarOffset()
			if m.searchActive() {
				return m, nil
			}
			if m.selectedUnifiedInbox() {
				return m, m.loadUnifiedInboxCmd()
			}
			if selected := m.selectedMailbox(); selected != nil {
				cmd := m.loadMailboxMessagesCmd(selected.ID)
				if m.selectedDraftsMailbox() {
					cmd = tea.Batch(cmd, m.loadDraftsCmd(selected.ID))
				}
				return m, cmd
			}
			m.clearMessages()
		}
	case paneMessages:
		if m.messageCursor > 0 {
			m.messageCursor--
			if m.messageCursor < m.listOffset {
				m.listOffset = m.messageCursor
			}
			if m.selectedDraftsMailbox() {
				return m, nil
			}
			if msg2 := m.currentRowMessage(); msg2 != nil {
				m.setViewportForCurrentRow()
				return m, m.focusedMessageChangedCmd(*msg2)
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
	case paneAccounts:
		if m.sidebarCursor < len(m.sidebarRows)-1 {
			m.sidebarCursor++
			m.clampSidebarOffset()
			if m.searchActive() {
				return m, nil
			}
			if m.selectedUnifiedInbox() {
				return m, m.loadUnifiedInboxCmd()
			}
			if selected := m.selectedMailbox(); selected != nil {
				cmd := m.loadMailboxMessagesCmd(selected.ID)
				if m.selectedDraftsMailbox() {
					cmd = tea.Batch(cmd, m.loadDraftsCmd(selected.ID))
				}
				return m, cmd
			}
			m.clearMessages()
		}
	case paneMessages:
		count := m.activeMessageRowCount()
		if m.selectedDraftsMailbox() {
			count = len(m.drafts)
		}
		if m.messageCursor < count-1 {
			m.messageCursor++
			visible := m.articleRowsVisible()
			if m.messageCursor >= m.listOffset+visible {
				m.listOffset = m.messageCursor - visible + 1
			}
			if m.selectedDraftsMailbox() {
				return m, nil
			}
			if msg2 := m.currentRowMessage(); msg2 != nil {
				m.setViewportForCurrentRow()
				return m, m.focusedMessageChangedCmd(*msg2)
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

// composeBodyIsVim reports whether the compose overlay is active with the body
// focused in vim mode — the one place `:` belongs to the editor's command line
// rather than TideMail's command palette.
func (m Model) composeBodyIsVim() bool {
	return m.overlay == overlayCompose &&
		m.compose.focusedField == composeFieldBody &&
		m.compose.bodyInput.vimMode()
}

func (m Model) handleOverlayKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	// The command palette keys (`:` / ctrl+p) open it over an overlay — except
	// `:` in the vim compose body, where it's the editor's command line
	// (`:w`/`:q`); there we fall through so the key reaches handleCompose → the
	// body editor. ctrl+p always opens the palette, so it stays reachable from
	// the vim body (where `:` is taken) and behaves the same as in the main UI.
	if keyMatches(msg, m.keys.Command) && (msg.String() != ":" || !m.composeBodyIsVim()) {
		switch m.overlay {
		case overlayCompose:
			if !m.compose.picker.active {
				m.openCommandPalette(overlayCompose, commandPaletteCompose)
				return m, nil
			}
		case overlaySummary:
			m.openCommandPalette(overlaySummary, commandPaletteSummary)
			return m, nil
		case overlaySaveAttach:
			m.openCommandPalette(overlaySaveAttach, commandPaletteSaveAttach)
			return m, nil
		}
	}

	switch m.overlay {
	case overlayQuitConfirm:
		switch {
		case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
			return m, tea.Quit
		case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
			m.overlay = overlayNone
		}
		return m, nil

	case overlayDraftCloseConfirm:
		switch {
		case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
			return m.closeComposeSavingDraft()
		case keyMatches(msg, m.keys.DiscardDraft, m.keys.No):
			return m.discardComposeDraft()
		case keyMatches(msg, m.keys.Cancel):
			m.overlay = overlayCompose
			return m, nil
		}
		return m, nil

	case overlayBulkDeleteConfirm:
		switch {
		case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
			selected := append([]db.Message(nil), m.pendingBulkDelete...)
			m.pendingBulkDelete = nil
			m.overlay = overlayNone
			m.clearSelection()
			if len(selected) == 0 {
				return m, nil
			}
			return m, m.deleteMessagesCmd(selected)
		case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
			m.pendingBulkDelete = nil
			m.overlay = overlayNone
			return m, nil
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
				if m.activeMessageRowCount() > 0 {
					m.setViewportForCurrentRow()
				}
			}
		case keyMatches(msg, m.keys.Down):
			if m.themeCursor < len(BuiltinThemes)-1 {
				m.themeCursor++
				m.activeTheme = m.themeCursor
				m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
				if m.activeMessageRowCount() > 0 {
					m.setViewportForCurrentRow()
				}
			}
		case keyMatches(msg, m.keys.Confirm):
			m.confirmedTheme = m.themeCursor
			m.overlay = overlayNone
			m.cfg.Theme = BuiltinThemes[m.confirmedTheme].Name
			m.saveConfig()
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
			}
		case keyMatches(msg, m.keys.Cancel):
			m.activeTheme = m.confirmedTheme
			m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
			m.overlay = overlayNone
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
			}
		}
		if m.activeTheme != prevTheme {
			return m, setTermColorsCmd(m.styles.Theme.Fg, m.styles.Theme.Bg)
		}
		return m, nil

	case overlayAccountManager:
		return m.handleAccountManager(msg)

	case overlayContactManager:
		return m.handleContactManager(msg)

	case overlayCompose:
		return m.handleCompose(msg)

	case overlaySaveAttach:
		return m.handleSaveAttachPicker(msg)

	case overlayMoveMessage:
		return m.handleMovePicker(msg)

	case overlayFilterManager:
		return m.handleFilterManager(msg)

	case overlayGrammarPreview:
		switch {
		case keyMatches(msg, m.keys.Yes):
			m.compose.bodyInput.SetValue(m.grammarCorrected + m.grammarQuoteSuffix)
			m.grammarQuoteSuffix = ""
			m.compose.statusMsg = "grammar checked"
			m.compose.isErr = false
			m.overlay = overlayCompose
			// Return focus to the body at the top in Insert mode — SetValue parks
			// the caret at the end (inside the quote, off-screen), which made it
			// look like typing did nothing.
			m.compose.focusBodyAtStart()
		case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
			m.grammarQuoteSuffix = ""
			m.compose.statusMsg = ""
			m.overlay = overlayCompose
			m.compose.focusBodyAtStart()
		}
		return m, nil

	case overlayLogViewer:
		if keyMatches(msg, m.keys.Cancel, m.keys.Back) {
			m.overlay = overlaySettings
			return m, nil
		}
		var cmd tea.Cmd
		m.helpVP, cmd = m.helpVP.Update(msg)
		return m, cmd

	case overlayHelp:
		if m.helpSearchActive {
			switch {
			case keyMatches(msg, m.keys.Cancel):
				m.helpSearchActive = false
				m.helpSearchInput.SetValue("")
				m.helpSearchInput.Blur()
				m.resetHelpVP()
			case keyMatches(msg, m.keys.Confirm):
				// Stop editing but keep the filter so the list stays scrollable.
				m.helpSearchActive = false
				m.helpSearchInput.Blur()
			default:
				var cmd tea.Cmd
				m.helpSearchInput, cmd = m.helpSearchInput.Update(msg)
				m.resetHelpVP()
				return m, cmd
			}
			return m, nil
		}
		switch {
		case keyMatches(msg, m.keys.Search):
			m.helpSearchActive = true
			m.helpSearchInput.Focus()
			return m, nil
		case keyMatches(msg, m.keys.Back, m.keys.Help, m.keys.Quit):
			// esc/q/? first clear an applied filter, then close.
			if m.helpSearchInput.Value() != "" && keyMatches(msg, m.keys.Back) {
				m.helpSearchInput.SetValue("")
				m.resetHelpVP()
				return m, nil
			}
			m.overlay = overlayNone
			m.helpSearchActive = false
			m.helpSearchInput.SetValue("")
			return m, nil
		}
		var cmd tea.Cmd
		m.helpVP, cmd = m.helpVP.Update(msg)
		return m, cmd

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

	case overlayCommandPalette:
		return m.handleCommandPaletteKey(msg)
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
		if m.activeMessageRowCount() > 0 {
			m.setViewportForCurrentRow()
		}
		cmd = tea.Batch(cmd, setTermColorsCmd(m.styles.Theme.Fg, m.styles.Theme.Bg))
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
		if m.updateInstall.Restartable && m.updateInstall.ExecutablePath != "" {
			// Record the target and quit. main execs it once the program has
			// exited and BubbleTea has restored the terminal — a clean handoff
			// with no second process competing for the terminal.
			m.restartExecPath = m.updateInstall.ExecutablePath
			return m, tea.Quit
		}
		return m, nil
	case settingsActionOpenRepo, settingsActionOpenIssues:
		if url := settingsActionURL(action); url != "" {
			return m, m.openBrowserCmd(url)
		}
		return m, nil
	case settingsActionViewLogs:
		m.overlay = overlayLogViewer
		return m, nil
	case settingsActionCopyManualInstall:
		cmd := strings.TrimSpace(m.settings.update.manualCommand)
		if cmd == "" {
			return m, nil
		}
		return m, clipboardWriteCmd(cmd)
	}
	if done {
		if m.settings.shouldSave {
			if msg := m.currentRowMessage(); msg != nil {
				m.pendingSelectMessageID = msg.ID
			}
			m.cfg = m.settings.ApplyTo(m.cfg)
			m.showUnreadOnly = m.cfg.Display.DefaultUnreadOnly
			// Apply the vim toggle to future compose editors (read in newEditorArea).
			setEditorVimMode(m.cfg.Display.ComposeVim)
			merged, _ := MergedThemeFromConfig(m.cfg)
			m.styles = BuildStyles(merged, m.cfg.Display.Density)
			if ThemeUsesASCII(merged.Name) {
				m.spinner.Spinner = spinner.Line
			} else {
				m.spinner.Spinner = spinner.Dot
			}
			m.saveConfig()
			summarizer, _ := ai.New(m.cfg.AI)
			m.summarizer = summarizer
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
			} else {
				m.clearViewportMessage()
			}
			m.overlay = overlayNone
			m.clearMessages()
			return m, tea.Batch(m.loadAccountsCmd())
		}
		m.overlay = overlayNone
		if previewingTheme {
			merged, _ := MergedThemeFromConfig(m.cfg)
			m.styles = BuildStyles(merged, m.cfg.Display.Density)
			if m.activeMessageRowCount() > 0 {
				m.setViewportForCurrentRow()
			}
			return m, setTermColorsCmd(m.styles.Theme.Fg, m.styles.Theme.Bg)
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
		if !m.summaryGenerating && m.summaryErr == "" && m.summaryMessage.Summary != "" {
			return m, clipboardWriteCmd(m.summaryMessage.Summary)
		}
	case keyMatches(msg, m.keys.SaveMD):
		if !m.summaryGenerating && m.summaryErr == "" && m.summaryMessage.Summary != "" {
			return m, saveSummaryMDCmd(m.summaryMessage, m.summaryMessage.Summary, m.cfg.AI.SavePath)
		}
	case keyMatches(msg, m.keys.ToggleQuote):
		if m.focused == paneContent && m.contentMessageID != 0 {
			m.contentQuotesCollapsed = !m.contentQuotesCollapsed
		}
	}
	return m, nil
}

func (m Model) handleAccountManager(msg tea.Msg) (tea.Model, tea.Cmd) {
	am := m.accountManager
	newAM, cmd, exit := am.Update(msg, m.keys)
	m.accountManager = newAM
	if exit {
		m.overlay = overlayNone
		return m, m.loadAccountsCmd()
	}
	return m, cmd
}

func (m Model) handleContactManager(msg tea.Msg) (tea.Model, tea.Cmd) {
	newCM, cmd, exit := m.contactManager.Update(msg, m.keys)
	m.contactManager = newCM
	if len(m.contactManager.composeTo) > 0 {
		var acfg config.AccountConfig
		if len(m.cfg.Accounts) > 0 {
			acfg = m.cfg.Accounts[0]
		}
		to := strings.Join(m.contactManager.composeTo, ", ")
		m.contactManager.composeTo = nil
		m.contactManager.clearMarks()
		m.compose = NewCompose(acfg, m.cfg.Accounts, m.addressBook)
		m.compose.toInput.SetValue(to)
		m.overlay = overlayCompose
		return m, nil
	}
	if exit {
		m.overlay = overlayNone
		// Refresh autocomplete: edits in the manager change suggestions.
		return m, m.loadAddressBookCmd()
	}
	return m, cmd
}

func (m Model) handleCompose(msg tea.Msg) (tea.Model, tea.Cmd) {
	// Keep the stored body editor sized to the SAME width and height View renders
	// with, so vertical navigation and the viewport track what the user sees. View
	// sizes only a copy (it's a value receiver), so the stored editor must be
	// synced here. The overlay dimensions mirror renderOverlay (overlays.go).
	if m.width > 0 && m.height > 0 {
		_, _, bw, bh := m.compose.composeLayout(composeOverlayWidth(m.width), min(m.height-4, 36), m.styles)
		m.compose.bodyInput.SetSize(bw, bh)
	}
	// Intercept grammar check key before compose gets it
	if km, ok := msg.(tea.KeyMsg); ok && keyMatches(km, m.keys.GrammarCheck) {
		// Only check the reply the user wrote, not the quoted/forwarded original.
		reply, quote := splitReplyAndQuote(m.compose.bodyInput.Value())
		if strings.TrimSpace(reply) == "" {
			m.compose.statusMsg = "nothing to check"
		} else if m.summarizer == nil {
			m.compose.statusMsg = "AI not configured — press S to open settings"
		} else {
			m.compose.busy = true
			m.compose.statusMsg = "checking grammar..."
			m.grammarQuoteSuffix = quote
			return m, m.grammarCheckCmd(reply)
		}
	}
	if km, ok := msg.(tea.KeyMsg); ok && keyMatches(km, m.keys.PasteText) && !m.compose.picker.active {
		return m, clipboardReadCmd()
	}
	// Sanitize system paste to prevent textarea corruption from combining marks.
	if km, ok := msg.(tea.KeyMsg); ok && km.Paste {
		sanitized := sanitizePaste(string(km.Runes))
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(sanitized), Paste: true}
	}
	before := m.compose
	newC, cmd, exit := m.compose.Update(msg, m.keys)
	m.compose = newC
	if km, ok := msg.(tea.KeyMsg); ok && km.Paste {
		m.setStatus(fmt.Sprintf("pasted %d chars", len(km.Runes)), false)
		m.compose.dirty = true
	}
	if composeChanged(before, m.compose) {
		m.compose.dirty = true
		if m.compose.hasContent() {
			save := m.saveDraftCmd(m.compose)
			// Preserve any command the edit produced (e.g. cut's clipboard
			// write) — autosaving must not swallow it.
			if cmd != nil {
				return m, tea.Batch(cmd, save)
			}
			return m, save
		}
	}
	if exit {
		if m.compose.hasContent() {
			m.overlay = overlayDraftCloseConfirm
			return m, nil
		}
		if m.compose.draftID != 0 {
			id := m.compose.draftID
			m.overlay = overlayNone
			m.compose = ComposeModel{}
			return m, m.deleteDraftCmd(id)
		}
		m.overlay = overlayNone
		m.compose = ComposeModel{}
	}
	return m, cmd
}

func composeChanged(a, b ComposeModel) bool {
	if a.toInput.Value() != b.toInput.Value() || a.ccInput.Value() != b.ccInput.Value() || a.bccInput.Value() != b.bccInput.Value() || a.subjectInput.Value() != b.subjectInput.Value() || a.bodyInput.Value() != b.bodyInput.Value() {
		return true
	}
	if a.accountIndex != b.accountIndex || len(a.attachments) != len(b.attachments) {
		return true
	}
	for i := range a.attachments {
		if a.attachments[i].Name != b.attachments[i].Name || a.attachments[i].Path != b.attachments[i].Path || string(a.attachments[i].Data) != string(b.attachments[i].Data) {
			return true
		}
	}
	return false
}

// ── Save-attachment folder picker ────────────────────────────────────────

type commandItem struct {
	id      string
	label   string
	enabled bool
}

func (m Model) commandMessage() *db.Message {
	if msg := m.currentContentMessage(); msg != nil {
		return msg
	}
	return m.currentRowMessage()
}

// ── View ─────────────────────────────────────────────────────────────────────

func (m Model) View() string {
	if m.width == 0 {
		return ""
	}

	right := lipgloss.JoinVertical(lipgloss.Left,
		m.renderMessagesPane(),
		m.renderContentPane(),
	)
	main := lipgloss.JoinHorizontal(lipgloss.Top,
		m.renderAccountsPane(),
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
	case paneAccounts:
		hint = m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " move  " +
			m.keyHint(m.keys.Enter) + " toggle  " + m.keyHint(m.keys.Sync) + " sync"
	case paneMessages:
		hint = m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " move  " +
			m.keyHint(m.keys.Space) + " select  " +
			m.keyHint(m.keys.MarkRead) + " read  " +
			m.keyHint(m.keys.Archive) + " archive  " + m.keyHint(m.keys.Move) + " move  " + m.keyHint(m.keys.Delete) + " delete  " +
			m.keyHint(m.keys.Command) + " command"
	case paneContent:
		progress := ""
		if m.contentLineCount > 0 {
			pct := min(100, (m.viewport.YOffset+m.viewport.Height)*100/m.contentLineCount)
			progress = fmt.Sprintf("%d%%  ", pct)
		}
		hint = progress + m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " line  " +
			m.keyHint(m.keys.VisualSelect) + "/" + m.keyHint(m.keys.VisualLine) + " select  " +
			"y/ctrl+c copy  " +
			m.keyHint(m.keys.Reply) + " reply  " + m.keyHint(m.keys.Forward) + " fwd  " +
			m.keyHint(m.keys.Search) + " find  " +
			m.keyHint(m.keys.ToggleHeaders) + " headers  " +
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
	return style.UnsetPadding().Render(s)
}

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
		seg(k.Command),
		seg(k.AccountManager),
		seg(k.Settings),
		seg(k.Search),
		seg(k.Help),
	)
}

func (m *Model) currentContentMessage() *db.Message {
	for i := range m.filteredMessages {
		if m.filteredMessages[i].ID == m.contentMessageID {
			return &m.filteredMessages[i]
		}
	}
	return nil
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

	if len(m.mailboxes) > 0 {
		if m.selectedUnifiedInbox() {
			parts = append(parts, m.statusBarInlineText(sb, "Unified Inbox"))
			if unread := m.unifiedUnreadCount(); unread > 0 {
				parts = append(parts, m.statusBarInlineText(sb, fmt.Sprintf("%d unread", unread)))
			}
		} else if mb := m.selectedMailbox(); mb != nil {
			parts = append(parts, m.statusBarInlineText(sb, cleanDisplayName(mb.DisplayName)))
			if mb.UnreadCount > 0 {
				parts = append(parts, m.statusBarInlineText(sb, fmt.Sprintf("%d unread", mb.UnreadCount)))
			}
			if !mb.LastSynced.IsZero() && mb.LastSynced.Unix() > 0 {
				parts = append(parts, m.statusBarInlineText(sb, "synced "+m.formatTime(mb.LastSynced)))
			}
		} else if accountID, ok := m.selectedAccountID(); ok {
			parts = append(parts, m.statusBarInlineText(sb, m.accountName(accountID)))
			if unread := m.accountUnreadCount(accountID); unread > 0 {
				parts = append(parts, m.statusBarInlineText(sb, fmt.Sprintf("%d unread", unread)))
			}
		}
	}

	if len(m.syncing) > 0 {
		parts = append(parts, m.styles.StatusSpinner.Render(
			m.spinner.View()+" syncing..."),
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

// ── Save-attachment folder picker rendering ──────────────────────────────

// ── Commands ─────────────────────────────────────────────────────────────────

// notifyCmd fires a desktop notification for new mail from an auto-sync.

// startSyncTimers kicks off initial auto-sync timers for all accounts.

// logFetch writes a one-line fetch summary to the log file. Errors are
// silently ignored — logging is best-effort and must never fail a sync.

func (m *Model) clearStatusCmd() tea.Cmd {
	return tea.Tick(4*time.Second, func(time.Time) tea.Msg {
		return StatusClearMsg{}
	})
}

// spinnerActive reports whether any state that displays the spinner is in
// flight. It is the single gate for the spinner tick loop: when this is false
// the loop stops (see the spinner.TickMsg case) so the idle TUI does no work.
// Keep this in exact sync with every call site that renders m.spinner.View()
// (sidebar sync indicator, status bar "syncing…", update check, AI summary) —
// a state shown there but missing here leaves that spinner frozen.
func (m Model) spinnerActive() bool {
	return len(m.syncing) > 0 ||
		m.updateState == updateStateChecking ||
		m.summaryGenerating
}

// ensureSpinner starts the spinner tick loop if a loading state is active and
// the loop isn't already running. It returns nil when no tick should be
// scheduled, so callers can safely batch it into their returned command (a nil
// Cmd is dropped by tea.Batch). Call it wherever a state in spinnerActive
// begins.
func (m *Model) ensureSpinner() tea.Cmd {
	if !m.spinnerActive() || m.spinnerRunning {
		return nil
	}
	m.spinnerRunning = true
	return m.spinner.Tick
}

// ── Helpers ───────────────────────────────────────────────────────────────────

func (m *Model) rebuildSidebar() {
	m.sidebarRows = buildSidebarRows(m.accounts, m.mailboxes, m.collapsedAccounts, m.collapsedSections)
	m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))
	m.clampSidebarOffset()
}

func (m Model) sidebarVisibleRows() int {
	bodyLines := max(0, m.mainHeight()-1-m.styles.ListItemLineStride())
	return max(1, bodyLines/m.styles.ListItemLineStride())
}

func (m *Model) clampSidebarOffset() {
	visible := m.sidebarVisibleRows()
	if m.sidebarCursor < m.sidebarOffset {
		m.sidebarOffset = m.sidebarCursor
	}
	if m.sidebarCursor >= m.sidebarOffset+visible {
		m.sidebarOffset = m.sidebarCursor - visible + 1
	}
	m.sidebarOffset = clamp(m.sidebarOffset, 0, max(0, len(m.sidebarRows)-visible))
}

// cleanDisplayName strips common IMAP namespace prefixes and tidies
// casing for display in the sidebar. The raw Name is preserved for IMAP
// SELECT commands; this is purely cosmetic.

// isGmailSystemFolder reports whether name is a Gmail system label that is
// typically uninteresting in an IMAP client. These are hidden when the
// HideGmailSystem config toggle is enabled.

func (m Model) newAccountManager() AccountManager {
	am := NewAccountManager(m.db)
	am.mode = amList
	am.setData(m.accounts, m.mailboxes, m.cfg.Accounts)
	return am
}

func (m *Model) clearMessages() {
	m.messages = nil
	m.filteredMessages = nil
	m.messageThreads = nil
	m.drafts = nil
	m.messageCursor = 0
	m.listOffset = 0
	m.clearViewportMessage()
	m.clearSelection()
}

func (m Model) exitSearchMode() (tea.Model, tea.Cmd) {
	m.searchMode = false
	m.searchEditing = false
	m.searchQuery = ""
	m.searchInput.Reset()
	return m, m.visibleMessagesCmd()
}

func (m Model) handleSearchCharacter(newValue string) (tea.Model, tea.Cmd) {
	m.searchInput.SetValue(newValue)
	m.searchQuery = strings.TrimSpace(newValue)
	m.messageCursor = 0
	m.listOffset = 0
	m.messages = nil
	m.filteredMessages = nil
	m.messageThreads = nil
	m.clearSelection()
	if m.searchQuery == "" {
		return m, nil
	}
	return m, m.searchAllMessagesCmd(m.searchQuery)
}

// effectiveManualCommand is the command shown in Settings (real install result, or suggested script when an update is available but the install path is not writable).

func (m Model) currentSidebarSelection() (sidebarRowKind, int64) {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return rowKindMailbox, 0
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind == rowKindUnified {
		return rowKindUnified, 0
	}
	if row.kind == rowKindAccount {
		return rowKindAccount, row.accountID
	}
	if row.kind == rowKindSysFolderHeader || row.kind == rowKindPersonalFolderHeader {
		return row.kind, row.accountID
	}
	return rowKindMailbox, row.mailboxID
}

func (m Model) selectedUnifiedInbox() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	return m.sidebarRows[m.sidebarCursor].kind == rowKindUnified
}

func (m Model) searchActive() bool {
	return m.searchMode
}

func (m Model) selectedMailbox() *db.Mailbox {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return nil
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindMailbox {
		return nil
	}
	return m.mailboxByID(row.mailboxID)
}

func (m Model) selectedAccountID() (int64, bool) {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return 0, false
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind != rowKindAccount {
		return 0, false
	}
	return row.accountID, true
}

func (m Model) mailboxByID(mailboxID int64) *db.Mailbox {
	for i := range m.mailboxes {
		if m.mailboxes[i].ID == mailboxID {
			return &m.mailboxes[i]
		}
	}
	return nil
}

func (m Model) accountByID(accountID int64) *db.Account {
	for i := range m.accounts {
		if m.accounts[i].ID == accountID {
			return &m.accounts[i]
		}
	}
	return nil
}

// toggleSelectedSection toggles a System or Labels section header.

// saveCollapseState persists sidebar collapse state to the database.

// loadCollapseState restores sidebar collapse state from the database.

func (m Model) indexOfFilteredMessage(messageID int64) int {
	if m.threadedMessagesEnabled() {
		for i, thread := range m.messageThreads {
			for _, msg := range thread.Messages {
				if msg.ID == messageID {
					return i
				}
			}
		}
		return -1
	}
	for i := range m.filteredMessages {
		if m.filteredMessages[i].ID == messageID {
			return i
		}
	}
	return -1
}

func (m *Model) setStatus(msg string, isErr bool) {
	m.statusMsg = msg
	m.statusErr = isErr
	// Push to log buffer (ring buffer, max 100 entries)
	const maxLogEntries = 100
	m.logBuffer = append(m.logBuffer, logEntry{Time: time.Now(), Message: msg, IsError: isErr})
	if len(m.logBuffer) > maxLogEntries {
		m.logBuffer = m.logBuffer[1:]
	}
}

// configSave is a seam over config.Save so tests can simulate a failed write.
var configSave = config.Save

// saveConfig persists the config and surfaces any failure on the status line, so a
// failed write (read-only dir, full disk) no longer silently drops account/OAuth/setting
// changes the way a fire-and-forget config.Save would.
func (m *Model) saveConfig() {
	if err := configSave(m.cfg); err != nil {
		m.setStatus(fmt.Sprintf("couldn't save settings: %v", err), true)
	}
}

// addToLog appends to the in-memory log buffer without updating the status bar.
func (m *Model) addToLog(msg string, isErr bool) {
	const maxLogEntries = 100
	m.logBuffer = append(m.logBuffer, logEntry{Time: time.Now(), Message: msg, IsError: isErr})
	if len(m.logBuffer) > maxLogEntries {
		m.logBuffer = m.logBuffer[1:]
	}
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

// renderSectionHeader renders a collapsible section header (System / Labels).

// ── Dimension helpers ─────────────────────────────────────────────────────────

const (
	defaultAccountsWidthPercent  = 28
	minAccountsWidthPercent      = 15
	maxAccountsWidthPercent      = 50
	accountsPaneStepPercent      = 2
	defaultMessagesHeightPercent = 40
	minMessagesHeightPercent     = 20
	maxMessagesHeightPercent     = 75
	messagesPaneStepPercent      = 5
)

func (m Model) accountsWidthPercent() int {
	pct := m.cfg.Display.AccountsWidthPercent
	if pct <= 0 {
		pct = defaultAccountsWidthPercent
	}
	return clamp(pct, minAccountsWidthPercent, maxAccountsWidthPercent)
}

func (m Model) messagesHeightPercent() int {
	pct := m.cfg.Display.MessagesHeightPercent
	if pct <= 0 {
		pct = defaultMessagesHeightPercent
	}
	return clamp(pct, minMessagesHeightPercent, maxMessagesHeightPercent)
}

func (m *Model) resizeAccountsPane(delta int) {
	m.cfg.Display.AccountsWidthPercent = clamp(m.accountsWidthPercent()+delta, minAccountsWidthPercent, maxAccountsWidthPercent)
	m.refreshContentAfterPaneResize()
	m.setStatus("pane layout saved", false)
	m.saveConfig()
}

func (m *Model) resizeMessagesPane(delta int) {
	m.cfg.Display.MessagesHeightPercent = clamp(m.messagesHeightPercent()+delta, minMessagesHeightPercent, maxMessagesHeightPercent)
	m.refreshContentAfterPaneResize()
	m.setStatus("pane layout saved", false)
	m.saveConfig()
}

func (m *Model) refreshContentAfterPaneResize() {
	m.viewport.Width = m.contentBodyWidth()
	m.viewport.Height = m.contentBodyHeight()
	if m.contentMessageID != 0 && m.activeMessageRowCount() > 0 {
		m.setViewportForCurrentRow()
	}
}

func (m Model) feedsPaneWidth() int {
	w := m.width * m.accountsWidthPercent() / 100
	return clamp(w, 1, max(1, m.width-1))
}

func (m Model) articlesPaneWidth() int { return m.width - m.feedsPaneWidth() }
func (m Model) mainHeight() int        { return m.height - 1 }
func (m Model) articlesPaneOuterHeight() int {
	h := m.mainHeight() * m.messagesHeightPercent() / 100
	if m.mainHeight() <= 6 {
		return max(1, h)
	}
	return clamp(h, 3, m.mainHeight()-3)
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
