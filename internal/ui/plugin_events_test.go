package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

const eventPerms = "events = [\"message.received\"]\n" + permMetaAndAnnotations

// newEventModel builds a database-backed model whose plugins in root are
// loaded with the event manager running, and stops it after the test.
func newEventModel(t *testing.T, root string, n int) (Model, *db.DB, []db.Message) {
	t.Helper()
	m, database, msgs := newAnnotationDBModel(t, root, n)
	t.Cleanup(m.closePlugins)
	return m, database, msgs
}

// nextEvent waits for the event manager's next finished run and feeds it
// through Update, returning the model and the follow-up command.
func nextEvent(t *testing.T, m Model) (Model, tea.Cmd, plugin.EventUpdate) {
	t.Helper()
	listen := m.pluginEventListenCmd()
	if listen == nil {
		t.Fatal("no event listener; is the event manager running?")
	}
	got := make(chan tea.Msg, 1)
	go func() { got <- listen() }()
	select {
	case msg := <-got:
		ev, ok := msg.(pluginEventMsg)
		if !ok {
			t.Fatalf("listener returned %T", msg)
		}
		next, cmd := m.Update(ev)
		return next.(Model), cmd, ev.Update
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for a plugin event")
	}
	return m, nil, plugin.EventUpdate{}
}

// collect runs the commands inside cmd (a batch or single command) and
// returns the messages that finish within a short time. Long-lived commands,
// like the event listener and status timers, are left behind.
func collect(cmd tea.Cmd) []tea.Msg {
	var cmds []tea.Cmd
	if cmd == nil {
		return nil
	}
	if batch, ok := cmd().(tea.BatchMsg); ok {
		cmds = batch
	} else {
		return nil
	}
	results := make(chan tea.Msg, len(cmds))
	for _, c := range cmds {
		if c == nil {
			continue
		}
		go func(c tea.Cmd) { results <- c() }(c)
	}
	var out []tea.Msg
	deadline := time.After(500 * time.Millisecond)
	for {
		select {
		case msg := <-results:
			out = append(out, msg)
		case <-deadline:
			return out
		}
	}
}

func synced(mailboxID int64, cold bool, news ...db.Message) MailboxSyncedMsg {
	return MailboxSyncedMsg{MailboxID: mailboxID, NewCount: len(news), NewMessages: news, Cold: cold, SyncedAt: time.Now()}
}

func enableAuto(t *testing.T, m Model, id string) Model {
	t.Helper()
	m.setAutoEvents(id, true)
	if !m.cfg.PluginAutoEvents(id) || !m.plugins.eventStatus[id].Enabled {
		t.Fatalf("enable failed: status %q", m.statusMsg)
	}
	return m
}

func TestAutoEventsDefaultOffAndOnlyForDeclaringPlugins(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	installDataPlugin(t, root, "manual", twoAnnotations, permMetaAndAnnotations) // no events
	m, _, msgs := newEventModel(t, root, 1)

	if st := m.plugins.eventStatus["smart"]; st.Enabled {
		t.Fatal("automatic events must start disabled")
	}
	if _, ok := m.plugins.eventStatus["manual"]; ok {
		t.Fatal("a plugin without message.received must not get a worker")
	}
	next, _ := m.Update(synced(msgs[0].MailboxID, false, msgs[0]))
	m = next.(Model)
	if st := m.plugins.eventStatus["smart"]; st.Queued != 0 || st.Running || st.Dropped != 0 {
		t.Fatalf("disabled plugin received work: %+v", st)
	}
}

func TestAutoEventStoresAnnotationsAndUpdatesBadgeLive(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	m, database, msgs := newEventModel(t, root, 2)
	m.cfg.Display.Icons = true
	m = enableAuto(t, m, "smart")

	next, _ := m.Update(synced(msgs[0].MailboxID, false, msgs[0]))
	m = next.(Model)
	m, cmd, u := nextEvent(t, m)
	if u.Err != nil || u.Failed || u.MessageID != msgs[0].ID || u.Result.Outcome != plugin.AnnotationsStored {
		t.Fatalf("update = %+v", u)
	}
	if got := storedKV(t, database, msgs[0].ID); fmt.Sprint(got) != "[smart:needs_reply=true smart:urgency=high]" {
		t.Fatalf("stored = %v", got)
	}

	// The badge arrives through a Bubble Tea message, not a mailbox reload.
	var refreshed bool
	for _, msg := range collect(cmd) {
		if r, ok := msg.(pluginAnnotationsRefreshedMsg); ok {
			next, _ = m.Update(r)
			m = next.(Model)
			refreshed = true
		}
	}
	if !refreshed {
		t.Fatal("no pluginAnnotationsRefreshedMsg followed the stored run")
	}
	if line := rowLine(t, m, "msg 1"); !strings.Contains(line, "↩ !") {
		t.Fatalf("row = %q", line)
	}
	if line := rowLine(t, m, "msg 2"); strings.Contains(line, "↩") {
		t.Fatalf("another row changed: %q", line)
	}
}

func TestAutoEventsIgnoreLoadsBackfillAndColdSyncs(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	m, _, msgs := newEventModel(t, root, 3)
	m = enableAuto(t, m, "smart")

	steps := []tea.Msg{
		// Opening a mailbox or startup reload of cached mail.
		MessagesLoadedMsg{MailboxID: msgs[0].MailboxID, Messages: msgs},
		MessagesLoadedMsg{MailboxID: 0, Messages: msgs},
		// Paging further back into history.
		OlderMessagesLoadedMsg{MailboxID: msgs[0].MailboxID, Count: 3},
		// First sync of a mailbox or a cache rebuild: a history page.
		synced(msgs[0].MailboxID, true, msgs...),
		// A sync that failed.
		MailboxSyncedMsg{MailboxID: msgs[0].MailboxID, NewMessages: msgs, Err: fmt.Errorf("network down")},
	}
	for _, step := range steps {
		next, _ := m.Update(step)
		m = next.(Model)
	}
	time.Sleep(100 * time.Millisecond)
	m.refreshPluginEventStatus()
	if st := m.plugins.eventStatus["smart"]; st.Queued != 0 || st.Running || st.Dropped != 0 {
		t.Fatalf("non-arrival paths queued work: %+v", st)
	}
	if n, _ := m.db.PluginAnnotationCounts(); n["smart"] != 0 {
		t.Fatalf("plugin ran: %v", n)
	}
}

func TestAutoEventsDedupeRepeatedArrival(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	// Make the plugin slow so the second report arrives while it runs.
	slow := "#!/bin/sh\nsleep 0.4\nexec \"$(dirname \"$0\")/real\"\n"
	dir := filepath.Join(root, "smart")
	if err := os.Rename(filepath.Join(dir, "run"), filepath.Join(dir, "real")); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(slow), 0o755); err != nil {
		t.Fatal(err)
	}
	m, _, msgs := newEventModel(t, root, 1)
	m = enableAuto(t, m, "smart")

	for range 3 {
		next, _ := m.Update(synced(msgs[0].MailboxID, false, msgs[0]))
		m = next.(Model)
	}
	if st := m.plugins.eventStatus["smart"]; st.Queued+boolInt(st.Running) != 1 || st.Dropped != 0 {
		t.Fatalf("the same message was queued more than once: %+v", st)
	}
	m, _, _ = nextEvent(t, m)
	time.Sleep(100 * time.Millisecond)
	m.refreshPluginEventStatus()
	if st := m.plugins.eventStatus["smart"]; st.Queued != 0 || st.Running {
		t.Fatalf("extra runs after dedupe: %+v", st)
	}
}

func boolInt(b bool) int {
	if b {
		return 1
	}
	return 0
}

func TestAutoToggleNeedsConfirmationAndPersists(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	m, _, _ := newEventModel(t, root, 1)
	m = openPluginList(t, m)
	view := m.View()
	for _, want := range []string{"events: message.received", "auto: disabled", "auto on/off"} {
		if !strings.Contains(view, want) {
			t.Errorf("plugin list missing %q", want)
		}
	}

	m, _ = press(t, m, "a")
	if m.overlay != overlayPluginConfirm || m.plugins.confirm == nil || m.plugins.confirm.kind != enableAutoEvents {
		t.Fatalf("overlay = %v", m.overlay)
	}
	if !strings.Contains(m.View(), "Message bodies are not sent.") {
		t.Fatal("confirmation must explain what the plugin receives")
	}
	m, _ = press(t, m, "n")
	if m.cfg.PluginAutoEvents("smart") {
		t.Fatal("cancel must not enable")
	}

	m, _ = press(t, m, "a")
	m, _ = press(t, m, "y")
	if m.overlay != overlayPlugins || !m.cfg.PluginAutoEvents("smart") || !m.plugins.eventStatus["smart"].Enabled {
		t.Fatalf("enable did not apply: overlay %v", m.overlay)
	}
	saved, err := config.Load()
	if err != nil || !saved.PluginAutoEvents("smart") {
		t.Fatalf("setting not persisted: %v", err)
	}
	if view := m.View(); !strings.Contains(view, "auto: enabled") || !strings.Contains(view, "queued: 0") {
		t.Fatal("enabled plugin should show its queue state")
	}

	// Turning it off is immediate.
	m, _ = press(t, m, "a")
	if m.overlay != overlayPlugins || m.cfg.PluginAutoEvents("smart") || m.plugins.eventStatus["smart"].Enabled {
		t.Fatal("disable should apply without confirmation")
	}
	if saved, _ := config.Load(); saved.PluginAutoEvents("smart") {
		t.Fatal("disable not persisted")
	}
}

func TestAutoToggleIgnoredForPluginsWithoutEvents(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "manual", twoAnnotations, permMetaAndAnnotations)
	m, _, _ := newEventModel(t, root, 1)
	m = openPluginList(t, m)
	if view := m.View(); strings.Contains(view, "events:") || strings.Contains(view, "auto on/off") {
		t.Fatal("plugins without events should not show event controls")
	}
	m, cmd := press(t, m, "a")
	if cmd != nil || m.overlay != overlayPlugins || m.cfg.PluginAutoEvents("manual") {
		t.Fatal("a must do nothing for a plugin without events")
	}
}

func TestAutoFailuresPauseVisiblyAndResume(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", "", eventPerms) // exits 1 every time
	m, _, msgs := newEventModel(t, root, plugin.EventFailureLimit+2)
	m = enableAuto(t, m, "smart")

	next, _ := m.Update(synced(msgs[0].MailboxID, false, msgs[:plugin.EventFailureLimit]...))
	m = next.(Model)
	for range plugin.EventFailureLimit {
		m, _, _ = nextEvent(t, m)
	}
	st := m.plugins.eventStatus["smart"]
	if !st.Paused || st.ConsecutiveFailures != plugin.EventFailureLimit {
		t.Fatalf("status = %+v", st)
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "paused after") {
		t.Fatalf("status line = %q", m.statusMsg)
	}

	// New mail while paused is refused and counted.
	next, _ = m.Update(synced(msgs[0].MailboxID, false, msgs[plugin.EventFailureLimit:]...))
	m = next.(Model)
	if st := m.plugins.eventStatus["smart"]; st.Dropped != 2 || st.Queued != 0 {
		t.Fatalf("paused plugin: %+v", st)
	}

	m = openPluginList(t, m)
	view := m.View()
	for _, want := range []string{"auto: paused", "reason: plugin \"smart\" failed", "dropped: 2", "resume"} {
		if !strings.Contains(view, want) {
			t.Errorf("plugin list missing %q", want)
		}
	}
	m, _ = press(t, m, "r")
	st = m.plugins.eventStatus["smart"]
	if st.Paused || st.ConsecutiveFailures != 0 || st.Dropped != 2 {
		t.Fatalf("after resume: %+v", st)
	}
	if m.overlay != overlayPlugins {
		t.Fatal("r on a paused plugin should resume, not open the last result")
	}
}

func TestManualRunDoesNotTouchAutoCounters(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", "", eventPerms) // fails
	m, _, _ := newEventModel(t, root, 1)
	m = enableAuto(t, m, "smart")
	m = runPlugin(t, m, "smart")
	if !m.statusErr {
		t.Fatalf("manual failure should be reported: %q", m.statusMsg)
	}
	m.refreshPluginEventStatus()
	if st := m.plugins.eventStatus["smart"]; st.ConsecutiveFailures != 0 || st.Paused {
		t.Fatalf("manual run changed automatic state: %+v", st)
	}
}

func TestManualRunWorksWhileAutoPaused(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", "", eventPerms) // fails for now
	m, database, msgs := newEventModel(t, root, plugin.EventFailureLimit)
	m = enableAuto(t, m, "smart")
	next, _ := m.Update(synced(msgs[0].MailboxID, false, msgs...))
	m = next.(Model)
	for range plugin.EventFailureLimit {
		m, _, _ = nextEvent(t, m)
	}
	if !m.plugins.eventStatus["smart"].Paused {
		t.Fatal("setup: plugin should be paused")
	}

	// Fix the plugin, then run it by hand while automatic mode stays paused.
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	m = runPlugin(t, m, "smart")
	target := m.plugins.result
	if target == nil || !strings.Contains(target.annotationNote, "2 stored") {
		t.Fatalf("manual run while paused failed: status %q", m.statusMsg)
	}
	stored := 0
	for _, msg := range msgs {
		stored += len(storedKV(t, database, msg.ID))
	}
	if stored != 2 {
		t.Fatalf("stored %d annotations", stored)
	}
	m.refreshPluginEventStatus()
	if st := m.plugins.eventStatus["smart"]; !st.Paused || st.ConsecutiveFailures != plugin.EventFailureLimit {
		t.Fatalf("manual success must not resume or reset automatic state: %+v", st)
	}
}

func TestQuitStopsEventWorkers(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "smart", twoAnnotations, eventPerms)
	m, _, _ := newEventModel(t, root, 1)
	listen := m.pluginEventListenCmd()
	start := time.Now()
	m.CloseSessions()
	if elapsed := time.Since(start); elapsed > 3*time.Second {
		t.Fatalf("shutdown took %v", elapsed)
	}
	done := make(chan tea.Msg, 1)
	go func() { done <- listen() }()
	select {
	case msg := <-done:
		if msg != nil {
			t.Fatalf("listener returned %T after shutdown", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("event listener kept waiting after shutdown")
	}
}

func TestNewMessageIDsComeFromTheDatabase(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()
	accountID, _ := database.AddAccount("", "Personal", "")
	mailboxID, _ := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	fetched := []db.Message{{UID: 7, Subject: "hello"}, {UID: 8, Subject: "seen", Read: true}}
	news, err := storeFetchedMessages(database, mailboxID, fetched)
	if err != nil {
		t.Fatal(err)
	}
	if len(news) != 1 || news[0].ID == 0 {
		t.Fatalf("new messages = %+v", news)
	}
	want, _ := database.MessageIDByUID(mailboxID, 7)
	if news[0].ID != want {
		t.Fatalf("ID = %d, want %d", news[0].ID, want)
	}
	// Re-fetching the same UID is not new again.
	if again, _ := storeFetchedMessages(database, mailboxID, fetched); len(again) != 0 {
		t.Fatalf("re-fetch reported new mail: %+v", again)
	}
}
