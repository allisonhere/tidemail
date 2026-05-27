package ui

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"slices"
	"sort"
	"strings"
	"time"
	"unicode"

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
	rowKindMailbox
)

type sidebarRow struct {
	kind      sidebarRowKind
	accountID int64
	mailboxID int64
}

type overlayMode int

const (
	overlayNone overlayMode = iota
	overlayQuitConfirm
	overlaySearch
	overlayThemePicker
	overlayAccountManager
	overlayHelp
	overlaySettings
	overlayUpdateConfirm
	overlayContentSearch
	overlaySummary
	overlayCompose
	overlayCommandPalette
	overlaySaveAttach
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

	messages         []db.Message
	filteredMessages []db.Message
	messageCursor    int
	listOffset       int
	searchQuery      string
	showUnreadOnly   bool

	viewport             viewport.Model
	contentLinks         []string
	contentLinkIdx       int
	contentMessageID     int64
	contentFocusLine     int
	contentLineCount     int
	contentFocusable     []bool
	contentSearchInput   textinput.Model
	contentSearchQuery   string
	contentSearchMatches []int
	contentSearchIdx     int

	contentAttachments    []db.Attachment
	contentQuotesCollapsed bool

	saveAttachPicker filePicker

	helpVP        viewport.Model
	overlay       overlayMode
	searchInput   textinput.Model
	commandInput  textinput.Model
	commandCursor int

	confirmedTheme int
	activeTheme    int
	styles         Styles
	themeCursor    int

	accountManager AccountManager
	compose        ComposeModel

	statusMsg string
	statusErr bool

	syncing map[int64]bool
	spinner spinner.Model

	firstLoad              bool
	pendingSelectMailboxID int64
	keys                   KeyMap

	settings Settings

	updateState          updateState
	updateInfo           update.ReleaseInfo
	updateInfoFresh      bool
	downloadedUpdate     *update.DownloadedAsset
	updateInstall        update.InstallResult
	updateErr            string
	updateDismissed      bool
	pendingUpdateInstall bool

	previewManualUpdateUI bool

	summarizer        ai.Summarizer
	summaryMessage    db.Message
	summaryGenerating bool
	summaryErr        string
}

func NewModel(database *db.DB, cfg config.Config, currentVersion string, previewManualUpdate bool) Model {
	merged, themeIdx := MergedThemeFromConfig(cfg)

	si := textinput.New()
	si.Placeholder = "search messages..."
	si.CharLimit = 100

	ci := textinput.New()
	ci.Placeholder = "type a command..."
	ci.CharLimit = 100

	csi := textinput.New()
	csi.Placeholder = "find in message..."
	csi.CharLimit = 100

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
		commandInput:          ci,
		spinner:               sp,
		syncing:               make(map[int64]bool),
		collapsedAccounts:     map[int64]bool{},
		firstLoad:             true,
		keys:                  DefaultKeys,
		summarizer:            summarizer,
		showUnreadOnly:        cfg.Display.DefaultUnreadOnly,
		contentLinkIdx:        -1,
		contentSearchInput:    csi,
		contentSearchIdx:      -1,
	}
	m.restoreCachedUpdateState()
	if previewManualUpdate {
		m.applyManualUpdatePreview()
	}
	return m
}

func (m Model) Init() tea.Cmd {
	cmds := []tea.Cmd{m.loadAccountsCmd(), m.spinner.Tick}
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
		if len(m.filteredMessages) > 0 {
			m.setViewportMessage(m.filteredMessages[m.messageCursor])
			m.ensureContentFocusVisible()
		}
		if m.overlay == overlayHelp {
			m.resetHelpVP()
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
				}
			}
		}
		m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))
		m.clampSidebarOffset()
		if len(m.mailboxes) == 0 {
			m.clearMessages()
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
		} else {
			m.clearMessages()
		}
		if statusCmd != nil {
			cmds = append(cmds, statusCmd)
		}
		return m, tea.Batch(cmds...)

	case MessagesLoadedMsg:
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
			m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
			m.listOffset = 0
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
			}
		}
		return m, nil

	case MailboxSyncedMsg:
		delete(m.syncing, msg.MailboxID)
		if msg.Err != nil {
			if msg.Manual {
				m.setStatus(fmt.Sprintf("sync failed: %v", msg.Err), true)
				return m, m.clearStatusCmd()
			}
			return m, nil
		}
		cmds := []tea.Cmd{m.loadAccountsCmd()}
		if m.selectedUnifiedInbox() {
			cmds = append(cmds, m.loadUnifiedInboxCmd())
		} else if selected := m.selectedMailbox(); selected != nil && msg.MailboxID == selected.ID {
			cmds = append(cmds, m.loadMailboxMessagesCmd(msg.MailboxID))
		}
		if msg.Manual && msg.NewCount > 0 {
			m.setStatus(fmt.Sprintf("synced: %d new", msg.NewCount), false)
			cmds = append(cmds, m.clearStatusCmd())
		} else if msg.Manual {
			m.setStatus("up to date", false)
			cmds = append(cmds, m.clearStatusCmd())
		}
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
		config.Save(m.cfg) //nolint:errcheck
		m.accountManager = m.newAccountManager()
		m.accountManager.mode = amList
		m.accountManager.statusMsg = fmt.Sprintf("SAVED: %s", strings.ToUpper(msg.Account.Name))
		m.setStatus(fmt.Sprintf("saved: %s", msg.Account.Name), false)
		if len(msg.Mailboxes) > 0 {
			m.pendingSelectMailboxID = msg.Mailboxes[0].ID
		}
		return m, tea.Batch(m.loadAccountsCmd(), m.clearStatusCmd())

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
		if idx := m.indexOfFilteredMessage(msg.MessageID); msg.Advance && idx >= 0 && idx == m.messageCursor && idx < len(m.filteredMessages)-1 {
			m.messageCursor = idx + 1
			visible := max(1, m.articleRowsVisible())
			if m.messageCursor >= m.listOffset+visible {
				m.listOffset = m.messageCursor - visible + 1
			}
		} else {
			m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
			m.listOffset = clamp(m.listOffset, 0, max(0, len(m.filteredMessages)-1))
		}
		current := m.filteredMessages[m.messageCursor]
		m.setViewportMessage(current)
		return m, nil

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

	case MessageDeletedMsg:
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("delete failed: %v", msg.Err), true)
			return m, m.clearStatusCmd()
		}
		if m.removeMessageFromMemory(msg.MessageID) {
			m.adjustMailboxUnreadCount(msg.MailboxID, -1)
		}
		m.setStatus("deleted", false)
		return m, m.clearStatusCmd()

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
		m.compose = ComposeModel{}
		m.overlay = overlayNone
		if msg.Err != nil {
			m.setStatus(fmt.Sprintf("send failed: %v", msg.Err), true)
		} else {
			m.setStatus("message sent", false)
		}
		return m, m.clearStatusCmd()

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

	case keyMatches(msg, m.keys.ThemePicker):
		m.overlay = overlayThemePicker
		m.themeCursor = m.confirmedTheme
		return m, nil

	case keyMatches(msg, m.keys.Search):
		m.overlay = overlaySearch
		m.searchInput.Reset()
		m.searchInput.Focus()
		return m, nil

	case keyMatches(msg, m.keys.Command):
		m.overlay = overlayCommandPalette
		m.commandInput.Reset()
		m.commandInput.Focus()
		m.commandCursor = 0
		return m, nil

	case keyMatches(msg, m.keys.UnreadOnly):
		if m.focused == paneAccounts {
			return m, nil
		}
		m.showUnreadOnly = !m.showUnreadOnly
		var currentID int64
		if len(m.filteredMessages) > 0 {
			currentID = m.filteredMessages[m.messageCursor].ID
		}
		m.applyFilter()
		if idx := m.indexOfFilteredMessage(currentID); idx >= 0 {
			m.messageCursor = idx
		} else {
			m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
		}
		if m.showUnreadOnly {
			m.setStatus("showing unread only", false)
		} else {
			m.setStatus("showing all messages", false)
		}
		if len(m.filteredMessages) > 0 {
			m.setViewportMessage(m.filteredMessages[m.messageCursor])
		} else {
			m.clearViewportMessage()
		}
		return m, m.clearStatusCmd()

	case keyMatches(msg, m.keys.NextPane):
		return m.focusPane(pane((int(m.focused) + 1) % 3))

	case keyMatches(msg, m.keys.PrevPane):
		return m.focusPane(pane((int(m.focused) + 2) % 3))

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

	case keyMatches(msg, m.keys.Enter):
		if m.focused == paneAccounts && m.toggleSelectedAccount() {
			return m, nil
		}
		if m.focused == paneMessages && len(m.filteredMessages) > 0 {
			m.focused = paneContent
			current := m.filteredMessages[m.messageCursor]
			if m.cfg.Display.MarkReadOnOpen && !current.Read {
				return m, m.setMessageReadCmd(current, true, false)
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.Back):
		if m.focused == paneContent {
			m.focused = paneMessages
		}
		return m, nil

	case keyMatches(msg, m.keys.Sync):
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
		if m.focused == paneMessages && len(m.filteredMessages) > 0 {
			msg2 := m.filteredMessages[m.messageCursor]
			read := !msg2.Read
			advance := !msg2.Read
			return m, m.setMessageReadCmd(msg2, read, advance)
		}
		return m, nil

	case keyMatches(msg, m.keys.Archive):
		if m.focused != paneAccounts && len(m.filteredMessages) > 0 {
			msg2 := m.filteredMessages[m.messageCursor]
			return m, m.archiveMessageCmd(msg2)
		}
		return m, nil

	case keyMatches(msg, m.keys.Delete):
		if m.focused != paneAccounts && len(m.filteredMessages) > 0 {
			msg2 := m.filteredMessages[m.messageCursor]
			return m, m.deleteMessageCmd(msg2)
		}
		return m, nil

	case keyMatches(msg, m.keys.Reply):
		if m.focused == paneContent && m.contentMessageID != 0 {
			if cur := m.currentContentMessage(); cur != nil {
				acfg := m.accountCfgForMailbox(cur.MailboxID)
				m.compose = NewReply(*cur, acfg)
				m.overlay = overlayCompose
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.Compose):
		var acfg config.AccountConfig
		if len(m.cfg.Accounts) > 0 {
			acfg = m.cfg.Accounts[0]
		}
		m.compose = NewCompose(acfg)
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
		if len(m.filteredMessages) > 0 {
			if m.focused == paneContent {
				if link, ok := m.currentContentLink(); ok {
					return m, m.openBrowserCmd(link)
				}
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.NextLink):
		if m.focused == paneContent && m.actionableLinksEnabled() {
			m.stepContentLink(1)
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
				m.viewport.GotoBottom()
			}
		}
		return m, nil

	case keyMatches(msg, m.keys.PrevLink):
		if m.focused == paneContent && m.actionableLinksEnabled() {
			m.stepContentLink(-1)
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
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
		if m.focused != paneAccounts && len(m.filteredMessages) > 0 {
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
		if m.focused == paneAccounts && m.toggleSelectedAccount() {
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
	m.focused = next
	if !wasMessages && next == paneMessages && len(m.filteredMessages) > 0 {
		return m, m.focusedMessageChangedCmd(m.filteredMessages[m.messageCursor])
	}
	return m, nil
}

func (m Model) handleUp() (tea.Model, tea.Cmd) {
	switch m.focused {
	case paneAccounts:
		if m.sidebarCursor > 0 {
			m.sidebarCursor--
			m.clampSidebarOffset()
			if m.selectedUnifiedInbox() {
				return m, m.loadUnifiedInboxCmd()
			}
			if selected := m.selectedMailbox(); selected != nil {
				return m, m.loadMailboxMessagesCmd(selected.ID)
			}
			m.clearMessages()
		}
	case paneMessages:
		if m.messageCursor > 0 {
			m.messageCursor--
			if m.messageCursor < m.listOffset {
				m.listOffset = m.messageCursor
			}
			if len(m.filteredMessages) > 0 {
				msg2 := m.filteredMessages[m.messageCursor]
				m.setViewportMessage(msg2)
				return m, m.focusedMessageChangedCmd(msg2)
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
			if m.selectedUnifiedInbox() {
				return m, m.loadUnifiedInboxCmd()
			}
			if selected := m.selectedMailbox(); selected != nil {
				return m, m.loadMailboxMessagesCmd(selected.ID)
			}
			m.clearMessages()
		}
	case paneMessages:
		if m.messageCursor < len(m.filteredMessages)-1 {
			m.messageCursor++
			visible := m.articleRowsVisible()
			if m.messageCursor >= m.listOffset+visible {
				m.listOffset = m.messageCursor - visible + 1
			}
			if len(m.filteredMessages) > 0 {
				msg2 := m.filteredMessages[m.messageCursor]
				m.setViewportMessage(msg2)
				return m, m.focusedMessageChangedCmd(msg2)
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
			m.messageCursor = 0
			m.listOffset = 0
		case keyMatches(msg, m.keys.Confirm):
			m.overlay = overlayNone
		default:
			var cmd tea.Cmd
			m.searchInput, cmd = m.searchInput.Update(msg)
			m.searchQuery = m.searchInput.Value()
			m.applyFilter()
			m.messageCursor = 0
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
				if len(m.filteredMessages) > 0 {
					m.setViewportMessage(m.filteredMessages[m.messageCursor])
				}
			}
		case keyMatches(msg, m.keys.Down):
			if m.themeCursor < len(BuiltinThemes)-1 {
				m.themeCursor++
				m.activeTheme = m.themeCursor
				m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
				if len(m.filteredMessages) > 0 {
					m.setViewportMessage(m.filteredMessages[m.messageCursor])
				}
			}
		case keyMatches(msg, m.keys.Confirm):
			m.confirmedTheme = m.themeCursor
			m.overlay = overlayNone
			m.cfg.Theme = BuiltinThemes[m.confirmedTheme].Name
			config.Save(m.cfg)
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
			}
		case keyMatches(msg, m.keys.Cancel):
			m.activeTheme = m.confirmedTheme
			m.styles = BuildStyles(MergedBuiltinThemeAtIndex(m.cfg, m.activeTheme), m.cfg.Display.Density)
			m.overlay = overlayNone
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
			}
		}
		if m.activeTheme != prevTheme {
			return m, setTermBgCmd(m.styles.Theme.Bg)
		}
		return m, nil

	case overlayAccountManager:
		return m.handleAccountManager(msg)

	case overlayCompose:
		return m.handleCompose(msg)

	case overlaySaveAttach:
		return m.handleSaveAttachPicker(msg)

	case overlayHelp:
		if keyMatches(msg, m.keys.Back, m.keys.Help, m.keys.Quit) {
			m.overlay = overlayNone
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
		if len(m.filteredMessages) > 0 {
			m.setViewportMessage(m.filteredMessages[m.messageCursor])
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
			config.Save(m.cfg)
			summarizer, _ := ai.New(m.cfg.AI)
			m.summarizer = summarizer
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
			} else {
				m.clearViewportMessage()
			}
			m.overlay = overlayNone
			m.sidebarCursor = 0
			m.sidebarOffset = 0
			m.messageCursor = 0
			m.clearMessages()
			return m, m.loadAccountsCmd()
		}
		m.overlay = overlayNone
		if previewingTheme {
			merged, _ := MergedThemeFromConfig(m.cfg)
			m.styles = BuildStyles(merged, m.cfg.Display.Density)
			if len(m.filteredMessages) > 0 {
				m.setViewportMessage(m.filteredMessages[m.messageCursor])
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
		if !m.summaryGenerating && m.summaryErr == "" && m.summaryMessage.Summary != "" {
			return m, copyToClipboardCmd(m.summaryMessage.Summary)
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

func (m Model) handleCompose(msg tea.Msg) (tea.Model, tea.Cmd) {
	newC, cmd, exit := m.compose.Update(msg, m.keys)
	m.compose = newC
	if exit {
		m.overlay = overlayNone
		m.compose = ComposeModel{}
	}
	return m, cmd
}

// ── Save-attachment folder picker ────────────────────────────────────────

func (m *Model) openSaveAttachPicker(dir string) {
	entries, err := os.ReadDir(dir)
	if err != nil {
		// Fall back to immediate save
		m.overlay = overlayNone
		return
	}

	var fe []fileEntry
	// Add "select this folder" entry at the top
	fe = append(fe, fileEntry{name: "✓ select this folder", isDir: false, size: 0})
	// Add parent-dir entry unless we're at filesystem root
	if dir != "/" {
		fe = append(fe, fileEntry{name: "..", isDir: true})
	}
	for _, e := range entries {
		info, err := e.Info()
		if err != nil {
			continue
		}
		// Skip hidden files
		if strings.HasPrefix(e.Name(), ".") {
			continue
		}
		fe = append(fe, fileEntry{
			name:  e.Name(),
			isDir: e.IsDir(),
			size:  info.Size(),
		})
	}

	// Sort: dirs first, then files; alphabetically within each group
	sort.Slice(fe, func(i, j int) bool {
		if fe[i].isDir != fe[j].isDir {
			return fe[i].isDir
		}
		return strings.ToLower(fe[i].name) < strings.ToLower(fe[j].name)
	})

	m.saveAttachPicker.currentDir = dir
	m.saveAttachPicker.entries = fe
	m.saveAttachPicker.cursor = 0
	m.saveAttachPicker.active = true
}

func (m *Model) saveAttachPickerUp() {
	if m.saveAttachPicker.cursor > 0 {
		m.saveAttachPicker.cursor--
	}
}

func (m *Model) saveAttachPickerDown() {
	if m.saveAttachPicker.cursor < len(m.saveAttachPicker.entries)-1 {
		m.saveAttachPicker.cursor++
	}
}

func (m *Model) saveAttachPickerEnterDir() {
	entry := m.saveAttachPicker.entries[m.saveAttachPicker.cursor]
	if entry.name == ".." {
		parent := filepath.Dir(m.saveAttachPicker.currentDir)
		m.openSaveAttachPicker(parent)
		return
	}
	if entry.isDir {
		sub := filepath.Join(m.saveAttachPicker.currentDir, entry.name)
		m.openSaveAttachPicker(sub)
		return
	}
}

func (m *Model) saveAttachPickerUpDir() {
	parent := filepath.Dir(m.saveAttachPicker.currentDir)
	if parent == m.saveAttachPicker.currentDir {
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return
	}
	m.openSaveAttachPicker(parent)
}

func (m Model) handleSaveAttachPicker(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return m, nil

	case keyMatches(msg, m.keys.Up):
		m.saveAttachPickerUp()
		return m, nil

	case keyMatches(msg, m.keys.Down):
		m.saveAttachPickerDown()
		return m, nil

	case keyMatches(msg, m.keys.Confirm):
		entry := m.saveAttachPicker.entries[m.saveAttachPicker.cursor]
		if entry.isDir {
			m.saveAttachPickerEnterDir()
			return m, nil
		}
		// "select this folder" entry or a file — choose current directory
		m.saveAttachPicker.active = false
		m.overlay = overlayNone
		return m, saveAttachmentsCmdTo(m.contentAttachments, m.saveAttachPicker.currentDir)

	case keyMatches(msg, m.keys.Left), keyMatches(msg, m.keys.Back):
		m.saveAttachPickerUpDir()
		return m, nil

	default:
		// Single-key quick-jump: press a letter to jump to first entry starting with it
		if len(msg.String()) == 1 && msg.String() >= "a" && msg.String() <= "z" || msg.String() >= "A" && msg.String() <= "Z" {
			lower := strings.ToLower(msg.String())
			for i, e := range m.saveAttachPicker.entries {
				if strings.HasPrefix(strings.ToLower(e.name), lower) {
					m.saveAttachPicker.cursor = i
					return m, nil
				}
			}
		}
		return m, nil
	}
}

func saveAttachmentsCmdTo(atts []db.Attachment, dir string) tea.Cmd {
	return func() tea.Msg {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return AttachmentsSavedMsg{Err: fmt.Errorf("create dir: %w", err)}
		}
		saved := 0
		for _, a := range atts {
			path := filepath.Join(dir, safeFilename(a.Filename))
			if err := os.WriteFile(path, a.Data, 0o644); err != nil {
				return AttachmentsSavedMsg{Err: fmt.Errorf("write %s: %w", a.Filename, err)}
			}
			saved++
		}
		return AttachmentsSavedMsg{Path: dir, Count: saved}
	}
}

type commandItem struct {
	id      string
	label   string
	enabled bool
}

func (m Model) handleCommandPaletteKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	items := m.filteredCommandItems()
	switch {
	case keyMatches(msg, m.keys.Cancel):
		m.overlay = overlayNone
		m.commandInput.Blur()
		return m, nil
	case keyMatches(msg, m.keys.Up):
		if len(items) > 0 {
			m.commandCursor = (m.commandCursor - 1 + len(items)) % len(items)
		}
		return m, nil
	case keyMatches(msg, m.keys.Down), keyMatches(msg, m.keys.Tab):
		if len(items) > 0 {
			m.commandCursor = (m.commandCursor + 1) % len(items)
		}
		return m, nil
	case keyMatches(msg, m.keys.Confirm):
		if len(items) == 0 {
			return m, nil
		}
		item := items[clamp(m.commandCursor, 0, len(items)-1)]
		if !item.enabled {
			return m, nil
		}
		m.overlay = overlayNone
		m.commandInput.Blur()
		return m.executeCommand(item.id)
	default:
		var cmd tea.Cmd
		m.commandInput, cmd = m.commandInput.Update(msg)
		items = m.filteredCommandItems()
		m.commandCursor = clamp(m.commandCursor, 0, max(0, len(items)-1))
		return m, cmd
	}
}

func (m Model) commandItems() []commandItem {
	hasMessage := len(m.filteredMessages) > 0 && m.focused != paneAccounts
	hasMailbox := m.selectedMailbox() != nil
	return []commandItem{
		{id: "compose", label: "Compose new message", enabled: len(m.cfg.Accounts) > 0},
		{id: "reply", label: "Reply to current message", enabled: m.contentMessageID != 0 || hasMessage},
		{id: "archive", label: "Archive current message", enabled: hasMessage},
		{id: "delete", label: "Delete current message", enabled: hasMessage},
		{id: "toggle-read", label: "Toggle read/unread", enabled: hasMessage},
		{id: "sync", label: "Sync current mailbox", enabled: hasMailbox},
		{id: "sync-all", label: "Sync all mailboxes", enabled: len(m.mailboxes) > 0},
		{id: "accounts", label: "Manage accounts", enabled: true},
		{id: "settings", label: "Open settings", enabled: true},
	}
}

func (m Model) filteredCommandItems() []commandItem {
	q := strings.ToLower(strings.TrimSpace(m.commandInput.Value()))
	items := m.commandItems()
	if q == "" {
		return items
	}
	filtered := make([]commandItem, 0, len(items))
	for _, item := range items {
		if strings.Contains(strings.ToLower(item.label), q) || strings.Contains(strings.ToLower(item.id), q) {
			filtered = append(filtered, item)
		}
	}
	return filtered
}

func (m Model) executeCommand(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "compose":
		var acfg config.AccountConfig
		if len(m.cfg.Accounts) > 0 {
			acfg = m.cfg.Accounts[0]
		}
		m.compose = NewCompose(acfg)
		m.overlay = overlayCompose
		return m, nil
	case "reply":
		msg := m.commandMessage()
		if msg == nil {
			return m, nil
		}
		acfg := m.accountCfgForMailbox(msg.MailboxID)
		m.compose = NewReply(*msg, acfg)
		m.overlay = overlayCompose
		return m, nil
	case "archive":
		if msg := m.commandMessage(); msg != nil {
			return m, m.archiveMessageCmd(*msg)
		}
	case "delete":
		if msg := m.commandMessage(); msg != nil {
			return m, m.deleteMessageCmd(*msg)
		}
	case "toggle-read":
		if msg := m.commandMessage(); msg != nil {
			return m, m.setMessageReadCmd(*msg, !msg.Read, !msg.Read)
		}
	case "sync":
		if selected := m.selectedMailbox(); selected != nil {
			return m, m.syncMailboxCmd(selected.ID, true)
		}
	case "sync-all":
		var cmds []tea.Cmd
		for _, mb := range m.mailboxes {
			cmds = append(cmds, m.syncMailboxCmd(mb.ID, false))
		}
		return m, tea.Batch(cmds...)
	case "accounts":
		m.overlay = overlayAccountManager
		m.accountManager = m.newAccountManager()
		return m, nil
	case "settings":
		m.settings = newSettings(m.cfg, m.settingsUpdateState())
		m.overlay = overlaySettings
		return m, nil
	}
	return m, nil
}

func (m Model) commandMessage() *db.Message {
	if msg := m.currentContentMessage(); msg != nil {
		return msg
	}
	if len(m.filteredMessages) == 0 {
		return nil
	}
	idx := clamp(m.messageCursor, 0, len(m.filteredMessages)-1)
	return &m.filteredMessages[idx]
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

func (m Model) renderAccountsPane() string {
	w := m.feedsPaneWidth()
	innerW := w - 1
	focused := m.focused == paneAccounts
	title := m.renderPaneHeader(paneAccounts, "Accounts", focused, innerW)
	rows := []string{title}

	end := min(m.sidebarOffset+m.sidebarVisibleRows(), len(m.sidebarRows))
	for i := m.sidebarOffset; i < end; i++ {
		row := m.sidebarRows[i]
		selected := i == m.sidebarCursor
		switch row.kind {
		case rowKindAccount:
			rows = append(rows, m.renderAccountHeader(row.accountID, selected, innerW))
		case rowKindUnified:
			rows = append(rows, m.renderUnifiedInboxRow(selected, innerW))
		case rowKindMailbox:
			if mb := m.mailboxByID(row.mailboxID); mb != nil {
				rows = append(rows, m.renderSidebarMailboxRow(*mb, selected, innerW))
			}
		}
	}

	if len(m.sidebarRows) == 0 {
		rows = append(rows, m.styles.FeedItem.Foreground(
			lipgloss.Color(m.styles.Theme.Dimmed),
		).Render(m.emptyAccountsHint()))
	}
	footer := fmt.Sprintf("  %d accounts", len(m.accounts))
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

func (m Model) renderMessagesPane() string {
	w := m.articlesPaneWidth()
	h := m.articlesPaneContentHeight()
	msgUnread, msgRead, msgSelected, headerActive, borderColor, borderFocus := m.messageRowStyles()

	rows := []string{}
	visible := m.filteredMessages
	end := min(m.listOffset+m.articleRowsVisible(), len(visible))
	for i := m.listOffset; i < end; i++ {
		msg2 := visible[i]
		age := m.formatTime(msg2.Date)
		dot := m.messageRowPrefix(msg2.Read)
		style := msgRead
		if !msg2.Read {
			style = msgUnread
		}
		if i == m.messageCursor {
			style = msgSelected
		}
		rows = append(rows, style.Width(w-2).Render(renderArticleRow(dot, unescapeDisplayText(msg2.Subject), age, w-2)))
	}

	if len(m.filteredMessages) == 0 {
		if m.searchQuery != "" {
			rows = append(rows, msgRead.Render("  no results"))
		} else {
			rows = append(rows, msgRead.Render("  no messages"))
		}
	}

	focused := m.focused == paneMessages
	border := m.styles.ArticlesPane
	if focused {
		border = border.BorderForeground(borderFocus)
	}
	title := "Messages"
	if m.selectedUnifiedInbox() {
		title = "Unified Inbox"
	}
	if m.searchQuery != "" {
		title = fmt.Sprintf("%s [/%s]", title, m.searchQuery)
	}
	if m.showUnreadOnly {
		title += " (unread)"
	}

	contentRows := append([]string{m.renderPaneHeaderWithAccent(paneMessages, title, focused, w, headerActive)}, rows...)
	for viewLineCount(contentRows) < h {
		contentRows = append(contentRows, msgRead.Width(w-2).Render(""))
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
	case paneAccounts:
		hint = m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " move  " +
			m.keyHint(m.keys.Enter) + " toggle  " + m.keyHint(m.keys.Sync) + " sync"
	case paneMessages:
		hint = m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " move  " +
			m.keyHint(m.keys.Archive) + " archive  " + m.keyHint(m.keys.Delete) + " delete  " +
			m.keyHint(m.keys.Command) + " command"
	case paneContent:
		progress := ""
		if m.contentLineCount > 0 {
			pct := min(100, (m.viewport.YOffset+m.viewport.Height)*100/m.contentLineCount)
			progress = fmt.Sprintf("%d%%  ", pct)
		}
		hint = progress + m.keyHint(m.keys.Up) + "/" + m.keyHint(m.keys.Down) + " line  " +
			m.keyHint(m.keys.Reply) + " reply  " + m.keyHint(m.keys.ContentSearch) + " find  " +
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

func (m Model) renderMessageContent(msg db.Message) string {
	contentWidth := m.contentBodyWidth()
	bodyWidth := m.contentBodyWidth()
	titleWidth := max(1, contentWidth-m.styles.ContentTitle.GetHorizontalFrameSize())
	metaWidth := max(1, contentWidth-m.styles.ContentMeta.GetHorizontalFrameSize())
	title := m.styles.ContentTitle.Width(contentWidth + 2).Render(truncate(unescapeDisplayText(msg.Subject), titleWidth+2))
	metaStr := msg.Date.Format("Mon, 02 Jan 2006 15:04")
	if msg.From != "" {
		metaStr += "  From: " + msg.From
	}
	meta := " " + m.styles.ContentMeta.Width(contentWidth).Render(truncate(metaStr, metaWidth))

	var body string
	if msg.BodyHTML != "" {
		body = renderHTMLBody(msg.BodyHTML, bodyWidth, m.styles.Theme, m.styles.PlainUI)
	}
	if body == "" {
		content := msg.BodyText
		if content == "" {
			content = "No message body."
		}
		if m.cfg.Display.FilterLinks {
			content = filterLinksFromContent(content)
		}
		body = indentBlock(m.styles.ContentBody.Width(bodyWidth).Render(formatArticleBody(content, bodyWidth, m.styles.Theme, m.styles.PlainUI)), 1)
	}

	body = collapseQuoteBlocks(body, m.contentQuotesCollapsed)

	if m.actionableLinksEnabled() && len(m.contentLinks) > 0 {
		body += "\n\n" + m.renderContentLinks(bodyWidth)
	}

	if len(m.contentAttachments) > 0 {
		body += "\n\n" + m.renderAttachmentList(bodyWidth)
	}

	return fillViewWidth(title+"\n"+meta+"\n\n"+body, m.articlesPaneWidth(), m.styles.Theme.Bg)
}

func (m Model) renderAttachmentList(width int) string {
	if len(m.contentAttachments) == 0 {
		return ""
	}
	th := m.styles.Theme
	accent := lipgloss.NewStyle().Foreground(th.BorderFocus)
	dimmed := lipgloss.NewStyle().Foreground(th.Dimmed)
	body := m.styles.ContentBody.Width(width)

	lines := []string{
		accent.Render("── " + strings.ToUpper("Attachments") + " ──") + dimmed.Render(strings.Repeat("─", width-ansi.StringWidth(accent.Render("── " + strings.ToUpper("Attachments") + " ──")))),
	}
	maxSizeLen := 0
	for _, a := range m.contentAttachments {
		if l := len(formatFileSize(a.Size)); l > maxSizeLen {
			maxSizeLen = l
		}
	}
	for _, a := range m.contentAttachments {
		icon := fileTypeIcon(a.Filename, a.ContentType)
		sizeStr := formatFileSize(a.Size)
		iconStyled := accent.Render(" " + icon + " ")
		line := iconStyled + a.Filename
		paddedSize := fmt.Sprintf("%*s", maxSizeLen, sizeStr)
		// Right-align size by padding to column end
		used := ansi.StringWidth(line)
		pad := width - used - maxSizeLen - 2
		if pad < 1 {
			pad = 1
		}
		line += strings.Repeat(" ", pad) + dimmed.Render(paddedSize)
		lines = append(lines, line)
	}
	lines = append(lines, "")
	lines = append(lines, dimmed.Render("  ctrl+d  save all to folder"))
	return indentBlock(body.Render(strings.Join(lines, "\n")), 1)
}

func formatFileSize(size int64) string {
	switch {
	case size < 1024:
		return fmt.Sprintf("%d B", size)
	case size < 1024*1024:
		return fmt.Sprintf("%.1f KB", float64(size)/1024)
	default:
		return fmt.Sprintf("%.1f MB", float64(size)/(1024*1024))
	}
}

func fileTypeIcon(filename, contentType string) string {
	ext := strings.ToLower(filepath.Ext(filename))
	switch ext {
	case ".pdf":
		return "▤"
	case ".jpg", ".jpeg", ".png", ".gif", ".webp", ".bmp", ".svg":
		return "▣"
	case ".zip", ".tar", ".gz", ".bz2", ".xz", ".7z", ".rar", ".tgz":
		return "⊞"
	case ".doc", ".docx", ".xls", ".xlsx", ".ppt", ".pptx", ".odt", ".ods", ".odp":
		return "◇"
	case ".go", ".py", ".js", ".ts", ".rs", ".c", ".cpp", ".h", ".hpp",
		".java", ".rb", ".sh", ".bash", ".zsh", ".fish", ".swift", ".kt",
		".css", ".scss", ".less", ".html", ".htm", ".xml", ".yaml", ".yml",
		".json", ".toml", ".ini", ".cfg", ".conf", ".sql", ".r", ".m",
		".ex", ".exs", ".php", ".pl", ".lua", ".vue", ".svelte", ".tsx", ".jsx":
		return "⌘"
	case ".txt", ".md", ".rst", ".adoc", ".org", ".log":
		return "≡"
	case ".mp3", ".wav", ".flac", ".ogg", ".aac", ".m4a", ".opus", ".wma":
		return "♫"
	case ".mp4", ".avi", ".mkv", ".mov", ".webm", ".m4v", ".flv", ".wmv":
		return "▶"
	case ".csv", ".tsv":
		return "≡"
	default:
		if strings.HasPrefix(contentType, "image/") {
			return "▣"
		}
		if strings.HasPrefix(contentType, "text/") {
			return "≡"
		}
		if strings.HasPrefix(contentType, "audio/") {
			return "♫"
		}
		if strings.HasPrefix(contentType, "video/") {
			return "▶"
		}
		if strings.Contains(contentType, "pdf") ||
			strings.Contains(contentType, "postscript") {
			return "▤"
		}
		if strings.Contains(contentType, "zip") ||
			strings.Contains(contentType, "compress") ||
			strings.Contains(contentType, "tar") ||
			strings.Contains(contentType, "gzip") {
			return "⊞"
		}
		return "○"
	}
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

func (m *Model) setViewportMessage(msg db.Message) {
	sameMsg := m.contentMessageID == msg.ID && m.contentLineCount > 0
	m.syncContentLinks(msg)
	m.contentAttachments = nil
	m.contentQuotesCollapsed = false
	if msg.HasAttachment {
		if atts, err := m.db.GetAttachments(msg.ID); err == nil {
			m.contentAttachments = atts
		}
	}
	content := m.renderMessageContent(msg)
	m.contentSearchMatches = collectSearchMatches(content, m.contentSearchQuery)
	m.viewport.SetContent(content)
	m.contentMessageID = msg.ID
	m.contentLineCount = strings.Count(content, "\n") + 1
	m.contentFocusable = messageFocusableLines(content)
	m.contentFocusLine = clamp(m.contentFocusLine, 0, max(0, m.contentLineCount-1))
	if !sameMsg {
		m.contentFocusLine = firstFocusableLine(m.contentFocusable)
		m.viewport.GotoTop()
	}
	m.ensureContentFocusVisible()
}

func (m *Model) clearViewportMessage() {
	m.viewport.SetContent("")
	m.contentLinks = nil
	m.contentLinkIdx = -1
	m.contentMessageID = 0
	m.contentFocusLine = 0
	m.contentLineCount = 0
	m.contentFocusable = nil
	m.contentAttachments = nil
	m.clearContentSearch()
	m.viewport.GotoTop()
}

func (m *Model) clearContentSearch() {
	m.overlay = overlayNone
	m.contentSearchQuery = ""
	m.contentSearchMatches = nil
	m.contentSearchIdx = -1
	m.contentSearchInput.Blur()
	if msg := m.currentContentMessage(); msg != nil {
		m.setViewportMessage(*msg)
	}
}

func (m *Model) currentContentMessage() *db.Message {
	for i := range m.filteredMessages {
		if m.filteredMessages[i].ID == m.contentMessageID {
			return &m.filteredMessages[i]
		}
	}
	return nil
}

func (m *Model) applyContentSearch() {
	q := strings.ToLower(strings.TrimSpace(m.contentSearchInput.Value()))
	m.contentSearchQuery = q
	if msg := m.currentContentMessage(); msg != nil {
		m.setViewportMessage(*msg)
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

func messageFocusableLines(content string) []bool {
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

func (m *Model) syncContentLinks(msg db.Message) {
	if !m.actionableLinksEnabled() {
		m.contentLinks = nil
		m.contentLinkIdx = -1
		return
	}

	links := extractActionableLinks(msg.BodyText, "")
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

	case overlayAccountManager:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 40)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.accountManager.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlayCompose:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.compose.View(winW, winH, m.styles)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)

	case overlaySaveAttach:
		winW := min(m.width-4, 74)
		winH := min(m.height-4, 36)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderSaveAttachPicker(winW, winH, chrome)
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

	case overlayCommandPalette:
		winW := min(m.width-6, 72)
		chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
		inner := m.renderCommandPalette(winW, chrome)
		inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
		box = renderChromeOverlayBox(inner, winW, chrome, chrome.accent)
	}

	return overlayOnBase(base, box, m.width, m.height, m.styles.Theme.Bg)
}

// ── Save-attachment folder picker rendering ──────────────────────────────

func (m Model) renderSaveAttachPicker(width, height int, chrome managerChrome) string {
	header := renderManagerHeader("SAVE ATTACHMENTS", width, chrome)

	// Current directory display
	dirStr := m.saveAttachPicker.currentDir
	if dirStr == "" {
		dirStr = "~"
	}
	dirLine := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.muted).
		Width(width).
		Padding(0, 2).
		Render(clampView(dirStr, width-2, 1, chrome.baseBg))

	// Entry list — scroll within available height
	listH := max(1, height-7) // header(1) + dir(1) + actions(1) + padding
	entries := m.saveAttachPicker.entries

	// Calculate visible range
	start := 0
	if m.saveAttachPicker.cursor >= listH {
		start = m.saveAttachPicker.cursor - listH + 1
	}
	end := min(start+listH, len(entries))
	visible := entries[start:end]

	var rows []string
	for i, e := range visible {
		idx := start + i
		cursor := idx == m.saveAttachPicker.cursor

		entryStyle := lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Padding(0, 2)

		if cursor {
			entryStyle = entryStyle.
				Background(chrome.accent).
				Foreground(contrastFg(chrome.accent))
		} else {
			entryStyle = entryStyle.Foreground(chrome.text)
		}

		var label string
		switch {
		case e.name == "✓ select this folder":
			label = lipgloss.NewStyle().
				Foreground(lipgloss.Color("2")).
				Render(e.name)
			if cursor {
				label = lipgloss.NewStyle().
					Background(chrome.accent).
					Foreground(contrastFg(chrome.accent)).
					Render(e.name)
			} else {
				label = lipgloss.NewStyle().
					Background(chrome.baseBg).
					Foreground(lipgloss.Color("2")).
					Render(e.name)
			}
		case e.isDir && e.name == "..":
			label = lipgloss.NewStyle().
				Foreground(chrome.accent).
				Render("📁 " + e.name)
			if cursor {
				label = lipgloss.NewStyle().
					Background(chrome.accent).
					Foreground(contrastFg(chrome.accent)).
					Render("📁 " + e.name)
			} else {
				label = lipgloss.NewStyle().
					Background(chrome.baseBg).
					Foreground(chrome.accent).
					Render("📁 " + e.name)
			}
		case e.isDir:
			label = "📁 " + e.name
			if !cursor {
				label = lipgloss.NewStyle().
					Foreground(chrome.accent).
					Render(label)
			}
		default:
			label = "📄 " + e.name
		}

		// Truncate to fit width
		entryWidth := max(1, width-4)
		label = clampView(label, entryWidth, 1, chrome.baseBg)

		row := entryStyle.Render(label)
		rows = append(rows, row)
	}

	// Fill remaining rows to maintain consistent height
	for len(rows) < listH {
		rows = append(rows, lipgloss.NewStyle().
			Background(chrome.baseBg).
			Width(width).
			Padding(0, 2).
			Render(""))
	}

	body := lipgloss.JoinVertical(lipgloss.Left, rows...)

	// Actions
	actions := renderManagerActions(width, chrome,
		"↵", "open/confirm",
		"←", "parent",
		"esc", "cancel",
	)

	return lipgloss.JoinVertical(lipgloss.Left, header, dirLine, body, actions)
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
	header := renderManagerHeader("SEARCH MESSAGES", width, chrome)
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

func (m Model) renderCommandPalette(width int, chrome managerChrome) string {
	header := renderManagerHeader("COMMAND", width, chrome)
	input := m.commandInput
	inputW := max(1, width-4)
	input.Width = inputW
	input.PromptStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.accent).Bold(true)
	input.TextStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
	input.PlaceholderStyle = lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	input.Cursor.Style = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))
	input.Cursor.TextStyle = lipgloss.NewStyle().Background(chrome.accent).Foreground(contrastFg(chrome.accent))

	items := m.filteredCommandItems()
	rows := []string{input.View(), ""}
	if len(items) == 0 {
		rows = append(rows, lipgloss.NewStyle().Foreground(chrome.muted).Render("No commands"))
	} else {
		limit := min(8, len(items))
		start := 0
		if m.commandCursor >= limit {
			start = m.commandCursor - limit + 1
		}
		for i := start; i < min(start+limit, len(items)); i++ {
			item := items[i]
			prefix := "  "
			style := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text)
			if !item.enabled {
				style = style.Foreground(chrome.muted)
			}
			if i == m.commandCursor {
				prefix = "> "
				style = style.Background(chrome.accent).Foreground(contrastFg(chrome.accent)).Bold(true)
			}
			rows = append(rows, style.Width(max(1, width-4)).Render(truncate(prefix+item.label, max(1, width-4))))
		}
	}
	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(strings.Join(rows, "\n"))
	actions := renderManagerActions(width, chrome, "enter", "run", "esc", "close")
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
		bodyText = formatSummaryBody(m.summaryMessage.Summary, width-4, m.styles.PlainUI)
	}

	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(width).
		Padding(1, 2).
		Render(bodyText)

	var hints string
	if !m.summaryGenerating && m.summaryErr == "" && m.summaryMessage.Summary != "" {
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

func (m *Model) loadAccountsCmd() tea.Cmd {
	database := m.db
	configuredAccounts := append([]config.AccountConfig(nil), m.cfg.Accounts...)
	return func() tea.Msg {
		accounts, err := database.ListAccounts()
		if err != nil {
			return AccountsLoadedMsg{Err: err}
		}
		if len(configuredAccounts) > 0 {
			accounts, err = ensureConfiguredAccounts(database, accounts, configuredAccounts)
			if err != nil {
				return AccountsLoadedMsg{Err: err}
			}
		}
		var mailboxes []db.Mailbox
		for _, a := range accounts {
			mbs, err2 := database.ListMailboxes(a.ID)
			if err2 != nil {
				return AccountsLoadedMsg{Err: err2}
			}
			mailboxes = append(mailboxes, mbs...)
		}
		return AccountsLoadedMsg{Accounts: accounts, Mailboxes: mailboxes}
	}
}

func ensureConfiguredAccounts(database *db.DB, accounts []db.Account, configs []config.AccountConfig) ([]db.Account, error) {
	existing := make(map[string]db.Account, len(accounts))
	for _, account := range accounts {
		existing[strings.TrimSpace(account.Name)] = account
	}
	changed := false
	for _, accountCfg := range configs {
		name := strings.TrimSpace(accountCfg.Name)
		if name == "" {
			continue
		}
		account, ok := existing[name]
		if !ok {
			accountID, err := database.AddAccount(name, "")
			if err != nil {
				return accounts, fmt.Errorf("import configured account %s: %w", name, err)
			}
			account = db.Account{ID: accountID, Name: name}
			existing[name] = account
			changed = true
		}
		mailboxes, err := database.ListMailboxes(account.ID)
		if err != nil {
			return accounts, fmt.Errorf("list imported account mailboxes %s: %w", name, err)
		}
		if len(mailboxes) == 0 {
			if _, err := database.UpsertMailbox(db.Mailbox{AccountID: account.ID, Name: "INBOX", DisplayName: cleanDisplayName("INBOX"), Delimiter: "/"}); err != nil {
				return accounts, fmt.Errorf("create starter inbox for %s: %w", name, err)
			}
			changed = true
		}
	}
	if !changed {
		return accounts, nil
	}
	return database.ListAccounts()
}

func (m *Model) loadMailboxMessagesCmd(mailboxID int64) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		msgs, err := database.ListMessages(mailboxID)
		if err != nil {
			return MessagesLoadedMsg{MailboxID: mailboxID, Err: err}
		}
		return MessagesLoadedMsg{MailboxID: mailboxID, Messages: msgs}
	}
}

func (m *Model) loadUnifiedInboxCmd() tea.Cmd {
	database := m.db
	unreadOnly := m.showUnreadOnly
	return func() tea.Msg {
		msgs, err := database.ListUnifiedInbox(unreadOnly)
		if err != nil {
			return MessagesLoadedMsg{Err: err}
		}
		return MessagesLoadedMsg{MailboxID: 0, Messages: msgs}
	}
}

func (m *Model) syncMailboxCmd(mailboxID int64, manual bool) tea.Cmd {
	m.syncing[mailboxID] = true
	database := m.db
	mailbox, _ := database.GetMailbox(mailboxID)
	acc, _ := database.GetAccount(mailbox.AccountID)
	var acfg config.AccountConfig
	for _, a := range m.cfg.Accounts {
		if a.Name == acc.Name {
			acfg = a
			break
		}
	}
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 60*time.Second)
		defer cancel()
		client := imapClient.New(acfg)
		if err := client.Connect(ctx); err != nil {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual}
		}
		defer client.Close()
		since := mailbox.LastSynced
		if existing, countErr := database.CountMessages(mailboxID); countErr == nil && existing == 0 {
			since = time.Time{}
		}
		msgs, err := client.FetchSince(ctx, mailbox.Name, since)
		if err != nil {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual}
		}
		newCount, err := storeFetchedMessages(database, mailboxID, msgs)
		if err != nil {
			return MailboxSyncedMsg{MailboxID: mailboxID, Err: err, Manual: manual}
		}
		unread, _ := database.CountUnread(mailboxID)
		database.SetMailboxLastSynced(mailboxID, time.Now()) //nolint:errcheck
		database.SetMailboxUnreadCount(mailboxID, unread)    //nolint:errcheck
		return MailboxSyncedMsg{MailboxID: mailboxID, NewCount: newCount, Manual: manual}
	}
}

func storeFetchedMessages(database *db.DB, mailboxID int64, msgs []db.Message) (int, error) {
	newCount := 0
	for _, msg := range msgs {
		msg.MailboxID = mailboxID
		if err := database.UpsertMessage(msg); err != nil {
			return newCount, err
		}
		if !msg.Read {
			newCount++
		}
	}
	return newCount, nil
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

func (m *Model) setMessageReadCmd(msg db.Message, read, advance bool) tea.Cmd {
	database := m.db
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox != nil && acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			client := imapClient.New(acfg)
			if err := client.Connect(ctx); err != nil {
				return MessageReadUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read, Advance: advance, Err: err}
			}
			defer client.Close()
			if err := client.MarkSeen(ctx, mailbox.Name, msg.UID, read); err != nil {
				return MessageReadUpdatedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, WasRead: msg.Read, Read: read, Advance: advance, Err: err}
			}
		}
		if err := database.MarkRead(msg.ID, read); err != nil {
			return MessageReadUpdatedMsg{
				MessageID: msg.ID,
				MailboxID: msg.MailboxID,
				WasRead:   msg.Read,
				Read:      read,
				Advance:   advance,
				Err:       err,
			}
		}
		return MessageReadUpdatedMsg{
			MessageID: msg.ID,
			MailboxID: msg.MailboxID,
			WasRead:   msg.Read,
			Read:      read,
			Advance:   advance,
		}
	}
}

func (m *Model) archiveMessageCmd(msg db.Message) tea.Cmd {
	database := m.db
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox == nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, Action: "archive", Err: fmt.Errorf("mailbox not found")}
		}
		archive, err := database.FindArchiveMailbox(mailbox.AccountID)
		if err != nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, Action: "archive", Err: err}
		}
		if acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			client := imapClient.New(acfg)
			if err := client.Connect(ctx); err != nil {
				return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
			}
			defer client.Close()
			if err := client.MoveMessage(ctx, mailbox.Name, msg.UID, archive.Name); err != nil {
				return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
			}
		}
		if err := database.MoveMessage(msg.ID, archive.ID); err != nil {
			return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive", Err: err}
		}
		return MessageMovedMsg{MessageID: msg.ID, FromMailboxID: msg.MailboxID, ToMailboxID: archive.ID, Action: "archive"}
	}
}

func (m *Model) deleteMessageCmd(msg db.Message) tea.Cmd {
	database := m.db
	mailbox := m.mailboxByID(msg.MailboxID)
	acfg := m.accountCfgForMailbox(msg.MailboxID)
	return func() tea.Msg {
		if mailbox == nil {
			return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: fmt.Errorf("mailbox not found")}
		}
		if acfg.IMAPHost != "" && msg.UID != 0 {
			ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
			defer cancel()
			client := imapClient.New(acfg)
			if err := client.Connect(ctx); err != nil {
				return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: err}
			}
			defer client.Close()
			if err := client.DeleteMessage(ctx, mailbox.Name, msg.UID); err != nil {
				return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: err}
			}
		}
		if err := database.DeleteMessage(msg.ID); err != nil {
			return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID, Err: err}
		}
		return MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID}
	}
}

func (m *Model) focusedMessageChangedCmd(msg db.Message) tea.Cmd {
	if !m.cfg.Display.MarkReadOnFocus || msg.Read {
		return nil
	}
	return m.setMessageReadCmd(msg, true, false)
}

func (m *Model) markMailboxReadCmd(mailboxID int64) tea.Cmd {
	database := m.db
	return func() tea.Msg {
		if err := database.MarkAllRead(mailboxID); err != nil {
			return MailboxReadUpdatedMsg{Err: err}
		}
		return MailboxReadUpdatedMsg{MailboxIDs: []int64{mailboxID}}
	}
}

func (m *Model) markAccountReadCmd(accountID int64) tea.Cmd {
	mailboxIDs := make([]int64, 0)
	for _, mb := range m.mailboxes {
		if mb.AccountID == accountID {
			mailboxIDs = append(mailboxIDs, mb.ID)
		}
	}
	database := m.db
	return func() tea.Msg {
		applied := make([]int64, 0, len(mailboxIDs))
		for _, mbID := range mailboxIDs {
			if err := database.MarkAllRead(mbID); err != nil {
				return MailboxReadUpdatedMsg{MailboxIDs: applied, Err: err}
			}
			applied = append(applied, mbID)
		}
		return MailboxReadUpdatedMsg{MailboxIDs: applied}
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
	if len(m.filteredMessages) == 0 {
		return m, nil
	}
	msg := m.filteredMessages[m.messageCursor]

	if msg.Summary != "" {
		m.summaryMessage = msg
		m.summaryGenerating = false
		m.summaryErr = ""
		m.overlay = overlaySummary
		return m, nil
	}

	if m.summarizer == nil {
		m.setStatus("AI not configured — press S to open settings", false)
		return m, m.clearStatusCmd()
	}

	m.summaryMessage = msg
	m.summaryGenerating = true
	m.summaryErr = ""
	m.overlay = overlaySummary
	return m, m.aiSummarizeCmd(msg)
}

func (m *Model) aiSummarizeCmd(msg db.Message) tea.Cmd {
	summarizer := m.summarizer
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		summary, err := summarizer.Summarize(ctx, msg.Subject, msg.BodyText)
		return AISummaryFetchedMsg{MessageID: msg.ID, Summary: summary, Err: err}
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

func saveSummaryMDCmd(msg db.Message, summary, savePath string) tea.Cmd {
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

		filename := summaryFilename(msg.Subject)
		fullPath := filepath.Join(savePath, filename)

		content := fmt.Sprintf("# %s\n\n**From:** %s\n**Date:** %s\n\n---\n\n%s\n",
			msg.Subject,
			msg.From,
			msg.Date.Format("Mon, 02 Jan 2006"),
			summary,
		)
		if err := os.WriteFile(fullPath, []byte(content), 0o644); err != nil {
			return SummarySavedMsg{Err: err}
		}
		return SummarySavedMsg{Path: fullPath}
	}
}

func saveAttachmentsCmd(atts []db.Attachment) tea.Cmd {
	return func() tea.Msg {
		home, err := os.UserHomeDir()
		if err != nil {
			return AttachmentsSavedMsg{Err: fmt.Errorf("home dir: %w", err)}
		}
		dir := filepath.Join(home, "Downloads", "tidemail-attachments")
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return AttachmentsSavedMsg{Err: fmt.Errorf("create dir: %w", err)}
		}
		saved := 0
		for _, a := range atts {
			path := filepath.Join(dir, safeFilename(a.Filename))
			if err := os.WriteFile(path, a.Data, 0o644); err != nil {
				return AttachmentsSavedMsg{Err: fmt.Errorf("write %s: %w", a.Filename, err)}
			}
			saved++
		}
		return AttachmentsSavedMsg{Path: dir, Count: saved}
	}
}

func safeFilename(name string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(name) {
		switch {
		case (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') || r == '.' || r == '_':
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
		s = "attachment"
	}
	if len(s) > 100 {
		s = s[:100]
	}
	return s
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
	m.sidebarRows = buildSidebarRows(m.accounts, m.mailboxes, m.collapsedAccounts)
	m.sidebarCursor = clamp(m.sidebarCursor, 0, max(0, len(m.sidebarRows)-1))
	m.clampSidebarOffset()
}

func (m Model) sidebarVisibleRows() int {
	return max(1, m.mainHeight()-2) // mainHeight minus title row and footer row
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

func buildSidebarRows(accounts []db.Account, mailboxes []db.Mailbox, collapsed map[int64]bool) []sidebarRow {
	byAccount := make(map[int64][]db.Mailbox)
	for _, mb := range mailboxes {
		byAccount[mb.AccountID] = append(byAccount[mb.AccountID], mb)
	}
	for id := range byAccount {
		slices.SortStableFunc(byAccount[id], func(a, b db.Mailbox) int {
			ra, rb := mailboxRank(a.Name), mailboxRank(b.Name)
			if ra != rb {
				return ra - rb
			}
			return strings.Compare(strings.ToLower(a.Name), strings.ToLower(b.Name))
		})
	}
	rows := make([]sidebarRow, 0, len(accounts)+len(mailboxes)+1)
	if len(accounts) > 0 {
		rows = append(rows, sidebarRow{kind: rowKindUnified})
	}
	for _, acc := range accounts {
		rows = append(rows, sidebarRow{kind: rowKindAccount, accountID: acc.ID})
		if collapsed[acc.ID] {
			continue
		}
		for _, mb := range byAccount[acc.ID] {
			rows = append(rows, sidebarRow{kind: rowKindMailbox, mailboxID: mb.ID})
		}
	}
	return rows
}

func mailboxRank(name string) int {
	lower := strings.ToLower(name)
	if i := strings.LastIndex(lower, "/"); i >= 0 {
		lower = strings.TrimSpace(lower[i+1:])
	}
	switch lower {
	case "inbox":
		return 0
	case "sent", "sent items", "sent mail":
		return 1
	case "drafts":
		return 2
	case "archive", "all mail":
		return 3
	case "trash", "deleted items", "deleted messages":
		return 4
	case "junk", "spam", "junk e-mail", "junk email":
		return 5
	}
	return 6
}

// cleanDisplayName strips common IMAP namespace prefixes and tidies
// casing for display in the sidebar. The raw Name is preserved for IMAP
// SELECT commands; this is purely cosmetic.
func cleanDisplayName(name string) string {
	// Capitalize INBOX -> Inbox (Gmail returns it uppercase)
	if strings.EqualFold(name, "INBOX") {
		return "Inbox"
	}

	// Strip Gmail's [Gmail]/ prefix (also covers [Google]/ etc.)
	cleaned := name
	if idx := strings.Index(name, "]/"); idx >= 0 {
		cleaned = strings.TrimSpace(name[idx+2:])
	}

	// Strip "INBOX." or "INBOX/" prefix for custom accounts
	// (e.g. Courier/Dovecot: INBOX.Sent -> Sent, INBOX/Drafts -> Drafts)
	upper := strings.ToUpper(cleaned)
	if strings.HasPrefix(upper, "INBOX.") {
		cleaned = strings.TrimSpace(cleaned[6:])
	} else if strings.HasPrefix(upper, "INBOX/") {
		cleaned = strings.TrimSpace(cleaned[6:])
	}

	return cleaned
}

func (m Model) newAccountManager() AccountManager {
	am := NewAccountManager(m.db)
	am.mode = amList
	am.setData(m.accounts, m.mailboxes, m.cfg.Accounts)
	return am
}

func (m *Model) selectSidebarMailbox(mailboxID int64) bool {
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == mailboxID {
			m.sidebarCursor = i
			return true
		}
	}
	return false
}

func (m *Model) clearMessages() {
	m.messages = nil
	m.filteredMessages = nil
	m.messageCursor = 0
	m.listOffset = 0
	m.clearViewportMessage()
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
		return rowKindMailbox, 0
	}
	row := m.sidebarRows[m.sidebarCursor]
	if row.kind == rowKindUnified {
		return rowKindUnified, 0
	}
	if row.kind == rowKindAccount {
		return rowKindAccount, row.accountID
	}
	return rowKindMailbox, row.mailboxID
}

func (m Model) selectedUnifiedInbox() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	return m.sidebarRows[m.sidebarCursor].kind == rowKindUnified
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

func (m *Model) adjustMailboxUnreadCount(mailboxID int64, delta int64) {
	for i := range m.mailboxes {
		if m.mailboxes[i].ID == mailboxID {
			m.mailboxes[i].UnreadCount = max(0, m.mailboxes[i].UnreadCount+delta)
			return
		}
	}
}

func (m *Model) markMailboxesReadInMemory(mailboxIDs []int64) {
	if len(mailboxIDs) == 0 {
		return
	}
	mbSet := make(map[int64]struct{}, len(mailboxIDs))
	for _, mbID := range mailboxIDs {
		mbSet[mbID] = struct{}{}
		for i := range m.mailboxes {
			if m.mailboxes[i].ID == mbID {
				m.mailboxes[i].UnreadCount = 0
				break
			}
		}
	}
	changed := false
	for i := range m.messages {
		if _, ok := mbSet[m.messages[i].MailboxID]; ok && !m.messages[i].Read {
			m.messages[i].Read = true
			changed = true
		}
	}
	if !changed {
		return
	}
	m.applyFilter()
	if len(m.filteredMessages) == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return
	}
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, len(m.filteredMessages)-1))
	m.setViewportMessage(m.filteredMessages[m.messageCursor])
}

func (m *Model) removeMessageFromMemory(messageID int64) bool {
	wasUnread := false
	for i := range m.messages {
		if m.messages[i].ID == messageID {
			wasUnread = !m.messages[i].Read
			m.messages = append(m.messages[:i], m.messages[i+1:]...)
			break
		}
	}
	m.applyFilter()
	if len(m.filteredMessages) == 0 {
		m.messageCursor = 0
		m.listOffset = 0
		m.clearViewportMessage()
		return wasUnread
	}
	m.messageCursor = clamp(m.messageCursor, 0, max(0, len(m.filteredMessages)-1))
	m.listOffset = clamp(m.listOffset, 0, max(0, len(m.filteredMessages)-1))
	m.setViewportMessage(m.filteredMessages[m.messageCursor])
	return wasUnread
}

func (m Model) accountName(accountID int64) string {
	for _, acc := range m.accounts {
		if acc.ID == accountID {
			return acc.Name
		}
	}
	return "Account"
}

func (m Model) accountCfgForMailbox(mailboxID int64) config.AccountConfig {
	mb := m.mailboxByID(mailboxID)
	if mb == nil {
		if len(m.cfg.Accounts) > 0 {
			return m.cfg.Accounts[0]
		}
		return config.AccountConfig{}
	}
	acc := m.accountByID(mb.AccountID)
	if acc != nil {
		for _, acfg := range m.cfg.Accounts {
			if acfg.Name == acc.Name {
				return acfg
			}
		}
	}
	if len(m.cfg.Accounts) > 0 {
		return m.cfg.Accounts[0]
	}
	return config.AccountConfig{}
}

func (m Model) accountColor(accountID int64) lipgloss.Color {
	if accountID == 0 {
		return ""
	}
	if config.IsRetroTerminalTheme(string(m.styles.Theme.Name)) {
		return ""
	}
	if acc := m.accountByID(accountID); acc != nil && acc.Color != "" {
		return lipgloss.Color(acc.Color)
	}
	return ""
}

func (m Model) selectedMailboxAccountColor() lipgloss.Color {
	if m.selectedUnifiedInbox() {
		return ""
	}
	if mb := m.selectedMailbox(); mb != nil {
		return m.accountColor(mb.AccountID)
	}
	return ""
}

func (m Model) accountUnreadCount(accountID int64) int64 {
	var total int64
	for _, mb := range m.mailboxes {
		if mb.AccountID == accountID {
			total += mb.UnreadCount
		}
	}
	return total
}

func (m Model) unifiedUnreadCount() int64 {
	var total int64
	for _, mb := range m.mailboxes {
		if isInboxMailbox(mb) {
			total += mb.UnreadCount
		}
	}
	return total
}

func isInboxMailbox(mb db.Mailbox) bool {
	if strings.EqualFold(strings.TrimSpace(mb.Name), "inbox") || strings.EqualFold(strings.TrimSpace(mb.DisplayName), "inbox") {
		return true
	}
	for _, flag := range mb.Flags {
		if strings.EqualFold(flag, `\Inbox`) {
			return true
		}
	}
	return false
}

func (m Model) accountHeaderStyle(accountID int64, selected bool) lipgloss.Style {
	accent := m.accountColor(accountID)
	style := m.styles.FeedItem.Copy().Foreground(lipgloss.Color(m.styles.Theme.Dimmed)).Bold(true)
	if accent != "" {
		style = style.Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
	}
	if selected {
		style = m.sidebarSelectedStyle(accent).Copy().Bold(true)
	}
	return style
}

func (m Model) accountBadgeStyle(accountID int64, selected bool) lipgloss.Style {
	accent := m.accountColor(accountID)
	if selected {
		return m.sidebarSelectedBadgeStyle(accent)
	}
	if accent == "" {
		return m.styles.UnreadBadge
	}
	return m.styles.UnreadBadge.Copy().Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
}

func (m Model) mailboxAccentStyle(mb db.Mailbox, selected bool) lipgloss.Style {
	style := m.styles.FeedItem
	accent := m.accountColor(mb.AccountID)
	if accent != "" {
		style = style.Copy().Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
	}
	if selected {
		style = m.sidebarSelectedStyle(accent)
	}
	return style
}

func (m Model) mailboxBadgeStyle(mb db.Mailbox, selected bool) lipgloss.Style {
	accent := m.accountColor(mb.AccountID)
	if selected {
		return m.sidebarSelectedBadgeStyle(accent)
	}
	if accent == "" {
		return m.styles.UnreadBadge
	}
	return m.styles.UnreadBadge.Copy().Foreground(accentReadableOn(accent, m.styles.Theme.Bg, 3))
}

func (m Model) sidebarSelectedStyle(accent lipgloss.Color) lipgloss.Style {
	if m.focused == paneAccounts {
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

func (m Model) messageRowStyles() (lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Style, lipgloss.Color, lipgloss.Color) {
	accent := m.selectedMailboxAccountColor()
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

func (m *Model) toggleSelectedAccount() bool {
	accountID, ok := m.selectedAccountID()
	if !ok {
		return false
	}
	m.collapsedAccounts[accountID] = !m.collapsedAccounts[accountID]
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindAccount && row.accountID == accountID {
			m.sidebarCursor = i
			break
		}
	}
	m.clearMessages()
	return true
}

func (m *Model) applyFilter() {
	q := strings.ToLower(m.searchQuery)
	if q == "" && !m.showUnreadOnly {
		m.filteredMessages = m.messages
		return
	}
	filtered := make([]db.Message, 0, len(m.messages))
	for _, msg := range m.messages {
		if m.showUnreadOnly && msg.Read {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(msg.Subject), q) {
			continue
		}
		filtered = append(filtered, msg)
	}
	m.filteredMessages = filtered
}

func (m Model) indexOfFilteredMessage(messageID int64) int {
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

func (m Model) renderAccountHeader(accountID int64, selected bool, width int) string {
	icon := "v "
	label := m.accountName(accountID)
	if m.iconsEnabled() {
		icon = "▾ "
	}
	if m.collapsedAccounts[accountID] {
		icon = "> "
		if m.iconsEnabled() {
			icon = "▸ "
		}
	}
	badge := ""
	if unread := m.accountUnreadCount(accountID); unread > 0 {
		badge = m.accountBadgeStyle(accountID, selected).Render(fmt.Sprintf("(%d)", unread))
	}
	row := renderFeedRow(icon, label, badge, width)
	style := m.accountHeaderStyle(accountID, selected)
	return style.Width(width).Render(row)
}

func (m Model) renderUnifiedInboxRow(selected bool, width int) string {
	badge := ""
	if unread := m.unifiedUnreadCount(); unread > 0 {
		badge = m.accountBadgeStyle(0, selected).Render(fmt.Sprintf("(%d)", unread))
	}
	prefix := "◎ "
	if !m.iconsEnabled() {
		prefix = "* "
	}
	row := renderFeedRow(prefix, "Unified Inbox", badge, width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

func (m Model) renderSidebarMailboxRow(mb db.Mailbox, selected bool, width int) string {
	badge := ""
	if mb.UnreadCount > 0 {
		badge = m.mailboxBadgeStyle(mb, selected).Render(fmt.Sprintf("(%d)", mb.UnreadCount))
	}
	raw := mb.DisplayName
	if raw == "" {
		raw = mb.Name
	}
	title := cleanDisplayName(raw)
	prefix := "    "
	if !m.iconsEnabled() {
		prefix = "    " + m.mailboxRowPrefix(selected)
	}
	if m.syncing[mb.ID] {
		prefix = "    " + m.spinner.View() + " "
	}
	row := renderFeedRow(prefix, title, badge, width)
	style := m.mailboxAccentStyle(mb, selected)
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
	case "Accounts":
		return "◉ Accounts"
	case "Content":
		return "▣ Content"
	}
	if strings.HasPrefix(label, "Messages") {
		return strings.Replace(label, "Messages", "≣ Messages", 1)
	}
	return label
}

func (m Model) mailboxRowPrefix(selected bool) string {
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

func (m Model) messageRowPrefix(read bool) string {
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

func (m Model) emptyAccountsHint() string {
	if m.styles.PlainUI {
		return "  press m to add accounts"
	}
	if m.iconsEnabled() {
		return "  ＋ press m to add accounts"
	}
	return "  press m to add accounts"
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

func collapseQuoteBlocks(body string, collapsed bool) string {
	if !collapsed {
		return body
	}
	// Blockquote lines are rendered with ANSI dimmed styling, so │
	// is preceded by escape codes, not at position 0. Use Contains.
	quoteRune := "│"
	if !strings.Contains(body, quoteRune) {
		return body
	}
	lines := strings.Split(body, "\n")
	var result []string
	i := 0
	for i < len(lines) {
		if strings.Contains(lines[i], quoteRune) {
			start := i
			for i < len(lines) && strings.Contains(lines[i], quoteRune) {
				i++
			}
			count := i - start
			result = append(result, fmt.Sprintf("│  [+%d quoted lines — press z to expand]", count))
		} else {
			result = append(result, lines[i])
			i++
		}
	}
	return strings.Join(result, "\n")
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
