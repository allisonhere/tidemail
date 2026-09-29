package ui

// Experimental plugin integration: a read-only plugin list, and a manual
// "run plugin on current message" action that sends header-level metadata to
// one plugin chosen by the user. Plugins run in a tea.Cmd, never in Update,
// and their responses are shown as text only; nothing in a response is ever
// acted on.

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"
	"unicode"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/charmbracelet/x/ansi"
)

const (
	// pluginResultMaxBytes and pluginResultMaxLines cap the result overlay.
	pluginResultMaxBytes = 16 << 10
	pluginResultMaxLines = 200
)

// pluginUI is all plugin state on the Model. The zero value means "no
// plugins": every entry point is hidden and nothing is ever launched.
type pluginUI struct {
	manager      *plugin.Manager
	dir          string
	discoveryErr error

	// ctx is cancelled by CloseSessions so a running plugin is killed when
	// TideMail exits.
	ctx    context.Context
	cancel context.CancelFunc

	// running is the ID of the plugin in flight; one run at a time.
	running string
	// last is the plugin most recently run by hand this session, for
	// "Run … again".
	last string

	picker       []plugin.Plugin
	pickerCursor int
	// pickerMetas are the messages the picked plugin will run on, and
	// pickerScope what they are (plugin_bulk.go).
	pickerMetas []plugin.MessageMetadata
	pickerScope reclassifyScope
	// bulk is the manual multi-message run in progress (plugin_bulk.go).
	bulk *pluginBulkRun

	result *pluginResult
	// scroll is the first visible line of the plugin list or result overlay.
	scroll int

	// annotations caches stored plugin annotations for the loaded messages,
	// keyed by message ID. It is filled by the message load commands and by
	// plugin runs, so rendering never touches the database.
	annotations map[int64][]db.PluginAnnotation
	overrides   map[int64]map[string]db.ClassificationOverride
	// annotationsFor is the message shown in the annotations overlay.
	annotationsFor int64

	// counts is stored annotations per plugin ID (see plugin_cleanup.go),
	// loaded outside rendering; countsLoaded is false until the first load.
	counts       map[string]int64
	countsLoaded bool
	// listCursor selects a plugin in the plugin list; annCursor a plugin
	// group in the annotations overlay.
	listCursor int
	annCursor  int
	// confirm is the cleanup awaiting y/n, and confirmOrigin the overlay to
	// return to afterwards.
	confirm       *pluginConfirmAction
	confirmOrigin overlayMode

	// events delivers message.received automatically (plugin_events.go); nil
	// when no plugin can receive it. eventStatus is its last snapshot, read by
	// rendering instead of the manager itself.
	events      *plugin.EventManager
	eventStatus map[string]plugin.RuntimeStatus

	// settings is the plugin settings form (plugin_settings.go);
	// settingsSrc hands stored settings to the manager.
	settings    pluginSettingsState
	settingsSrc *pluginSettingsSource
	// listOrigin is where Esc from the plugin list returns (Settings or
	// nothing).
	listOrigin overlayMode
	// reportOrigin remembers where an asynchronous report was launched.
	reportOrigin overlayMode
	// resultOrigin returns a report opened from Settings to its plugin list.
	resultOrigin overlayMode
}

type pluginResult struct {
	pluginID   string
	pluginName string
	subject    string
	// heading replaces the "message: subject" line (reports have no message).
	heading string
	body    string
	// view is a report's structured view, drawn instead of body.
	view *plugin.View
	// card is the human-facing result, shown by default; details the raw
	// record shown with d. showDetails toggles between them.
	card        *resultCard
	details     []string
	showDetails bool
	// message is TideMail's own source message for host-owned card actions.
	// It is never sent to the plugin.
	message *db.Message
	// annotationNote says what happened to the response's annotations.
	annotationNote string
}

// pluginResultMsg reports a finished plugin run back to Update.
type pluginResultMsg struct {
	PluginID  string
	MessageID int64
	Subject   string
	Result    plugin.MessageMetadataResult
	Err       error
	Elapsed   time.Duration
	// Refreshed is the message's complete stored annotation set after the
	// run, valid when RefreshedOK. It replaces the cache entry.
	Refreshed   []db.PluginAnnotation
	RefreshedOK bool
	// Compared is set for a successful run whose before and after sets were
	// both read; Changed then says whether this plugin's set differs.
	Compared bool
	Changed  bool
	// Source is TideMail's snapshot of the message at the start of a manual
	// single-message run. It is never part of the plugin request.
	Source *db.Message
}

// LoadPlugins discovers plugins in the config directory. Failure is never
// fatal: TideMail then behaves as if no plugins were installed, and the error
// is shown in the plugin list.
func (m *Model) LoadPlugins() {
	dir, err := config.PluginDir()
	if err != nil {
		m.SetPlugins(nil, "", err)
		return
	}
	mgr, err := plugin.Discover(dir)
	m.SetPlugins(mgr, dir, err)
}

// SetPlugins installs a discovered plugin manager. dir is used only to shorten
// paths in displayed errors.
func (m *Model) SetPlugins(mgr *plugin.Manager, dir string, discoveryErr error) {
	if m.plugins.cancel != nil {
		m.plugins.cancel()
	}
	m.stopPluginEvents()
	ctx, cancel := context.WithCancel(context.Background())
	m.plugins = pluginUI{
		manager: mgr, dir: dir, discoveryErr: discoveryErr, ctx: ctx, cancel: cancel,
		annotations: m.plugins.annotations, overrides: m.plugins.overrides, counts: m.plugins.counts, countsLoaded: m.plugins.countsLoaded,
		settingsSrc: &pluginSettingsSource{},
	}
	m.plugins.settingsSrc.update(m.cfg)
	if mgr != nil {
		mgr.Settings = m.plugins.settingsSrc
	}
	m.startPluginEvents()
}

// closePlugins kills any running plugin, manual or automatic, and stops the
// event workers.
func (m Model) closePlugins() {
	if m.plugins.cancel != nil {
		m.plugins.cancel()
	}
	m.stopPluginEvents()
}

// pluginsVisible reports whether plugin entry points should appear at all.
// Without a plugins directory (the normal case) TideMail looks unchanged.
func (m Model) pluginsVisible() bool {
	return len(m.plugins.manager.Plugins()) > 0 || len(m.plugins.manager.Errors()) > 0 || m.plugins.discoveryErr != nil
}

func (m Model) pluginCommandItems(hasMessage bool) []commandItem {
	var items []commandItem
	if m.pluginsVisible() {
		items = append(items,
			commandItem{id: "plugins", label: "Plugins (experimental)", enabled: true},
			commandItem{id: "plugin-run", label: m.pluginRunLabel(), enabled: hasMessage || m.contentMessageID != 0},
		)
		if p, ok := m.plugins.manager.Plugin(m.plugins.last); ok && p.Manifest.Permissions.MessageMetadata {
			items = append(items, commandItem{id: "plugin-run-last",
				label:   "Run " + sanitizePluginLine(p.Manifest.Name) + " again" + strings.TrimPrefix(m.pluginRunLabel(), "Run plugin"),
				enabled: hasMessage || m.contentMessageID != 0})
		}
		// The explicit whole-view scope (plugin_bulk.go).
		if n := len(m.viewTargets()); n > 0 {
			items = append(items, commandItem{
				id: "plugin-reclassify-view", label: "Run plugin on all " + m.viewScopeLabel(n) + "…", enabled: true,
			})
		}
		if r := m.plugins.bulk; r != nil && !r.cancelled {
			items = append(items, commandItem{
				id: "plugin-cancel", label: fmt.Sprintf("Cancel plugin run (%d / %d)", r.done, r.total), enabled: true,
			})
		}
	}
	if msg := m.commandMessage(); msg != nil {
		items = append(items, commandItem{id: "classification-correct", label: "Edit tags / Correct classification", enabled: true})
		if len(m.plugins.overrides[msg.ID]) > 0 {
			items = append(items, commandItem{id: "classification-reset", label: "Reset all corrections", enabled: true})
		}
	}
	// Stored annotations stay inspectable even if their plugin was removed.
	if msg := m.commandMessage(); msg != nil && len(m.plugins.annotations[msg.ID]) > 0 {
		items = append(items, commandItem{id: "plugin-annotations", label: "Message annotations", enabled: true})
	}
	return items
}

func (m Model) executePluginCommand(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "plugins":
		return m.openPluginList(overlayNone)
	case "plugin-run":
		targets := m.pluginTargets()
		return m.openPluginPicker(targets, reclassifyScope{label: m.contextScope(len(targets))})
	case "plugin-run-last":
		p, ok := m.plugins.manager.Plugin(m.plugins.last)
		if !ok {
			return m, nil
		}
		targets := m.pluginTargets()
		next, cmd := m.openPluginPicker(targets, reclassifyScope{label: m.contextScope(len(targets))})
		if mm, isModel := next.(Model); isModel && mm.overlay == overlayPluginPicker {
			return mm.startPluginRun(p) // same flow and confirmation, no picker
		}
		return next, cmd
	case "plugin-reclassify-view":
		targets := m.viewTargets()
		return m.openPluginPicker(targets, reclassifyScope{label: m.viewScopeLabel(len(targets)), view: true})
	case "plugin-cancel":
		return m.cancelBulk()
	case "plugin-annotations":
		if msg := m.commandMessage(); msg != nil {
			m.plugins.annotationsFor = msg.ID
			m.plugins.scroll = 0
			m.plugins.annCursor = 0
			m.overlay = overlayPluginAnnotations
		}
		return m, nil
	case "classification-correct":
		return m.openClassificationEditor()
	case "classification-reset":
		if msg := m.commandMessage(); msg != nil {
			return m, classificationOverrideCmd(m.db, msg.ID, "", "", true)
		}
	}
	return m, nil
}

// openPluginList shows the plugin list; Esc returns to origin.
func (m Model) openPluginList(origin overlayMode) (tea.Model, tea.Cmd) {
	m.plugins.scroll = 0
	m.plugins.listCursor = 0
	m.plugins.listOrigin = origin
	m.overlay = overlayPlugins
	m.refreshPluginEventStatus()
	// Counts are refreshed each time the list opens, never while drawing.
	return m, loadPluginAnnotationCountsCmd(m.db)
}

// metadataPlugins lists the plugins allowed to receive message metadata.
func (m Model) metadataPlugins() []plugin.Plugin {
	var out []plugin.Plugin
	for _, p := range m.plugins.manager.Plugins() {
		if p.Manifest.Permissions.MessageMetadata {
			out = append(out, p)
		}
	}
	return out
}

// openPluginPicker asks once which plugin to run on targets.
func (m Model) openPluginPicker(targets []db.Message, scope reclassifyScope) (tea.Model, tea.Cmd) {
	if m.plugins.running != "" {
		m.setStatus("plugin "+m.plugins.running+" is still running", false)
		return m, m.clearStatusCmd()
	}
	if len(targets) == 0 {
		m.setStatus("no message selected", false)
		return m, m.clearStatusCmd()
	}
	eligible := m.metadataPlugins()
	if len(eligible) == 0 {
		m.setStatus("no plugins have permission to read message metadata", false)
		return m, m.clearStatusCmd()
	}
	m.plugins.picker = eligible
	m.plugins.pickerCursor = 0
	m.plugins.pickerScope = scope
	// Metadata is built once per message, the same way as a single run.
	m.plugins.pickerMetas = make([]plugin.MessageMetadata, len(targets))
	for i, t := range targets {
		m.plugins.pickerMetas[i] = m.pluginMessageMetadata(t)
	}
	m.overlay = overlayPluginPicker
	return m, nil
}

// pluginMessageMetadata copies the header-level fields of msg. Body text,
// HTML, raw headers, attachments, and the AI summary are deliberately left out.
func (m Model) pluginMessageMetadata(msg db.Message) plugin.MessageMetadata {
	meta := plugin.MessageMetadata{
		ID:            msg.ID,
		MessageID:     msg.MessageID,
		From:          msg.From,
		To:            msg.To,
		CC:            msg.CC,
		ReplyTo:       msg.ReplyTo,
		Subject:       msg.Subject,
		Read:          msg.Read,
		Starred:       msg.Starred,
		HasAttachment: msg.HasAttachment,
		Flags:         append([]string(nil), msg.Flags...),
		AccountName:   msg.AccountName,
		MailboxName:   msg.MailboxName,
	}
	if !msg.Date.IsZero() {
		meta.Date = msg.Date.Format(time.RFC3339)
	}
	// Only search results carry account and mailbox names; otherwise look
	// them up from the message's mailbox.
	if mb := m.mailboxByID(msg.MailboxID); mb != nil {
		if meta.MailboxName == "" {
			meta.MailboxName = mb.Name
		}
		if meta.AccountName == "" {
			if acc := m.accountByID(mb.AccountID); acc != nil {
				meta.AccountName = acc.Name
			}
		}
	}
	return meta
}

// runPluginCmd runs the plugin off the Update loop. It captures only the
// manager, database, context, and a copy of the metadata, never the Model.
// Annotation storage happens here too, so Update never waits on SQLite.
func runPluginCmd(ctx context.Context, mgr *plugin.Manager, database *db.DB, pluginID string, meta plugin.MessageMetadata, source *db.Message) tea.Cmd {
	var sourceCopy *db.Message
	if source != nil {
		copy := *source
		sourceCopy = &copy
	}
	return func() tea.Msg {
		start := time.Now()
		var store plugin.AnnotationStore
		if database != nil {
			store = dbAnnotationStore{database}
		}
		// The set before the run, to tell whether Reclassify changed it.
		var before []db.PluginAnnotation
		beforeOK := false
		if database != nil {
			var beforeErr error
			before, beforeErr = database.ListPluginAnnotations(meta.ID)
			beforeOK = beforeErr == nil
		}
		result, err := mgr.MessageMetadata(ctx, pluginID, meta, store)
		msg := pluginResultMsg{
			PluginID:  pluginID,
			MessageID: meta.ID,
			Subject:   meta.Subject,
			Result:    result,
			Err:       err,
			Elapsed:   time.Since(start),
			Source:    sourceCopy,
		}
		switch {
		case err != nil:
		case result.Outcome == plugin.AnnotationsStored:
			// Reload only this message's annotations for the cache.
			if anns, loadErr := database.ListPluginAnnotations(meta.ID); loadErr == nil {
				msg.Refreshed, msg.RefreshedOK = anns, true
				msg.Compared = beforeOK
				msg.Changed = annotationSetChanged(before, anns, pluginID)
			}
		case result.Outcome == plugin.AnnotationsNotPermitted:
			// Nothing could be stored, so nothing changed.
			msg.Compared = true
		}
		return msg
	}
}

func (m Model) handlePluginResult(msg pluginResultMsg) (tea.Model, tea.Cmd) {
	if m.plugins.running == msg.PluginID {
		m.plugins.running = ""
	}
	source := msg.Source
	if source == nil {
		source = m.pluginResultMessage(msg.MessageID)
	}
	if msg.Err != nil {
		// A failed run stored nothing, so the cached badges stay as they are.
		name := m.pluginDisplayName(msg.PluginID)
		card := finishMessageCard(errorCard(name, msg.Err), source)
		m.plugins.result = &pluginResult{pluginID: msg.PluginID, pluginName: name,
			subject: sanitizePluginLine(msg.Subject), card: &card, details: m.messageDetails(msg), message: source}
		if m.overlay == overlayNone || m.overlay == overlayPlugins {
			m.plugins.scroll = 0
			m.overlay = overlayPluginResult
			m.setStatus(card.title, true)
		} else {
			m.setStatus(card.title+"; open Plugins to view the result", true)
		}
		return m, m.clearStatusCmd()
	}
	if msg.RefreshedOK {
		if m.plugins.annotations == nil {
			m.plugins.annotations = map[int64][]db.PluginAnnotation{}
		}
		if len(msg.Refreshed) == 0 {
			delete(m.plugins.annotations, msg.MessageID)
		} else {
			m.plugins.annotations[msg.MessageID] = msg.Refreshed
		}
	}
	name := msg.PluginID
	if p, ok := m.plugins.manager.Plugin(msg.PluginID); ok {
		name = p.Manifest.Name
	}
	note := annotationNote(msg.Result)
	if change := changeNote(msg); change != "" {
		note = strings.TrimPrefix(note+"; "+change, "; ")
	}
	card := messageCard(sanitizePluginLine(name), msg, source)
	m.plugins.result = &pluginResult{
		pluginID:       msg.PluginID,
		pluginName:     sanitizePluginLine(name),
		subject:        sanitizePluginLine(msg.Subject),
		body:           formatPluginData(msg.Result.Response.Data),
		annotationNote: note,
		card:           &card,
		details:        m.messageDetails(msg),
		message:        source,
	}
	elapsed := msg.Elapsed.Round(time.Millisecond)
	status := fmt.Sprintf("plugin %s completed (%v)", msg.PluginID, elapsed)
	if change := changeNote(msg); change != "" {
		status += "; " + change
	}
	isErr := msg.Result.Outcome == plugin.AnnotationsRejected || msg.Result.Outcome == plugin.AnnotationsNotStored
	if isErr {
		status += "; " + note
	}
	// Don't take over the screen if the user moved on to another overlay
	// (compose, settings) while the plugin ran.
	if m.overlay == overlayNone || m.overlay == overlayPlugins {
		m.plugins.scroll = 0
		m.overlay = overlayPluginResult
	} else {
		status += "; open Plugins to view the result"
	}
	m.setStatus(status, isErr)
	if msg.Result.Outcome == plugin.AnnotationsStored {
		return m, tea.Batch(m.clearStatusCmd(), loadPluginAnnotationCountsCmd(m.db), m.needsYouRefreshCmd())
	}
	return m, m.clearStatusCmd()
}

// changeNote says whether a run changed the plugin's classification of the
// message, when that is known.
func changeNote(msg pluginResultMsg) string {
	switch {
	case !msg.Compared:
		return ""
	case msg.Changed:
		return "classification changed"
	}
	return "classification unchanged"
}

func (m Model) handlePluginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayPluginPicker:
		switch {
		case keyMatches(msg, m.keys.Cancel, m.keys.Back):
			m.plugins.picker = nil
			m.plugins.pickerMetas = nil
			m.plugins.pickerScope = reclassifyScope{}
			m.overlay = overlayNone
		case keyMatches(msg, m.keys.Up):
			if m.plugins.pickerCursor > 0 {
				m.plugins.pickerCursor--
			}
		case keyMatches(msg, m.keys.Down):
			if m.plugins.pickerCursor < len(m.plugins.picker)-1 {
				m.plugins.pickerCursor++
			}
		case keyMatches(msg, m.keys.Confirm):
			if len(m.plugins.picker) == 0 {
				return m, nil
			}
			return m.startPluginRun(m.plugins.picker[m.plugins.pickerCursor])
		}
		return m, nil
	case overlayPlugins:
		return m.handlePluginListKey(msg)
	case overlayPluginAnnotations:
		return m.handleAnnotationsKey(msg)
	case overlayPluginConfirm:
		return m.handlePluginConfirm(msg)
	case overlayPluginSettings:
		return m.handlePluginSettingsKey(msg)
	case overlayPluginResult:
		switch {
		case msg.String() == "u" && m.plugins.result != nil && m.plugins.result.message != nil &&
			messageUnsubscribeTarget(*m.plugins.result.message) != "":
			return m.handleUnsubscribe(*m.plugins.result.message)
		case keyMatches(msg, m.keys.Cancel, m.keys.Back, m.keys.Confirm):
			m.overlay = m.plugins.resultOrigin
			m.plugins.resultOrigin = overlayNone
			m.plugins.scroll = 0
		case msg.String() == "d" && m.plugins.result != nil && len(m.plugins.result.details) > 0:
			m.plugins.result.showDetails = !m.plugins.result.showDetails
			m.plugins.scroll = 0
		case keyMatches(msg, m.keys.Up):
			m.plugins.scroll = max(0, m.plugins.scroll-1)
		case keyMatches(msg, m.keys.Down):
			m.plugins.scroll = min(m.plugins.scroll+1, m.pluginMaxScroll())
		}
		return m, nil
	}
	return m, nil
}

// ── Rendering ────────────────────────────────────────────────────────────────

// pluginOverlaySize returns the panel size for the current plugin overlay.
func (m Model) pluginOverlaySize() (int, int) {
	switch m.overlay {
	case overlayPluginPicker:
		return max(1, min(m.width-4, 64)), max(1, min(m.height-4, 20))
	case overlayPluginResult:
		if r := m.plugins.result; r != nil && r.view != nil {
			// Dashboards get the room they need.
			return max(1, min(m.width-4, 110)), max(1, m.height-4)
		}
		return max(1, min(m.width-4, 84)), max(1, min(m.height-4, 32))
	case overlayPluginAnnotations:
		return max(1, min(m.width-4, 80)), max(1, min(m.height-4, 28))
	default:
		return max(1, min(m.width-4, 74)), max(1, min(m.height-4, 32))
	}
}

func (m Model) renderPluginOverlay() string {
	winW, winH := m.pluginOverlaySize()
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	var inner, title string
	switch m.overlay {
	case overlayPluginPicker:
		inner, title = m.renderPluginPicker(winW, winH, chrome), "run plugin"
	case overlayPluginResult:
		var pairs []string
		if r := m.plugins.result; r != nil && r.card != nil && r.card.canUnsubscribe {
			pairs = append(pairs, "u", "unsubscribe")
		}
		pairs = append(pairs, "enter", "done")
		if r := m.plugins.result; r != nil && len(r.details) > 0 {
			if r.showDetails {
				pairs = append(pairs, "d", "summary")
			} else {
				pairs = append(pairs, "d", "details")
			}
		}
		pairs = append(pairs, "↑↓", "scroll", "esc", "close")
		inner = m.renderPluginScroll(m.pluginResultLines(winW-4, chrome), winW, winH, chrome, pairs...)
		title = "plugin result"
		if r := m.plugins.result; r != nil && r.card != nil {
			title = r.pluginName
		}
	case overlayPluginConfirm:
		return m.renderPluginConfirm()
	case overlayPluginSettings:
		return m.renderPluginSettings()
	case overlayPluginAnnotations:
		lines, _ := m.annotationLines(winW-4, chrome)
		pairs := []string{"esc", "close"}
		if len(m.annotationGroups()) > 0 {
			pairs = []string{"↑↓", "select", "e", "correct", "c", "clear plugin", "C", "clear all", "esc", "close"}
		} else if len(m.plugins.overrides[m.plugins.annotationsFor]) > 0 {
			pairs = []string{"e", "correct", "esc", "close"}
		}
		inner = m.renderPluginScroll(lines, winW, winH, chrome, pairs...)
		title = "message annotations"
	default:
		lines, _ := m.pluginListLines(winW-4, chrome)
		pairs := m.pluginListHintPairs()
		pairs = append(pairs, "esc", "close")
		inner = m.renderPluginScroll(lines, winW, winH, chrome, pairs...)
		title = "plugins"
	}
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", title, chrome)
}

func (m Model) pluginListHintPairs() []string {
	pairs := []string{"↑↓", "select"}
	resumable := false
	if entries := m.pluginListEntries(); len(entries) > 0 {
		e := entries[clamp(m.plugins.listCursor, 0, len(entries)-1)]
		eventHints := m.pluginEventHints(e)
		pairs = append(pairs, eventHints...)
		if e.installed && m.pluginHasReport(e.pluginID) {
			pairs = append(pairs, "enter", "run report")
		}
		if e.installed && m.hasPluginSettingsPage(e.pluginID) {
			pairs = append(pairs, "s", "settings")
		}
		resumable = len(eventHints) > 2 // includes "r resume"
		// Only offer clearing when the selected plugin has stored data.
		if e.count > 0 {
			pairs = append(pairs, "c", "clear stored")
		}
	}
	// r resumes a paused plugin; otherwise it reopens the last result.
	if m.plugins.result != nil && !resumable {
		pairs = append(pairs, "r", "last result")
	}

	return pairs
}

// pluginResultMessage snapshots the exact cached message a manual plugin run
// described. The snapshot keeps a later card action tied to that message even
// if the list selection moves.
func (m Model) pluginResultMessage(messageID int64) *db.Message {
	if messageID <= 0 {
		return nil
	}
	if msg := m.commandMessage(); msg != nil && msg.ID == messageID {
		copy := *msg
		return &copy
	}
	for i := range m.filteredMessages {
		if m.filteredMessages[i].ID == messageID {
			copy := m.filteredMessages[i]
			return &copy
		}
	}
	for i := range m.messages {
		if m.messages[i].ID == messageID {
			copy := m.messages[i]
			return &copy
		}
	}
	return nil
}

// pluginMaxScroll is the largest useful scroll offset for the list or result
// overlay at the current size.
func (m Model) pluginMaxScroll() int {
	winW, winH := m.pluginOverlaySize()
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	var lines []string
	switch m.overlay {
	case overlayPluginResult:
		lines = m.pluginResultLines(winW-4, chrome)
	case overlayPluginAnnotations:
		lines, _ = m.annotationLines(winW-4, chrome)
	default:
		lines, _ = m.pluginListLines(winW-4, chrome)
	}
	return max(0, len(lines)-pluginScrollHeight(winH))
}

func pluginScrollHeight(height int) int { return max(1, height-3) }

func (m Model) renderPluginPicker(width, height int, chrome managerChrome) string {
	bodyW := max(1, width-4)
	muted := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	header := []string{
		muted.Render(truncate(m.pickerTargetLine(), bodyW)),
		muted.Render(truncate("sends sender, recipients, subject, date, flags; never the body", bodyW)),
		"",
	}
	var rows []string
	for _, line := range header {
		rows = append(rows, padStyled(lipgloss.NewStyle().Background(chrome.baseBg).Padding(0, 2).Render(line), width, chrome.baseBg))
	}
	labelW := max(1, width-2)
	listH := max(1, height-len(header)-2)
	start := max(0, m.plugins.pickerCursor-listH+1)
	end := min(start+listH, len(m.plugins.picker))
	nameW := 0
	for _, p := range m.plugins.picker {
		nameW = max(nameW, lipgloss.Width(sanitizePluginLine(p.Manifest.Name)))
	}
	nameW = min(nameW, max(8, labelW/2))
	for i := start; i < end; i++ {
		p := m.plugins.picker[i]
		name := padRight(truncate(sanitizePluginLine(p.Manifest.Name), nameW), nameW)
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + name)
		if desc := sanitizePluginLine(p.Manifest.Description); desc != "" {
			cell += lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted).Render("  " + truncate(desc, max(1, labelW-nameW-3)))
		}
		rows = append(rows, softRail(chrome, i == m.plugins.pickerCursor, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	for len(rows) < height-1 {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	}
	hints := renderSoftHints(width, chrome, "enter", "run", "↑↓", "choose", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinVertical(lipgloss.Left, rows...), hints)
}

// pluginListLines renders the plugin list as styled lines wrapped to width.
// pluginListLines renders the plugin list, and returns the line index where
// each pluginListEntries entry starts (for the cursor and scrolling). Every
// line starts with a 2-cell rail column that marks the selected entry.
func (m Model) pluginListLines(width int, chrome managerChrome) ([]string, []int) {
	width = max(1, width-2) // minus the rail
	plain := m.styles.PlainUI
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	okStyle := base.Foreground(chrome.accent)
	errStyle := base.Foreground(chrome.errorFg)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)
	indentW := max(1, width-2)
	blankRail := softRail(chrome, false, chrome.baseBg)

	var lines []string
	add := func(style lipgloss.Style, s string, indent bool, rail string) {
		prefix := ""
		w := width
		if indent {
			prefix = "  "
			w = indentW
		}
		for i, part := range strings.Split(ansi.Wrap(s, max(1, w), ""), "\n") {
			r := blankRail
			if i == 0 && rail != "" {
				r = rail
			}
			lines = append(lines, r+style.Render(prefix+part))
		}
	}
	blank := func() { lines = append(lines, "") }

	mgr := m.plugins.manager
	if m.plugins.discoveryErr != nil {
		add(errStyle, aiConnectionStatusGlyph(plain, aiConnectionError)+" plugins directory", false, "")
		add(muted, m.displayPluginError(m.plugins.discoveryErr), true, "")
		blank()
	}

	entries := m.pluginListEntries()
	starts := make([]int, len(entries))
	railFor := func(i int) string { return softRail(chrome, i == m.plugins.listCursor, chrome.baseBg) }
	countLine := func(n int64) {
		if m.plugins.countsLoaded {
			add(muted, fmt.Sprintf("Stored tags  %d", n), true, "")
		}
	}
	i := 0
	for _, p := range mgr.Plugins() {
		man := p.Manifest
		starts[i] = len(lines)
		add(okStyle, aiConnectionStatusGlyph(plain, aiConnectionSuccess)+" "+sanitizePluginLine(man.Name), false, railFor(i))
		if desc := sanitizePluginLine(man.Description); desc != "" {
			add(text, desc, true, "")
		}
		if version := sanitizePluginLine(man.Version); version != "" {
			add(muted, "Version "+version, true, "")
		}
		add(muted, "Permissions", true, "")
		for _, item := range permissionChecklist(man.Permissions, plain) {
			add(muted, "  "+item, true, "")
		}
		for _, l := range m.pluginEventLines(man.ID) {
			add(muted, l, true, "")
		}
		countLine(entries[i].count)
		blank()
		i++
	}
	if i < len(entries) {
		add(text.Bold(true), "Stored data from removed plugins", false, "")
		blank()
		for ; i < len(entries); i++ {
			starts[i] = len(lines)
			add(muted, aiConnectionStatusGlyph(plain, aiConnectionPending)+" "+sanitizePluginLine(entries[i].pluginID), false, railFor(i))
			countLine(entries[i].count)
			blank()
		}
	}
	for _, e := range mgr.Errors() {
		add(errStyle, aiConnectionStatusGlyph(plain, aiConnectionError)+" "+sanitizePluginLine(filepath.Base(e.Dir)), false, "")
		for _, l := range m.displayPluginErrorLines(e.Err) {
			add(muted, l, true, "")
		}
		blank()
	}
	if len(lines) == 0 {
		add(muted, "no plugins installed", false, "")
	}
	if m.plugins.running != "" {
		add(muted, m.bulkProgressLine(), false, "")
	}
	return lines, starts
}

func (m Model) pluginResultLines(bodyW int, chrome managerChrome) []string {
	bodyW = max(1, bodyW)
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	res := m.plugins.result
	if res == nil {
		return []string{base.Foreground(chrome.muted).Render("no result")}
	}
	if res.showDetails && len(res.details) > 0 {
		var lines []string
		for _, l := range res.details {
			// sanitizePluginText keeps indentation; fields were sanitized as the
			// details were built.
			for _, part := range strings.Split(ansi.Wrap(strings.ReplaceAll(sanitizePluginText(l), "\n", " "), bodyW, ""), "\n") {
				lines = append(lines, base.Foreground(chrome.text).Render(part))
			}
		}
		return lines
	}
	if res.card != nil {
		return m.cardLines(*res.card, bodyW, chrome)
	}
	var lines []string
	lines = append(lines,
		base.Foreground(chrome.accent).Bold(true).Render(truncate(res.pluginName+"  ("+res.pluginID+")", bodyW)),
	)
	heading := res.heading
	if heading == "" {
		heading = "message: " + res.subject
	}
	lines = append(lines, base.Foreground(chrome.muted).Render(truncate(heading, bodyW)))
	if res.annotationNote != "" {
		for _, part := range strings.Split(ansi.Wrap(res.annotationNote, bodyW, ""), "\n") {
			lines = append(lines, base.Foreground(chrome.muted).Render(part))
		}
	}
	lines = append(lines, "")
	if res.view != nil {
		return append(lines, m.pluginViewLines(res.view, bodyW, chrome)...)
	}
	body := res.body
	if body == "" {
		body = "plugin completed with no data"
	}
	textStyle := base.Foreground(chrome.text)
	for _, line := range strings.Split(body, "\n") {
		for _, part := range strings.Split(ansi.Wrap(line, bodyW, ""), "\n") {
			lines = append(lines, textStyle.Render(part))
		}
	}
	return lines
}

// renderPluginScroll shows lines from the current scroll offset above a hints
// row.
func (m Model) renderPluginScroll(lines []string, width, height int, chrome managerChrome, hintPairs ...string) string {
	listH := pluginScrollHeight(height)
	scroll := clamp(m.plugins.scroll, 0, max(0, len(lines)-listH))
	visible := lines[scroll:min(len(lines), scroll+listH)]
	rows := make([]string, 0, listH+1)
	rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	for _, line := range visible {
		rows = append(rows, padStyled(lipgloss.NewStyle().Background(chrome.baseBg).Padding(0, 2).Render(line), width, chrome.baseBg))
	}
	for len(rows) < listH+1 {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	}
	hints := renderSoftHints(width, chrome, hintPairs...)
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinVertical(lipgloss.Left, rows...), hints)
}

// ── Sanitizing untrusted plugin text ─────────────────────────────────────────

// displayPluginError shortens paths under the plugin directory and strips
// control characters. Discovery errors can quote absolute paths.
func (m Model) displayPluginError(err error) string {
	s := err.Error()
	if m.plugins.dir != "" {
		s = strings.ReplaceAll(s, m.plugins.dir+string(filepath.Separator), "plugins/")
		s = strings.ReplaceAll(s, m.plugins.dir, "plugins")
	}
	return sanitizePluginLine(s)
}

// displayPluginErrorLines is displayPluginError laid out for the plugin list:
// one line per error, and unknown manifest keys listed one per line with a
// hint, instead of one long comma-separated sentence.
func (m Model) displayPluginErrorLines(err error) []string {
	s := err.Error()
	if m.plugins.dir != "" {
		s = strings.ReplaceAll(s, m.plugins.dir+string(filepath.Separator), "plugins/")
		s = strings.ReplaceAll(s, m.plugins.dir, "plugins")
	}
	var out []string
	for _, part := range strings.Split(sanitizePluginText(s), "\n") {
		part = strings.Join(strings.Fields(part), " ")
		if part == "" {
			continue
		}
		head, keys, ok := strings.Cut(part, "unknown keys: ")
		if !ok {
			out = append(out, part)
			continue
		}
		out = append(out, head+"unknown keys:")
		for _, k := range strings.Split(keys, ", ") {
			out = append(out, "  • "+k)
		}
		out = append(out, "this TideMail may be older than the plugin; update TideMail or the plugin")
	}
	return out
}

// sanitizePluginText makes plugin-supplied text safe to draw: no control or
// format characters (escape sequences, bidi overrides, zero-width runes), and
// hard line breaks normalized. Newlines and tabs survive as layout.
func sanitizePluginText(s string) string {
	s = strings.ToValidUTF8(normalizeHardBreaks(s), "?")
	return strings.Map(func(r rune) rune {
		switch {
		case r == '\n':
			return r
		case r == '\t':
			return ' '
		case unicode.Is(unicode.Cc, r), unicode.Is(unicode.Cf, r):
			return -1
		}
		return r
	}, s)
}

// sanitizePluginLine is sanitizePluginText flattened to one line.
func sanitizePluginLine(s string) string {
	return strings.Join(strings.Fields(sanitizePluginText(s)), " ")
}

// formatPluginData turns response JSON into a readable key/value tree. The
// data is informational only; formatting it here keeps plugin output from
// looking like source code in the result overlay.
func formatPluginData(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	text := string(raw)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		var lines []string
		appendPluginValue(&lines, v, 0, "")
		text = strings.Join(lines, "\n")
	}
	text = sanitizePluginText(text)

	truncated := false
	if len(text) > pluginResultMaxBytes {
		text = strings.ToValidUTF8(text[:pluginResultMaxBytes], "")
		truncated = true
	}
	if lines := strings.Split(text, "\n"); len(lines) > pluginResultMaxLines {
		text = strings.Join(lines[:pluginResultMaxLines], "\n")
		truncated = true
	}
	if truncated {
		text += "\n… truncated"
	}
	return text
}

func appendPluginValue(lines *[]string, value any, depth int, label string) {
	indent := strings.Repeat("  ", depth)
	switch v := value.(type) {
	case map[string]any:
		if label != "" {
			*lines = append(*lines, indent+humanPluginLabel(label)+":")
			depth++
			indent = strings.Repeat("  ", depth)
		}
		keys := make([]string, 0, len(v))
		for key := range v {
			keys = append(keys, key)
		}
		sort.Strings(keys)
		if len(keys) == 0 {
			if label == "" {
				*lines = append(*lines, indent+"None")
			} else {
				(*lines)[len(*lines)-1] += " None"
			}
			return
		}
		for _, key := range keys {
			appendPluginValue(lines, v[key], depth, key)
		}
	case []any:
		if label != "" {
			*lines = append(*lines, indent+humanPluginLabel(label)+":")
			depth++
			indent = strings.Repeat("  ", depth)
		}
		if len(v) == 0 {
			*lines = append(*lines, indent+"None")
			return
		}
		for _, item := range v {
			if isPluginComposite(item) {
				*lines = append(*lines, indent+"-")
				appendPluginValue(lines, item, depth+1, "")
			} else {
				*lines = append(*lines, indent+"- "+pluginScalar(item))
			}
		}
	default:
		if label == "" {
			*lines = append(*lines, indent+pluginScalar(v))
		} else {
			*lines = append(*lines, indent+humanPluginLabel(label)+": "+pluginScalar(v))
		}
	}
}

func isPluginComposite(value any) bool {
	switch value.(type) {
	case map[string]any, []any:
		return true
	default:
		return false
	}
}

func pluginScalar(value any) string {
	switch v := value.(type) {
	case nil:
		return "none"
	case string:
		return v
	case bool:
		if v {
			return "yes"
		}
		return "no"
	default:
		return fmt.Sprint(v)
	}
}

func humanPluginLabel(value string) string {
	words := strings.Fields(strings.NewReplacer("_", " ", "-", " ").Replace(value))
	for i, word := range words {
		if word == "" {
			continue
		}
		words[i] = strings.ToUpper(word[:1]) + word[1:]
	}
	return strings.Join(words, " ")
}

// pickerTargetLine says what the picked plugin will run on.
func (m Model) pickerTargetLine() string {
	metas := m.plugins.pickerMetas
	if len(metas) == 1 && !m.plugins.pickerScope.view {
		return "Run plugin on message: " + sanitizePluginLine(metas[0].Subject)
	}
	if scope := m.plugins.pickerScope.label; scope != "" {
		return "Run plugin on " + scope
	}
	if len(metas) > 0 {
		return fmt.Sprintf("Run plugin on %d messages", len(metas))
	}
	return ""
}

// permissionChecklist spells out a plugin's permissions in plain words, one
// per line: the four basics always, read-only query permissions when granted.
func permissionChecklist(p plugin.Permissions, plain bool) []string {
	yes, no := "✓ ", "✗ "
	if plain {
		yes, no = "+ ", "- "
	}
	mark := func(on bool, label string) string {
		if on {
			return yes + label
		}
		return no + label
	}
	items := []string{
		mark(p.MessageMetadata, "Message details (sender, subject, date)"),
		mark(p.Annotations, "Save tags"),
		mark(p.MessageBody, "Message body"),
		mark(p.Network, "Network"),
	}
	for _, q := range []struct {
		on    bool
		label string
	}{{p.MessagesQuery, "Read message lists"}, {p.ThreadsQuery, "Read conversations"},
		{p.AnnotationsQuery, "Read stored tags"}, {p.AnalyticsRead, "Read statistics"}} {
		if q.on {
			items = append(items, yes+q.label)
		}
	}
	return items
}
