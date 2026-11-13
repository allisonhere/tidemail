package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/plugin"
)

// installContactsPlugin installs a contacts_read plugin that answers every
// contacts.search with data (a JSON object).
func installContactsPlugin(t *testing.T, root, data string) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("shell-script test plugins need a Unix shell")
	}
	dir := filepath.Join(root, "book")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	manifest := "id = \"book\"\nname = \"Book\"\napi = 1\ncommand = \"run\"\n\n[permissions]\ncontacts_read = true\n"
	reply := fmt.Sprintf(`{"api":1,"type":"response","request_id":"%%s","ok":true,"data":%s}`, data)
	script := "#!/bin/sh\nread -r line\nid=$(printf '%s' \"$line\" | sed -n 's/.*\"request_id\":\"\\([^\"]*\\)\".*/\\1/p')\nprintf '" + reply + "\\n' \"$id\"\n"
	if err := os.WriteFile(filepath.Join(dir, plugin.ManifestFile), []byte(manifest), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "run"), []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
}

func contactsModel(t *testing.T, provider string) Model {
	t.Helper()
	root := t.TempDir()
	installContactsPlugin(t, root, `{"contacts":[{"addr":"ann@example.com","name":"Ann Lee"},{"addr":"anna@example.com"}]}`)
	m := newSendTestModel(t, 5)
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	m.cfg.Contacts = config.ContactsConfig{Provider: provider}
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.overlay = overlayCompose
	m.compose.focusedField = composeFieldTo
	m.compose.toInput.SetValue("bob@example.com, an")
	return m
}

var ctrlT = tea.KeyMsg{Type: tea.KeyCtrlT}

func TestLookupContactsNeedsAProvider(t *testing.T) {
	m := contactsModel(t, "")
	next, cmd := m.handleCompose(ctrlT)
	m = next.(Model)
	if cmd != nil || !m.compose.isErr || !strings.Contains(m.compose.statusMsg, "No contacts provider") {
		t.Fatalf("no provider: cmd=%v err=%v status=%q", cmd != nil, m.compose.isErr, m.compose.statusMsg)
	}
	m = contactsModel(t, "nope")
	next, cmd = m.handleCompose(ctrlT)
	if cmd != nil || !strings.Contains(next.(Model).compose.statusMsg, "not installed") {
		t.Fatalf("unknown provider: cmd=%v status=%q", cmd != nil, next.(Model).compose.statusMsg)
	}
}

func TestLookupContactsFillsTheDropdown(t *testing.T) {
	m := contactsModel(t, "book")
	next, cmd := m.handleCompose(ctrlT)
	m = next.(Model)
	if cmd == nil {
		t.Fatalf("expected a lookup command, status=%q", m.compose.statusMsg)
	}
	msg := cmd()
	found, ok := msg.(ContactsFoundMsg)
	if !ok || found.Err != nil || found.Query != "an" {
		t.Fatalf("lookup result = %#v", msg)
	}
	next, _ = m.Update(found)
	m = next.(Model)
	want := []string{"Ann Lee <ann@example.com>", "anna@example.com"}
	if len(m.compose.suggestions) != 2 || m.compose.suggestions[0] != want[0] || m.compose.suggestions[1] != want[1] {
		t.Fatalf("suggestions = %q, want %q", m.compose.suggestions, want)
	}
	m.compose.acceptSuggestion()
	if got := m.compose.toInput.Value(); got != "bob@example.com, Ann Lee <ann@example.com>" {
		t.Fatalf("accepting kept the wrong text: %q", got)
	}
}

func TestLookupContactsDropsAnAnswerForOldText(t *testing.T) {
	m := contactsModel(t, "book")
	m.compose.toInput.SetValue("bob@example.com, zed")
	next, _ := m.Update(ContactsFoundMsg{Query: "an", Matches: []plugin.ContactMatch{{Addr: "ann@example.com"}}})
	if got := next.(Model).compose.suggestions; len(got) != 0 {
		t.Fatalf("a stale answer reached the dropdown: %q", got)
	}
}
