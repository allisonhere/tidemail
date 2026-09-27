package ui

// Automatic message.received events: the UI side. plugin.EventManager owns
// the queues, workers, limits, and failure pauses; this file starts it, feeds
// it genuinely new mail, persists the per-plugin switch, and turns its
// updates into Bubble Tea messages. Nothing here runs a plugin itself.

import (
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

// eventShutdownTimeout bounds how long quitting waits for plugin workers.
const eventShutdownTimeout = 2 * time.Second

// pluginEventMsg carries one finished automatic run from the event manager.
type pluginEventMsg struct {
	Update plugin.EventUpdate
}

// pluginAnnotationsRefreshedMsg carries one message's reloaded annotations
// after an automatic run stored some.
type pluginAnnotationsRefreshedMsg struct {
	MessageID   int64
	Annotations []db.PluginAnnotation
	Err         error
}

// startPluginEvents creates the event manager for event-capable plugins and
// applies the saved switches. Without a database there is nowhere to store
// results, so automatic events stay off.
func (m *Model) startPluginEvents() {
	if m.db == nil || m.plugins.manager == nil {
		return
	}
	events := plugin.NewEventManager(m.plugins.ctx, m.plugins.manager, dbAnnotationStore{m.db}, plugin.EventOptions{})
	if len(events.PluginIDs()) == 0 {
		events.Close(0)
		return
	}
	for _, id := range events.PluginIDs() {
		events.SetEnabled(id, m.cfg.PluginAutoEvents(id))
	}
	m.plugins.events = events
	m.refreshPluginEventStatus()
}

// stopPluginEvents cancels running plugin processes and waits briefly for
// the workers to exit.
func (m Model) stopPluginEvents() {
	m.plugins.events.Close(eventShutdownTimeout)
}

// pluginEventListenCmd waits for the next finished automatic run. It is
// re-armed after every pluginEventMsg, so updates always reach Update through
// Bubble Tea rather than touching the model from a worker goroutine.
func (m Model) pluginEventListenCmd() tea.Cmd {
	updates := m.plugins.events.Updates()
	if updates == nil {
		return nil
	}
	ctx := m.plugins.ctx
	return func() tea.Msg {
		select {
		case u := <-updates:
			return pluginEventMsg{Update: u}
		case <-ctx.Done():
			return nil
		}
	}
}

// refreshPluginEventStatus copies the manager's state into the model so
// rendering never takes its lock.
func (m *Model) refreshPluginEventStatus() {
	m.plugins.eventStatus = m.plugins.events.Status()
}

// enqueuePluginEvents hands genuinely new mail from a sync to the event
// manager. Cold syncs (first setup or a cache reset) fetch a page of history,
// not new arrivals, so they are skipped; so is anything without a stored row.
// Opening a mailbox, loading older mail, and startup reloads never come here.
func (m *Model) enqueuePluginEvents(msg MailboxSyncedMsg) {
	if m.plugins.events == nil || msg.Err != nil || msg.Cold || len(msg.NewMessages) == 0 {
		return
	}
	metas := make([]plugin.MessageMetadata, 0, len(msg.NewMessages))
	for _, nm := range msg.NewMessages {
		if nm.ID > 0 {
			metas = append(metas, m.pluginMessageMetadata(nm))
		}
	}
	m.plugins.events.Enqueue(metas...)
	m.refreshPluginEventStatus()
}

func (m Model) handlePluginEvent(msg pluginEventMsg) (tea.Model, tea.Cmd) {
	u := msg.Update
	m.refreshPluginEventStatus()
	cmds := []tea.Cmd{m.pluginEventListenCmd()}
	if u.Paused {
		m.setStatus(fmt.Sprintf("plugin %s paused after %d failures: %s", u.PluginID, plugin.EventFailureLimit, sanitizePluginLine(m.plugins.eventStatus[u.PluginID].LastError)), true)
		cmds = append(cmds, m.clearStatusCmd())
	}
	if u.Err == nil && u.Result.Outcome == plugin.AnnotationsStored {
		cmds = append(cmds, loadMessageAnnotationsCmd(m.db, u.MessageID), loadPluginAnnotationCountsCmd(m.db))
	}
	return m, tea.Batch(cmds...)
}

// loadMessageAnnotationsCmd reloads one message's annotations off the Update
// loop.
func loadMessageAnnotationsCmd(database *db.DB, messageID int64) tea.Cmd {
	if database == nil {
		return nil
	}
	return func() tea.Msg {
		anns, err := database.ListPluginAnnotations(messageID)
		return pluginAnnotationsRefreshedMsg{MessageID: messageID, Annotations: anns, Err: err}
	}
}

func (m Model) handlePluginAnnotationsRefreshed(msg pluginAnnotationsRefreshedMsg) (tea.Model, tea.Cmd) {
	if msg.Err != nil {
		return m, nil // badges catch up on the next mailbox load
	}
	if m.plugins.annotations == nil {
		m.plugins.annotations = map[int64][]db.PluginAnnotation{}
	}
	if len(msg.Annotations) == 0 {
		delete(m.plugins.annotations, msg.MessageID)
	} else {
		m.plugins.annotations[msg.MessageID] = msg.Annotations
	}
	return m, nil
}

// setAutoEvents persists the switch for a plugin and applies it.
func (m *Model) setAutoEvents(pluginID string, on bool) {
	previous := m.cfg
	m.cfg.SetPluginAutoEvents(pluginID, on)
	if err := m.saveConfig(); err != nil {
		m.cfg = previous
		return
	}
	m.plugins.events.SetEnabled(pluginID, on)
	m.refreshPluginEventStatus()
	state := "off"
	if on {
		state = "on"
	}
	m.setStatus(fmt.Sprintf("auto-processing for plugin %s is %s", pluginID, state), false)
}

// eventCapable reports whether pluginID is installed and can receive
// message.received.
func (m Model) eventCapable(pluginID string) bool {
	p, ok := m.plugins.manager.Plugin(pluginID)
	return ok && plugin.EventCapable(p)
}

// handlePluginEventKey handles a to toggle auto-processing and r to resume a
// paused plugin in the plugin list. It reports whether it used the key.
func (m Model) handlePluginEventKey(msg tea.KeyMsg, e pluginListEntry) (Model, tea.Cmd, bool) {
	if !e.installed || !m.eventCapable(e.pluginID) || m.plugins.events == nil {
		return m, nil, false
	}
	status := m.plugins.eventStatus[e.pluginID]
	switch msg.String() {
	case "a":
		if m.cfg.PluginAutoEvents(e.pluginID) {
			// Turning it off needs no confirmation.
			m.setAutoEvents(e.pluginID, false)
			return m, m.clearStatusCmd(), true
		}
		m.confirmPluginAction(pluginConfirmAction{kind: enableAutoEvents, pluginID: e.pluginID, label: e.label})
		return m, nil, true
	case "r":
		if !status.Paused {
			return m, nil, false
		}
		m.plugins.events.Resume(e.pluginID)
		m.refreshPluginEventStatus()
		m.setStatus("resumed plugin "+e.pluginID+"; dropped events are not replayed", false)
		return m, m.clearStatusCmd(), true
	}
	return m, nil, false
}

// pluginEventLines are the plugin-list lines for an event-capable plugin.
// Plugins without events get none, keeping their entry short.
func (m Model) pluginEventLines(pluginID string) []string {
	if !m.eventCapable(pluginID) {
		return nil
	}
	lines := []string{"events: " + plugin.EventMessageReceived}
	st := m.plugins.eventStatus[pluginID]
	switch {
	case m.plugins.events == nil:
		lines = append(lines, "auto: unavailable")
	case st.Paused:
		lines = append(lines, "auto: paused", "reason: "+sanitizePluginLine(st.LastError))
	case m.cfg.PluginAutoEvents(pluginID):
		lines = append(lines, "auto: enabled")
	default:
		lines = append(lines, "auto: disabled")
	}
	if m.cfg.PluginAutoEvents(pluginID) || st.Dropped > 0 || st.ConsecutiveFailures > 0 {
		running := "no"
		if st.Running {
			running = "yes"
		}
		dot := strings.TrimSpace(m.styles.InlineMidDot())
		lines = append(lines,
			fmt.Sprintf("queued: %d %s running: %s", st.Queued, dot, running),
			fmt.Sprintf("failures: %d %s dropped: %d", st.ConsecutiveFailures, dot, st.Dropped))
	}
	return lines
}

// pluginEventHints are the plugin-list hint pairs for the selected entry.
func (m Model) pluginEventHints(e pluginListEntry) []string {
	if !e.installed || !m.eventCapable(e.pluginID) || m.plugins.events == nil {
		return nil
	}
	hints := []string{"a", "auto on/off"}
	if m.plugins.eventStatus[e.pluginID].Paused {
		hints = append(hints, "r", "resume")
	}
	return hints
}
