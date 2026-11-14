package ui

import (
	"runtime"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/notify"
	tea "github.com/charmbracelet/bubbletea"
)

const maxEarlyNotifyActions = 16

type notifierOpenedMsg struct {
	conn notify.Conn
}

type notifySentMsg struct {
	notifyID  uint32
	messageID int64
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
		conn, _ := notify.Open("tidemail")
		return notifierOpenedMsg{conn: conn}
	}
}

func (m Model) handleNotifierOpened(msg notifierOpenedMsg) (tea.Model, tea.Cmd) {
	if msg.conn == nil {
		return m, nil
	}
	if m.notifier != nil {
		_ = m.notifier.Close()
	}
	m.notifier = msg.conn
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
	if msg.notifyID == 0 || msg.messageID == 0 {
		return m, nil
	}
	if m.pendingNotifyActions == nil {
		m.pendingNotifyActions = make(map[uint32]int64)
	}
	m.pendingNotifyActions[msg.notifyID] = msg.messageID
	if event, ok := m.earlyNotifyActions[msg.notifyID]; ok {
		delete(m.earlyNotifyActions, msg.notifyID)
		return m.handleNotifyActionEvent(notifyActionEventMsg{event: event})
	}
	return m, nil
}

func (m Model) handleNotifyActionEvent(msg notifyActionEventMsg) (tea.Model, tea.Cmd) {
	if msg.closed {
		m.notifier = nil
		m.pendingNotifyActions = make(map[uint32]int64)
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
	messageID, ok := m.pendingNotifyActions[event.ID]
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
	if event.Closed || event.ActionKey == "ignore" {
		return m, tea.Batch(cmds...)
	}
	message, err := m.db.GetMessage(messageID)
	if err != nil {
		return m, tea.Batch(cmds...)
	}
	switch event.ActionKey {
	case "mark-read":
		cmds = append(cmds, m.setMessageReadCmd(message, true, false))
	case "delete":
		cmds = append(cmds, m.scheduleDelete([]db.Message{message}))
	}
	return m, tea.Batch(cmds...)
}

func knownNotifyAction(action string) bool {
	switch action {
	case "mark-read", "delete", "ignore":
		return true
	default:
		return false
	}
}
