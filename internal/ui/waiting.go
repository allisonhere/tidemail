package ui

// Waiting on Them: conversations where the user sent the latest meaningful
// message to someone else and no reply has arrived yet. It is conversation
// state only: no plugin, annotation, or network call is involved. Threads come
// from TideMail's existing Message-ID/In-Reply-To/References threading
// (buildMessageThreads), over header-only rows from every folder except
// Trash, Junk, and Drafts, so archived and Sent-only conversations count.

import (
	"fmt"
	"net/mail"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// waitingThread is one conversation in Waiting on Them.
type waitingThread struct {
	// message is the user's latest message: the row shown in the view.
	message db.Message
	// key identifies this waiting cycle for "Stop waiting".
	key string
	// since is when the user sent that message.
	since time.Time
	// waitingFor are the other people it was sent to, for display.
	waitingFor []string
}

// waitingState is Waiting on Them's cached state; rendering reads only this.
type waitingState struct {
	threads []waitingThread
	byID    map[int64]waitingThread // by message row ID
	// stopped maps the row ID of a stopped cycle's message to its key, for
	// "Resume waiting".
	stopped map[int64]string
	count   int
	loaded  bool
	// inFlight and dirty coalesce refreshes: one computation at a time, with
	// at most one more queued behind it.
	inFlight bool
	dirty    bool
	// lastStopped is the most recent Stop waiting, for ctrl+z.
	lastStopped []string
}

type (
	waitingLoadedMsg struct {
		// SnoozesChanged means ended waiting cycles' snoozes were removed.
		SnoozesChanged bool
		Threads        []waitingThread
		Stopped        map[int64]string
		Messages       []db.Message // full rows for the view, in order
		Annotations    map[int64][]db.PluginAnnotation
		Overrides      map[int64]map[string]db.ClassificationOverride
		Err            error
	}
	waitingStopMsg struct {
		Keys    []string
		Stopped bool
		Err     error
	}
)

// myAddresses are the user's own addresses, from every configured account's
// login and From address. Display names never count.
func myAddresses(accounts []accountIdentity) map[string]bool {
	me := map[string]bool{}
	for _, a := range accounts {
		for _, raw := range []string{a.user, a.from} {
			if addr := bareAddress(raw); addr != "" {
				me[addr] = true
			}
		}
	}
	return me
}

type accountIdentity struct{ user, from string }

func (m Model) accountIdentities() []accountIdentity {
	out := make([]accountIdentity, len(m.cfg.Accounts))
	for i, a := range m.cfg.Accounts {
		out[i] = accountIdentity{user: a.User, from: a.From}
	}
	return out
}

// bareAddress returns the lowercase address in s, or "" if there is none.
func bareAddress(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if addr, err := mail.ParseAddress(s); err == nil {
		return strings.ToLower(addr.Address)
	}
	if strings.Contains(s, "@") && !strings.ContainsAny(s, " <>") {
		return strings.ToLower(s)
	}
	return ""
}

// participant is one parsed address.
type participant struct {
	name, addr string
}

func parseParticipants(fields ...string) []participant {
	var out []participant
	for _, f := range fields {
		if strings.TrimSpace(f) == "" {
			continue
		}
		list, err := mail.ParseAddressList(f)
		if err != nil {
			// Sloppy or hostile headers (a control character in one display
			// name rejects the whole list): split on commas and pull out each
			// <address> and the name before it.
			for _, part := range strings.Split(f, ",") {
				if m := angleAddressRe.FindStringSubmatch(part); m != nil {
					name := strings.Trim(strings.TrimSpace(part[:strings.Index(part, "<")]), `"`)
					out = append(out, participant{name: name, addr: strings.ToLower(m[1])})
				} else if a := bareAddress(part); a != "" {
					out = append(out, participant{addr: a})
				}
			}
			continue
		}
		for _, a := range list {
			out = append(out, participant{name: strings.TrimSpace(a.Name), addr: strings.ToLower(a.Address)})
		}
	}
	return out
}

var angleAddressRe = regexp.MustCompile(`<([^<>\s@]+@[^<>\s]+)>`)

// automatedLocalParts mark addresses nobody reads replies from: no-reply
// senders, bounce handlers, and notification robots.
var automatedLocalParts = []string{
	"noreply", "no-reply", "no_reply", "donotreply", "do-not-reply", "do_not_reply",
	"mailer-daemon", "postmaster", "bounce", "bounces", "notifications", "notification",
}

// automatedAddress is a conservative heuristic for robots and mailing lists.
func automatedAddress(addr string) bool {
	at := strings.LastIndex(addr, "@")
	if at <= 0 {
		return false
	}
	local, domain := addr[:at], addr[at+1:]
	for _, p := range automatedLocalParts {
		if local == p || strings.HasPrefix(local, p+"+") || strings.HasPrefix(local, p+"-") || strings.HasPrefix(local, p+".") {
			return true
		}
	}
	// Obvious mailing lists.
	if strings.HasSuffix(local, "-list") || local == "list" || strings.HasPrefix(domain, "lists.") ||
		domain == "googlegroups.com" || domain == "groups.io" {
		return true
	}
	return false
}

// meaningful reports whether a message can decide a conversation's state:
// drafts and messages from robots (bounces, notifications) cannot.
func meaningful(msg db.Message) bool {
	if hasFlag(msg.Flags, `\Draft`) {
		return false
	}
	from := bareAddress(msg.From)
	return from != "" && !automatedAddress(from)
}

// computeWaiting finds waiting conversations. A thread qualifies when its
// newest meaningful message is from the user and was sent to at least one
// real person who is not the user; stopped cycles are left out. The oldest
// wait comes first.
func computeWaiting(candidates []db.Message, me map[string]bool, stoppedKeys map[string]bool) ([]waitingThread, map[int64]string) {
	threads, stopped, _ := computeWaitingWithSnoozes(candidates, me, stoppedKeys, nil)
	return threads, stopped
}

// computeWaitingWithSnoozes also hides snoozed waiting cycles and reports
// snoozes whose cycle has ended: someone replied, or the user wrote again. A
// snooze belongs to one wait, like Stop waiting, so those are deleted rather
// than left to hide a new wait.
func computeWaitingWithSnoozes(candidates []db.Message, me, stoppedKeys, snoozedKeys map[string]bool) ([]waitingThread, map[int64]string, []string) {
	stopped := map[int64]string{}
	var stale []string
	if len(me) == 0 {
		return nil, stopped, nil
	}
	var out []waitingThread
	for _, thread := range buildMessageThreads(candidates) {
		var latest *db.Message
		for i := len(thread.Messages) - 1; i >= 0; i-- { // oldest to newest
			if meaningful(thread.Messages[i]) {
				latest = &thread.Messages[i]
				break
			}
		}
		currentKey := ""
		if latest != nil && me[bareAddress(latest.From)] {
			currentKey = db.WaitingKey(*latest)
		}
		for _, msg := range thread.Messages {
			if key := db.WaitingKey(msg); snoozedKeys[key] && key != currentKey {
				stale = append(stale, key)
			}
		}
		if latest == nil || !me[bareAddress(latest.From)] {
			continue
		}
		var waitingFor []string
		seen := map[string]bool{}
		for _, p := range parseParticipants(latest.To, latest.CC) {
			if p.addr == "" || me[p.addr] || automatedAddress(p.addr) || seen[p.addr] {
				continue
			}
			seen[p.addr] = true
			waitingFor = append(waitingFor, displayParticipant(p))
		}
		if len(waitingFor) == 0 {
			continue // self-mail, or only robots and lists
		}
		key := currentKey
		if stoppedKeys[key] {
			stopped[latest.ID] = key
		}
		// Snoozed and stopped both hide the wait; neither clears the other.
		if stoppedKeys[key] || snoozedKeys[key] {
			continue
		}
		out = append(out, waitingThread{message: *latest, key: key, since: latest.Date, waitingFor: waitingFor})
	}
	sort.SliceStable(out, func(i, j int) bool {
		if !out[i].since.Equal(out[j].since) {
			return out[i].since.Before(out[j].since)
		}
		return out[i].message.ID < out[j].message.ID
	})
	return out, stopped, stale
}

func displayParticipant(p participant) string {
	if name := sanitizePluginLine(unescapeDisplayText(p.name)); name != "" {
		return name
	}
	return sanitizePluginLine(p.addr)
}

// loadWaitingCmd computes Waiting on Them off the Update loop.
func (m *Model) loadWaitingCmd() tea.Cmd {
	database := m.db
	me := myAddresses(m.accountIdentities())
	return func() tea.Msg {
		if database == nil {
			return waitingLoadedMsg{}
		}
		candidates, err := database.ListWaitingCandidates()
		if err != nil {
			return waitingLoadedMsg{Err: err}
		}
		stoppedKeys, err := database.WaitingStopped()
		if err != nil {
			return waitingLoadedMsg{Err: err}
		}
		snoozes, err := database.ListSnoozes()
		if err != nil {
			return waitingLoadedMsg{Err: err}
		}
		snoozedKeys := map[string]bool{}
		for _, sn := range snoozes {
			if sn.TargetType == db.SnoozeThread {
				snoozedKeys[sn.TargetKey] = true
			}
		}
		threads, stopped, stale := computeWaitingWithSnoozes(candidates, me, stoppedKeys, snoozedKeys)
		// Snoozes of waits that ended (a reply, or a new message of mine) go.
		snoozesChanged := len(stale) > 0 && database.DeleteSnoozeKeys(db.SnoozeThread, stale) == nil
		ids := make([]int64, len(threads))
		for i, t := range threads {
			ids[i] = t.message.ID
		}
		msgs, err := database.ListMessagesByIDs(ids)
		if err != nil {
			return waitingLoadedMsg{Err: err}
		}
		anns, overrides := loadMessageClassification(database, msgs)
		return waitingLoadedMsg{Threads: threads, Stopped: stopped, Messages: msgs, Annotations: anns, Overrides: overrides, SnoozesChanged: snoozesChanged}
	}
}

// requestWaitingRefresh recomputes Waiting on Them in the background. While a
// computation runs, further requests collapse into one follow-up.
func (m *Model) requestWaitingRefresh() tea.Cmd {
	if m.db == nil {
		return nil
	}
	if m.waiting.inFlight {
		m.waiting.dirty = true
		return nil
	}
	m.waiting.inFlight = true
	return m.loadWaitingCmd()
}

func (m Model) selectedWaiting() bool {
	if m.sidebarCursor < 0 || m.sidebarCursor >= len(m.sidebarRows) {
		return false
	}
	return m.sidebarRows[m.sidebarCursor].kind == rowKindWaiting
}

func (m Model) handleWaitingMsg(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case waitingLoadedMsg:
		m.waiting.inFlight = false
		var cmds []tea.Cmd
		if m.waiting.dirty {
			m.waiting.dirty = false
			cmds = append(cmds, m.requestWaitingRefresh())
		}
		if msg.Err != nil {
			return m, tea.Batch(cmds...)
		}
		if msg.SnoozesChanged {
			cmds = append(cmds, loadSnoozeStateCmd(m.db))
		}
		m.waiting.threads = msg.Threads
		m.waiting.stopped = msg.Stopped
		m.waiting.count = len(msg.Threads)
		m.waiting.loaded = true
		m.waiting.byID = make(map[int64]waitingThread, len(msg.Threads))
		for _, t := range msg.Threads {
			m.waiting.byID[t.message.ID] = t
		}
		if m.selectedWaiting() && !m.searchActive() {
			// Reuse the normal list path: cursor, selection, and threads.
			next, cmd := m.update(MessagesLoadedMsg{Waiting: true, Messages: msg.Messages, Annotations: msg.Annotations, Overrides: msg.Overrides})
			m = next.(Model)
			cmds = append(cmds, cmd)
		}
		return m, tea.Batch(cmds...)

	case waitingStopMsg:
		if msg.Err != nil {
			m.setStatus("Waiting on Them: "+msg.Err.Error(), true)
			return m, m.clearStatusCmd()
		}
		if msg.Stopped {
			m.waiting.lastStopped = msg.Keys
			m.setStatus(fmt.Sprintf("stopped waiting%sctrl+z undo", m.styles.InlineMidDot()), false)
		} else {
			m.waiting.lastStopped = nil
			m.setStatus("resumed waiting", false)
		}
		return m, tea.Batch(m.requestWaitingRefresh(), m.clearStatusCmd())
	}
	return m, nil
}

func setWaitingStoppedCmd(database *db.DB, keys []string, stopped bool) tea.Cmd {
	keys = append([]string(nil), keys...)
	return func() tea.Msg {
		if database == nil {
			return waitingStopMsg{Keys: keys, Stopped: stopped, Err: fmt.Errorf("no database")}
		}
		return waitingStopMsg{Keys: keys, Stopped: stopped, Err: database.SetWaitingStopped(keys, stopped)}
	}
}

// stopWaitingCurrent stops waiting on the current row's conversation(s).
func (m Model) stopWaitingCurrent() (tea.Model, tea.Cmd) {
	var keys []string
	for _, msg := range m.currentRowMessages() {
		if t, ok := m.waiting.byID[msg.ID]; ok {
			keys = append(keys, t.key)
		}
	}
	if len(keys) == 0 {
		return m, nil
	}
	return m, setWaitingStoppedCmd(m.db, keys, true)
}

// undoStopWaiting resumes the latest Stop waiting while the view is open.
func (m *Model) undoStopWaiting() (tea.Cmd, bool) {
	if !m.selectedWaiting() || len(m.waiting.lastStopped) == 0 {
		return nil, false
	}
	keys := m.waiting.lastStopped
	m.waiting.lastStopped = nil
	return setWaitingStoppedCmd(m.db, keys, false), true
}

func (m Model) waitingCommandItems() []commandItem {
	msg := m.commandMessage()
	if msg == nil {
		return nil
	}
	var items []commandItem
	if _, ok := m.waiting.byID[msg.ID]; ok && m.selectedWaiting() {
		items = append(items,
			commandItem{id: "waiting-why", label: "Why is this in Waiting on Them?", enabled: true},
			commandItem{id: "waiting-stop", label: "Stop waiting", enabled: true},
		)
	}
	if _, ok := m.waiting.stopped[msg.ID]; ok {
		items = append(items, commandItem{id: "waiting-resume", label: "Resume waiting", enabled: true})
	}
	return items
}

func (m Model) executeWaitingCommand(id string) (tea.Model, tea.Cmd) {
	msg := m.commandMessage()
	if msg == nil {
		return m, nil
	}
	switch id {
	case "waiting-why":
		m.plugins.annotationsFor = msg.ID
		m.plugins.scroll = 0
		m.plugins.annCursor = 0
		m.overlay = overlayPluginAnnotations
	case "waiting-stop":
		return m.stopWaitingCurrent()
	case "waiting-resume":
		if key, ok := m.waiting.stopped[msg.ID]; ok {
			return m, setWaitingStoppedCmd(m.db, []string{key}, false)
		}
	}
	return m, nil
}

// waitingExplanationLines explain a waiting conversation, deterministically.
func (m Model) waitingExplanationLines(messageID int64, now time.Time) []string {
	t, ok := m.waiting.byID[messageID]
	if !ok {
		return nil
	}
	return []string{
		"Waiting on Them because:",
		"  You sent the latest message " + agoText(now.Sub(t.since)) + ".",
		"  Waiting for: " + summarizeNames(t.waitingFor),
	}
}

// agoText is a coarse "N units ago".
func agoText(d time.Duration) string {
	switch {
	case d < time.Minute:
		return "just now"
	case d < time.Hour:
		return plural(int(d/time.Minute), "minute") + " ago"
	case d < 24*time.Hour:
		return plural(int(d/time.Hour), "hour") + " ago"
	default:
		return plural(int(d/(24*time.Hour)), "day") + " ago"
	}
}

func plural(n int, unit string) string {
	if n == 1 {
		return "1 " + unit
	}
	return fmt.Sprintf("%d %ss", n, unit)
}

// summarizeNames shows up to two names, then "+N".
func summarizeNames(names []string) string {
	switch {
	case len(names) <= 2:
		return strings.Join(names, ", ")
	default:
		return fmt.Sprintf("%s, %s +%d", names[0], names[1], len(names)-2)
	}
}

// waitingSender is the sender column in Waiting on Them: who you are waiting
// for, not yourself.
func (m Model) waitingSender(msg db.Message) (string, bool) {
	if !m.selectedWaiting() {
		return "", false
	}
	t, ok := m.waiting.byID[msg.ID]
	if !ok {
		return "", false
	}
	prefix := "↗ "
	if !m.iconsEnabled() || m.styles.PlainUI {
		prefix = "> "
	}
	return prefix + summarizeNames(t.waitingFor), true
}

func (m Model) renderWaitingRow(selected bool, width int) string {
	badge := ""
	if m.waiting.count > 0 {
		badge = m.accountBadgeStyle(0, selected).Render(fmt.Sprintf("(%d)", m.waiting.count))
	}
	prefix := "↗ "
	if !m.iconsEnabled() {
		prefix = "> "
	}
	row := renderFeedRow(prefix, "Waiting on Them", badge, width)
	style := m.styles.FeedItem
	if selected {
		style = m.sidebarSelectedStyle("")
	}
	return style.Width(width).Render(row)
}

// syncSentAfterSendCmd syncs the sending account's Sent folder after a
// successful send: that sync stores the sent message locally, which is the
// earliest reliable point for the conversation to enter Waiting on Them.
func (m *Model) syncSentAfterSendCmd(accountConfigID string) tea.Cmd {
	for _, acc := range m.accounts {
		if acc.ConfigID != accountConfigID {
			continue
		}
		for _, mb := range m.mailboxes {
			if mb.AccountID == acc.ID && db.IsSentMailbox(mb) {
				return m.syncMailboxCmd(mb.ID, false)
			}
		}
	}
	return nil
}
