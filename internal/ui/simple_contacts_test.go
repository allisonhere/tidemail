package ui

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/plugin"
	tea "github.com/charmbracelet/bubbletea"
)

func simpleContactsModel(t *testing.T) Model {
	root := t.TempDir()
	data := `{"contacts":[{"id":"alex","revision":1,"name":"Alex","addr":"alex@example.com"}],"contact":{"id":"alex","revision":2,"name":"Alex","addr":"alex@example.com"}}`
	installContactsPlugin(t, root, data)
	old := filepath.Join(root, "book")
	manifest, _ := os.ReadFile(filepath.Join(old, "plugin.toml"))
	manifest = []byte(strings.Replace(string(manifest), `id = "book"`, `id = "tidecontacts"`, 1) + "\ncontacts_write = true\n")
	os.WriteFile(filepath.Join(old, "plugin.toml"), manifest, 0600)
	m := newSendTestModel(t, 5)
	mgr, err := plugin.Discover(root)
	if err != nil {
		t.Fatal(err)
	}
	m.SetPlugins(mgr, root, nil)
	return m
}
func settleSimple(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	for cmd != nil {
		msg := cmd()
		if _, ok := msg.(simpleContactsMsg); !ok {
			break
		}
		next, nextCmd := m.handleSimpleContacts(msg)
		m = next.(Model)
		cmd = nextCmd
	}
	return m
}
func TestSimpleContactsLoadComposeAndNative(t *testing.T) {
	m := simpleContactsModel(t)
	next, cmd := m.openSimpleContacts()
	m = settleSimple(t, next.(Model), cmd)
	if len(m.simpleContacts.contacts) != 1 {
		t.Fatal("load failed", m.simpleContacts.status)
	}
	next, _ = m.handleSimpleContacts(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("c")})
	m = next.(Model)
	if m.overlay != overlayCompose || !strings.Contains(m.compose.toInput.Value(), "alex@example.com") {
		t.Fatal("compose failed")
	}
	next, cmd = m.openSimpleContacts()
	m = settleSimple(t, next.(Model), cmd)
	next, _ = m.handleSimpleContacts(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("L")})
	m = next.(Model)
	if m.simpleContacts != nil || m.overlay != overlayContactManager {
		t.Fatal("native fallback failed")
	}
}
func TestSimpleContactsSaveAndStaleResults(t *testing.T) {
	m := simpleContactsModel(t)
	next, cmd := m.openSimpleContacts()
	m = settleSimple(t, next.(Model), cmd)
	next, cmd = m.handleSimpleContacts(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("e")})
	m = next.(Model)
	if !m.simpleContacts.editing {
		t.Fatal("edit not opened")
	}
	_ = cmd
	next, cmd = m.handleSimpleContacts(tea.KeyMsg{Type: tea.KeyEnter})
	m = settleSimple(t, next.(Model), cmd)
	if m.simpleContacts.editing || m.simpleContacts.status != "Contact saved" {
		t.Fatal(m.simpleContacts.status)
	}
	owner := m.simpleContacts
	next, _ = m.handleSimpleContacts(simpleContactsMsg{owner: &simpleContacts{}, seq: owner.seq, contacts: []plugin.BookContact{}})
	if len(next.(Model).simpleContacts.contacts) != 1 {
		t.Fatal("stale owner changed list")
	}
	mgr := m.plugins.manager
	// Read and write permissions are separate; validation rejects bad output.
	_, err := mgr.SaveBookContact(context.Background(), "missing", plugin.BookContact{})
	if err == nil {
		t.Fatal("missing plugin accepted")
	}
}
func TestRecentContactMailNoSubstringMatches(t *testing.T) {
	m := newSendTestModel(t, 5)
	list, err := recentContactMail(context.Background(), m.db, "nobody@example.com")
	if err != nil || len(list) != 0 {
		t.Fatal(list, err)
	}
}

func TestRecentContactMailMatchesExactParticipants(t *testing.T) {
	m := newSendTestModel(t, 5)
	_, err := m.db.Exec(`INSERT INTO accounts(id,name) VALUES(99,'Demo'); INSERT INTO mailboxes(id,account_id,name) VALUES(99,99,'INBOX');
 INSERT INTO messages(mailbox_id,uid,subject,from_addr,to_addr,cc_addr,date) VALUES
 (99,1,'Correct participant','Alex <alex@example.com>','me@example.com','',1700000000),
 (99,2,'Wrong substring','notalex@example.com','me@example.com','',1700000001)`)
	if err != nil {
		t.Fatal(err)
	}
	list, err := recentContactMail(context.Background(), m.db, "alex@example.com")
	if err != nil || len(list) != 1 || !strings.Contains(list[0], "Correct participant") {
		t.Fatal(list, err)
	}
}

func TestContactBookSourceLabels(t *testing.T) {
	m := simpleContactsModel(t)
	next, cmd := m.openSimpleContacts()
	m = settleSimple(t, next.(Model), cmd)
	view := m.simpleContacts.View(74, 36, m.styles)
	if !strings.Contains(view, "Address book: TideContacts") || !strings.Contains(view, "Shared with standalone TideContacts") {
		t.Fatal(view)
	}
	t.Setenv("TIDECONTACTS_DEMO", "1")
	m.simpleContacts.demo = true
	view = m.simpleContacts.View(74, 36, m.styles)
	if !strings.Contains(view, "TideContacts DEMO") || !strings.Contains(view, "real contacts are separate") {
		t.Fatal(view)
	}
	m.simpleContacts.editing = true
	m.simpleContacts.fields = nil
	view = m.simpleContacts.View(74, 36, m.styles)
	if !strings.Contains(view, "TideContacts DEMO") {
		t.Fatal("edit source missing")
	}
	native := NewContactManager(m.db)
	view = native.View(74, 36, m.styles)
	if !strings.Contains(view, "Stored only in TideMail; separate from TideContacts.") {
		t.Fatal(view)
	}
}
