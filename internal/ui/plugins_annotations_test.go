package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

// installDataPlugin writes a shell-script plugin that answers every request
// with the given response data (a JSON object without single quotes), or
// fails when data is "".
func installDataPlugin(t *testing.T, root, id, data, permissions string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test plugins need a Unix shell")
	}
	if strings.Contains(data, "'") {
		t.Fatal("test data must not contain single quotes")
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("id = %q\nname = \"Plugin %s\"\nversion = \"0.1.0\"\napi = 1\ncommand = \"run\"\n\n%s", id, id, permissions)
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	script := "#!/bin/sh\necho 'plugin failed' >&2\nexit 1\n"
	if data != "" {
		script = `#!/bin/sh
read -r line
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":%s}\n' "$id" '` + data + `'
`
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

const (
	permMetaAndAnnotations = "[permissions]\nmessage_metadata = true\nannotations = true\n"
	permMetaOnly           = "[permissions]\nmessage_metadata = true\n"
	twoAnnotations         = `{"summary":"hello","annotations":[{"key":"needs_reply","value":"true","confidence":0.94},{"key":"urgency","value":"high"}]}`
)

// newAnnotationDBModel builds a model backed by a real database with one
// mailbox of n messages, plugins loaded from root.
func newAnnotationDBModel(t *testing.T, root string, n int) (Model, *db.DB, []db.Message) {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	accountID, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX", Delimiter: "/"})
	if err != nil {
		t.Fatal(err)
	}
	for i := range n {
		if err := database.UpsertMessage(db.Message{MailboxID: mailboxID, UID: uint32(i + 1), Subject: fmt.Sprintf("msg %d", i+1), Date: time.Unix(int64(1000-i), 0)}); err != nil {
			t.Fatal(err)
		}
	}
	msgs, err := database.ListMessages(mailboxID)
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.width, m.height = 110, 30
	m.focused = paneMessages
	m.accounts = []db.Account{{ID: accountID, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: mailboxID, AccountID: accountID, Name: "INBOX"}}
	m.rebuildSidebar()
	for i, row := range m.sidebarRows {
		if row.kind == rowKindMailbox && row.mailboxID == mailboxID {
			m.sidebarCursor = i
		}
	}
	m.messages = msgs
	m.applyFilter()
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	return m, database, msgs
}

// runPlugin drives the picker for the current message and feeds the result
// back through Update, the way the program would.
func runPlugin(t *testing.T, m Model, pluginID string) Model {
	t.Helper()
	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	if m.overlay != overlayPluginPicker {
		t.Fatalf("picker not open: overlay=%v status=%q", m.overlay, m.statusMsg)
	}
	for i, p := range m.plugins.picker {
		if p.Manifest.ID == pluginID {
			m.plugins.pickerCursor = i
		}
	}
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	next, _ = m.Update(cmd())
	return next.(Model)
}

func storedKV(t *testing.T, database *db.DB, messageID int64) []string {
	t.Helper()
	anns, err := database.ListPluginAnnotations(messageID)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, a := range anns {
		out = append(out, a.PluginID+":"+a.Key+"="+a.Value)
	}
	return out
}

func cacheAnn(pluginID, key, value string) db.PluginAnnotation {
	return db.PluginAnnotation{PluginID: pluginID, Key: key, Value: value}
}

// rowLine returns the rendered message-list line containing text.
func rowLine(t *testing.T, m Model, text string) string {
	t.Helper()
	for _, line := range strings.Split(m.renderMessagesPane(), "\n") {
		if strings.Contains(line, text) {
			return line
		}
	}
	t.Fatalf("no row contains %q", text)
	return ""
}

func TestAnnotationRunStoresAndRefreshesBadges(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "jev", twoAnnotations, permMetaAndAnnotations)
	m, database, msgs := newAnnotationDBModel(t, root, 2)
	target := m.commandMessage().ID

	m = runPlugin(t, m, "jev")
	if got := storedKV(t, database, target); fmt.Sprint(got) != "[jev:needs_reply=true jev:urgency=high]" {
		t.Fatalf("stored = %v", got)
	}
	if len(m.plugins.annotations[target]) != 2 {
		t.Fatalf("cache = %v", m.plugins.annotations)
	}
	if m.overlay != overlayPluginResult || !strings.Contains(m.plugins.result.annotationNote, "2 stored") {
		t.Fatalf("overlay=%v note=%q", m.overlay, m.plugins.result.annotationNote)
	}
	m.overlay = overlayNone
	if line := rowLine(t, m, "msg 1"); !strings.Contains(line, "↩ !") {
		t.Fatalf("row = %q", line)
	}
	// Only the run message was touched.
	if _, ok := m.plugins.annotations[msgs[1].ID]; ok {
		t.Fatal("unrelated message gained annotations")
	}
	if !strings.Contains(strings.Join(commandIDs(m), " "), "plugin-annotations") {
		t.Fatal("Message annotations command should appear for an annotated message")
	}
}

func TestAnnotationRunWithoutPermissionShowsResultButStoresNothing(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "meta", twoAnnotations, permMetaOnly)
	m, database, _ := newAnnotationDBModel(t, root, 1)
	target := m.commandMessage().ID

	m = runPlugin(t, m, "meta")
	if got := storedKV(t, database, target); len(got) != 0 {
		t.Fatalf("stored without permission: %v", got)
	}
	if len(m.plugins.annotations[target]) != 0 {
		t.Fatal("cache gained annotations without permission")
	}
	if m.overlay != overlayPluginResult || !strings.Contains(m.plugins.result.body, "hello") {
		t.Fatal("the response should still be displayed")
	}
	if !strings.Contains(m.plugins.result.annotationNote, "lacks the annotations permission") {
		t.Fatalf("note = %q", m.plugins.result.annotationNote)
	}
}

func TestAnnotationFailedRunKeepsPriorState(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "jev", "", permMetaAndAnnotations) // exits 1
	m, database, _ := newAnnotationDBModel(t, root, 1)
	target := m.commandMessage().ID
	if err := database.ReplacePluginAnnotations("jev", target, []db.PluginAnnotation{{Key: "urgency", Value: "high"}}); err != nil {
		t.Fatal(err)
	}
	m.plugins.annotations = loadMessageAnnotations(database, m.messages)

	m = runPlugin(t, m, "jev")
	if !m.statusErr {
		t.Fatalf("failure not reported: %q", m.statusMsg)
	}
	if got := storedKV(t, database, target); fmt.Sprint(got) != "[jev:urgency=high]" {
		t.Fatalf("stored = %v", got)
	}
	if line := rowLine(t, m, "msg 1"); !strings.Contains(line, "!") {
		t.Fatalf("badge lost after failed run: %q", line)
	}
}

func TestAnnotationRejectedResponseKeepsPriorState(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "jev", `{"annotations":[{"key":"urgency","value":"high","confidence":2.0}]}`, permMetaAndAnnotations)
	m, database, _ := newAnnotationDBModel(t, root, 1)
	target := m.commandMessage().ID
	if err := database.ReplacePluginAnnotations("jev", target, []db.PluginAnnotation{{Key: "needs_reply", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	m.plugins.annotations = loadMessageAnnotations(database, m.messages)

	m = runPlugin(t, m, "jev")
	if got := storedKV(t, database, target); fmt.Sprint(got) != "[jev:needs_reply=true]" {
		t.Fatalf("stored = %v", got)
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "annotations rejected") {
		t.Fatalf("status = %q", m.statusMsg)
	}
}

func TestAnnotationEmptyResultClearsOnlyThatPlugin(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "jev", `{"annotations":[]}`, permMetaAndAnnotations)
	m, database, _ := newAnnotationDBModel(t, root, 1)
	target := m.commandMessage().ID
	if err := database.ReplacePluginAnnotations("jev", target, []db.PluginAnnotation{{Key: "needs_reply", Value: "true"}}); err != nil {
		t.Fatal(err)
	}
	if err := database.ReplacePluginAnnotations("other", target, []db.PluginAnnotation{{Key: "urgency", Value: "high"}}); err != nil {
		t.Fatal(err)
	}
	m.plugins.annotations = loadMessageAnnotations(database, m.messages)

	m = runPlugin(t, m, "jev")
	if got := storedKV(t, database, target); fmt.Sprint(got) != "[other:urgency=high]" {
		t.Fatalf("stored = %v", got)
	}
	m.overlay = overlayNone
	line := rowLine(t, m, "msg 1")
	if strings.Contains(line, "↩") || !strings.Contains(line, "!") {
		t.Fatalf("row = %q", line)
	}
}

func TestMessagesLoadedCarriesAnnotations(t *testing.T) {
	m, database, msgs := newAnnotationDBModel(t, t.TempDir(), 3)
	if err := database.ReplacePluginAnnotations("jev", msgs[2].ID, []db.PluginAnnotation{{Key: "urgency", Value: "high"}}); err != nil {
		t.Fatal(err)
	}
	loaded := m.loadMailboxMessagesCmd(msgs[0].MailboxID)().(MessagesLoadedMsg)
	next, _ := m.Update(loaded)
	m = next.(Model)
	if len(m.plugins.annotations) != 1 || len(m.plugins.annotations[msgs[2].ID]) != 1 {
		t.Fatalf("cache = %v", m.plugins.annotations)
	}
}

func TestAnnotationBadgesRenderFromCacheWithoutDatabase(t *testing.T) {
	// newMailboxListModel has no database at all: if rendering ever queried
	// SQLite for badges, this would panic instead of drawing them.
	m, msgs := newMailboxListModel(7, 2)
	m.cfg.Display.Icons = true
	m.plugins.annotations = map[int64][]db.PluginAnnotation{
		msgs[0].ID: {cacheAnn("a", "needs_reply", "true")},
	}
	if m.db != nil {
		t.Fatal("expected a model without a database")
	}
	if line := rowLine(t, m, "msg 1"); !strings.Contains(line, "↩") {
		t.Fatalf("row = %q", line)
	}
	if line := rowLine(t, m, "msg 2"); strings.Contains(line, "↩") {
		t.Fatalf("badge leaked to another row: %q", line)
	}
}

func TestAnnotationBadgesCompactAndConventionalOnly(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	id := msgs[0].ID
	m.cfg.Display.Icons = true
	m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {
		cacheAnn("a", "category", "receipt"),
		cacheAnn("a", "importance", "High"),
		cacheAnn("a", "needs_reply", "true"),
		cacheAnn("b", "urgency", "urgent"),
		cacheAnn("b", "mood", "high"),       // unknown key: never a badge
		cacheAnn("b", "needs_reply", "yes"), // same key from another plugin: one badge
	}}
	if got := m.annotationBadges(id); got != "↩ ! ◆" {
		t.Fatalf("badges = %q, want at most three, in rule order", got)
	}

	m.cfg.Display.Icons = false
	if got := m.annotationBadges(id); got != "R ! ^" {
		t.Fatalf("ascii badges = %q", got)
	}

	m.plugins.annotations[id] = []db.PluginAnnotation{
		cacheAnn("a", "category", "receipt"),
		cacheAnn("a", "urgency", "low"),
		cacheAnn("a", "mood", "high"),
	}
	if got := m.annotationBadges(id); got != "#receipt" {
		t.Fatalf("badges = %q", got)
	}
	// Free-form or long category values never reach the row.
	for _, v := range []string{"Receipts and bills", "averyveryverylongcategory", "rec\x1b[31m"} {
		m.plugins.annotations[id] = []db.PluginAnnotation{cacheAnn("a", "category", v)}
		if got := m.annotationBadges(id); got != "" {
			t.Fatalf("category %q produced badge %q", v, got)
		}
	}
	m.plugins.annotations[id] = []db.PluginAnnotation{cacheAnn("a", "mood", "high")}
	if got := m.annotationBadges(id); got != "" {
		t.Fatalf("unknown key produced %q", got)
	}
}

func TestThreadedRowUsesRepresentativeAnnotationsOnly(t *testing.T) {
	m, msgs := newMailboxListModel(7, 2)
	m.cfg.Display.ThreadedConversations = true
	m.cfg.Display.Icons = true
	msgs[0].MessageID, msgs[1].MessageID = "<b@x>", "<a@x>"
	msgs[0].InReplyTo = "<a@x>"
	m.messages = msgs
	m.applyFilter()
	if len(m.messageThreads) != 1 || m.messageThreads[0].Count != 2 {
		t.Fatalf("threads = %+v", m.messageThreads)
	}
	rep := m.messageThreads[0].Representative.ID
	other := msgs[0].ID
	if other == rep {
		other = msgs[1].ID
	}

	m.plugins.annotations = map[int64][]db.PluginAnnotation{other: {cacheAnn("a", "needs_reply", "true")}}
	if got := m.renderMessagesPane(); strings.Contains(got, "↩") {
		t.Fatal("a non-representative message's annotation reached the thread row")
	}
	m.plugins.annotations = map[int64][]db.PluginAnnotation{rep: {cacheAnn("a", "needs_reply", "true")}}
	if got := m.renderMessagesPane(); !strings.Contains(got, "↩") {
		t.Fatal("the representative message's annotation should show on the thread row")
	}
}

func TestAnnotationOverlayGroupsSanitizesAndFallsBack(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "jev", true)
	m := newPluginModel(t, root)
	id := m.commandMessage().ID
	c := 0.94
	m.plugins.annotations = map[int64][]db.PluginAnnotation{id: {
		{PluginID: "gone", Key: "category", Value: "receipt"},
		{PluginID: "jev", Key: "needs_reply", Value: "true", Confidence: &c},
		// Stored before validation existed, or tampered with: still drawn safely.
		{PluginID: "jev", Key: "urgency", Value: "high\x1b[31m\u202eevil"},
	}}
	if !strings.Contains(strings.Join(commandIDs(m), " "), "plugin-annotations") {
		t.Fatal("command missing")
	}
	next, _ := m.executeCommand("plugin-annotations")
	m = next.(Model)
	if m.overlay != overlayPluginAnnotations {
		t.Fatalf("overlay = %v", m.overlay)
	}
	view := m.View()
	for _, want := range []string{"gone (not installed)", "Plugin jev  (jev)", "needs_reply", "94%", "receipt", "high[31mevil"} {
		if !strings.Contains(view, want) {
			t.Errorf("overlay missing %q", want)
		}
	}
	if strings.ContainsRune(view, 0x202e) {
		t.Error("bidi override reached the terminal")
	}
	if strings.Index(view, "gone (not installed)") > strings.Index(view, "Plugin jev") {
		t.Error("plugins should be grouped in plugin ID order")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).overlay != overlayNone {
		t.Fatal("esc should close the overlay")
	}
}

func TestAnnotationCommandHiddenWithoutAnnotations(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	if strings.Contains(strings.Join(commandIDs(m), " "), "plugin-annotations") {
		t.Fatal("Message annotations shown for a message without annotations")
	}
}
