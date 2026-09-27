package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

// echoPluginScript answers any request by echoing the whole request back as
// the response data, so tests can see exactly what TideMail sent.
const echoPluginScript = `#!/bin/sh
read -r line
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":{"category":"notification","request":%s}}\n' "$id" "$line"
`

// installTestPlugin writes a shell-script plugin into root/id.
func installTestPlugin(t *testing.T, root, id string, metadata bool) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test plugins need a Unix shell")
	}
	dir := filepath.Join(root, id)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := fmt.Sprintf("id = %q\nname = \"Plugin %s\"\nversion = \"0.1.0\"\napi = 1\ncommand = \"run\"\n\n[permissions]\nmessage_metadata = %t\n", id, id, metadata)
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(echoPluginScript), 0o755); err != nil {
		t.Fatal(err)
	}
}

// newPluginModel is a message-list model with the plugins in root loaded.
func newPluginModel(t *testing.T, root string) Model {
	t.Helper()
	m, msgs := newMailboxListModel(7, 3)
	msgs[0].BodyText = "SECRET BODY"
	msgs[0].BodyHTML = "<p>SECRET HTML</p>"
	msgs[0].Summary = "SECRET SUMMARY"
	msgs[0].Headers = "X-Secret: SECRET HEADER"
	msgs[0].From = "Ann <ann@example.com>"
	m.messages = msgs
	m.applyFilter()
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	return m
}

func commandIDs(m Model) []string {
	var ids []string
	for _, item := range m.mainCommandItems() {
		ids = append(ids, item.id)
	}
	return ids
}

func TestPluginCommandsHiddenWithoutPlugins(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	for _, id := range commandIDs(m) {
		if strings.HasPrefix(id, "plugin") {
			t.Fatalf("plugin command %q shown with no plugin manager", id)
		}
	}
	// An empty (or missing) plugins directory looks the same.
	mgr, err := plugin.Discover(filepath.Join(t.TempDir(), "missing"))
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, "", nil)
	for _, id := range commandIDs(m) {
		if strings.HasPrefix(id, "plugin") {
			t.Fatalf("plugin command %q shown with no plugins installed", id)
		}
	}
}

func TestPluginCommandsShownWithPluginsOrErrors(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	m := newPluginModel(t, root)
	if ids := strings.Join(commandIDs(m), " "); !strings.Contains(ids, "plugins") || !strings.Contains(ids, "plugin-run") {
		t.Fatalf("commands = %s", ids)
	}

	// A plugins directory that could not be read still gets the list, so the
	// user can see why.
	m2, _ := newMailboxListModel(7, 1)
	m2.SetPlugins(nil, "", errors.New("permission denied"))
	if ids := strings.Join(commandIDs(m2), " "); !strings.Contains(ids, "plugins") {
		t.Fatalf("commands = %s", ids)
	}
}

func TestPluginRunWithoutEligiblePlugins(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "nometa", false)
	m := newPluginModel(t, root)

	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	if m.overlay != overlayNone {
		t.Fatalf("overlay = %v, want none", m.overlay)
	}
	if m.statusMsg != "no plugins have permission to read message metadata" || m.statusErr {
		t.Fatalf("status = %q (err %v)", m.statusMsg, m.statusErr)
	}
}

func TestPluginPickerListsOnlyMetadataPlugins(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "allowed", true)
	installTestPlugin(t, root, "denied", false)
	m := newPluginModel(t, root)

	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	if m.overlay != overlayPluginPicker {
		t.Fatalf("overlay = %v, want picker", m.overlay)
	}
	if len(m.plugins.picker) != 1 || m.plugins.picker[0].Manifest.ID != "allowed" {
		t.Fatalf("picker = %+v", m.plugins.picker)
	}
	m.width, m.height = 100, 30
	if view := m.View(); !strings.Contains(view, "Plugin allowed") || strings.Contains(view, "Plugin denied") {
		t.Fatal("picker should render only the permitted plugin")
	}
}

func TestPluginRunEndToEndSendsOnlyMetadata(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	m := newPluginModel(t, root)

	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("confirming the picker should return a command")
	}
	if m.plugins.running != "hello" || m.overlay != overlayNone || !strings.Contains(m.statusMsg, "running plugin hello") {
		t.Fatalf("running=%q overlay=%v status=%q", m.plugins.running, m.overlay, m.statusMsg)
	}

	// The plugin runs inside the command, off the Update loop.
	msg, ok := cmd().(pluginResultMsg)
	if !ok {
		t.Fatalf("command returned %T", msg)
	}
	if msg.Err != nil {
		t.Fatal(msg.Err)
	}
	var echoed struct {
		Request plugin.Request `json:"request"`
	}
	if err := json.Unmarshal(msg.Result.Response.Data, &echoed); err != nil {
		t.Fatal(err)
	}
	if echoed.Request.Method != plugin.MethodMessageMetadata {
		t.Fatalf("method = %q", echoed.Request.Method)
	}
	var sent map[string]any
	if err := json.Unmarshal(echoed.Request.Data, &sent); err != nil {
		t.Fatal(err)
	}
	if sent["subject"] != "msg 1" || sent["from"] != "Ann <ann@example.com>" || sent["account_name"] != "Personal" || sent["mailbox_name"] != "INBOX" {
		t.Fatalf("metadata sent = %v", sent)
	}
	if raw := string(echoed.Request.Data); strings.Contains(raw, "SECRET") {
		t.Fatalf("body, summary, or headers leaked to the plugin: %s", raw)
	}

	next, _ = m.Update(msg)
	m = next.(Model)
	if m.plugins.running != "" {
		t.Fatal("running flag not cleared")
	}
	if m.overlay != overlayPluginResult {
		t.Fatalf("overlay = %v, want result", m.overlay)
	}
	if !strings.Contains(m.statusMsg, "plugin hello completed") {
		t.Fatalf("status = %q", m.statusMsg)
	}
	if !strings.Contains(m.plugins.result.body, `"category": "notification"`) {
		t.Fatalf("result body = %q", m.plugins.result.body)
	}
	if view := m.View(); !strings.Contains(view, "notification") {
		t.Fatal("result overlay should show the response data")
	}
}

func TestPluginResultDoesNotHijackOtherOverlays(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	m := newPluginModel(t, root)
	m.plugins.running = "hello"
	m.overlay = overlaySettings

	next, _ := m.Update(pluginResultMsg{PluginID: "hello", Result: plugin.MessageMetadataResult{Response: plugin.Response{OK: true, Data: json.RawMessage(`{"a":1}`)}}})
	m = next.(Model)
	if m.overlay != overlaySettings {
		t.Fatalf("overlay = %v, want settings kept", m.overlay)
	}
	if !strings.Contains(m.statusMsg, "open Plugins to view") || m.plugins.result == nil {
		t.Fatalf("status = %q, result = %v", m.statusMsg, m.plugins.result)
	}
	// The result stays reachable from the plugin list.
	m.overlay = overlayPlugins
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("r")})
	if next.(Model).overlay != overlayPluginResult {
		t.Fatal("r should open the last result")
	}
}

func TestPluginFailureIsReportedAndClearsRunning(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	m.plugins.running = "hello"
	next, _ := m.Update(pluginResultMsg{PluginID: "hello", Err: errors.New("plugin \"hello\" timed out after 5s: stderr: \x1b[31mboom")})
	m = next.(Model)
	if m.plugins.running != "" || m.overlay != overlayNone {
		t.Fatalf("running=%q overlay=%v", m.plugins.running, m.overlay)
	}
	if !m.statusErr || !strings.Contains(m.statusMsg, "timed out") || strings.ContainsRune(m.statusMsg, 0x1b) {
		t.Fatalf("status = %q (err %v)", m.statusMsg, m.statusErr)
	}
}

func TestPluginRunRefusedWhileRunning(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	m := newPluginModel(t, root)
	m.plugins.running = "hello"
	next, _ := m.executeCommand("plugin-run")
	m = next.(Model)
	// No picker means no path to a second launch.
	if m.overlay != overlayNone || m.plugins.picker != nil || !strings.Contains(m.statusMsg, "still running") {
		t.Fatalf("overlay=%v picker=%v status=%q", m.overlay, m.plugins.picker, m.statusMsg)
	}
}

func TestPluginListShowsPluginsAndSanitizedErrors(t *testing.T) {
	root := t.TempDir()
	installTestPlugin(t, root, "hello", true)
	broken := filepath.Join(root, "broken")
	if err := os.MkdirAll(broken, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(broken, plugin.ManifestFile), []byte("id = \"broken\"\nname = \"B\"\napi = 1\ncommand = \"missing\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	m := newPluginModel(t, root)
	next, _ := m.executeCommand("plugins")
	m = next.(Model)
	if m.overlay != overlayPlugins {
		t.Fatalf("overlay = %v", m.overlay)
	}
	view := m.View()
	for _, want := range []string{"hello", "Plugin hello", "v0.1.0", "API 1", "permissions: metadata", "broken", "plugins/broken/missing"} {
		if !strings.Contains(view, want) {
			t.Errorf("plugin list missing %q", want)
		}
	}
	if strings.Contains(view, root) {
		t.Error("plugin list shows the absolute plugin directory")
	}
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	if next.(Model).overlay != overlayNone {
		t.Fatal("esc should close the plugin list")
	}
}

func TestPluginMessageMetadataFields(t *testing.T) {
	m, msgs := newMailboxListModel(7, 1)
	msg := msgs[0]
	msg.Date = time.Date(2026, 9, 26, 10, 0, 0, 0, time.UTC)
	msg.Flags = []string{"\\Seen"}
	msg.BodyText, msg.Summary, msg.Headers = "body", "summary", "headers"
	meta := m.pluginMessageMetadata(msg)
	if meta.Date != "2026-09-26T10:00:00Z" || meta.AccountName != "Personal" || meta.MailboxName != "INBOX" || meta.ID != msg.ID {
		t.Fatalf("meta = %+v", meta)
	}
	// Search results carry their own names, which win.
	msg.AccountName, msg.MailboxName = "Work", "Archive"
	meta = m.pluginMessageMetadata(msg)
	if meta.AccountName != "Work" || meta.MailboxName != "Archive" {
		t.Fatalf("meta = %+v", meta)
	}
}

func TestFormatPluginDataSanitizesAndCaps(t *testing.T) {
	got := formatPluginData(json.RawMessage(`{"summary":"a\u001b[31mb\u202ec\u200bd\u2028e"}`))
	for _, bad := range []rune{0x1b, 0x202e, 0x200b, 0x2028} {
		if strings.ContainsRune(got, bad) {
			t.Fatalf("%U survived: %q", bad, got)
		}
	}
	if !strings.Contains(got, `"summary"`) {
		t.Fatalf("not pretty-printed: %q", got)
	}
	if formatPluginData(nil) != "" || formatPluginData(json.RawMessage("null")) != "" {
		t.Fatal("empty data should format as empty")
	}

	items := make([]string, 1000)
	for i := range items {
		items[i] = fmt.Sprintf("%q", strings.Repeat("x", 40))
	}
	big := formatPluginData(json.RawMessage("[" + strings.Join(items, ",") + "]"))
	if !strings.HasSuffix(big, "… truncated") || strings.Count(big, "\n") > pluginResultMaxLines+1 || len(big) > pluginResultMaxBytes+32 {
		t.Fatalf("output not capped: %d bytes, %d lines", len(big), strings.Count(big, "\n"))
	}
}

func TestCloseSessionsCancelsRunningPlugins(t *testing.T) {
	m, _ := newMailboxListModel(7, 1)
	m.SetPlugins(nil, "", nil)
	ctx := m.plugins.ctx
	m.CloseSessions()
	if ctx.Err() == nil {
		t.Fatal("plugin context should be cancelled on shutdown")
	}
}
