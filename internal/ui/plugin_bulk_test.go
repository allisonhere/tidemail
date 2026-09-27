package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// installLoggingPlugin writes a plugin that appends every request to
// requests.log, fails for subjects containing "BAD" (with a hostile stderr),
// and otherwise answers with a needs_reply annotation.
func installLoggingPlugin(t *testing.T, root, id, permissions string) string {
	t.Helper()
	installDataPlugin(t, root, id, `{}`, permissions)
	script := `#!/bin/sh
read -r line
printf '%s\n' "$line" >> requests.log
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
case "$line" in
*BAD*) printf 'boom \033[31m\n' >&2; exit 1;;
esac
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":{"annotations":[{"key":"needs_reply","value":"true"}]}}\n' "$id"
`
	dir := filepath.Join(root, id)
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// loggedRequests returns the requests a logging plugin received.
func loggedRequests(t *testing.T, dir string) []string {
	t.Helper()
	data, err := os.ReadFile(filepath.Join(dir, "requests.log"))
	if os.IsNotExist(err) {
		return nil
	}
	if err != nil {
		t.Fatal(err)
	}
	return strings.Split(strings.TrimSpace(string(data)), "\n")
}

// requestedIDs extracts the message IDs a plugin was asked about.
func requestedIDs(t *testing.T, dir string) []int64 {
	t.Helper()
	var ids []int64
	for _, line := range loggedRequests(t, dir) {
		i := strings.Index(line, `"data":{"id":`)
		if i < 0 {
			t.Fatalf("unexpected request %q", line)
		}
		var id int64
		fmt.Sscanf(line[i+len(`"data":{"id":`):], "%d", &id)
		ids = append(ids, id)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	return ids
}

// pickPlugin opens the run command and chooses pluginID, returning the model
// and the command the choice produced.
func pickPlugin(t *testing.T, m Model, pluginID string) (Model, func() Model) {
	t.Helper()
	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	if m.overlay != overlayPluginPicker {
		t.Fatalf("picker not open: overlay %v, status %q", m.overlay, m.statusMsg)
	}
	for i, p := range m.plugins.picker {
		if p.Manifest.ID == pluginID {
			m.plugins.pickerCursor = i
		}
	}
	m, cmd := press(t, m, "enter")
	return m, func() Model { return settle(t, m, cmd) }
}

func selectIDs(m *Model, msgs []db.Message, idx ...int) []int64 {
	var ids []int64
	for _, i := range idx {
		m.selectedMessages[msgs[i].ID] = true
		ids = append(ids, msgs[i].ID)
	}
	sort.Slice(ids, func(a, b int) bool { return ids[a] < ids[b] })
	return ids
}

func TestPluginRunTargets(t *testing.T) {
	root := t.TempDir()
	dir := installLoggingPlugin(t, root, "smart", permMetaAndAnnotations)
	m, _, msgs := newEventModel(t, root, 5)

	// No selection: the current message, with the familiar label.
	if m.pluginRunLabel() != "Run plugin on current message…" {
		t.Fatalf("label = %q", m.pluginRunLabel())
	}
	current := m.commandMessage().ID
	_, run := pickPlugin(t, m, "smart")
	m = run()
	if got := requestedIDs(t, dir); fmt.Sprint(got) != fmt.Sprint([]int64{current}) {
		t.Fatalf("no selection ran %v, want current %d", got, current)
	}
	if m.overlay != overlayPluginResult {
		t.Fatal("a single run still opens its result")
	}
	_ = os.Remove(filepath.Join(dir, "requests.log"))
	m.overlay = overlayNone

	// One selected message (not the cursor row) runs that message.
	want := selectIDs(&m, msgs, 3)
	if m.pluginRunLabel() != "Run plugin on selected message…" {
		t.Fatalf("label = %q", m.pluginRunLabel())
	}
	_, run = pickPlugin(t, m, "smart")
	m = run()
	if got := requestedIDs(t, dir); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("one selected ran %v, want %v", got, want)
	}
}

func TestBulkRunsEverySelectedMessageOnce(t *testing.T) {
	root := t.TempDir()
	dir := installLoggingPlugin(t, root, "smart", permMetaAndAnnotations)
	other := installLoggingPlugin(t, root, "other", permMetaAndAnnotations)
	m, database, msgs := newEventModel(t, root, 5)
	if _, err := database.Exec(`UPDATE messages SET body_text = 'SECRET BODY', body_html = '<p>SECRET HTML</p>'`); err != nil {
		t.Fatal(err)
	}
	want := selectIDs(&m, msgs, 0, 2, 4)
	if m.pluginRunLabel() != "Run plugin on 3 selected messages…" {
		t.Fatalf("label = %q", m.pluginRunLabel())
	}

	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	if !strings.Contains(m.View(), "3 selected messages") {
		t.Fatal("the picker should say how many messages it will run on")
	}
	for i, p := range m.plugins.picker {
		if p.Manifest.ID == "other" {
			m.plugins.pickerCursor = i
		}
	}
	m, cmd := press(t, m, "enter")
	if m.overlay == overlayPluginPicker {
		t.Fatal("the picker must not reopen per message")
	}
	m = settle(t, m, cmd)

	// The chosen plugin, and only it, ran once per selected message.
	if got := requestedIDs(t, other); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("other ran %v, want %v", got, want)
	}
	if got := loggedRequests(t, dir); len(got) != 0 {
		t.Fatalf("the unchosen plugin ran: %v", got)
	}
	for _, line := range loggedRequests(t, other) {
		if strings.Contains(line, "SECRET") || !strings.Contains(line, `"method":"message.metadata"`) {
			t.Fatalf("request = %s", line)
		}
	}
	// Annotations were stored for each, and the selection is intact.
	for _, id := range want {
		if len(m.plugins.annotations[id]) != 1 {
			t.Fatalf("message %d has no cached annotation", id)
		}
	}
	if len(m.selectedMessages) != 3 {
		t.Fatalf("selection changed: %v", m.selectedMessages)
	}
	if !strings.Contains(m.statusMsg, "3 succeeded") || m.overlay != overlayPluginResult {
		t.Fatalf("status %q, overlay %v", m.statusMsg, m.overlay)
	}
	if m.plugins.running != "" || m.plugins.bulk != nil {
		t.Fatal("the run should be finished")
	}
}

func TestBulkPermissionsApplyToEveryMessage(t *testing.T) {
	root := t.TempDir()
	installLoggingPlugin(t, root, "noperm", "")
	metaOnly := installLoggingPlugin(t, root, "metaonly", permMetaOnly)
	m, database, msgs := newEventModel(t, root, 3)
	ids := selectIDs(&m, msgs, 0, 1, 2)
	m, run := pickPlugin(t, m, "metaonly")
	for _, p := range m.plugins.picker {
		if p.Manifest.ID == "noperm" {
			t.Fatal("a plugin without message_metadata must not be offered")
		}
	}
	m = run()
	if got := requestedIDs(t, metaOnly); len(got) != 3 {
		t.Fatalf("ran %v", got)
	}
	for _, id := range ids {
		if got := storedKV(t, database, id); len(got) != 0 {
			t.Fatalf("stored without the annotations permission: %v", got)
		}
	}
}

func TestBulkContinuesPastFailuresAndSummarizes(t *testing.T) {
	root := t.TempDir()
	dir := installLoggingPlugin(t, root, "smart", "events = [\"message.received\"]\n"+permMetaAndAnnotations)
	m, database, msgs := newEventModel(t, root, 4)
	if _, err := database.Exec(`UPDATE messages SET subject = 'BAD one' WHERE id = ?`, msgs[1].ID); err != nil {
		t.Fatal(err)
	}
	m.messages[1].Subject = "BAD one"
	m.applyFilter()
	m = enableAuto(t, m, "smart")
	selectIDs(&m, msgs, 0, 1, 2, 3)
	_, run := pickPlugin(t, m, "smart")
	m = run()

	if got := requestedIDs(t, dir); len(got) != 4 {
		t.Fatalf("one failure stopped the run: %v", got)
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "3 succeeded") || !strings.Contains(m.statusMsg, "1 failed") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	body := m.plugins.result.body
	if !strings.Contains(body, "Failures:") || !strings.Contains(body, "BAD one") || strings.ContainsRune(body, 0x1b) {
		t.Fatalf("summary = %q", body)
	}
	// Manual failures never count against automatic processing.
	m.refreshPluginEventStatus()
	if st := m.plugins.eventStatus["smart"]; st.ConsecutiveFailures != 0 || st.Paused {
		t.Fatalf("automatic state changed: %+v", st)
	}
}

func TestBulkUpdatesCachePerMessageAndNeedsYou(t *testing.T) {
	root := t.TempDir()
	installLoggingPlugin(t, root, "smart", permMetaAndAnnotations)
	m, _, msgs := newEventModel(t, root, 6)
	selectIDs(&m, msgs, 0, 1, 2, 3, 4, 5)
	m, cmd := pickPluginNoRun(t, m, "smart")

	// Step through the run by hand: the first finished message is cached
	// before the rest have run.
	step := cmd()
	next, cmd := m.Update(step)
	m = next.(Model)
	if m.plugins.bulk == nil || m.plugins.bulk.done != 1 {
		t.Fatalf("bulk = %+v", m.plugins.bulk)
	}
	cached := 0
	for _, anns := range m.plugins.annotations {
		cached += len(anns)
	}
	if cached != 1 {
		t.Fatalf("cached after one step = %d", cached)
	}
	if !strings.Contains(m.statusMsg, "1 / 6") {
		t.Fatalf("progress = %q", m.statusMsg)
	}
	m = settle(t, m, cmd)
	if m.needsYou.count != 6 {
		t.Fatalf("Needs You count = %d, want 6", m.needsYou.count)
	}
}

// pickPluginNoRun chooses a plugin and returns the first step's command
// without running it.
func pickPluginNoRun(t *testing.T, m Model, pluginID string) (Model, tea.Cmd) {
	t.Helper()
	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	for i, p := range m.plugins.picker {
		if p.Manifest.ID == pluginID {
			m.plugins.pickerCursor = i
		}
	}
	return press(t, m, "enter")
}

func TestLargeSelectionConfirmsOnce(t *testing.T) {
	root := t.TempDir()
	dir := installLoggingPlugin(t, root, "smart", permMetaAndAnnotations)
	m, _, msgs := newEventModel(t, root, bulkConfirmThreshold)
	all := make([]int, bulkConfirmThreshold)
	for i := range all {
		all[i] = i
	}
	selectIDs(&m, msgs, all...)

	// Cancel: nothing runs.
	_, run := pickPlugin(t, m, "smart")
	m = run()
	if m.overlay != overlayPluginConfirm || !strings.Contains(m.View(), fmt.Sprintf("Run Plugin smart on %d selected messages", bulkConfirmThreshold)) {
		t.Fatalf("overlay = %v", m.overlay)
	}
	if !strings.Contains(m.View(), "never message bodies") {
		t.Fatal("the confirmation should say what is sent")
	}
	m, cmd := press(t, m, "n")
	m = settle(t, m, cmd)
	if len(loggedRequests(t, dir)) != 0 || m.plugins.running != "" || m.overlay != overlayNone {
		t.Fatal("cancelling must launch nothing")
	}

	// Confirm: one question, then every message runs.
	_, run = pickPlugin(t, m, "smart")
	m = run()
	m, cmd = press(t, m, "y")
	m = settle(t, m, cmd)
	if got := loggedRequests(t, dir); len(got) != bulkConfirmThreshold {
		t.Fatalf("ran %d, want %d", len(got), bulkConfirmThreshold)
	}
	if m.overlay != overlayPluginResult {
		t.Fatalf("overlay = %v", m.overlay)
	}
}

func TestSmallSelectionDoesNotConfirm(t *testing.T) {
	root := t.TempDir()
	installLoggingPlugin(t, root, "smart", permMetaAndAnnotations)
	m, _, msgs := newEventModel(t, root, 3)
	selectIDs(&m, msgs, 0, 1)
	m, _ = pickPlugin(t, m, "smart")
	if m.overlay == overlayPluginConfirm {
		t.Fatal("two messages should not ask for confirmation")
	}
}

func TestSelectionPrunedWhenRowsLeaveNeedsYou(t *testing.T) {
	m, f := newNeedsYouModel(t)
	a := f.add(f.inbox, "stays", 1, "p needs_reply true")
	b := f.add(f.inbox, "leaves", 2, "p needs_reply true")
	m = openNeedsYou(t, m)
	m.selectedMessages[a] = true
	m.selectedMessages[b] = true

	if err := f.database.DeletePluginAnnotations("p", b); err != nil {
		t.Fatal(err)
	}
	m = settle(t, m, m.needsYouRefreshCmd())
	if len(m.selectedMessages) != 1 || !m.selectedMessages[a] {
		t.Fatalf("selection = %v", m.selectedMessages)
	}
	if !strings.Contains(m.messagesPaneTitle(), "✓ 1 selected") {
		t.Fatalf("title = %q", m.messagesPaneTitle())
	}
	if got := m.pluginTargets(); len(got) != 1 || got[0].ID != a {
		t.Fatalf("targets = %v", got)
	}
}
