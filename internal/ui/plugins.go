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
	"regexp"
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

	picker       []plugin.Plugin
	pickerCursor int
	pickerMeta   plugin.MessageMetadata

	result *pluginResult
	// scroll is the first visible line of the plugin list or result overlay.
	scroll int

	// annotations caches stored plugin annotations for the loaded messages,
	// keyed by message ID. It is filled by the message load commands and by
	// plugin runs, so rendering never touches the database.
	annotations map[int64][]db.PluginAnnotation
	// annotationsFor is the message shown in the annotations overlay.
	annotationsFor int64
}

type pluginResult struct {
	pluginID   string
	pluginName string
	subject    string
	body       string
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
	ctx, cancel := context.WithCancel(context.Background())
	m.plugins = pluginUI{
		manager: mgr, dir: dir, discoveryErr: discoveryErr, ctx: ctx, cancel: cancel,
		annotations: m.plugins.annotations,
	}
}

// closePlugins kills any running plugin.
func (m Model) closePlugins() {
	if m.plugins.cancel != nil {
		m.plugins.cancel()
	}
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
			commandItem{id: "plugin-run", label: "Run plugin on current message", enabled: hasMessage || m.contentMessageID != 0},
		)
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
		m.plugins.scroll = 0
		m.overlay = overlayPlugins
		return m, nil
	case "plugin-run":
		return m.openPluginPicker()
	case "plugin-annotations":
		if msg := m.commandMessage(); msg != nil {
			m.plugins.annotationsFor = msg.ID
			m.plugins.scroll = 0
			m.overlay = overlayPluginAnnotations
		}
		return m, nil
	}
	return m, nil
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

func (m Model) openPluginPicker() (tea.Model, tea.Cmd) {
	if m.plugins.running != "" {
		m.setStatus("plugin "+m.plugins.running+" is still running", false)
		return m, m.clearStatusCmd()
	}
	msg := m.commandMessage()
	if msg == nil {
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
	m.plugins.pickerMeta = m.pluginMessageMetadata(*msg)
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
func runPluginCmd(ctx context.Context, mgr *plugin.Manager, database *db.DB, pluginID string, meta plugin.MessageMetadata) tea.Cmd {
	return func() tea.Msg {
		start := time.Now()
		var store plugin.AnnotationStore
		if database != nil {
			store = dbAnnotationStore{database}
		}
		result, err := mgr.MessageMetadata(ctx, pluginID, meta, store)
		msg := pluginResultMsg{
			PluginID:  pluginID,
			MessageID: meta.ID,
			Subject:   meta.Subject,
			Result:    result,
			Err:       err,
			Elapsed:   time.Since(start),
		}
		if err == nil && result.Outcome == plugin.AnnotationsStored {
			// Reload only this message's annotations for the cache.
			if anns, loadErr := database.ListPluginAnnotations(meta.ID); loadErr == nil {
				msg.Refreshed, msg.RefreshedOK = anns, true
			}
		}
		return msg
	}
}

// dbAnnotationStore adapts the database to plugin.AnnotationStore. It is only
// ever handed to Manager.MessageMetadata, which checks permissions and
// validates the set before calling it.
type dbAnnotationStore struct{ db *db.DB }

func (s dbAnnotationStore) ReplaceAnnotations(pluginID string, messageID int64, anns []plugin.Annotation) error {
	rows := make([]db.PluginAnnotation, len(anns))
	for i, a := range anns {
		rows[i] = db.PluginAnnotation{Key: a.Key, Value: a.Value, Confidence: a.Confidence}
	}
	return s.db.ReplacePluginAnnotations(pluginID, messageID, rows)
}

// loadMessageAnnotations batch-loads annotations for a message list inside a
// load command. Failure only costs the badges, never the message list.
func loadMessageAnnotations(database *db.DB, msgs []db.Message) map[int64][]db.PluginAnnotation {
	if database == nil || len(msgs) == 0 {
		return nil
	}
	ids := make([]int64, len(msgs))
	for i, msg := range msgs {
		ids[i] = msg.ID
	}
	anns, err := database.ListPluginAnnotationsForMessages(ids)
	if err != nil {
		return nil
	}
	return anns
}

// annotationNote describes what happened to a run's annotations.
func annotationNote(r plugin.MessageMetadataResult) string {
	n := len(r.Annotations)
	switch r.Outcome {
	case plugin.AnnotationsStored:
		if n == 0 {
			return "annotations: none returned; this plugin's previous annotations were cleared"
		}
		return fmt.Sprintf("annotations: %d stored", n)
	case plugin.AnnotationsNotPermitted:
		if n == 0 {
			return ""
		}
		return fmt.Sprintf("annotations: %d ignored; this plugin lacks the annotations permission", n)
	case plugin.AnnotationsRejected:
		return "annotations rejected, nothing changed: " + sanitizePluginLine(r.AnnotationErr.Error())
	case plugin.AnnotationsNotStored:
		return "annotations not saved, nothing changed: " + sanitizePluginLine(r.AnnotationErr.Error())
	}
	return ""
}

func (m Model) handlePluginResult(msg pluginResultMsg) (tea.Model, tea.Cmd) {
	if m.plugins.running == msg.PluginID {
		m.plugins.running = ""
	}
	if msg.Err != nil {
		// A failed run stored nothing, so the cached badges stay as they are.
		m.setStatus(sanitizePluginLine(msg.Err.Error()), true)
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
	m.plugins.result = &pluginResult{
		pluginID:       msg.PluginID,
		pluginName:     sanitizePluginLine(name),
		subject:        sanitizePluginLine(msg.Subject),
		body:           formatPluginData(msg.Result.Response.Data),
		annotationNote: note,
	}
	elapsed := msg.Elapsed.Round(time.Millisecond)
	status := fmt.Sprintf("plugin %s completed (%v)", msg.PluginID, elapsed)
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
	return m, m.clearStatusCmd()
}

func (m Model) handlePluginKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	switch m.overlay {
	case overlayPluginPicker:
		switch {
		case keyMatches(msg, m.keys.Cancel, m.keys.Back):
			m.plugins.picker = nil
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
			p := m.plugins.picker[m.plugins.pickerCursor]
			meta := m.plugins.pickerMeta
			m.plugins.picker = nil
			m.overlay = overlayNone
			m.plugins.running = p.Manifest.ID
			m.setStatus("running plugin "+p.Manifest.ID+"…", false)
			return m, runPluginCmd(m.plugins.ctx, m.plugins.manager, m.db, p.Manifest.ID, meta)
		}
		return m, nil
	case overlayPlugins, overlayPluginResult, overlayPluginAnnotations:
		switch {
		case keyMatches(msg, m.keys.Cancel, m.keys.Back):
			m.overlay = overlayNone
			m.plugins.scroll = 0
		case keyMatches(msg, m.keys.Up):
			m.plugins.scroll = max(0, m.plugins.scroll-1)
		case keyMatches(msg, m.keys.Down):
			m.plugins.scroll = min(m.plugins.scroll+1, m.pluginMaxScroll())
		case m.overlay == overlayPlugins && msg.String() == "r" && m.plugins.result != nil:
			m.plugins.scroll = 0
			m.overlay = overlayPluginResult
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
		inner = m.renderPluginScroll(m.pluginResultLines(winW-4, chrome), winW, winH, chrome, "↑↓", "scroll", "esc", "close")
		title = "plugin result"
	case overlayPluginAnnotations:
		inner = m.renderPluginScroll(m.annotationLines(winW-4, chrome), winW, winH, chrome, "↑↓", "scroll", "esc", "close")
		title = "message annotations"
	default:
		pairs := []string{"↑↓", "scroll", "esc", "close"}
		if m.plugins.result != nil {
			pairs = append([]string{"r", "last result"}, pairs...)
		}
		inner = m.renderPluginScroll(m.pluginListLines(winW-4, chrome), winW, winH, chrome, pairs...)
		title = "plugins"
	}
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", title, chrome)
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
		lines = m.annotationLines(winW-4, chrome)
	default:
		lines = m.pluginListLines(winW-4, chrome)
	}
	return max(0, len(lines)-pluginScrollHeight(winH))
}

func pluginScrollHeight(height int) int { return max(1, height-3) }

func (m Model) renderPluginPicker(width, height int, chrome managerChrome) string {
	bodyW := max(1, width-4)
	muted := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.muted)
	header := []string{
		muted.Render(truncate("message: "+sanitizePluginLine(m.plugins.pickerMeta.Subject), bodyW)),
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
	for i := start; i < end; i++ {
		p := m.plugins.picker[i]
		label := sanitizePluginLine(p.Manifest.Name) + "  (" + p.Manifest.ID + ")"
		cell := lipgloss.NewStyle().Background(chrome.baseBg).Foreground(chrome.text).Render(" " + truncate(label, max(1, labelW-1)))
		rows = append(rows, softRail(chrome, i == m.plugins.pickerCursor, chrome.baseBg)+padStyled(cell, labelW, chrome.baseBg))
	}
	for len(rows) < height-1 {
		rows = append(rows, lipgloss.NewStyle().Background(chrome.baseBg).Width(width).Render(""))
	}
	hints := renderSoftHints(width, chrome, "enter", "run", "↑↓", "choose", "esc", "cancel")
	return lipgloss.JoinVertical(lipgloss.Left, lipgloss.JoinVertical(lipgloss.Left, rows...), hints)
}

// pluginListLines renders the plugin list as styled lines wrapped to width.
func (m Model) pluginListLines(width int, chrome managerChrome) []string {
	width = max(1, width)
	plain := m.styles.PlainUI
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	okStyle := base.Foreground(chrome.accent)
	errStyle := base.Foreground(chrome.errorFg)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)
	indentW := max(1, width-2)

	var lines []string
	add := func(style lipgloss.Style, s string, indent bool) {
		prefix := ""
		w := width
		if indent {
			prefix = "  "
			w = indentW
		}
		for _, part := range strings.Split(ansi.Wrap(s, max(1, w), ""), "\n") {
			lines = append(lines, style.Render(prefix+part))
		}
	}

	mgr := m.plugins.manager
	if m.plugins.discoveryErr != nil {
		add(errStyle, aiConnectionStatusGlyph(plain, aiConnectionError)+" plugins directory", false)
		add(muted, m.displayPluginError(m.plugins.discoveryErr), true)
		lines = append(lines, "")
	}
	for _, p := range mgr.Plugins() {
		man := p.Manifest
		add(okStyle, aiConnectionStatusGlyph(plain, aiConnectionSuccess)+" "+man.ID, false)
		add(text, sanitizePluginLine(man.Name), true)
		version := sanitizePluginLine(man.Version)
		if version == "" {
			version = "?"
		}
		add(muted, fmt.Sprintf("v%s%sAPI %d", version, m.styles.InlineMidDot(), man.API), true)
		perms := strings.Join(man.Permissions.Names(), ", ")
		if perms == "" {
			perms = "none"
		}
		add(muted, "permissions: "+perms, true)
		lines = append(lines, "")
	}
	for _, e := range mgr.Errors() {
		add(errStyle, aiConnectionStatusGlyph(plain, aiConnectionError)+" "+sanitizePluginLine(filepath.Base(e.Dir)), false)
		add(muted, m.displayPluginError(e.Err), true)
		lines = append(lines, "")
	}
	if len(lines) == 0 {
		add(muted, "no plugins installed", false)
	}
	if m.plugins.running != "" {
		add(muted, "running: "+m.plugins.running, false)
	}
	return lines
}

func (m Model) pluginResultLines(bodyW int, chrome managerChrome) []string {
	bodyW = max(1, bodyW)
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	res := m.plugins.result
	if res == nil {
		return []string{base.Foreground(chrome.muted).Render("no result")}
	}
	var lines []string
	lines = append(lines,
		base.Foreground(chrome.accent).Bold(true).Render(truncate(res.pluginName+"  ("+res.pluginID+")", bodyW)),
		base.Foreground(chrome.muted).Render(truncate("message: "+res.subject, bodyW)),
	)
	if res.annotationNote != "" {
		for _, part := range strings.Split(ansi.Wrap(res.annotationNote, bodyW, ""), "\n") {
			lines = append(lines, base.Foreground(chrome.muted).Render(part))
		}
	}
	lines = append(lines, "")
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

// ── Annotations ──────────────────────────────────────────────────────────────

// annotationBadgeRule maps one conventional annotation key to a row badge.
// These keys are display conventions only: the protocol and storage accept
// any key, and unknown keys never appear in rows (they stay inspectable in the
// annotations overlay).
type annotationBadgeRule struct {
	key   string
	icon  string
	ascii string
	// badge returns the badge text for a value, or "" for no badge.
	badge func(rule annotationBadgeRule, value string, icons bool) string
}

func fixedBadge(values ...string) func(annotationBadgeRule, string, bool) string {
	return func(rule annotationBadgeRule, value string, icons bool) string {
		for _, v := range values {
			if value == v {
				if icons {
					return rule.icon
				}
				return rule.ascii
			}
		}
		return ""
	}
}

// categoryBadgePattern limits the category tag to short, plain identifiers so
// a row never carries free-form plugin text.
var categoryBadgePattern = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,11}$`)

var annotationBadgeRules = []annotationBadgeRule{
	{key: "needs_reply", icon: "↩", ascii: "R", badge: fixedBadge("true", "yes", "1")},
	{key: "urgency", icon: "!", ascii: "!", badge: fixedBadge("high", "urgent", "critical")},
	{key: "importance", icon: "◆", ascii: "^", badge: fixedBadge("high")},
	{key: "category", badge: func(_ annotationBadgeRule, value string, _ bool) string {
		if categoryBadgePattern.MatchString(value) {
			return "#" + value
		}
		return ""
	}},
}

// maxAnnotationBadges caps badges per row.
const maxAnnotationBadges = 3

// annotationBadges renders the row badges for one message from the cache, in
// rule order, at most one per key whichever plugin set it. It never queries
// the database.
func (m Model) annotationBadges(messageID int64) string {
	anns := m.plugins.annotations[messageID]
	if len(anns) == 0 {
		return ""
	}
	icons := m.iconsEnabled() && !m.styles.PlainUI
	var badges []string
	for _, rule := range annotationBadgeRules {
		for _, a := range anns {
			if a.Key != rule.key {
				continue
			}
			if b := rule.badge(rule, strings.ToLower(strings.TrimSpace(a.Value)), icons); b != "" {
				badges = append(badges, b)
				break
			}
		}
		if len(badges) == maxAnnotationBadges {
			break
		}
	}
	return strings.Join(badges, " ")
}

// withAnnotationBadges puts a message's badges in front of its age column.
func (m Model) withAnnotationBadges(messageID int64, age string) string {
	badges := m.annotationBadges(messageID)
	switch {
	case badges == "":
		return age
	case age == "":
		return badges
	}
	return badges + "  " + age
}

// annotationLines renders the annotations overlay: every stored annotation on
// the message, grouped by plugin.
func (m Model) annotationLines(width int, chrome managerChrome) []string {
	width = max(1, width)
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	head := base.Foreground(chrome.accent).Bold(true)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)

	anns := m.plugins.annotations[m.plugins.annotationsFor]
	if len(anns) == 0 {
		return []string{muted.Render("no annotations")}
	}
	keyW := 0
	for _, a := range anns {
		keyW = max(keyW, len(a.Key))
	}
	keyW = min(keyW, 24)

	var lines []string
	prev := ""
	for _, a := range anns { // sorted by plugin ID, then key
		if a.PluginID != prev {
			if prev != "" {
				lines = append(lines, "")
			}
			prev = a.PluginID
			title := sanitizePluginLine(a.PluginID) + " (not installed)"
			if p, ok := m.plugins.manager.Plugin(a.PluginID); ok {
				title = sanitizePluginLine(p.Manifest.Name) + "  (" + p.Manifest.ID + ")"
			}
			lines = append(lines, head.Render(truncate(title, width)))
		}
		confidence := ""
		if a.Confidence != nil {
			confidence = fmt.Sprintf("%3.0f%%", *a.Confidence*100)
		}
		key := padRight(truncate(sanitizePluginLine(a.Key), keyW), keyW)
		valueW := max(1, width-2-keyW-2-lipgloss.Width(confidence)-2)
		value := padRight(truncate(sanitizePluginLine(a.Value), valueW), valueW)
		row := "  " + key + "  " + value
		if confidence != "" {
			row += "  " + confidence
		}
		lines = append(lines, text.Render(row))
	}
	lines = append(lines, "", muted.Render("read-only; annotations never change your mail"))
	return lines
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

// formatPluginData pretty-prints a response's JSON data for display, then
// sanitizes and caps it. The data is informational only.
func formatPluginData(raw json.RawMessage) string {
	if len(bytes.TrimSpace(raw)) == 0 || bytes.Equal(bytes.TrimSpace(raw), []byte("null")) {
		return ""
	}
	text := string(raw)
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.UseNumber()
	var v any
	if err := dec.Decode(&v); err == nil {
		var buf bytes.Buffer
		enc := json.NewEncoder(&buf)
		enc.SetEscapeHTML(false)
		enc.SetIndent("", "  ")
		if err := enc.Encode(v); err == nil {
			text = strings.TrimRight(buf.String(), "\n")
		}
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
