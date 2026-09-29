package ui

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// fakeSnoozeClock controls time for snooze tests. Wait records the requested
// duration and returns only when fire is called (or its context ends).
type fakeSnoozeClock struct {
	mu    sync.Mutex
	now   time.Time
	waits []time.Duration
	fire  chan struct{}
}

func (c *fakeSnoozeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.now
}

func (c *fakeSnoozeClock) Wait(ctx context.Context, d time.Duration) bool {
	c.mu.Lock()
	c.waits = append(c.waits, d)
	c.mu.Unlock()
	select {
	case <-c.fire:
		return true
	case <-ctx.Done():
		return false
	}
}

func (c *fakeSnoozeClock) set(t time.Time) {
	c.mu.Lock()
	c.now = t
	c.mu.Unlock()
}

func (c *fakeSnoozeClock) waitCount() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return len(c.waits)
}

func useFakeSnoozeClock(t *testing.T, now time.Time) *fakeSnoozeClock {
	t.Helper()
	c := &fakeSnoozeClock{now: now, fire: make(chan struct{})}
	prev := clockForSnooze
	clockForSnooze = c
	t.Cleanup(func() { clockForSnooze = prev })
	return c
}

func mustLoc(t *testing.T, name string) *time.Location {
	t.Helper()
	loc, err := time.LoadLocation(name)
	if err != nil {
		t.Skipf("no tz data for %s: %v", name, err)
	}
	return loc
}

// ── Presets and parsing ──────────────────────────────────────────────────────

func presetMap(now time.Time) map[string]time.Time {
	out := map[string]time.Time{}
	for _, p := range snoozePresets(now) {
		out[p.label] = p.at
	}
	return out
}

func TestSnoozePresets(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	d := func(y int, mo time.Month, day, h, min int) time.Time { return time.Date(y, mo, day, h, min, 0, 0, ny) }
	cases := []struct {
		name  string
		now   time.Time
		label string
		want  time.Time
	}{
		{"later today, afternoon", d(2026, 9, 23, 14, 10), "Later today", d(2026, 9, 23, 19, 0)},
		{"later today, late evening", d(2026, 9, 23, 21, 40), "Later today", d(2026, 9, 24, 1, 0)},
		{"tomorrow morning", d(2026, 9, 23, 14, 10), "Tomorrow morning", d(2026, 9, 24, 8, 0)},
		{"tomorrow evening", d(2026, 9, 23, 14, 10), "Tomorrow evening", d(2026, 9, 24, 19, 0)},
		{"weekend from Wednesday", d(2026, 9, 23, 14, 10), "This weekend", d(2026, 9, 26, 9, 0)},
		{"weekend from Friday night", d(2026, 9, 25, 23, 30), "This weekend", d(2026, 9, 26, 9, 0)},
		{"weekend from early Saturday", d(2026, 9, 26, 7, 0), "This weekend", d(2026, 9, 26, 9, 0)},
		{"weekend from Saturday afternoon", d(2026, 9, 26, 15, 0), "This weekend", d(2026, 10, 3, 9, 0)},
		{"weekend from Sunday", d(2026, 9, 27, 10, 0), "This weekend", d(2026, 10, 3, 9, 0)},
		{"next week from Wednesday", d(2026, 9, 23, 14, 10), "Next week", d(2026, 9, 28, 9, 0)},
		{"next week from Monday", d(2026, 9, 28, 8, 0), "Next week", d(2026, 10, 5, 9, 0)},
		// US DST starts 2026-03-08: "8:00 AM" stays 8:00 AM local.
		{"across DST", d(2026, 3, 7, 12, 0), "Tomorrow morning", d(2026, 3, 8, 8, 0)},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			got := presetMap(c.now)[c.label]
			if !got.Equal(c.want) {
				t.Fatalf("%s at %v = %v, want %v", c.label, c.now, got, c.want)
			}
			if got.Hour() != c.want.Hour() || !got.After(c.now) {
				t.Fatalf("not local wall-clock time, or not in the future: %v", got)
			}
		})
	}
}

func TestParseCustomSnooze(t *testing.T) {
	ny := mustLoc(t, "America/New_York")
	now := time.Date(2026, 9, 23, 14, 0, 0, 0, ny)
	got, err := parseCustomSnooze("2026-09-30 09:15", now)
	if err != nil || !got.Equal(time.Date(2026, 9, 30, 9, 15, 0, 0, ny)) {
		t.Fatalf("got %v %v", got, err)
	}
	if _, err := parseCustomSnooze("2026-09-22 09:15", now); err == nil || !strings.Contains(err.Error(), "in the past") {
		t.Fatalf("past time: %v", err)
	}
	if _, err := parseCustomSnooze("tomorrow", now); err == nil {
		t.Fatal("garbage should be rejected")
	}
	if wakeLabel(time.Date(2026, 9, 24, 8, 0, 0, 0, ny), now) != "Tomorrow 8:00 AM" ||
		wakeLabel(time.Date(2026, 9, 28, 9, 0, 0, 0, ny), now) != "Mon 9:00 AM" ||
		wakeLabel(time.Date(2026, 10, 30, 9, 0, 0, 0, ny), now) != "Oct 30 9:00 AM" {
		t.Fatal("wake labels wrong")
	}
}

// ── Needs You ────────────────────────────────────────────────────────────────

// snoozeFirstPreset opens the picker with Z and picks preset idx.
func snoozeWithPreset(t *testing.T, m Model, idx int) Model {
	t.Helper()
	m, _ = press(t, m, "Z")
	if m.overlay != overlaySnooze {
		t.Fatalf("picker not open: %v, status %q", m.overlay, m.statusMsg)
	}
	for range idx {
		m, _ = press(t, m, "down")
	}
	m, cmd := press(t, m, "enter")
	return settle(t, m, cmd)
}

func TestSnoozeNeedsYouMessageLifecycle(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	a := f.add(f.inbox, "snooze me", 1, "smart needs_reply true")
	f.add(f.inbox, "stays", 2, "smart urgency high")
	m = openNeedsYou(t, m)
	for m.commandMessage().ID != a {
		m, _ = press(t, m, "down")
	}

	m = snoozeWithPreset(t, m, 1) // Tomorrow morning
	if got := listedSubjects(m); fmt.Sprint(got) != "[stays]" || m.needsYou.count != 1 || m.snooze.count != 1 {
		t.Fatalf("after snooze: %v, needs you %d, snoozed %d", got, m.needsYou.count, m.snooze.count)
	}
	if m.messageCursor < 0 || m.messageCursor >= m.activeMessageRowCount() {
		t.Fatal("cursor out of range")
	}
	if anns := mustAnnotations(t, f.database, a); len(anns) != 1 {
		t.Fatal("snooze changed annotations")
	}
	// A plugin reclassifies it while snoozed: stored, still snoozed.
	f.annotate(a, "smart importance high")
	next, cmd := m.Update(pluginAnnotationsRefreshedMsg{MessageID: a, Annotations: mustAnnotations(t, f.database, a)})
	m = settle(t, next.(Model), cmd)
	if m.needsYou.count != 1 || m.snooze.count != 1 {
		t.Fatal("reclassification must not unsnooze")
	}

	// Expiry: the timer fires after its deadline and the current annotations
	// decide.
	clk.set(clk.Now().Add(48 * time.Hour))
	m = settle(t, m, expireSnoozesCmd(m.db))
	if m.snooze.count != 0 || m.needsYou.count != 2 {
		t.Fatalf("after expiry: snoozed %d, needs you %d", m.snooze.count, m.needsYou.count)
	}
	if got := listedSubjects(m); fmt.Sprint(got) != "[stays snooze me]" {
		t.Fatalf("listed = %v (latest annotations: importance, below urgency)", got)
	}
}

func TestSnoozeExpiryKeepsDismissal(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	a := f.add(f.inbox, "dismissed too", 1, "smart needs_reply true")
	m = openNeedsYou(t, m)
	m = snoozeWithPreset(t, m, 0)
	if err := f.database.SetNeedsYouDismissed(a, true); err != nil {
		t.Fatal(err)
	}
	clk.set(clk.Now().Add(48 * time.Hour))
	m = settle(t, m, expireSnoozesCmd(m.db))
	if m.needsYou.count != 0 || m.snooze.count != 0 {
		t.Fatalf("needs you %d, snoozed %d", m.needsYou.count, m.snooze.count)
	}
}

func TestSnoozeMultiSelectOnePicker(t *testing.T) {
	useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	var ids []int64
	for i := range 4 {
		ids = append(ids, f.add(f.inbox, fmt.Sprintf("m%d", i), i+1, "smart needs_reply true"))
	}
	m = openNeedsYou(t, m)
	for _, id := range ids[:3] {
		m.selectedMessages[id] = true
	}
	m, _ = press(t, m, "Z")
	if m.snooze.picker.title != "Snooze 3 selected messages" || len(m.snooze.picker.targets) != 3 {
		t.Fatalf("picker = %q, %d targets", m.snooze.picker.title, len(m.snooze.picker.targets))
	}
	m, cmd := press(t, m, "enter")
	if m.overlay != overlayNone {
		t.Fatal("the picker must not reopen per message")
	}
	m = settle(t, m, cmd)
	snoozes, _ := f.database.ListSnoozes()
	if len(snoozes) != 3 || !snoozes[0].Until.Equal(snoozes[2].Until) {
		t.Fatalf("snoozes = %+v", snoozes)
	}
	if m.needsYou.count != 1 || m.snooze.count != 3 {
		t.Fatalf("needs you %d, snoozed %d", m.needsYou.count, m.snooze.count)
	}
	// Selected rows that left the view are dropped from the selection.
	if len(m.selectedMessages) != 0 {
		t.Fatalf("selection = %v", m.selectedMessages)
	}
}

func TestSnoozePartialFailureSummary(t *testing.T) {
	useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	at := time.Now().Add(time.Hour)
	targets := []snoozeTarget{{db.SnoozeMessage, "<a@x>"}, {"bogus", "<b@x>"}, {db.SnoozeMessage, "<c@x>"}}
	msg := setSnoozesCmd(f.database, targets, at, false)().(snoozeAppliedMsg)
	next, _ := m.Update(msg)
	m = next.(Model)
	if msg.Failed != 1 || !m.statusErr || !strings.Contains(m.statusMsg, "1 of 3 failed") {
		t.Fatalf("msg %+v, status %q", msg, m.statusMsg)
	}
	if snoozes, _ := f.database.ListSnoozes(); len(snoozes) != 2 {
		t.Fatalf("the good targets should stay snoozed: %v", snoozes)
	}
}

// ── Waiting on Them ──────────────────────────────────────────────────────────

func TestSnoozeWaitingThreadLifecycle(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newWaitingModel(t)
	f.add(f.sent, "me@work.example", "Sarah <sarah@x.example>", "<a@x>", "", "Contract", 10)
	f.add(f.sent, "me@work.example", "mike@x.example", "<m@x>", "", "Other", 20)
	m = openWaiting(t, m)
	for m.commandMessage().Subject != "Contract" {
		m, _ = press(t, m, "down")
	}
	if title, _ := m.snoozeTargets(); len(title) == 0 {
		t.Fatal("no targets")
	}
	m = snoozeWithPreset(t, m, 1)
	if got := listedSubjects(m); fmt.Sprint(got) != "[Other]" || m.waiting.count != 1 || m.snooze.count != 1 {
		t.Fatalf("after snooze: %v, waiting %d, snoozed %d", got, m.waiting.count, m.snooze.count)
	}
	if snoozes, _ := f.database.ListSnoozes(); len(snoozes) != 1 || snoozes[0].TargetType != db.SnoozeThread {
		t.Fatalf("snoozes = %+v", snoozes)
	}

	// Expiry brings the still-waiting conversation back.
	clk.set(clk.Now().Add(48 * time.Hour))
	m = settle(t, m, expireSnoozesCmd(m.db))
	if m.waiting.count != 2 || m.snooze.count != 0 {
		t.Fatalf("after expiry: waiting %d, snoozed %d", m.waiting.count, m.snooze.count)
	}
}

func TestSnoozeWaitingEndsOnReplyAndNewCycleShows(t *testing.T) {
	useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newWaitingModel(t)
	f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	m = openWaiting(t, m)
	m = snoozeWithPreset(t, m, 1)
	if m.snooze.count != 1 || m.waiting.count != 0 {
		t.Fatal("setup")
	}

	// Sarah replies: the snooze is cancelled, the conversation is no longer
	// waiting, and her reply can enter Needs You on its own.
	reply := f.add(f.inbox, "sarah@x.example", "me@work.example", "<r@x>", "<a@x>", "Re: Contract", 1)
	if err := f.database.ReplacePluginAnnotations("smart", reply, []db.PluginAnnotation{{Key: "needs_reply", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: f.inbox, NewCount: 1, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if m.snooze.count != 0 || m.waiting.count != 0 || m.needsYou.count != 1 {
		t.Fatalf("after reply: snoozed %d, waiting %d, needs you %d", m.snooze.count, m.waiting.count, m.needsYou.count)
	}
	if snoozes, _ := f.database.ListSnoozes(); len(snoozes) != 0 {
		t.Fatalf("stale snooze kept: %v", snoozes)
	}

	// I answer: a new wait, not hidden by the old snooze.
	f.add(f.sent, "me@work.example", "sarah@x.example", "<b@x>", "<r@x>", "Re: Contract", 0)
	next, cmd = m.Update(MailboxSyncedMsg{MailboxID: f.sent, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if m.waiting.count != 1 {
		t.Fatalf("new cycle waiting = %d", m.waiting.count)
	}
}

func TestSnoozeNewNudgeEndsOldCycleSnooze(t *testing.T) {
	useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newWaitingModel(t)
	f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	m = openWaiting(t, m)
	m = snoozeWithPreset(t, m, 1)
	f.add(f.sent, "me@work.example", "sarah@x.example", "<n@x>", "<a@x>", "Nudge", 1)
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: f.sent, SyncedAt: time.Now()})
	m = settle(t, next.(Model), cmd)
	if m.waiting.count != 1 || m.snooze.count != 0 {
		t.Fatalf("waiting %d, snoozed %d", m.waiting.count, m.snooze.count)
	}
}

func TestStopWaitingSurvivesSnoozeExpiry(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newWaitingModel(t)
	f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	m = openWaiting(t, m)
	if err := f.database.SetSnooze(db.SnoozeThread, "<a@x>", clk.Now().Add(time.Hour)); err != nil {
		t.Fatal(err)
	}
	if err := f.database.SetWaitingStopped([]string{"<a@x>"}, true); err != nil {
		t.Fatal(err)
	}
	clk.set(clk.Now().Add(2 * time.Hour))
	m = settle(t, m, expireSnoozesCmd(m.db))
	if m.waiting.count != 0 || m.snooze.count != 0 {
		t.Fatalf("waiting %d, snoozed %d", m.waiting.count, m.snooze.count)
	}
	if stopped, _ := f.database.WaitingStopped(); !stopped["<a@x>"] {
		t.Fatal("the snooze must not clear Stop waiting")
	}
}

// ── Snoozed view ─────────────────────────────────────────────────────────────

func openSnoozed(t *testing.T, m Model) Model {
	t.Helper()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindSnoozed {
			m.sidebarCursor = i
		}
	}
	m.focused = paneMessages
	return settle(t, m, m.virtualViewCmd())
}

func TestSnoozedViewOrderUnsnoozeAndDeletion(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	late := f.add(f.inbox, "wakes late", 1, "smart needs_reply true")
	early := f.add(f.inbox, "wakes early", 2, "smart needs_reply true")
	gone := f.add(f.inbox, "will be deleted", 3, "smart needs_reply true")
	for id, h := range map[int64]int{late: 30, early: 5, gone: 10} {
		msg, _ := f.database.GetMessage(id)
		if err := f.database.SetSnooze(db.SnoozeMessage, db.MessageKey(msg), clk.Now().Add(time.Duration(h)*time.Hour)); err != nil {
			t.Fatal(err)
		}
	}
	m = openSnoozed(t, m)
	if got := listedSubjects(m); fmt.Sprint(got) != "[wakes early will be deleted wakes late]" || m.snooze.count != 3 {
		t.Fatalf("snoozed view = %v (count %d)", got, m.snooze.count)
	}
	if !strings.Contains(m.renderAccountsPane(), "Snoozed") || !strings.Contains(m.renderAccountsPane(), "(3)") {
		t.Fatal("sidebar entry or count missing")
	}
	if line := rowLine(t, m, "wakes early"); !strings.Contains(line, wakeLabel(clk.Now().Add(5*time.Hour), clk.Now())) {
		t.Fatalf("row should show its wake time: %q", line)
	}

	// Deleted mail disappears safely.
	if err := f.database.DeleteMessage(gone); err != nil {
		t.Fatal(err)
	}
	m = settle(t, m, m.virtualViewCmd())
	if got := listedSubjects(m); fmt.Sprint(got) != "[wakes early wakes late]" || m.snooze.count != 2 {
		t.Fatalf("after delete = %v", got)
	}

	// Z unsnoozes the current row and it returns to Needs You.
	m, cmd := press(t, m, "Z")
	m = settle(t, m, cmd)
	if got := listedSubjects(m); fmt.Sprint(got) != "[wakes late]" || m.needsYou.count != 1 {
		t.Fatalf("after unsnooze = %v, needs you %d", got, m.needsYou.count)
	}
	if m.messageCursor < 0 || m.messageCursor >= m.activeMessageRowCount() {
		t.Fatal("cursor out of range")
	}
	// Why shows when it wakes.
	next, _ := m.executeCommand("snooze-why")
	if view := next.(Model).View(); !strings.Contains(view, "Snoozed from Needs You until") {
		t.Fatal("details missing")
	}
}

func TestSnoozedViewRendersFromCache(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	for i, row := range m.sidebarRows {
		if row.kind == rowKindSnoozed {
			m.sidebarCursor = i
		}
	}
	m.snooze.count = 1
	m.snooze.byMessageID = map[int64]db.Snooze{msgs[0].ID: {TargetType: db.SnoozeMessage, Until: time.Now().Add(time.Hour)}}
	if m.db != nil {
		t.Fatal("expected no database")
	}
	if view := m.View(); !strings.Contains(view, "Snoozed") || !strings.Contains(view, "(1)") {
		t.Fatal("snoozed view should render from the cache")
	}
}

// ── Timer ────────────────────────────────────────────────────────────────────

func TestSnoozeTimerSchedulesNearestAndReschedules(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, _ := newNeedsYouModel(t)
	now := clk.Now()

	first := m.scheduleSnoozeTimer(now.Add(3*time.Hour), true)
	firstDone := make(chan tea.Msg, 1)
	go func() { firstDone <- first() }()
	waitUntil(t, func() bool { return clk.waitCount() == 1 })
	if clk.waits[0] != 3*time.Hour {
		t.Fatalf("waited %v", clk.waits[0])
	}

	// An earlier snooze reschedules: the first wait is cancelled.
	second := m.scheduleSnoozeTimer(now.Add(time.Hour), true)
	if msg := <-firstDone; msg != nil {
		t.Fatalf("replaced wait returned %v", msg)
	}
	secondDone := make(chan tea.Msg, 1)
	go func() { secondDone <- second() }()
	waitUntil(t, func() bool { return clk.waitCount() == 2 })
	if clk.waits[1] != time.Hour {
		t.Fatalf("rescheduled wait = %v", clk.waits[1])
	}
	clk.fire <- struct{}{}
	tick := (<-secondDone).(snoozeTickMsg)

	// An old generation's tick is ignored; the current one expires snoozes.
	if _, cmd := m.handleSnoozeTick(snoozeTickMsg{gen: tick.gen - 1}); cmd != nil {
		t.Fatal("stale tick acted")
	}
	if _, cmd := m.handleSnoozeTick(tick); cmd == nil {
		t.Fatal("current tick should expire snoozes")
	}
	// Only one wait at a time was ever pending: no polling.
	if clk.waitCount() != 2 {
		t.Fatalf("waits = %d", clk.waitCount())
	}

	// Quitting cancels the pending wait.
	third := m.scheduleSnoozeTimer(now.Add(24*time.Hour), true)
	thirdDone := make(chan tea.Msg, 1)
	go func() { thirdDone <- third() }()
	waitUntil(t, func() bool { return clk.waitCount() == 3 })
	m.CloseSessions()
	select {
	case msg := <-thirdDone:
		if msg != nil {
			t.Fatalf("cancelled wait returned %v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("quit did not stop the timer")
	}
	if m.scheduleSnoozeTimer(time.Time{}, false) != nil {
		t.Fatal("no deadline, no timer")
	}
}

func TestSnoozeTickExpiresAllDueAndSchedulesNext(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	for i, h := range []int{1, 1, 5} {
		id := f.add(f.inbox, fmt.Sprintf("m%d", i), i+1, "smart needs_reply true")
		msg, _ := f.database.GetMessage(id)
		_ = f.database.SetSnooze(db.SnoozeMessage, db.MessageKey(msg), clk.Now().Add(time.Duration(h)*time.Hour))
	}
	m = settle(t, m, loadSnoozeStateCmd(m.db))
	if m.snooze.count != 3 || !m.snooze.deadline.Equal(clk.Now().Add(time.Hour)) {
		t.Fatalf("count %d, deadline %v", m.snooze.count, m.snooze.deadline)
	}
	clk.set(clk.Now().Add(90 * time.Minute))
	m = settle(t, m, expireSnoozesCmd(m.db))
	if m.snooze.count != 1 || m.needsYou.count != 2 {
		t.Fatalf("after tick: snoozed %d, needs you %d", m.snooze.count, m.needsYou.count)
	}
	if !m.snooze.deadline.Equal(clk.Now().Add(-90 * time.Minute).Add(5 * time.Hour)) {
		t.Fatalf("next deadline = %v", m.snooze.deadline)
	}
}

func TestStartupExpiresOverdueSnoozes(t *testing.T) {
	clk := useFakeSnoozeClock(t, time.Now().Truncate(time.Minute))
	m, f := newNeedsYouModel(t)
	id := f.add(f.inbox, "overdue", 1, "smart needs_reply true")
	msg, _ := f.database.GetMessage(id)
	// Snoozed until three days ago: TideMail was closed.
	if _, err := f.database.Exec(`INSERT INTO snoozes (target_type, target_key, snooze_until, created_at) VALUES (?, ?, ?, ?)`,
		db.SnoozeMessage, db.MessageKey(msg), clk.Now().Add(-72*time.Hour).Unix(), clk.Now().Add(-96*time.Hour).Unix()); err != nil {
		t.Fatal(err)
	}
	m = settle(t, m, m.Init())
	if m.snooze.count != 0 || m.needsYou.count != 1 {
		t.Fatalf("snoozed %d, needs you %d", m.snooze.count, m.needsYou.count)
	}
}

func TestVirtualViewsKeepOrderWhenThreaded(t *testing.T) {
	m, f := newNeedsYouModel(t)
	f.add(f.inbox, "important but newest", 1, "smart importance high")
	f.add(f.inbox, "urgent but oldest", 90, "smart urgency high")
	m.cfg.Display.ThreadedConversations = true
	m = openNeedsYou(t, m)
	if got := listedSubjects(m); fmt.Sprint(got) != "[urgent but oldest important but newest]" {
		t.Fatalf("list = %v", got)
	}
	if len(m.messageThreads) != 2 || m.messageThreads[0].Representative.Subject != "urgent but oldest" {
		t.Fatal("threaded rows must keep the attention rank, not sort newest-first")
	}
}

func waitUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("timed out")
		}
		time.Sleep(time.Millisecond)
	}
}
