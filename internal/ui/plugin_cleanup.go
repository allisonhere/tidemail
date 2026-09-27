package ui

// Annotation cleanup for the experimental plugin system: stored-annotation
// counts in the plugin list, and explicit, confirmed removal of annotations
// from one message or for one plugin everywhere. Cleanup only ever deletes
// plugin_annotations rows; it never touches messages, mail state, or plugins.

import (
	"errors"
	"fmt"
	"sort"
	"strings"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// pluginConfirmKind says which rows a cleanup removes.
type pluginConfirmKind int

const (
	// clearPluginOnMessage removes one plugin's annotations from one message.
	clearPluginOnMessage pluginConfirmKind = iota + 1
	// clearAllOnMessage removes every plugin's annotations from one message.
	clearAllOnMessage
	// clearPluginEverywhere removes one plugin's annotations from all messages.
	clearPluginEverywhere
	// enableAutoEvents turns on automatic message.received for a plugin (see
	// plugin_events.go). It shares this confirmation dialog.
	enableAutoEvents
	// runPluginBulk confirms a large manual run (plugin_bulk.go).
	runPluginBulk
)

// pluginConfirmAction is a pending or finished cleanup. The plugin ID always
// comes from stored annotations or the plugin manager, never from typed input.
type pluginConfirmAction struct {
	kind      pluginConfirmKind
	pluginID  string
	messageID int64
	// label is the display name used in the confirmation and status text.
	label string
	// count is the number of messages, for runPluginBulk, scope what they
	// are, and view whether they are a whole view.
	count int
	scope string
	view  bool
}

// pluginAnnotationCountsMsg carries freshly loaded per-plugin counts.
type pluginAnnotationCountsMsg struct {
	Counts map[string]int64
	Err    error
}

// pluginAnnotationsClearedMsg reports a finished cleanup. Counts is the
// reloaded per-plugin count map when CountsOK.
type pluginAnnotationsClearedMsg struct {
	Action   pluginConfirmAction
	Err      error
	Counts   map[string]int64
	CountsOK bool
}

// loadPluginAnnotationCountsCmd loads counts off the Update loop. It returns
// nil without a database, so the plugin list simply shows no counts.
func loadPluginAnnotationCountsCmd(database *db.DB) tea.Cmd {
	if database == nil {
		return nil
	}
	return func() tea.Msg {
		counts, err := database.PluginAnnotationCounts()
		return pluginAnnotationCountsMsg{Counts: counts, Err: err}
	}
}

// clearAnnotationsCmd performs a confirmed cleanup off the Update loop, then
// reloads the counts in the same command.
func clearAnnotationsCmd(database *db.DB, action pluginConfirmAction) tea.Cmd {
	return func() tea.Msg {
		if database == nil {
			return pluginAnnotationsClearedMsg{Action: action, Err: errors.New("no database")}
		}
		var err error
		switch action.kind {
		case clearPluginOnMessage:
			err = database.DeletePluginAnnotations(action.pluginID, action.messageID)
		case clearAllOnMessage:
			err = database.DeleteMessagePluginAnnotations(action.messageID)
		case clearPluginEverywhere:
			err = database.DeletePluginAnnotationsForPlugin(action.pluginID)
		default:
			err = fmt.Errorf("unknown cleanup kind %d", action.kind)
		}
		msg := pluginAnnotationsClearedMsg{Action: action, Err: err}
		if err == nil {
			if counts, countErr := database.PluginAnnotationCounts(); countErr == nil {
				msg.Counts, msg.CountsOK = counts, true
			}
		}
		return msg
	}
}

// handlePluginCleanupMsg applies count and cleanup results. It never changes
// the open overlay, so a late result cannot take over the screen.
func (m Model) handlePluginCleanupMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case pluginAnnotationCountsMsg:
		if msg.Err != nil {
			m.setStatus("could not load plugin annotation counts: "+sanitizePluginLine(msg.Err.Error()), true)
			return m, m.clearStatusCmd()
		}
		m.plugins.counts = msg.Counts
		m.plugins.countsLoaded = true
		m.clampPluginCursors()
		return m, nil

	case pluginAnnotationsClearedMsg:
		if msg.Err != nil {
			// Nothing was deleted, so the cache and counts stay as they are.
			m.setStatus("clearing annotations failed: "+sanitizePluginLine(msg.Err.Error()), true)
			return m, m.clearStatusCmd()
		}
		m.patchAnnotationCache(msg.Action)
		if msg.CountsOK {
			m.plugins.counts = msg.Counts
			m.plugins.countsLoaded = true
		}
		m.clampPluginCursors()
		m.setStatus(clearedStatus(msg.Action), false)
		return m, tea.Batch(m.clearStatusCmd(), m.needsYouRefreshCmd())
	}
	return m, nil
}

// patchAnnotationCache applies a finished cleanup to the in-memory cache only
// for the affected entries; nothing is reloaded from SQLite.
func (m *Model) patchAnnotationCache(action pluginConfirmAction) {
	switch action.kind {
	case clearAllOnMessage:
		delete(m.plugins.annotations, action.messageID)
	case clearPluginOnMessage:
		m.dropPluginAnnotations(action.messageID, action.pluginID)
	case clearPluginEverywhere:
		for id := range m.plugins.annotations {
			m.dropPluginAnnotations(id, action.pluginID)
		}
	}
}

func (m *Model) dropPluginAnnotations(messageID int64, pluginID string) {
	anns := m.plugins.annotations[messageID]
	kept := anns[:0:0]
	for _, a := range anns {
		if a.PluginID != pluginID {
			kept = append(kept, a)
		}
	}
	if len(kept) == 0 {
		delete(m.plugins.annotations, messageID)
	} else {
		m.plugins.annotations[messageID] = kept
	}
}

func clearedStatus(a pluginConfirmAction) string {
	switch a.kind {
	case clearPluginOnMessage:
		return "cleared " + a.label + " annotations from this message"
	case clearAllOnMessage:
		return "cleared all plugin annotations from this message"
	default:
		return "cleared all stored annotations from " + a.label
	}
}

// ── Plugin list entries ──────────────────────────────────────────────────────

// pluginListEntry is one selectable row in the plugin list: an installed
// plugin, or a plugin ID that only exists in stored annotations.
type pluginListEntry struct {
	pluginID  string
	installed bool
	label     string
	count     int64
}

// pluginListEntries lists installed plugins in discovery order, then plugin
// IDs with stored annotations that are no longer installed, sorted.
func (m Model) pluginListEntries() []pluginListEntry {
	var entries []pluginListEntry
	installed := map[string]bool{}
	for _, p := range m.plugins.manager.Plugins() {
		installed[p.Manifest.ID] = true
		entries = append(entries, pluginListEntry{
			pluginID: p.Manifest.ID, installed: true,
			label: sanitizePluginLine(p.Manifest.Name), count: m.plugins.counts[p.Manifest.ID],
		})
	}
	var removed []string
	for id, n := range m.plugins.counts {
		if !installed[id] && n > 0 {
			removed = append(removed, id)
		}
	}
	sort.Strings(removed)
	for _, id := range removed {
		entries = append(entries, pluginListEntry{
			pluginID: id, label: `plugin "` + sanitizePluginLine(id) + `"`, count: m.plugins.counts[id],
		})
	}
	return entries
}

// annotationGroups lists the plugin IDs with annotations on the message shown
// in the annotations overlay, in display order.
func (m Model) annotationGroups() []string {
	var ids []string
	for _, a := range m.plugins.annotations[m.plugins.annotationsFor] {
		if len(ids) == 0 || ids[len(ids)-1] != a.PluginID {
			ids = append(ids, a.PluginID)
		}
	}
	return ids
}

func (m *Model) clampPluginCursors() {
	m.plugins.listCursor = clamp(m.plugins.listCursor, 0, max(0, len(m.pluginListEntries())-1))
	m.plugins.annCursor = clamp(m.plugins.annCursor, 0, max(0, len(m.annotationGroups())-1))
}

// pluginDisplayName is the installed plugin's name, or its ID.
func (m Model) pluginDisplayName(pluginID string) string {
	if p, ok := m.plugins.manager.Plugin(pluginID); ok {
		return sanitizePluginLine(p.Manifest.Name)
	}
	return sanitizePluginLine(pluginID)
}

// ── Keys ─────────────────────────────────────────────────────────────────────

// handlePluginListKey drives the plugin list: the cursor moves between
// plugins, enter runs the selected plugin's report, s opens its settings, and
// c asks to clear its stored annotations.
func (m Model) handlePluginListKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	entries := m.pluginListEntries()
	if len(entries) > 0 {
		if next, cmd, handled := m.handlePluginEventKey(msg, entries[clamp(m.plugins.listCursor, 0, len(entries)-1)]); handled {
			return next, cmd
		}
	}
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = m.plugins.listOrigin
		m.plugins.scroll = 0
	case msg.String() == "s" && len(entries) > 0:
		if e := entries[clamp(m.plugins.listCursor, 0, len(entries)-1)]; e.installed {
			return m.openPluginSettings(e.pluginID)
		}
	case msg.Type == tea.KeyEnter && len(entries) > 0:
		if e := entries[clamp(m.plugins.listCursor, 0, len(entries)-1)]; e.installed && m.pluginHasReport(e.pluginID) {
			return m.startPluginReport(e.pluginID)
		}
	case keyMatches(msg, m.keys.Up):
		if m.plugins.listCursor > 0 {
			m.plugins.listCursor--
			m.revealPluginListCursor()
		} else {
			m.plugins.scroll = max(0, m.plugins.scroll-1)
		}
	case keyMatches(msg, m.keys.Down):
		if m.plugins.listCursor < len(entries)-1 {
			m.plugins.listCursor++
			m.revealPluginListCursor()
		} else {
			// Past the last plugin, keep scrolling to reach discovery errors.
			m.plugins.scroll = min(m.plugins.scroll+1, m.pluginMaxScroll())
		}
	case msg.String() == "r" && m.plugins.result != nil:
		m.plugins.scroll = 0
		m.overlay = overlayPluginResult
	case msg.String() == "c":
		if len(entries) == 0 {
			return m, nil
		}
		e := entries[clamp(m.plugins.listCursor, 0, len(entries)-1)]
		if e.count == 0 {
			return m, nil
		}
		m.confirmPluginAction(pluginConfirmAction{kind: clearPluginEverywhere, pluginID: e.pluginID, label: e.label})
	}
	return m, nil
}

// handleAnnotationsKey drives the annotations overlay: the cursor moves
// between plugin groups; c clears the selected plugin's annotations on this
// message, C clears all of them.
func (m Model) handleAnnotationsKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	groups := m.annotationGroups()
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.overlay = overlayNone
		m.plugins.scroll = 0
	case keyMatches(msg, m.keys.Up):
		if m.plugins.annCursor > 0 {
			m.plugins.annCursor--
			m.revealAnnotationCursor()
		}
	case keyMatches(msg, m.keys.Down):
		if m.plugins.annCursor < len(groups)-1 {
			m.plugins.annCursor++
			m.revealAnnotationCursor()
		} else {
			m.plugins.scroll = min(m.plugins.scroll+1, m.pluginMaxScroll())
		}
	case msg.String() == "c" && len(groups) > 0:
		id := groups[clamp(m.plugins.annCursor, 0, len(groups)-1)]
		m.confirmPluginAction(pluginConfirmAction{
			kind: clearPluginOnMessage, pluginID: id, messageID: m.plugins.annotationsFor, label: m.pluginDisplayName(id),
		})
	case msg.String() == "C" && len(groups) > 0:
		m.confirmPluginAction(pluginConfirmAction{kind: clearAllOnMessage, messageID: m.plugins.annotationsFor})
	case msg.String() == "e":
		return m.openClassificationEditorFor(m.plugins.annotationsFor)
	}
	return m, nil
}

func (m *Model) confirmPluginAction(action pluginConfirmAction) {
	m.plugins.confirm = &action
	m.plugins.confirmOrigin = m.overlay
	m.overlay = overlayPluginConfirm
}

// handlePluginConfirm runs the pending cleanup on y/enter and drops
// it on n/esc; either way it returns to the overlay it came from.
func (m Model) handlePluginConfirm(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch {
	case keyMatches(msg, m.keys.Yes), keyMatches(msg, m.keys.Confirm):
		action := m.plugins.confirm
		m.plugins.confirm = nil
		m.overlay = m.plugins.confirmOrigin
		if action == nil {
			return m, nil
		}
		if action.kind == enableAutoEvents {
			m.setAutoEvents(action.pluginID, true)
			return m, m.clearStatusCmd()
		}
		if action.kind == runPluginBulk {
			return m.startBulk(action.pluginID)
		}
		return m, clearAnnotationsCmd(m.db, *action)
	case keyMatches(msg, m.keys.No), keyMatches(msg, m.keys.Cancel):
		if a := m.plugins.confirm; a != nil && a.kind == runPluginBulk {
			m.plugins.pickerMetas = nil // nothing is launched
			m.plugins.pickerScope = reclassifyScope{}
		}
		m.plugins.confirm = nil
		m.overlay = m.plugins.confirmOrigin
	}
	return m, nil
}

// revealPluginListCursor scrolls so the selected plugin's first line shows.
func (m *Model) revealPluginListCursor() {
	winW, winH := m.pluginOverlaySize()
	_, starts := m.pluginListLines(winW-4, newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI))
	m.revealLine(starts, m.plugins.listCursor, winH)
}

func (m *Model) revealAnnotationCursor() {
	winW, winH := m.pluginOverlaySize()
	_, starts := m.annotationLines(winW-4, newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI))
	m.revealLine(starts, m.plugins.annCursor, winH)
}

func (m *Model) revealLine(starts []int, cursor, winH int) {
	if cursor < 0 || cursor >= len(starts) {
		return
	}
	line, h := starts[cursor], pluginScrollHeight(winH)
	switch {
	case line < m.plugins.scroll:
		m.plugins.scroll = line
	case line >= m.plugins.scroll+h:
		m.plugins.scroll = line - h + 1
	}
}

// ── Confirmation dialog ──────────────────────────────────────────────────────

func (m Model) renderPluginConfirm() string {
	winW := max(1, min(m.width-4, 58))
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	var text string
	title, verb := "clear annotations?", "clear"
	if a := m.plugins.confirm; a != nil && a.kind == runPluginBulk {
		title, verb = "run plugin?", "run"
		scope := a.scope
		if scope == "" {
			scope = fmt.Sprintf("%d messages", a.count)
		}
		text = fmt.Sprintf("Run %s on %s?\n\n",
			a.label, scope) +
			"This will run the plugin once for each message. Existing annotations from this plugin may be replaced.\n\n" +
			"Only the metadata this plugin is permitted to receive will be sent (sender, recipients, subject, date, flags), never message bodies."
	} else if a != nil && a.kind == enableAutoEvents {
		title, verb = "auto-process new mail?", "enable"
		text = "Let " + a.label + " process new mail automatically?\n\n" +
			"This plugin will automatically receive metadata (sender, recipients, subject, date, flags) for newly received messages. Message bodies are not sent.\n\n" +
			"You can turn this off at any time with a in the plugin list."
	} else if a != nil {
		switch a.kind {
		case clearPluginOnMessage:
			text = "Clear " + a.label + " annotations from this message?"
		case clearAllOnMessage:
			text = "Clear all plugin annotations from this message?"
		case clearPluginEverywhere:
			text = "Clear all stored annotations from " + a.label + "?"
			if _, installed := m.plugins.manager.Plugin(a.pluginID); installed {
				text += "\n\nThe plugin stays installed."
			}
		}
		text += "\n\nThis removes only TideMail's stored annotation data. It does not delete or change any messages."
	}
	body := lipgloss.NewStyle().
		Background(chrome.baseBg).
		Foreground(chrome.text).
		Width(winW).
		Padding(1, 2).
		Render(text)
	hints := renderSoftHints(winW, chrome, "y/enter", verb, "esc", "cancel")
	inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", title, chrome)
}
