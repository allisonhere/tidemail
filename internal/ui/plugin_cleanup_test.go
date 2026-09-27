package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func press(t *testing.T, m Model, key string) (Model, tea.Cmd) {
	t.Helper()
	var msg tea.KeyMsg
	switch key {
	case "esc":
		msg = tea.KeyMsg{Type: tea.KeyEsc}
	case "enter":
		msg = tea.KeyMsg{Type: tea.KeyEnter}
	case "down":
		msg = tea.KeyMsg{Type: tea.KeyDown}
	case "up":
		msg = tea.KeyMsg{Type: tea.KeyUp}
	case "left":
		msg = tea.KeyMsg{Type: tea.KeyLeft}
	case "ctrl+z":
		msg = tea.KeyMsg{Type: tea.KeyCtrlZ}
	default:
		msg = tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(key)}
	}
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}

// finish runs a command and feeds its message back through Update.
func finish(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		t.Fatal("expected a command")
	}
	next, _ := m.Update(cmd())
	return next.(Model)
}

// newCleanupModel has the "smart" plugin installed and annotations from both
// "smart" and the no-longer-installed "echo" on two messages:
//
//	msg 1: echo needs_reply=true, smart category=github
//	msg 2: echo urgency=high
func newCleanupModel(t *testing.T) (Model, *db.DB, []db.Message) {
	t.Helper()
	root := t.TempDir()
	installTestPlugin(t, root, "smart", true)
	m, database, msgs := newAnnotationDBModel(t, root, 2)
	m.cfg.Display.Icons = true
	seed := []struct {
		plugin string
		msg    int64
		key    string
		value  string
	}{
		{"echo", msgs[0].ID, "needs_reply", "true"},
		{"smart", msgs[0].ID, "category", "github"},
		{"echo", msgs[1].ID, "urgency", "high"},
	}
	for _, s := range seed {
		existing, _ := database.ListPluginAnnotations(s.msg)
		var mine []db.PluginAnnotation
		for _, a := range existing {
			if a.PluginID == s.plugin {
				mine = append(mine, a)
			}
		}
		mine = append(mine, db.PluginAnnotation{Key: s.key, Value: s.value})
		if err := database.ReplacePluginAnnotations(s.plugin, s.msg, mine); err != nil {
			t.Fatal(err)
		}
	}
	m.plugins.annotations = loadMessageAnnotations(database, m.messages)
	return m, database, msgs
}

func openAnnotations(t *testing.T, m Model) Model {
	t.Helper()
	next, _ := m.executeCommand("plugin-annotations")
	m = next.(Model)
	if m.overlay != overlayPluginAnnotations {
		t.Fatalf("overlay = %v", m.overlay)
	}
	return m
}

func openPluginList(t *testing.T, m Model) Model {
	t.Helper()
	next, cmd := m.executeCommand("plugins")
	m = finish(t, next.(Model), cmd) // loads counts
	if m.overlay != overlayPlugins || !m.plugins.countsLoaded {
		t.Fatalf("overlay = %v, countsLoaded = %v", m.overlay, m.plugins.countsLoaded)
	}
	return m
}

func TestAnnotationsOverlayShowsCleanupHints(t *testing.T) {
	m, _, _ := newCleanupModel(t)
	view := openAnnotations(t, m).View()
	for _, want := range []string{"clear plugin", "clear all", "echo (not installed)", "Plugin smart  (smart)"} {
		if !strings.Contains(view, want) {
			t.Errorf("annotations overlay missing %q", want)
		}
	}
}

func TestClearPluginOnMessageNeedsConfirmationAndCancelChangesNothing(t *testing.T) {
	m, database, msgs := newCleanupModel(t)
	m = openAnnotations(t, m)

	m, cmd := press(t, m, "c")
	if cmd != nil {
		t.Fatal("c must not delete anything by itself")
	}
	if m.overlay != overlayPluginConfirm || m.plugins.confirm == nil || m.plugins.confirm.kind != clearPluginOnMessage || m.plugins.confirm.pluginID != "echo" {
		t.Fatalf("overlay=%v confirm=%+v", m.overlay, m.plugins.confirm)
	}
	if view := m.View(); !strings.Contains(view, "Clear echo annotations from this message?") || !strings.Contains(view, "does not delete") {
		t.Fatal("confirmation text missing")
	}

	m, cmd = press(t, m, "n")
	if cmd != nil || m.overlay != overlayPluginAnnotations || m.plugins.confirm != nil {
		t.Fatalf("cancel: overlay=%v confirm=%v", m.overlay, m.plugins.confirm)
	}
	if got := storedKV(t, database, msgs[0].ID); len(got) != 2 {
		t.Fatalf("cancel changed stored annotations: %v", got)
	}
	// Esc cancels too.
	m, _ = press(t, m, "c")
	m, cmd = press(t, m, "esc")
	if cmd != nil || m.overlay != overlayPluginAnnotations {
		t.Fatal("esc should cancel back to the annotations overlay")
	}
}

func TestClearPluginOnMessageRemovesOnlyItsBadges(t *testing.T) {
	m, database, msgs := newCleanupModel(t)
	m = openAnnotations(t, m) // cursor on "echo" (plugin groups sort by ID)
	m, _ = press(t, m, "c")
	m, cmd := press(t, m, "y")
	if m.overlay != overlayPluginAnnotations {
		t.Fatalf("overlay after confirm = %v", m.overlay)
	}
	m = finish(t, m, cmd)

	if got := storedKV(t, database, msgs[0].ID); fmt.Sprint(got) != "[smart:category=github]" {
		t.Fatalf("stored = %v", got)
	}
	if got := storedKV(t, database, msgs[1].ID); fmt.Sprint(got) != "[echo:urgency=high]" {
		t.Fatalf("other message changed: %v", got)
	}
	m.overlay = overlayNone
	line := rowLine(t, m, "msg 1")
	if strings.Contains(line, "↩") || !strings.Contains(line, "GITHUB") {
		t.Fatalf("row = %q", line)
	}
	if !strings.Contains(m.statusMsg, "cleared echo annotations from this message") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestClearSelectedSecondGroup(t *testing.T) {
	m, database, msgs := newCleanupModel(t)
	m = openAnnotations(t, m)
	m, _ = press(t, m, "down") // "smart"
	m, _ = press(t, m, "c")
	if m.plugins.confirm == nil || m.plugins.confirm.pluginID != "smart" {
		t.Fatalf("confirm = %+v", m.plugins.confirm)
	}
	m, cmd := press(t, m, "enter")
	m = finish(t, m, cmd)
	if got := storedKV(t, database, msgs[0].ID); fmt.Sprint(got) != "[echo:needs_reply=true]" {
		t.Fatalf("stored = %v", got)
	}
}

func TestClearAllOnMessage(t *testing.T) {
	m, database, msgs := newCleanupModel(t)
	m = openAnnotations(t, m)
	m, _ = press(t, m, "C")
	if m.plugins.confirm == nil || m.plugins.confirm.kind != clearAllOnMessage {
		t.Fatalf("confirm = %+v", m.plugins.confirm)
	}
	if !strings.Contains(m.View(), "Clear all plugin annotations from this message?") {
		t.Fatal("confirmation text missing")
	}
	m, cmd := press(t, m, "y")
	m = finish(t, m, cmd)

	if got := storedKV(t, database, msgs[0].ID); len(got) != 0 {
		t.Fatalf("stored = %v", got)
	}
	if _, ok := m.plugins.annotations[msgs[0].ID]; ok {
		t.Fatal("cache entry should be gone")
	}
	if len(m.plugins.annotations[msgs[1].ID]) != 1 {
		t.Fatal("other message's cache changed")
	}
	if _, err := database.GetMessage(msgs[0].ID); err != nil {
		t.Fatalf("the message itself must survive: %v", err)
	}
	m.overlay = overlayNone
	if line := rowLine(t, m, "msg 1"); strings.Contains(line, "↩") || strings.Contains(line, "GITHUB") {
		t.Fatalf("row still has badges: %q", line)
	}
}

func TestPluginListShowsCountsAndRemovedPlugins(t *testing.T) {
	m, _, _ := newCleanupModel(t)
	m = openPluginList(t, m)
	view := m.View()
	for _, want := range []string{"Plugin smart", "stored annotations: 1", "Stored data from removed plugins", "echo", "stored annotations: 2", "clear stored"} {
		if !strings.Contains(view, want) {
			t.Errorf("plugin list missing %q", want)
		}
	}
	if strings.Contains(view, "broken") {
		t.Error("a removed plugin is not broken")
	}
	entries := m.pluginListEntries()
	if len(entries) != 2 || !entries[0].installed || entries[1].installed || entries[1].pluginID != "echo" {
		t.Fatalf("entries = %+v", entries)
	}
}

func TestRemovedPluginGlobalCleanup(t *testing.T) {
	m, database, msgs := newCleanupModel(t)
	m = openPluginList(t, m)
	m, _ = press(t, m, "down") // echo
	m, cmd := press(t, m, "c")
	if cmd != nil || m.overlay != overlayPluginConfirm {
		t.Fatalf("overlay = %v", m.overlay)
	}
	view := m.View()
	if !strings.Contains(view, `Clear all stored annotations from plugin "echo"?`) || strings.Contains(view, "stays installed") {
		t.Fatal("removed-plugin confirmation text wrong")
	}
	m, cmd = press(t, m, "y")
	if m.overlay != overlayPlugins {
		t.Fatalf("overlay after confirm = %v", m.overlay)
	}
	m = finish(t, m, cmd)

	if strings.Contains(m.View(), "Stored data from removed plugins") {
		t.Fatal("removed-plugin row should disappear once its count is zero")
	}
	// Badges from echo vanish from every loaded message immediately.
	m.overlay = overlayNone
	if line := rowLine(t, m, "msg 1"); strings.Contains(line, "↩") || !strings.Contains(line, "GITHUB") {
		t.Fatalf("msg 1 row = %q", line)
	}
	if line := rowLine(t, m, "msg 2"); strings.Contains(line, "!") {
		t.Fatalf("msg 2 row = %q", line)
	}
	if got := storedKV(t, database, msgs[0].ID); fmt.Sprint(got) != "[smart:category=github]" {
		t.Fatalf("stored = %v", got)
	}
	for _, msg := range msgs {
		if _, err := database.GetMessage(msg.ID); err != nil {
			t.Fatalf("message deleted: %v", err)
		}
	}
}

func TestInstalledPluginGlobalCleanupKeepsPlugin(t *testing.T) {
	m, database, msgs := newCleanupModel(t)
	m = openPluginList(t, m) // cursor on smart
	m, _ = press(t, m, "c")
	if !strings.Contains(m.View(), "The plugin stays installed.") {
		t.Fatal("installed-plugin confirmation should say it stays installed")
	}
	m, cmd := press(t, m, "y")
	m = finish(t, m, cmd)

	if _, ok := m.plugins.manager.Plugin("smart"); !ok {
		t.Fatal("plugin should stay installed")
	}
	view := m.View()
	if !strings.Contains(view, "Plugin smart") || !strings.Contains(view, "stored annotations: 0") {
		t.Fatal("installed plugin should stay listed with a zero count")
	}
	// Zero stored annotations: no clear hint, and c does nothing.
	if strings.Contains(view, "clear stored") {
		t.Fatal("clear hint shown for a plugin with nothing stored")
	}
	m, cmd = press(t, m, "c")
	if cmd != nil || m.overlay != overlayPlugins {
		t.Fatal("c should do nothing when nothing is stored")
	}
	if got := storedKV(t, database, msgs[0].ID); fmt.Sprint(got) != "[echo:needs_reply=true]" {
		t.Fatalf("stored = %v", got)
	}
}

func TestPluginListCountsIncludeNewRunResults(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "jev", twoAnnotations, permMetaAndAnnotations)
	m, _, _ := newAnnotationDBModel(t, root, 1)
	m = runPlugin(t, m, "jev")
	// runPlugin drops follow-up commands; the plugin list reloads on open.
	m.overlay = overlayNone
	m = openPluginList(t, m)
	if m.plugins.counts["jev"] != 2 || !strings.Contains(m.View(), "stored annotations: 2") {
		t.Fatalf("counts = %v", m.plugins.counts)
	}
}

func TestCleanupResultNeverChangesOverlay(t *testing.T) {
	m, _, msgs := newCleanupModel(t)
	for _, err := range []error{nil, errors.New("disk I/O error")} {
		m.overlay = overlaySettings
		next, _ := m.Update(pluginAnnotationsClearedMsg{
			Action: pluginConfirmAction{kind: clearAllOnMessage, messageID: msgs[1].ID},
			Err:    err,
		})
		if got := next.(Model).overlay; got != overlaySettings {
			t.Fatalf("err=%v: overlay changed to %v", err, got)
		}
	}
}

func TestCleanupFailureKeepsCacheAndCounts(t *testing.T) {
	m, _, msgs := newCleanupModel(t)
	m = openPluginList(t, m)
	before := fmt.Sprint(m.plugins.annotations[msgs[0].ID], m.plugins.counts)
	next, _ := m.Update(pluginAnnotationsClearedMsg{
		Action: pluginConfirmAction{kind: clearPluginEverywhere, pluginID: "echo", label: "echo"},
		Err:    errors.New("database is locked"),
	})
	m = next.(Model)
	if after := fmt.Sprint(m.plugins.annotations[msgs[0].ID], m.plugins.counts); after != before {
		t.Fatalf("state changed on failure:\n%s\n%s", before, after)
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "clearing annotations failed") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

// All cleanup screens render from model state; with no database at all they
// still draw (a render-time query would panic on the nil database).
func TestCleanupRenderingIsDatabaseFree(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	if m.db != nil {
		t.Fatal("expected a model without a database")
	}
	m.plugins.annotations = map[int64][]db.PluginAnnotation{msgs[0].ID: {cacheAnn("echo", "needs_reply", "true")}}
	m.plugins.counts = map[string]int64{"echo": 5}
	m.plugins.countsLoaded = true

	m.overlay = overlayPlugins
	if view := m.View(); !strings.Contains(view, "stored annotations: 5") {
		t.Fatal("plugin list should render from cached counts")
	}
	m = openAnnotations(t, m)
	m, _ = press(t, m, "C")
	if view := m.View(); !strings.Contains(view, "Clear all plugin annotations") {
		t.Fatal("confirmation should render")
	}
	// Confirming without a database reports an error instead of crashing.
	m, cmd := press(t, m, "y")
	m = finish(t, m, cmd)
	if !m.statusErr || len(m.plugins.annotations[msgs[0].ID]) != 1 {
		t.Fatalf("status=%q cache=%v", m.statusMsg, m.plugins.annotations)
	}
}

func TestNoCleanupControlsBeforeCountsLoadOrWithoutAnnotations(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "smart", true)
	m := newPluginModel(t, root)
	m.overlay = overlayPlugins // counts never loaded
	if view := m.View(); strings.Contains(view, "clear stored") || strings.Contains(view, "stored annotations") {
		t.Fatal("no counts or clear controls before counts are loaded")
	}
	m, cmd := press(t, m, "c")
	if cmd != nil || m.overlay != overlayPlugins {
		t.Fatal("c should do nothing without stored data")
	}

	// A message with no annotations: no clear hints, and c/C do nothing.
	m.overlay = overlayPluginAnnotations
	m.plugins.annotationsFor = 12345
	if view := m.View(); strings.Contains(view, "clear plugin") || strings.Contains(view, "clear all") {
		t.Fatal("clear hints shown with nothing to clear")
	}
	for _, k := range []string{"c", "C"} {
		if next, cmd := press(t, m, k); cmd != nil || next.overlay != overlayPluginAnnotations {
			t.Fatalf("%s should do nothing with nothing to clear", k)
		}
	}
}
