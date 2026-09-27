package ui

// Snooze: hide messages from Needs You, or waiting conversations from Waiting
// on Them, until a chosen time. It is local state only (db.Snooze): mail,
// folders, flags, and plugin annotations are never touched, and nothing is
// sent anywhere. When a snooze ends, the views re-evaluate the item's current
// state, so it returns only if it still qualifies.

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

// customSnoozeLayout is how "Pick date/time" is typed, in local time.
const customSnoozeLayout = "2006-01-02 15:04"

// snoozeState is Snooze's cached state; rendering reads only this.
type snoozeState struct {
	ctx         context.Context
	cancel      context.CancelFunc
	timerCancel context.CancelFunc
	gen         uint64
	deadline    time.Time

	items []db.SnoozedMessage
	// byMessageID finds the snooze shown for a message row.
	byMessageID map[int64]db.Snooze
	count       int
	loaded      bool

	picker snoozePicker
}

// snoozePicker is the Snooze overlay.
type snoozePicker struct {
	targets []snoozeTarget
	title   string
	presets []snoozePreset
	cursor  int // len(presets) is "Pick date/time"
	custom  bool
	input   textinput.Model
}

type snoozeTarget struct{ typ, key string }

type snoozePreset struct {
	label string
	at    time.Time
}

type (
	snoozeLoadedMsg struct {
		Items       []db.SnoozedMessage
		Annotations map[int64][]db.PluginAnnotation
		Next        time.Time
		HasNext     bool
		Expired     int
		Err         error
	}
	snoozeAppliedMsg struct {
		At       time.Time
		Unsnooze bool
		Total    int
		Failed   int
		Reason   string
	}
)

func newSnoozeState() snoozeState {
	ctx, cancel := context.WithCancel(context.Background())
	return snoozeState{ctx: ctx, cancel: cancel}
}

// snoozePresets computes the preset wake times in now's location, using
// wall-clock times so daylight-saving changes keep "9:00 AM" at 9:00 AM:
//
//   - Later today: 7:00 PM, or if it is already past 5:00 PM, three hours
//     after the next full hour.
//   - Tomorrow morning: 8:00 AM tomorrow.
//   - Tomorrow evening: 7:00 PM tomorrow.
//   - This weekend: Saturday 9:00 AM, the upcoming one unless it has passed.
//   - Next week: next Monday 9:00 AM (a week ahead on a Monday).
func snoozePresets(now time.Time) []snoozePreset {
	loc := now.Location()
	at := func(daysAhead, hour int) time.Time {
		return time.Date(now.Year(), now.Month(), now.Day()+daysAhead, hour, 0, 0, 0, loc)
	}
	later := at(0, 19)
	if !now.Before(at(0, 17)) {
		later = now.Truncate(time.Hour).Add(4 * time.Hour)
	}
	daysToSat := (int(time.Saturday) - int(now.Weekday()) + 7) % 7
	weekend := at(daysToSat, 9)
	if !weekend.After(now) {
		weekend = at(daysToSat+7, 9)
	}
	daysToMon := (int(time.Monday) - int(now.Weekday()) + 7) % 7
	if daysToMon == 0 {
		daysToMon = 7
	}
	return []snoozePreset{
		{"Later today", later},
		{"Tomorrow morning", at(1, 8)},
		{"Tomorrow evening", at(1, 19)},
		{"This weekend", weekend},
		{"Next week", at(daysToMon, 9)},
	}
}

// parseCustomSnooze reads "YYYY-MM-DD HH:MM" in loc and requires a future
// time. A past time is an error, never silently moved.
func parseCustomSnooze(s string, now time.Time) (time.Time, error) {
	t, err := time.ParseInLocation(customSnoozeLayout, strings.TrimSpace(s), now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("use YYYY-MM-DD HH:MM, e.g. %s", now.Add(24*time.Hour).Format(customSnoozeLayout))
	}
	if !t.After(now) {
		return time.Time{}, fmt.Errorf("%s is in the past", t.Format("Mon Jan 2 3:04 PM"))
	}
	return t, nil
}

// wakeLabel formats a wake time relative to now, in now's location.
func wakeLabel(t, now time.Time) string {
	t = t.In(now.Location())
	y1, m1, d1 := now.Date()
	y2, m2, d2 := t.Date()
	days := int(time.Date(y2, m2, d2, 0, 0, 0, 0, now.Location()).Sub(time.Date(y1, m1, d1, 0, 0, 0, 0, now.Location())).Hours()+12) / 24
	clock := t.Format("3:04 PM")
	switch {
	case days == 0:
		return "Today " + clock
	case days == 1:
		return "Tomorrow " + clock
	case days > 1 && days < 7:
		return t.Format("Mon") + " " + clock
	}
	return t.Format("Jan 2") + " " + clock
}

// snoozeTargets are what Snooze acts on: waiting conversations in Waiting on
// Them, otherwise messages (the selection, or the current row).
func (m Model) snoozeTargets() ([]snoozeTarget, string) {
	msgs := m.pluginTargets()
	if !m.hasSelection() {
		msgs = m.currentRowMessages()
	}
	seen := map[string]bool{}
	var targets []snoozeTarget
	add := func(t snoozeTarget) {
		if !seen[t.typ+t.key] {
			seen[t.typ+t.key] = true
			targets = append(targets, t)
		}
	}
	if m.selectedWaiting() {
		for _, msg := range msgs {
			if t, ok := m.waiting.byID[msg.ID]; ok {
				add(snoozeTarget{db.SnoozeThread, t.key})
			}
		}
		switch n := len(targets); {
		case n == 1:
			return targets, "Snooze conversation"
		case n > 1:
			return targets, fmt.Sprintf("Snooze %d conversations", n)
		}
		return nil, ""
	}
	for _, msg := range msgs {
		add(snoozeTarget{db.SnoozeMessage, db.MessageKey(msg)})
	}
	switch n := len(msgs); {
	case n > 1 && m.hasSelection():
		return targets, fmt.Sprintf("Snooze %d selected messages", n)
	case n > 1:
		return targets, fmt.Sprintf("Snooze conversation (%d messages)", n)
	case n == 1:
		return targets, "Snooze message"
	}
	return nil, ""
}

// openSnoozePicker opens the picker once for all targets.
func (m Model) openSnoozePicker() (tea.Model, tea.Cmd) {
	targets, title := m.snoozeTargets()
	if len(targets) == 0 {
		m.setStatus("nothing to snooze here", false)
		return m, m.clearStatusCmd()
	}
	m.snooze.picker = snoozePicker{targets: targets, title: title, presets: snoozePresets(clockForSnooze.Now())}
	m.overlay = overlaySnooze
	return m, nil
}

func (m Model) handleSnoozeKey(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	p := &m.snooze.picker
	if p.custom {
		switch {
		case keyMatches(msg, m.keys.Cancel):
			p.custom = false
			p.input.Blur()
		case keyMatches(msg, m.keys.Confirm):
			at, err := parseCustomSnooze(p.input.Value(), clockForSnooze.Now())
			if err != nil {
				m.setStatus("snooze: "+err.Error(), true)
				return m, m.clearStatusCmd()
			}
			return m.applySnooze(at)
		default:
			var cmd tea.Cmd
			p.input, cmd = p.input.Update(msg)
			return m, cmd
		}
		return m, nil
	}
	switch {
	case keyMatches(msg, m.keys.Cancel, m.keys.Back):
		m.snooze.picker = snoozePicker{}
		m.overlay = overlayNone
	case keyMatches(msg, m.keys.Up):
		p.cursor = max(0, p.cursor-1)
	case keyMatches(msg, m.keys.Down):
		p.cursor = min(len(p.presets), p.cursor+1)
	case keyMatches(msg, m.keys.Confirm):
		if p.cursor < len(p.presets) {
			return m.applySnooze(p.presets[p.cursor].at)
		}
		in := textinput.New()
		in.Placeholder = customSnoozeLayout
		in.CharLimit = len(customSnoozeLayout)
		in.SetValue(clockForSnooze.Now().Add(24*time.Hour).Format("2006-01-02") + " 09:00")
		in.Focus()
		p.input = in
		p.custom = true
	}
	return m, nil
}

// applySnooze stores the snooze for every target and closes the picker.
func (m Model) applySnooze(at time.Time) (tea.Model, tea.Cmd) {
	targets := m.snooze.picker.targets
	m.snooze.picker = snoozePicker{}
	m.overlay = overlayNone
	return m, setSnoozesCmd(m.db, targets, at, false)
}

// setSnoozesCmd writes (or removes) snoozes off the Update loop. Each target
// is written on its own, so one failure does not undo the rest.
func setSnoozesCmd(database *db.DB, targets []snoozeTarget, at time.Time, unsnooze bool) tea.Cmd {
	targets = append([]snoozeTarget(nil), targets...)
	return func() tea.Msg {
		res := snoozeAppliedMsg{At: at, Unsnooze: unsnooze, Total: len(targets)}
		for _, t := range targets {
			var err error
			switch {
			case database == nil:
				err = fmt.Errorf("no database")
			case unsnooze:
				err = database.DeleteSnooze(t.typ, t.key)
			default:
				err = database.SetSnooze(t.typ, t.key, at)
			}
			if err != nil {
				res.Failed++
				if res.Reason == "" {
					res.Reason = err.Error()
				}
			}
		}
		return res
	}
}

// expireSnoozesCmd deletes due snoozes and reloads the snooze state. It runs
// at startup and whenever the timer fires.
func expireSnoozesCmd(database *db.DB) tea.Cmd {
	if database == nil {
		return nil
	}
	clock := clockForSnooze
	return func() tea.Msg {
		expired, err := database.DeleteDueSnoozes(clock.Now())
		if err != nil {
			return snoozeLoadedMsg{Err: err}
		}
		msg := loadSnoozeState(database, clock)
		msg.Expired = expired
		return msg
	}
}

func loadSnoozeStateCmd(database *db.DB) tea.Cmd {
	if database == nil {
		return nil
	}
	clock := clockForSnooze
	return func() tea.Msg { return loadSnoozeState(database, clock) }
}

func loadSnoozeState(database *db.DB, clock snoozeClock) snoozeLoadedMsg {
	items, err := database.ListSnoozedMessages()
	if err != nil {
		return snoozeLoadedMsg{Err: err}
	}
	next, ok, err := database.NextSnoozeDeadline(clock.Now())
	if err != nil {
		return snoozeLoadedMsg{Err: err}
	}
	msgs := make([]db.Message, len(items))
	for i, it := range items {
		msgs[i] = it.Message
	}
	return snoozeLoadedMsg{Items: items, Annotations: loadMessageAnnotations(database, msgs), Next: next, HasNext: ok}
}

func (m Model) handleSnoozeMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case snoozeLoadedMsg:
		if msg.Err != nil {
			return m, nil
		}
		m.snooze.items = msg.Items
		m.snooze.count = len(msg.Items)
		m.snooze.loaded = true
		m.snooze.byMessageID = make(map[int64]db.Snooze, len(msg.Items))
		for _, it := range msg.Items {
			m.snooze.byMessageID[it.Message.ID] = it.Snooze
		}
		cmds := []tea.Cmd{m.scheduleSnoozeTimer(msg.Next, msg.HasNext)}
		if msg.Expired > 0 {
			// Expired items return only if they still qualify.
			cmds = append(cmds, m.needsYouRefreshCmd(), m.requestWaitingRefresh())
		}
		if m.selectedSnoozed() && !m.searchActive() {
			msgs := make([]db.Message, len(msg.Items))
			for i, it := range msg.Items {
				msgs[i] = it.Message
			}
			next, cmd := m.update(MessagesLoadedMsg{Snoozed: true, Messages: msgs, Annotations: msg.Annotations})
			m = next.(Model)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case snoozeAppliedMsg:
		ok := msg.Total - msg.Failed
		switch {
		case msg.Failed > 0:
			m.setStatus(fmt.Sprintf("%d of %d failed: %s", msg.Failed, msg.Total, sanitizePluginLine(msg.Reason)), true)
		case msg.Unsnooze:
			m.setStatus(fmt.Sprintf("unsnoozed %d", ok), false)
		default:
			m.setStatus(fmt.Sprintf("snoozed %d until %s", ok, wakeLabel(msg.At, clockForSnooze.Now())), false)
		}
		return m, tea.Batch(loadSnoozeStateCmd(m.db), m.needsYouRefreshCmd(), m.requestWaitingRefresh(), m.clearStatusCmd())
	}
	return m, nil
}

func (m Model) selectedSnoozed() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	return m.sidebarRows[m.sidebarCursor].kind == rowKindSnoozed
}

// unsnoozeCurrent removes the snoozes of the current row (or selection) in
// the Snoozed view.
func (m Model) unsnoozeCurrent() (tea.Model, tea.Cmd) {
	msgs := m.pluginTargets()
	var targets []snoozeTarget
	for _, msg := range msgs {
		if s, ok := m.snooze.byMessageID[msg.ID]; ok {
			targets = append(targets, snoozeTarget{s.TargetType, s.TargetKey})
		}
	}
	if len(targets) == 0 {
		return m, nil
	}
	return m, setSnoozesCmd(m.db, targets, time.Time{}, true)
}

// handleSnoozeKeyPress is the Z key: snooze, or unsnooze in Snoozed.
func (m Model) handleSnoozeKeyPress() (tea.Model, tea.Cmd) {
	if m.selectedSnoozed() {
		return m.unsnoozeCurrent()
	}
	return m.openSnoozePicker()
}

func (m Model) snoozeCommandItems() []commandItem {
	if m.commandMessage() == nil {
		return nil
	}
	if m.selectedSnoozed() {
		return []commandItem{
			{id: "snooze-why", label: "Why is this snoozed?", enabled: true},
			{id: "snooze-unsnooze", label: "Unsnooze", enabled: true},
		}
	}
	if _, title := m.snoozeTargets(); title != "" {
		return []commandItem{{id: "snooze", label: title + "…", enabled: true}}
	}
	return nil
}

func (m Model) executeSnoozeCommand(id string) (tea.Model, tea.Cmd) {
	switch id {
	case "snooze":
		return m.openSnoozePicker()
	case "snooze-unsnooze":
		return m.unsnoozeCurrent()
	case "snooze-why":
		if msg := m.commandMessage(); msg != nil {
			m.plugins.annotationsFor = msg.ID
			m.plugins.scroll = 0
			m.plugins.annCursor = 0
			m.overlay = overlayPluginAnnotations
		}
	}
	return m, nil
}

// snoozeExplanationLines head the details overlay for a snoozed message.
func (m Model) snoozeExplanationLines(messageID int64) []string {
	s, ok := m.snooze.byMessageID[messageID]
	if !ok {
		return nil
	}
	what := "Snoozed from Needs You"
	if s.TargetType == db.SnoozeThread {
		what = "Conversation snoozed from Waiting on Them"
	}
	return []string{what + " until " + s.Until.In(time.Local).Format("Monday, January 2 at 3:04 PM") + "."}
}

// snoozedRowTime is the time column in Snoozed: when the item wakes.
func (m Model) snoozedRowTime(msg db.Message) (string, bool) {
	if !m.selectedSnoozed() {
		return "", false
	}
	s, ok := m.snooze.byMessageID[msg.ID]
	if !ok {
		return "", false
	}
	return wakeLabel(s.Until, clockForSnooze.Now()), true
}

// snoozedSender marks snoozed conversations with who they wait on.
func (m Model) snoozedSender(msg db.Message) (string, bool) {
	if !m.selectedSnoozed() {
		return "", false
	}
	s, ok := m.snooze.byMessageID[msg.ID]
	if !ok || s.TargetType != db.SnoozeThread {
		return "", false
	}
	me := myAddresses(m.accountIdentities())
	var names []string
	for _, p := range parseParticipants(msg.To, msg.CC) {
		if !me[p.addr] {
			names = append(names, displayParticipant(p))
		}
	}
	prefix := "↗ "
	if !m.iconsEnabled() || m.styles.PlainUI {
		prefix = "> "
	}
	return prefix + summarizeNames(names), true
}

func (m Model) renderSnoozedRow(selected bool, width int) string {
	badge := ""
	if m.snooze.count > 0 {
		badge = m.accountBadgeStyle(0, selected).Render(fmt.Sprintf("(%d)", m.snooze.count))
	}
	prefix := "◔ "
	if !m.iconsEnabled() {
		prefix = "z "
	}
	row := renderFeedRow(prefix, "Snoozed", badge, width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

func (m Model) renderSnoozePicker() string {
	winW := max(1, min(m.width-4, 56))
	chrome := newManagerChrome(winW, m.styles.Theme, m.styles.PlainUI)
	p := m.snooze.picker
	now := clockForSnooze.Now()
	base := lipgloss.NewStyle().Background(chrome.baseBg)
	text := base.Foreground(chrome.text)
	muted := base.Foreground(chrome.muted)
	bodyW := max(1, winW-4)

	lines := []string{text.Bold(true).Render(truncate(p.title, bodyW)), ""}
	labelW := 18
	for i, preset := range p.presets {
		row := padRight(preset.label, labelW) + "  " + wakeLabel(preset.at, now)
		lines = append(lines, softRail(chrome, i == p.cursor, chrome.baseBg)+text.Render(truncate(row, bodyW-2)))
	}
	lines = append(lines, softRail(chrome, p.cursor == len(p.presets), chrome.baseBg)+text.Render("Pick date/time"))
	pairs := []string{"↑↓", "choose", "enter", "snooze", "esc", "cancel"}
	if p.custom {
		in := p.input
		in.Width = max(1, bodyW-4)
		in.PromptStyle = base.Foreground(chrome.accent)
		in.TextStyle = text
		in.PlaceholderStyle = muted
		lines = append(lines, "", text.Render("wake at (local, YYYY-MM-DD HH:MM):"), in.View())
		pairs = []string{"enter", "snooze", "esc", "back"}
	}
	lines = append(lines, "", muted.Render("Local only: mail, folders, and plugin tags are untouched."))
	body := lipgloss.NewStyle().Background(chrome.baseBg).Width(winW).Padding(1, 2).Render(strings.Join(lines, "\n"))
	hints := renderSoftHints(winW, chrome, pairs...)
	inner := lipgloss.JoinVertical(lipgloss.Left, body, hints)
	inner = clampView(inner, winW, strings.Count(inner, "\n")+1, chrome.baseBg)
	return renderSoftPanelBox(inner, winW, "tidemail", "snooze", chrome)
}
