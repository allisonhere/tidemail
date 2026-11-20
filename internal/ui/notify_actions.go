package ui

import (
	"fmt"
	"log"
	"runtime"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/notify"
	tea "github.com/charmbracelet/bubbletea"
)

const maxEarlyNotifyActions = 16

type notifierOpenedMsg struct {
	conn   notify.Conn
	window string
}

type notifySentMsg struct {
	notifyID   uint32
	messageIDs []int64
}

type notifyActionEventMsg struct {
	event  notify.Event
	closed bool
	rearm  bool
}

func (m Model) openNotifierCmd() tea.Cmd {
	if runtime.GOOS != "linux" || !m.cfg.Display.Notifications {
		return nil
	}
	return func() tea.Msg {
		conn, err := notify.Open("tidemail")
		if err != nil {
			// Not fatal — notifyCmd falls back to plain notify-send — but
			// this is only tried once at startup, so a daemon that doesn't
			// advertise actions (or any other D-Bus hiccup) would otherwise
			// silently disable actionable notifications for the whole
			// session with no way to tell why.
			logNotify("notify\terror=%q", err)
			return notifierOpenedMsg{conn: nil}
		}
		return notifierOpenedMsg{conn: conn, window: notificationWindow()}
	}
}

// logNotify appends notifier diagnostics to the fetch log (same file syncs
// log to, viewable via Settings → Advanced → View Logs).
func logNotify(format string, args ...any) {
	f, openErr := openFetchLog()
	if openErr != nil {
		return
	}
	defer f.Close()
	log.New(f, "", log.LstdFlags).Printf(format, args...)
}

func (m Model) handleNotifierOpened(msg notifierOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.conn == nil {
		return m, nil
	}
	if m.notifier != nil {
		_ = m.notifier.Close()
	}
	m.notifier = msg.conn
	m.notificationWindow = msg.window
	return m, m.listenNotifyEventsCmd()
}

func (m Model) listenNotifyEventsCmd() tea.Cmd {
	if m.notifier == nil {
		return nil
	}
	events := m.notifier.Events()
	return func() tea.Msg {
		event, ok := <-events
		return notifyActionEventMsg{event: event, closed: !ok, rearm: true}
	}
}

func (m Model) handleNotifySent(msg notifySentMsg) (tea.Model, tea.Cmd) {
	if msg.notifyID == 0 || len(msg.messageIDs) == 0 {
		return m, nil
	}
	if m.pendingNotifyActions == nil {
		m.pendingNotifyActions = make(map[uint32][]int64)
	}
	m.pendingNotifyActions[msg.notifyID] = msg.messageIDs
	if event, ok := m.earlyNotifyActions[msg.notifyID]; ok {
		delete(m.earlyNotifyActions, msg.notifyID)
		return m.handleNotifyActionEvent(notifyActionEventMsg{event: event})
	}
	return m, nil
}

func (m Model) handleNotifyActionEvent(msg notifyActionEventMsg) (tea.Model, tea.Cmd) {
	if msg.closed {
		m.notifier = nil
		m.pendingNotifyActions = make(map[uint32][]int64)
		m.earlyNotifyActions = make(map[uint32]notify.Event)
		return m, nil
	}
	var cmds []tea.Cmd
	if msg.rearm {
		cmds = append(cmds, m.listenNotifyEventsCmd())
	}
	event := msg.event
	if event.ID == 0 {
		return m, tea.Batch(cmds...)
	}
	ids, ok := m.pendingNotifyActions[event.ID]
	if !ok {
		// Notify and the signal listener are concurrent Bubble Tea commands. A
		// very fast click can arrive before notifySentMsg registers the ID.
		if !event.Closed && knownNotifyAction(event.ActionKey) {
			if m.earlyNotifyActions == nil {
				m.earlyNotifyActions = make(map[uint32]notify.Event)
			}
			if len(m.earlyNotifyActions) < maxEarlyNotifyActions {
				m.earlyNotifyActions[event.ID] = event
			}
		}
		return m, tea.Batch(cmds...)
	}
	delete(m.pendingNotifyActions, event.ID)
	if m.notifier != nil {
		_ = m.notifier.CloseNotification(event.ID)
	}
	if event.Closed {
		return m, tea.Batch(cmds...)
	}
	if (event.ActionKey == "default" || event.ActionKey == "read") && len(ids) > 0 {
		m.pendingNotificationMessage = ids[0]
		cmds = append(cmds, raiseNotificationWindowCmd(m.notificationWindow))
		if m.overlay == overlayNone {
			cmds = append(cmds, m.openPendingNotification())
		} else {
			m.setStatus("Close the current dialog to read the notified message", false)
		}
		return m, tea.Batch(cmds...)
	}
	var messages []db.Message
	for _, id := range ids {
		if message, err := m.db.GetMessage(id); err == nil {
			messages = append(messages, message)
		}
	}
	if len(messages) == 0 {
		return m, tea.Batch(cmds...)
	}
	switch event.ActionKey {
	case "mark-read":
		read := true
		cmds = append(cmds, m.acceptReadChanges(messages, &read, false))
	case "delete":
		cmds = append(cmds, m.scheduleDelete(messages))
	}
	return m, tea.Batch(cmds...)
}

func knownNotifyAction(action string) bool {
	switch action {
	case "default", "read", "mark-read", "delete":
		return true
	default:
		return false
	}
}

// openPendingNotification runs on the update loop, after any modal has closed.
// Resolve the ID again: mail may have moved or disappeared since notification.
func (m *Model) openPendingNotification() tea.Cmd {
	id := m.pendingNotificationMessage
	m.pendingNotificationMessage = 0
	msg, err := m.db.GetMessage(id)
	if err != nil || m.messageHiddenDestructive(id) {
		m.setStatus("Notified message is no longer available", true)
		return m.clearStatusCmd()
	}
	mailbox := m.mailboxByID(msg.MailboxID)
	if mailbox == nil {
		m.setStatus("Notified message's folder is no longer available", true)
		return m.clearStatusCmd()
	}
	var messages []db.Message
	if m.cfg.Display.UnreadFirst {
		messages, err = m.db.ListMessagesUnreadFirst(mailbox.ID)
	} else {
		messages, err = m.db.ListMessages(mailbox.ID)
	}
	if err != nil {
		m.setStatus("Could not open notified message: "+err.Error(), true)
		return m.clearStatusCmd()
	}
	// Reveal the destination without persisting changes to collapsed/hidden folders.
	delete(m.collapsedAccounts, mailbox.AccountID)
	delete(m.collapsedSections, fmt.Sprintf("system:%d", mailbox.AccountID))
	delete(m.collapsedSections, fmt.Sprintf("personal:%d", mailbox.AccountID))
	for _, parent := range m.mailboxes {
		if mailboxIsDescendant(parent, *mailbox) {
			delete(m.collapsedSections, folderCollapseKey(parent))
		}
	}
	if (folderView{prefs: m.mailboxPrefs}).pref(*mailbox).Hidden {
		m.showHiddenFolders = true
	}
	m.rebuildSidebar()
	found := false
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == mailbox.ID {
			m.sidebarCursor = i
			found = true
			break
		}
	}
	if !found {
		m.setStatus("Notified message's folder is no longer available", true)
		return m.clearStatusCmd()
	}
	m.cancelFolderSettle()
	m.pendingSelectMessageID = 0
	m.heldReadID = 0
	m.searchMode, m.searchEditing, m.showUnreadOnly = false, false, false
	m.searchQuery = ""
	m.searchInput.Reset()
	m.closeSearchHistory()
	m.clearMessages()
	m.messages = messages
	m.plugins.annotations, m.plugins.overrides = loadMessageClassification(m.db, messages)
	m.applyFilter()
	idx := m.indexOfFilteredMessage(id)
	if idx < 0 {
		m.setStatus("Notified message is no longer available", true)
		return m.clearStatusCmd()
	}
	m.messageCursor = idx
	m.keepMessageCursorVisible()
	m.clampSidebarOffset()
	m.setStatus("", false)
	m.focused = paneContent
	m.setViewportForCurrentRow()
	m.setStatus("", false)
	return m.openedMessageCmd(msg)
}
