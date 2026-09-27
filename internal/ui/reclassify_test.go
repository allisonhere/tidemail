package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

// installRulesPlugin writes a plugin whose answer the test controls: it logs
// every request, fails for subjects containing BAD (with a hostile stderr),
// hangs for HANG (recording its pid), returns no annotations for EMPTY, and
// otherwise returns the annotation array in answer.json (none if missing).
func installRulesPlugin(t *testing.T, root, id, permissions string) string {
	t.Helper()
	installDataPlugin(t, root, id, `{}`, permissions)
	script := `#!/bin/sh
read -r line
printf '%s\n' "$line" >> requests.log
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
case "$line" in
*BAD*) printf 'boom \033[31m\n' >&2; exit 1;;
*HANG*) echo $$ > pid; exec sleep 30;;
*EMPTY*) anns='[]';;
*) anns=$(cat answer.json 2>/dev/null || printf '[]');;
esac
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":{"annotations":%s}}\n' "$id" "$anns"
`
	dir := filepath.Join(root, id)
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return dir
}

// setAnswer sets the annotations a rules plugin returns from now on.
func setAnswer(t *testing.T, dir, anns string) {
	t.Helper()
	if err := os.WriteFile(filepath.Join(dir, "answer.json"), []byte(anns), 0o644); err != nil {
		t.Fatal(err)
	}
}

// setSubject renames a message in the database and the loaded list.
func setSubject(t *testing.T, m *Model, database *db.DB, id int64, subject string) {
	t.Helper()
	if _, err := database.Exec(`UPDATE messages SET subject = ? WHERE id = ?`, subject, id); err != nil {
		t.Fatal(err)
	}
	for i := range m.messages {
		if m.messages[i].ID == id {
			m.messages[i].Subject = subject
		}
	}
	m.applyFilter()
}

func store(t *testing.T, database *db.DB, pluginID string, id int64, kv ...string) {
	t.Helper()
	var anns []db.PluginAnnotation
	for i := 0; i+1 < len(kv); i += 2 {
		anns = append(anns, db.PluginAnnotation{Key: kv[i], Value: kv[i+1]})
	}
	if err := database.ReplacePluginAnnotations(pluginID, id, anns); err != nil {
		t.Fatal(err)
	}
}

// storedFor lists one plugin's stored key=value pairs on a message, sorted.
func storedFor(t *testing.T, database *db.DB, pluginID string, id int64) string {
	t.Helper()
	anns, err := database.ListPluginAnnotations(id)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range anns {
		if a.PluginID == pluginID {
			out = append(out, a.Key+"="+a.Value)
		}
	}
	return strings.Join(sortedStrings(out), ",")
}

func sortedStrings(s []string) []string {
	out := append([]string(nil), s...)
	for i := 1; i < len(out); i++ {
		for j := i; j > 0 && out[j] < out[j-1]; j-- {
			out[j], out[j-1] = out[j-1], out[j]
		}
	}
	return out
}

// pickView opens "Reclassify all … in <view>" and chooses pluginID. It
// returns the model on the confirmation.
func pickView(t *testing.T, m Model, pluginID string) Model {
	t.Helper()
	next, _ := m.executeCommand("plugin-reclassify-view")
	m = next.(Model)
	if m.overlay != overlayPluginPicker {
		t.Fatalf("picker not open: overlay %v, status %q", m.overlay, m.statusMsg)
	}
	for i, p := range m.plugins.picker {
		if p.Manifest.ID == pluginID {
			m.plugins.pickerCursor = i
		}
	}
	m, _ = press(t, m, "enter")
	if m.overlay != overlayPluginConfirm {
		t.Fatalf("a whole-view run must confirm first: overlay %v", m.overlay)
	}
	return m
}

// reclassifyView runs pluginID over the current view to completion.
func reclassifyView(t *testing.T, m Model, pluginID string) Model {
	t.Helper()
	m = pickView(t, m, pluginID)
	m, cmd := press(t, m, "y")
	return settle(t, m, cmd)
}

// bulkStep runs cmd until it yields the next bulk step, then feeds it back.
func bulkStep(t *testing.T, m Model, cmd tea.Cmd) (Model, tea.Cmd, bool) {
	t.Helper()
	if cmd == nil {
		return m, nil, false
	}
	msgs := []tea.Msg{cmd()}
	if batch, ok := msgs[0].(tea.BatchMsg); ok {
		msgs = nil
		for _, c := range batch {
			if c != nil {
				msgs = append(msgs, c())
			}
		}
	}
	for _, msg := range msgs {
		if step, ok := msg.(pluginBulkStepMsg); ok {
			next, follow := m.Update(step)
			return next.(Model), follow, true
		}
	}
	return m, nil, false
}

func commandLabel(m Model, id string) string {
	for _, item := range m.commandItems() {
		if item.id == id {
			return item.label
		}
	}
	return ""
}

// Cases 1, 8, 15, 16: the current message goes through the single-message
// path; its set is replaced, and the result says whether it changed.
func TestReclassifyCurrentMessage(t *testing.T) {
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "rules", permMetaAndAnnotations)
	m, database, _ := newEventModel(t, root, 2)
	current := m.commandMessage().ID
	store(t, database, "rules", current, "needs_reply", "true")
	store(t, database, "other", current, "category", "github")
	setAnswer(t, dir, `[{"key":"urgency","value":"high"}]`)

	if got := commandLabel(m, "plugin-run"); got != "Reclassify current message…" {
		t.Fatalf("label = %q", got)
	}
	m = runPlugin(t, m, "rules")
	if got := requestedIDs(t, dir); fmt.Sprint(got) != fmt.Sprint([]int64{current}) {
		t.Fatalf("ran %v", got)
	}
	if got := storedFor(t, database, "rules", current); got != "urgency=high" {
		t.Fatalf("stored %q; a successful run replaces the plugin's set", got)
	}
	if got := storedFor(t, database, "other", current); got != "category=github" {
		t.Fatalf("other plugin's annotations changed: %q", got)
	}
	if r := m.plugins.result; r == nil || !strings.Contains(r.annotationNote, "classification changed") {
		t.Fatalf("result = %+v", m.plugins.result)
	}
	if !strings.Contains(m.statusMsg, "classification changed") {
		t.Fatalf("status = %q", m.statusMsg)
	}

	m.overlay = overlayNone
	m = runPlugin(t, m, "rules")
	if r := m.plugins.result; r == nil || !strings.Contains(r.annotationNote, "classification unchanged") {
		t.Fatalf("second run: %+v", m.plugins.result)
	}
}

// Cases 3–6, 8–17, 24, 26: the whole view, confirmed with its count, runs
// once per loaded row with one plugin choice, and the summary splits the
// results.
func TestReclassifyViewCountsAndSemantics(t *testing.T) {
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "rules", permMetaAndAnnotations)
	installRulesPlugin(t, root, "noperm", "")
	m, database, msgs := newEventModel(t, root, 4)
	if _, err := database.Exec(`UPDATE messages SET body_text = '', body_html = ''`); err != nil {
		t.Fatal(err)
	}
	same, differs, bad, empty := msgs[0].ID, msgs[1].ID, msgs[2].ID, msgs[3].ID
	setSubject(t, &m, database, bad, "BAD \x1b[31minvoice")
	setSubject(t, &m, database, empty, "EMPTY")
	store(t, database, "rules", same, "category", "billing", "urgency", "high")
	store(t, database, "rules", differs, "needs_reply", "true")
	store(t, database, "rules", bad, "needs_reply", "true")
	store(t, database, "rules", empty, "needs_reply", "true")
	// Same classification in another order, case and spacing, with a
	// confidence: unchanged.
	setAnswer(t, dir, `[{"key":"urgency","value":" High ","confidence":0.7},{"key":"category","value":"billing"}]`)

	if got := commandLabel(m, "plugin-reclassify-view"); got != "Reclassify all 4 messages in INBOX…" {
		t.Fatalf("view label = %q", got)
	}
	next, _ := m.executeCommand("plugin-reclassify-view")
	m = next.(Model)
	for _, p := range m.plugins.picker {
		if p.Manifest.ID == "noperm" {
			t.Fatal("plugins without message_metadata must not be offered")
		}
	}
	if !strings.Contains(m.View(), "Reclassify 4 messages in INBOX") {
		t.Fatal("the picker should state the scope")
	}
	m.overlay = overlayNone
	m.plugins.picker = nil

	m = pickView(t, m, "rules")
	view := m.View()
	for _, want := range []string{"Reclassify 4 messages in INBOX with Plugin rules?", "Existing annotations from this plugin may be", "never message bodies"} {
		if !strings.Contains(strings.Join(strings.Fields(view), " "), want) {
			t.Fatalf("confirmation lacks %q", want)
		}
	}
	if len(loggedRequests(t, dir)) != 0 {
		t.Fatal("nothing may run before confirmation")
	}

	// Step through, watching progress.
	m, cmd := press(t, m, "y")
	if !strings.Contains(m.statusMsg, "Plugin rules: Reclassifying 4 messages in INBOX… 0 / 4") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	for i := 1; i < 4; i++ {
		var ok bool
		m, cmd, ok = bulkStep(t, m, cmd)
		if !ok {
			t.Fatalf("step %d missing", i)
		}
		if want := fmt.Sprintf("%d / 4", i); !strings.Contains(m.statusMsg, want) {
			t.Fatalf("status %q, want %q", m.statusMsg, want)
		}
	}
	m = settle(t, m, cmd)

	if got := requestedIDs(t, dir); len(got) != 4 {
		t.Fatalf("ran %v; the plugin must run once per message", got)
	}
	for _, line := range loggedRequests(t, dir) {
		if strings.Contains(line, "body") || !strings.Contains(line, `"method":"message.metadata"`) {
			t.Fatalf("request = %s", line)
		}
	}
	if got := storedFor(t, database, "rules", same); got != "category=billing,urgency= High " {
		t.Fatalf("same: %q", got)
	}
	if got := storedFor(t, database, "rules", differs); got != "category=billing,urgency= High " {
		t.Fatalf("differs: %q", got)
	}
	if got := storedFor(t, database, "rules", bad); got != "needs_reply=true" {
		t.Fatalf("a failure must keep the old set: %q", got)
	}
	if got := storedFor(t, database, "rules", empty); got != "" {
		t.Fatalf("an empty success must clear the set: %q", got)
	}
	if _, ok := m.plugins.annotations[empty]; ok {
		t.Fatal("the cleared message should have no cached badges")
	}

	r := m.plugins.result
	if r == nil || m.overlay != overlayPluginResult {
		t.Fatalf("overlay = %v", m.overlay)
	}
	for _, want := range []string{"Plugin rules finished", "Reclassified 4 messages in INBOX", "3 succeeded", "1 failed", "2 changed", "1 unchanged"} {
		if !strings.Contains(r.body, want) {
			t.Fatalf("summary lacks %q:\n%s", want, r.body)
		}
	}
	if strings.Contains(r.body, "\x1b") || !strings.Contains(r.body, "boom") {
		t.Fatalf("failure reason should be shown sanitized:\n%q", r.body)
	}
	if m.plugins.running != "" || m.plugins.bulk != nil {
		t.Fatal("the run should be finished")
	}
}

// Case 7: a plugin without the annotations permission still runs on every
// message, stores nothing, and changes nothing.
func TestReclassifyViewWithoutAnnotationPermission(t *testing.T) {
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "metaonly", permMetaOnly)
	m, database, msgs := newEventModel(t, root, 3)
	setAnswer(t, dir, `[{"key":"needs_reply","value":"true"}]`)
	store(t, database, "metaonly", msgs[0].ID, "urgency", "high")
	m = reclassifyView(t, m, "metaonly")
	if got := requestedIDs(t, dir); len(got) != 3 {
		t.Fatalf("ran %v", got)
	}
	if got := storedFor(t, database, "metaonly", msgs[0].ID); got != "urgency=high" {
		t.Fatalf("stored %q", got)
	}
	if r := m.plugins.result; r == nil || !strings.Contains(r.body, "0 changed") || !strings.Contains(r.body, "3 unchanged") {
		t.Fatalf("summary: %+v", m.plugins.result)
	}
}

// Cases 2, 12: a small selection runs without asking and names its scope.
func TestReclassifySelectionScope(t *testing.T) {
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "rules", permMetaAndAnnotations)
	m, _, msgs := newEventModel(t, root, 4)
	want := selectIDs(&m, msgs, 1, 3)
	if got := commandLabel(m, "plugin-run"); got != "Reclassify 2 selected messages…" {
		t.Fatalf("label = %q", got)
	}
	m, cmd := pickPluginNoRun(t, m, "rules")
	if m.overlay == overlayPluginConfirm {
		t.Fatal("two selected messages should not confirm")
	}
	if !strings.Contains(m.statusMsg, "Reclassifying 2 selected messages… 0 / 2") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	m = settle(t, m, cmd)
	if got := requestedIDs(t, dir); fmt.Sprint(got) != fmt.Sprint(want) {
		t.Fatalf("ran %v, want %v", got, want)
	}
	if r := m.plugins.result; r == nil || !strings.Contains(r.body, "Reclassified 2 selected messages") {
		t.Fatalf("summary: %+v", m.plugins.result)
	}
}

// Cases 23, 24: a whole view confirms even when small; cancelling launches
// nothing.
func TestReclassifyViewAlwaysConfirms(t *testing.T) {
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "rules", permMetaAndAnnotations)
	m, _, _ := newEventModel(t, root, 1)
	m = pickView(t, m, "rules")
	if !strings.Contains(m.View(), "Reclassify 1 message in INBOX") {
		t.Fatal("confirmation should show the count")
	}
	m, cmd := press(t, m, "n")
	m = settle(t, m, cmd)
	if len(loggedRequests(t, dir)) != 0 || m.plugins.running != "" || m.plugins.pickerScope.view {
		t.Fatal("cancelling must launch nothing and forget the scope")
	}
}

// Cases 18, 19: Needs You follows the new annotations live; a snoozed
// message stays snoozed and out of Needs You.
func TestReclassifyUpdatesNeedsYouAndKeepsSnoozes(t *testing.T) {
	m, f := newNeedsYouModel(t)
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "p", permMetaAndAnnotations)
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	t.Cleanup(m.closePlugins)

	a := f.add(f.inbox, "listed", 1, "p needs_reply true")
	b := f.add(f.inbox, "snoozed", 2, "p needs_reply true")
	c := f.add(f.inbox, "plain", 3)
	if err := f.database.SetSnooze(db.SnoozeMessage, db.MessageKey(db.Message{ID: b}), time.Now().Add(24*time.Hour)); err != nil {
		t.Fatal(err)
	}
	m = openNeedsYou(t, m)
	if got := listedSubjects(m); fmt.Sprint(got) != "[listed]" {
		t.Fatalf("listed = %v", got)
	}

	// Rerun Needs You itself with a plugin that now finds nothing: the row
	// leaves the view.
	setAnswer(t, dir, `[]`)
	m = reclassifyView(t, m, "p")
	if got := requestedIDs(t, dir); fmt.Sprint(got) != fmt.Sprint([]int64{a}) {
		t.Fatalf("Needs You scope ran %v; only its loaded rows count", got)
	}
	if m.needsYou.count != 0 || len(listedSubjects(m)) != 0 {
		t.Fatalf("needs you %d, listed %v", m.needsYou.count, listedSubjects(m))
	}

	// Rerun the inbox with a plugin that finds replies everywhere: the
	// unsnoozed messages enter Needs You, the snoozed one stays snoozed.
	_ = os.Remove(filepath.Join(dir, "requests.log"))
	setAnswer(t, dir, `[{"key":"needs_reply","value":"true"}]`)
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == f.inbox {
			m.sidebarCursor = i
		}
	}
	m.messages, _ = f.database.ListMessages(f.inbox)
	m.applyFilter()
	m = reclassifyView(t, m, "p")
	if got := requestedIDs(t, dir); len(got) != 3 {
		t.Fatalf("inbox scope ran %v", got)
	}
	if m.needsYou.count != 2 {
		t.Fatalf("needs you = %d, want a and c", m.needsYou.count)
	}
	snoozes, err := f.database.ListSnoozes()
	if err != nil || len(snoozes) != 1 {
		t.Fatalf("snoozes = %v, %v; reclassify must not unsnooze", snoozes, err)
	}
	_ = c
}

// Case 20: Waiting on Them stays conversation-driven.
func TestReclassifyLeavesWaitingConversationDriven(t *testing.T) {
	m, f := newWaitingModel(t)
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "p", permMetaAndAnnotations)
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	t.Cleanup(m.closePlugins)
	a := f.add(f.sent, "me@work.example", "sarah@x.example", "<a@x>", "", "Contract", 10)
	m = openWaiting(t, m)
	if m.waiting.count != 1 {
		t.Fatal("setup: one waiting conversation")
	}
	for _, answer := range []string{`[{"key":"needs_reply","value":"true"}]`, `[]`} {
		setAnswer(t, dir, answer)
		m = reclassifyView(t, m, "p")
		if m.waiting.count != 1 || fmt.Sprint(listedSubjects(m)) != "[Contract]" {
			t.Fatalf("answer %s: waiting %d, listed %v", answer, m.waiting.count, listedSubjects(m))
		}
	}
	if got := requestedIDs(t, dir); fmt.Sprint(got) != fmt.Sprint([]int64{a, a}) {
		t.Fatalf("ran %v", got)
	}
}

// Cases 21, 22: a paused automatic plugin can still be reclassified by hand,
// and the run leaves the automatic state alone.
func TestReclassifyWhileAutoPaused(t *testing.T) {
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "smart", eventPerms)
	m, database, msgs := newEventModel(t, root, plugin.EventFailureLimit)
	for _, msg := range msgs {
		setSubject(t, &m, database, msg.ID, "BAD "+msg.Subject)
	}
	m = enableAuto(t, m, "smart")
	next, _ := m.Update(synced(msgs[0].MailboxID, false, msgs...))
	m = next.(Model)
	for range plugin.EventFailureLimit {
		m, _, _ = nextEvent(t, m)
	}
	m.refreshPluginEventStatus()
	before := m.plugins.eventStatus["smart"]
	if !before.Paused {
		t.Fatal("setup: plugin should be paused")
	}

	for _, msg := range msgs {
		setSubject(t, &m, database, msg.ID, strings.TrimPrefix(msg.Subject, "BAD "))
	}
	setAnswer(t, dir, `[{"key":"urgency","value":"high"}]`)
	m = reclassifyView(t, m, "smart")
	if r := m.plugins.result; r == nil || !strings.Contains(r.body, fmt.Sprintf("%d succeeded", len(msgs))) {
		t.Fatalf("summary: %+v", m.plugins.result)
	}
	m.refreshPluginEventStatus()
	after := m.plugins.eventStatus["smart"]
	if !after.Paused || after.ConsecutiveFailures != before.ConsecutiveFailures || after.Dropped != before.Dropped {
		t.Fatalf("automatic state changed: %+v -> %+v", before, after)
	}
}

// waitForPID waits for a HANG plugin to record its pid.
func waitForPID(t *testing.T, dir string) int {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(filepath.Join(dir, "pid")); err == nil {
			if pid, err := strconv.Atoi(strings.TrimSpace(string(data))); err == nil {
				return pid
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("plugin never started")
	return 0
}

// processGone reports whether pid has exited.
func processGone(pid int) bool {
	proc, err := os.FindProcess(pid)
	if err != nil {
		return true
	}
	return proc.Signal(syscall.Signal(0)) != nil
}

// startHangingRun starts a whole-view run whose first message hangs, and
// returns the step command running in the background.
func startHangingRun(t *testing.T) (Model, string, chan tea.Msg) {
	t.Helper()
	root := t.TempDir()
	dir := installRulesPlugin(t, root, "rules", permMetaAndAnnotations)
	m, database, msgs := newEventModel(t, root, 3)
	for _, msg := range msgs {
		setSubject(t, &m, database, msg.ID, "HANG "+msg.Subject)
	}
	m = pickView(t, m, "rules")
	m, cmd := press(t, m, "y")
	got := make(chan tea.Msg, 1)
	go func() { got <- cmd() }()
	return m, dir, got
}

// Case 25: Cancel kills the message in flight and schedules nothing more.
func TestReclassifyCancelKillsProcess(t *testing.T) {
	m, dir, got := startHangingRun(t)
	pid := waitForPID(t, dir)
	if commandLabel(m, "plugin-cancel") != "Cancel reclassify (0 / 3)" {
		t.Fatalf("cancel label = %q", commandLabel(m, "plugin-cancel"))
	}
	next, _ := m.executeCommand("plugin-cancel")
	m = next.(Model)
	var step tea.Msg
	select {
	case step = <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("cancel did not stop the plugin")
	}
	next, cmd := m.Update(step)
	m = settle(t, next.(Model), cmd)
	if !processGone(pid) {
		t.Fatal("the plugin process survived cancel")
	}
	if n := len(loggedRequests(t, dir)); n != 1 {
		t.Fatalf("%d requests; nothing more may be scheduled", n)
	}
	r := m.plugins.result
	if r == nil || !strings.Contains(r.body, "Plugin rules cancelled") || !strings.Contains(r.body, "3 not run") || !strings.Contains(r.body, "0 failed") {
		t.Fatalf("summary: %+v", r)
	}
	if m.plugins.running != "" || m.plugins.bulk != nil {
		t.Fatal("the run should be over")
	}
}

// Case 25: quitting cancels the run and leaves no plugin process.
func TestReclassifyQuitKillsProcess(t *testing.T) {
	m, dir, got := startHangingRun(t)
	pid := waitForPID(t, dir)
	m.closePlugins()
	select {
	case <-got:
	case <-time.After(5 * time.Second):
		t.Fatal("quit did not stop the plugin")
	}
	if !processGone(pid) {
		t.Fatal("the plugin process survived quit")
	}
}

// Case 17: the comparison ignores order, case, spacing, confidence and other
// plugins.
func TestAnnotationSetChanged(t *testing.T) {
	conf := 0.5
	ann := func(plugin, key, value string) db.PluginAnnotation {
		return db.PluginAnnotation{PluginID: plugin, Key: key, Value: value}
	}
	withConf := ann("p", "urgency", "high")
	withConf.Confidence = &conf
	cases := []struct {
		name          string
		before, after []db.PluginAnnotation
		changed       bool
	}{
		{"both empty", nil, nil, false},
		{"reordered", []db.PluginAnnotation{ann("p", "a", "1"), ann("p", "b", "2")}, []db.PluginAnnotation{ann("p", "b", "2"), ann("p", "a", "1")}, false},
		{"case and space", []db.PluginAnnotation{ann("p", "urgency", "high")}, []db.PluginAnnotation{ann("p", "urgency", " HIGH ")}, false},
		{"confidence only", []db.PluginAnnotation{ann("p", "urgency", "high")}, []db.PluginAnnotation{withConf}, false},
		{"other plugin", []db.PluginAnnotation{ann("q", "x", "1")}, []db.PluginAnnotation{ann("q", "y", "2")}, false},
		{"value", []db.PluginAnnotation{ann("p", "urgency", "high")}, []db.PluginAnnotation{ann("p", "urgency", "low")}, true},
		{"added", nil, []db.PluginAnnotation{ann("p", "category", "billing")}, true},
		{"cleared", []db.PluginAnnotation{ann("p", "category", "billing")}, nil, true},
	}
	for _, tc := range cases {
		if got := annotationSetChanged(tc.before, tc.after, "p"); got != tc.changed {
			t.Errorf("%s: changed = %v, want %v", tc.name, got, tc.changed)
		}
	}
}
