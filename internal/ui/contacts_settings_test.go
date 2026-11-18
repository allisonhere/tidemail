package ui

import (
	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func contactsSettingsModel(t *testing.T) Model {
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())
	t.Setenv("TIDECONTACTS_DEMO", "")
	m := simpleContactsModel(t)
	root := m.plugins.manager.Dir()
	path := filepath.Join(root, "book", "plugin.toml")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	text := strings.Replace(string(raw), "[permissions]", "capabilities = [\"plugin.test\"]\n[permissions]", 1) + `
[[settings]]
key = "default_view"
label = "Default contacts view"
type = "select"
default = "TideContacts"
options = ["TideContacts", "TideMail local"]
[[settings]]
key = "data_profile"
label = "Contact data"
type = "select"
default = "Real contacts"
options = ["Real contacts", "Demo contacts"]
`
	if err := os.WriteFile(path, []byte(text), 0600); err != nil {
		t.Fatal(err)
	}
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	return m
}
func TestTideContactsSettingsRowsAndDefault(t *testing.T) {
	m := contactsSettingsModel(t)
	rows := m.pluginSettingsRows("tidecontacts")
	if len(rows) != 4 {
		t.Fatal(rows)
	}
	m.plugins.settings.pluginID = "tidecontacts"
	if label, _ := m.settingsRowText("tidecontacts", rows[2]); label != "Check connection" {
		t.Fatal(label)
	}
	if label, _ := m.settingsRowText("tidecontacts", rows[3]); label != "Open Advanced CRM" {
		t.Fatal(label)
	}
	m.cfg.SetPluginSetting("tidecontacts", "default_view", "TideMail local")
	m.overlay = overlayNone
	next, _ := m.handleKey(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("C")})
	m = next.(Model)
	if m.simpleContacts != nil || m.overlay != overlayContactManager {
		t.Fatal("local view not honoured")
	}
	m.cfg.SetPluginSetting("tidecontacts", "default_view", "TideContacts")
	m.cfg.SetPluginSetting("tidecontacts", "data_profile", "Demo contacts")
	next, _ = m.openSimpleContacts()
	m = next.(Model)
	if m.simpleContacts == nil || !m.simpleContacts.demo {
		t.Fatal("demo setting not honoured")
	}
}
func TestTideContactsSettingsPersistAndForcedDemo(t *testing.T) {
	m := contactsSettingsModel(t)
	m.savePluginSetting("tidecontacts", "default_view", "TideMail local")
	if m.tideContactsViewEnabled() {
		t.Fatal("setting not applied")
	}
	raw, err := os.ReadFile(filepath.Join(os.Getenv("XDG_CONFIG_HOME"), "tidemail", "config.toml"))
	if err != nil || !strings.Contains(string(raw), "TideMail local") {
		t.Fatal("setting not persisted", err)
	}
	t.Setenv("TIDECONTACTS_DEMO", "1")
	m.cfg.SetPluginSetting("tidecontacts", "data_profile", "Real contacts")
	if !m.tideContactsDemo() {
		t.Fatal("demo launcher escaped")
	}
	m.plugins.settings.pluginID = "tidecontacts"
	rows := m.pluginSettingsRows("tidecontacts")
	label, value := m.settingsRowText("tidecontacts", rows[1])
	if label != "Contact data" || value != "Demo contacts (forced)" {
		t.Fatal(label, value)
	}
}

func TestAdvancedCRMUsesRealTerminal(t *testing.T) {
	cmd := advancedCRMCommand("tidecontacts", []string{"--demo"})
	if cmd.Stdin != os.Stdin || cmd.Stdout != os.Stdout || cmd.Stderr != os.Stderr {
		t.Fatal("interactive child must inherit real terminal files")
	}
	if len(cmd.Args) != 2 || cmd.Args[1] != "--demo" {
		t.Fatal(cmd.Args)
	}
}
