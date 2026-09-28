package ui

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/lipgloss"
)

// memorySecrets replaces the keychain in tests.
type memorySecrets struct {
	mu     sync.Mutex
	values map[string]string
}

func (s *memorySecrets) Get(pluginID, key string) (string, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()
	v, ok := s.values[pluginID+"/"+key]
	return v, ok
}

func (s *memorySecrets) Set(pluginID, key, value string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.values[pluginID+"/"+key] = value
	return nil
}

func (s *memorySecrets) Delete(pluginID, key string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.values, pluginID+"/"+key)
	return nil
}

func useMemorySecrets(t *testing.T) *memorySecrets {
	t.Helper()
	mem := &memorySecrets{values: map[string]string{}}
	prev := pluginSecrets
	pluginSecrets = mem
	t.Cleanup(func() { pluginSecrets = prev })
	return mem
}

const testSecret = "sk-live-TOPSECRET-123456"

const settingsManifest = `capabilities = ["plugin.test"]
events = ["message.received"]

[permissions]
message_metadata = true
annotations = true

[[settings]]
key = "mode"
label = "Classification mode"
type = "select"
default = "hybrid"
options = ["local", "hybrid", "jev"]

[[settings]]
key = "jev_enabled"
label = "TypeSafe / Jev"
type = "bool"
default = true

[[settings]]
key = "api_key"
label = "API key"
type = "secret"
help = "Stored in the system keychain."
`

// installSettingsPlugin writes a plugin that answers plugin.test by echoing
// its secret back (which TideMail must mask) and anything else with its
// settings.
func installSettingsPlugin(t *testing.T, root, id string) {
	t.Helper()
	installDataPlugin(t, root, id, `{}`, settingsManifest)
	script := `#!/bin/sh
read -r line
id=$(printf '%s' "$line" | sed -n 's/.*"request_id":"\([^"]*\)".*/\1/p')
printf '{"api":1,"type":"response","request_id":"%s","ok":true,"data":{"message":"connected with key %s","request":%s}}\n' "$id" "$TIDEMAIL_SECRET_API_KEY" "$line"
`
	if err := os.WriteFile(filepath.Join(root, id, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func openSettingsForm(t *testing.T, m Model, id string) Model {
	t.Helper()
	m = openPluginList(t, m)
	for i, e := range m.pluginListEntries() {
		if e.pluginID == id {
			m.plugins.listCursor = i
		}
	}
	next, cmd := press(t, m, "s")
	if next.overlay != overlayPluginSettings {
		t.Fatalf("overlay = %v", next.overlay)
	}
	if cmd != nil {
		updated, _ := next.Update(cmd())
		next = updated.(Model)
	}
	return next
}

// rowIndex finds a form row by its label.
func rowIndex(t *testing.T, m Model, label string) int {
	t.Helper()
	for i, row := range m.pluginSettingsRows(m.plugins.settings.pluginID) {
		if l, _ := m.settingsRowText(m.plugins.settings.pluginID, row); l == label {
			return i
		}
	}
	t.Fatalf("no row %q", label)
	return -1
}

func TestPluginSettingsFormRendersSchema(t *testing.T) {
	useMemorySecrets(t)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	m = openSettingsForm(t, m, "smart")
	view := m.View()
	for _, want := range []string{"Auto-process new mail", "[off]", "Classification mode", "‹ hybrid ›", "TypeSafe / Jev", "[on]", "API key", "not set", "Test plugin configuration"} {
		if !strings.Contains(view, want) {
			t.Errorf("form missing %q", want)
		}
	}
}

func TestPluginSettingsSelectAndBoolPersist(t *testing.T) {
	useMemorySecrets(t)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	m = openSettingsForm(t, m, "smart")

	m.plugins.settings.cursor = rowIndex(t, m, "Classification mode")
	m, _ = press(t, m, "enter")
	m.plugins.settings.cursor = rowIndex(t, m, "TypeSafe / Jev")
	m, _ = press(t, m, " ")
	if view := m.View(); !strings.Contains(view, "‹ jev ›") || !strings.Contains(view, "TypeSafe / Jev") {
		t.Fatal("form did not update")
	}
	saved, err := config.Load()
	if err != nil {
		t.Fatal(err)
	}
	stored := saved.PluginStoredSettings("smart")
	if stored["mode"] != "jev" || stored["jev_enabled"] != false {
		t.Fatalf("saved = %v", stored)
	}
	// The manager hands the plugin the new values.
	if got := m.plugins.settingsSrc.StoredSettings("smart"); got["mode"] != "jev" {
		t.Fatalf("source = %v", got)
	}
	// Left cycles back.
	m.plugins.settings.cursor = rowIndex(t, m, "Classification mode")
	m, _ = press(t, m, "left")
	if saved, _ := config.Load(); saved.PluginStoredSettings("smart")["mode"] != "hybrid" {
		t.Fatal("left should select the previous option")
	}
}

func TestPluginSecretSetMaskedAndCleared(t *testing.T) {
	mem := useMemorySecrets(t)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	m = openSettingsForm(t, m, "smart")
	m.plugins.settings.cursor = rowIndex(t, m, "API key")

	m, _ = press(t, m, "enter")
	if !m.plugins.settings.editing {
		t.Fatal("enter should start entering the secret")
	}
	for _, r := range testSecret {
		m, _ = press(t, m, string(r))
	}
	if strings.Contains(m.View(), testSecret) {
		t.Fatal("secret characters were echoed")
	}
	m, cmd := press(t, m, "enter")
	if m.plugins.settings.input.Value() != "" {
		t.Fatal("the typed secret must not stay in model state")
	}
	next, _ := m.Update(cmd())
	m = next.(Model)
	if v, _ := mem.Get("smart", "api_key"); v != testSecret {
		t.Fatalf("stored secret = %q", v)
	}
	view := m.View()
	if !strings.Contains(view, secretMask) || strings.Contains(view, testSecret) {
		t.Fatal("secret should display masked")
	}
	data, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tidemail", "config.toml"))
	if err == nil && strings.Contains(string(data), testSecret) {
		t.Fatal("secret written to config.toml")
	}
	for _, e := range m.logBuffer {
		if strings.Contains(e.Message, testSecret) {
			t.Fatal("secret in the log")
		}
	}

	m, cmd = press(t, m, "x")
	next, _ = m.Update(cmd())
	m = next.(Model)
	if _, ok := mem.Get("smart", "api_key"); ok {
		t.Fatal("secret not cleared")
	}
	if !strings.Contains(m.View(), "not set") {
		t.Fatal("cleared secret should show not set")
	}
}

func TestPluginTestRunsAsyncAndMasksSecret(t *testing.T) {
	mem := useMemorySecrets(t)
	_ = mem.Set("smart", "api_key", testSecret)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	m = openSettingsForm(t, m, "smart")
	m.plugins.settings.cursor = rowIndex(t, m, "Test plugin configuration")

	m, cmd := press(t, m, "enter")
	if cmd == nil || !m.plugins.settings.testing || m.overlay != overlayPluginSettings {
		t.Fatal("test should start in the background")
	}
	msg := cmd()
	result, ok := msg.(pluginTestResultMsg)
	if !ok || result.Err != nil {
		t.Fatalf("msg = %#v", msg)
	}
	next, _ := m.Update(result)
	m = next.(Model)
	if m.overlay != overlayPluginResult || !strings.Contains(m.statusMsg, "connected with key ********") {
		t.Fatalf("overlay = %v status = %q", m.overlay, m.statusMsg)
	}
	view := m.View()
	for _, s := range []string{view, m.statusMsg, m.plugins.result.body} {
		if strings.Contains(s, testSecret) {
			t.Fatal("secret shown in the test result")
		}
	}
	// The plugin received its settings, resolved.
	if !strings.Contains(m.plugins.result.body, "Mode: hybrid") {
		t.Fatalf("result = %s", m.plugins.result.body)
	}
}

func TestPluginWithoutSettingsHasNoPage(t *testing.T) {
	root := t.TempDir()
	installDataPlugin(t, root, "plain", twoAnnotations, permMetaAndAnnotations)
	m, _, _ := newEventModel(t, root, 1)
	if m.hasPluginSettingsPage("plain") {
		t.Fatal("a plugin with nothing to configure should have no settings page")
	}
	m = openPluginList(t, m)
	if strings.Contains(m.View(), "s settings") {
		t.Fatal("settings hint shown for a plugin without settings")
	}
	m, _ = press(t, m, "s")
	if m.overlay != overlayPlugins {
		t.Fatal("s should do nothing without settings")
	}
}

func TestSettingsPluginsInline(t *testing.T) {
	useMemorySecrets(t)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	next, _ := m.executeCommand("settings")
	m = next.(Model)
	m.settings.setActiveSection(ssPlugins)
	m, _ = press(t, m, "right")
	if m.overlay != overlaySettings || !strings.Contains(m.View(), "Plugin smart") {
		t.Fatal("plugin list should render directly in Settings")
	}
	m, _ = press(t, m, "s")
	if m.overlay != overlaySettings || !m.settings.pluginEditing {
		t.Fatal("plugin form should stay inside Settings")
	}
	m.plugins.settings.cursor = rowIndex(t, m, "Classification mode")
	m, _ = press(t, m, "enter")
	if got := m.cfg.PluginStoredSettings("smart")["mode"]; got != "jev" {
		t.Fatalf("inline select did not persist: %v", got)
	}
	m.plugins.settings.cursor = rowIndex(t, m, "Auto-process new mail")
	m, _ = press(t, m, "enter")
	if m.overlay != overlayPluginConfirm || m.plugins.confirmOrigin != overlaySettings {
		t.Fatal("auto-processing confirmation must return to the Settings pane")
	}
	m, _ = press(t, m, "esc")
	if m.overlay != overlaySettings || !m.settings.pluginEditing {
		t.Fatal("canceling confirmation lost the inline form")
	}
	m, _ = press(t, m, "esc")
	if m.overlay != overlaySettings || m.settings.pluginEditing || m.settings.focusedPane != settingsPaneDetail {
		t.Fatal("esc should return from the form to the inline plugin list")
	}
	m, _ = press(t, m, "esc")
	if m.overlay != overlaySettings || m.settings.focusedPane != settingsPaneSidebar {
		t.Fatal("esc from plugin list should focus sections")
	}
}

func TestSettingsPluginPaneEmptyAndBounded(t *testing.T) {
	trueColor(t)
	m, _ := newMailboxListModel(7, 1)
	next, _ := m.executeCommand("settings")
	m = next.(Model)
	m.settings.setActiveSection(ssPlugins)
	chrome := newManagerChrome(60, CatppuccinMocha, false)
	if out := m.renderSettingsPluginPane(60, 20, chrome); !strings.Contains(out, "no plugins installed") {
		t.Fatal("missing empty state")
	}
	for _, size := range [][2]int{{24, 8}, {40, 12}, {60, 24}} {
		out := m.renderSettingsPluginPane(size[0], size[1], chrome)
		gap := strings.Split(out, "\n")[1]
		if at := unpaintedCell(gap); at >= 0 {
			t.Fatalf("unpainted header gap at byte %d: %q", at, gap)
		}
		if lipgloss.Width(out) > size[0] || lipgloss.Height(out) > size[1] {
			t.Fatalf("pane exceeds %dx%d", size[0], size[1])
		}
	}
}

func TestSettingsPluginSecretStaysInline(t *testing.T) {
	useMemorySecrets(t)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	next, _ := m.executeCommand("settings")
	m = next.(Model)
	m.settings.setActiveSection(ssPlugins)
	m, _ = press(t, m, "right")
	m, _ = press(t, m, "s")
	m.plugins.settings.cursor = rowIndex(t, m, "API key")
	m, _ = press(t, m, "enter")
	m, _ = press(t, m, "q")
	if m.overlay != overlaySettings || !m.plugins.settings.editing || m.plugins.settings.input.Value() != "q" {
		t.Fatal("secret keystrokes should reach the input")
	}
	m.plugins.settings.input.SetValue("private-test-key")
	view := m.renderSettingsPluginPane(40, 12, newManagerChrome(40, CatppuccinMocha, false))
	if strings.Contains(view, "private-test-key") || !strings.Contains(view, "new value:") {
		t.Fatal("secret input must be visible and masked")
	}
	m, _ = press(t, m, "esc")
	if m.plugins.settings.editing || !m.settings.pluginEditing || m.overlay != overlaySettings {
		t.Fatal("esc should cancel secret entry without leaving the inline form")
	}
}

func TestAutoRowInSettingsUsesConfirmation(t *testing.T) {
	useMemorySecrets(t)
	root := t.TempDir()
	installSettingsPlugin(t, root, "smart")
	m, _, _ := newEventModel(t, root, 1)
	m = openSettingsForm(t, m, "smart")
	m.plugins.settings.cursor = rowIndex(t, m, "Auto-process new mail")
	m, _ = press(t, m, "enter")
	if m.overlay != overlayPluginConfirm {
		t.Fatalf("overlay = %v", m.overlay)
	}
	m, _ = press(t, m, "y")
	if m.overlay != overlayPluginSettings || !m.cfg.PluginAutoEvents("smart") {
		t.Fatal("confirming should enable and return to the form")
	}
}
