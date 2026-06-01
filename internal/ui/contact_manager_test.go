package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func rune1(r rune) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{r}} }

func newContactTestDB(t *testing.T) *db.DB {
	t.Helper()
	database, err := db.Open()
	if err != nil {
		t.Fatalf("open db: %v", err)
	}
	for _, stmt := range []string{
		"DELETE FROM attachments",
		"DELETE FROM messages",
		"DELETE FROM mailboxes",
		"DELETE FROM accounts",
		"DELETE FROM contacts",
	} {
		if _, err := database.Exec(stmt); err != nil {
			t.Fatalf("reset db with %q: %v", stmt, err)
		}
	}
	t.Cleanup(func() { database.Close() })
	return database
}

func TestContactManagerAddContact(t *testing.T) {
	d := newContactTestDB(t)
	cm := NewContactManager(d)

	cm, _, _ = cm.Update(rune1('n'), DefaultKeys)
	if cm.mode != cmAdd {
		t.Fatalf("expected add mode, got %v", cm.mode)
	}
	cm.nameInput.SetValue("Dave")
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	cm.emailInput.SetValue("DAVE@example.com")
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	if cm.mode != cmList {
		t.Fatalf("expected return to list, got %v", cm.mode)
	}
	addrs, _ := d.ContactAddresses()
	if len(addrs) != 1 || addrs[0] != "Dave <dave@example.com>" {
		t.Fatalf("manual contact not in autocomplete: %v", addrs)
	}
}

func TestContactManagerEditSavesMetadataFields(t *testing.T) {
	d := newContactTestDB(t)
	id, err := d.AddContactWithMetadata("meta@example.com", "Meta", "manual", db.ContactMetadata{
		Phone:        "555-0100",
		Organization: "Example Co",
		Title:        "Engineer",
		Note:         "Met at conf",
	})
	if err != nil {
		t.Fatal(err)
	}
	cm := NewContactManager(d)

	cm, _, _ = cm.Update(rune1('e'), DefaultKeys)
	if cm.mode != cmEdit || cm.editID != id {
		t.Fatalf("expected edit mode for %d, got mode=%v id=%d", id, cm.mode, cm.editID)
	}
	if cm.phoneInput.Value() != "555-0100" || cm.organizationInput.Value() != "Example Co" || cm.titleInput.Value() != "Engineer" || cm.noteInput.Value() != "Met at conf" {
		t.Fatalf("metadata did not preload")
	}

	cm.phoneInput.SetValue("555-0101")
	cm.organizationInput.SetValue("New Co")
	cm.titleInput.SetValue("Lead")
	cm.noteInput.SetValue("Updated note")
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	got, err := d.ListContacts()
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Phone != "555-0101" || got[0].Organization != "New Co" || got[0].Title != "Lead" || got[0].Note != "Updated note" {
		t.Fatalf("metadata did not save: %+v", got)
	}
}

func TestContactManagerFormScrollsFocusedFieldIntoView(t *testing.T) {
	d := newContactTestDB(t)
	cm := NewContactManager(d)
	cm.beginAdd()
	cm.setFormFocus(5)

	view := ansi.Strip(cm.View(60, 8, BuildStyles(CatppuccinMocha, "compact")))
	if !strings.Contains(view, "Note") {
		t.Fatalf("focused Note field should be visible in short form:\n%s", view)
	}
	if strings.Contains(view, "Name") {
		t.Fatalf("top form rows should scroll out when Note is focused:\n%s", view)
	}
}

func TestContactManagerListScrolls(t *testing.T) {
	d := newContactTestDB(t)
	for i := 0; i < 40; i++ {
		if _, err := d.AddContact(fmt.Sprintf("c%02d@example.com", i), "", "manual"); err != nil {
			t.Fatal(err)
		}
	}
	cm := NewContactManager(d)
	if len(cm.contacts) != 40 {
		t.Fatalf("setup: contacts=%d", len(cm.contacts))
	}

	const w, h = 60, 12
	top := ansi.Strip(cm.View(w, h, BuildStyles(CatppuccinMocha, "compact")))
	last := cm.contacts[len(cm.contacts)-1].Addr
	if strings.Contains(top, last) {
		t.Fatalf("last contact should not be visible before scrolling:\n%s", top)
	}

	for i := 0; i < len(cm.contacts)-1; i++ {
		cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	}
	bottom := ansi.Strip(cm.View(w, h, BuildStyles(CatppuccinMocha, "compact")))
	if !strings.Contains(bottom, last) {
		t.Fatalf("selected last contact not visible after scrolling:\n%s", bottom)
	}
	if strings.Contains(bottom, cm.contacts[0].Addr) {
		t.Fatalf("first contact should have scrolled off:\n%s", bottom)
	}
}

func TestContactManagerBulkDelete(t *testing.T) {
	d := newContactTestDB(t)
	for i := 0; i < 3; i++ {
		if _, err := d.AddContact(fmt.Sprintf("a%d@example.com", i), "", "manual"); err != nil {
			t.Fatal(err)
		}
	}
	cm := NewContactManager(d)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	if len(cm.marked) != 2 {
		t.Fatalf("expected 2 marked, got %d", len(cm.marked))
	}
	cm, _, _ = cm.Update(rune1('d'), DefaultKeys)
	if cm.mode != cmConfirmDelete {
		t.Fatalf("expected confirm-delete mode, got %v", cm.mode)
	}
	cm, _, _ = cm.Update(rune1('y'), DefaultKeys)

	if len(cm.contacts) != 1 {
		t.Fatalf("expected 1 remaining contact after bulk delete, got %d", len(cm.contacts))
	}
	if addrs, _ := d.ContactAddresses(); len(addrs) != 1 {
		t.Fatalf("expected 1 autocomplete address after delete, got %v", addrs)
	}
}

func TestContactManagerPickerAddsSeenAddresses(t *testing.T) {
	d := newContactTestDB(t)
	accountID, _ := d.AddAccount("Acct", "")
	mailboxID, _ := d.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	_ = d.UpsertMessage(db.Message{
		MailboxID: mailboxID,
		UID:       1,
		From:      "Carol <carol@example.com>",
		To:        "Alice <alice@example.com>, bob@example.com",
	})
	_, _ = d.AddContact("alice@example.com", "Alice", "manual")

	cm := NewContactManager(d)
	cm, _, _ = cm.Update(rune1('f'), DefaultKeys)
	if cm.mode != cmPickSeen {
		t.Fatalf("expected picker mode, got %v", cm.mode)
	}
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("caro"), Paste: true}, DefaultKeys)
	if len(cm.filteredSeen()) != 1 || cm.filteredSeen()[0].Addr != "carol@example.com" {
		t.Fatalf("filter did not narrow to carol: %+v", cm.filteredSeen())
	}
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	addrs, _ := d.ContactAddresses()
	if len(addrs) != 2 || addrs[0] != "Alice <alice@example.com>" || addrs[1] != "Carol <carol@example.com>" {
		t.Fatalf("picker did not add carol to contacts: %v", addrs)
	}
}

func TestContactManagerImportsVCardThroughFilePicker(t *testing.T) {
	d := newContactTestDB(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(home, "contacts.vcf")
	if err := os.WriteFile(path, []byte("BEGIN:VCARD\r\nVERSION:4.0\r\nFN:Imported Person\r\nEMAIL:imported@example.com\r\nEND:VCARD\r\n"), 0o644); err != nil {
		t.Fatal(err)
	}

	cm := NewContactManager(d)
	cm, _, _ = cm.Update(rune1('i'), DefaultKeys)
	if cm.mode != cmPickFile {
		t.Fatalf("expected file picker mode, got %v", cm.mode)
	}
	cm, _, _ = cm.Update(rune1('c'), DefaultKeys)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	addrs, _ := d.ContactAddresses()
	if len(addrs) != 1 || addrs[0] != "Imported Person <imported@example.com>" {
		t.Fatalf("import picker did not add contact: %v", addrs)
	}
}

func TestContactManagerExportsVCardThroughFolderPicker(t *testing.T) {
	d := newContactTestDB(t)
	_, _ = d.AddContact("exported@example.com", "Exported Person", "manual")
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	downloads := filepath.Join(home, "Downloads")
	if err := os.MkdirAll(downloads, 0o755); err != nil {
		t.Fatal(err)
	}

	cm := NewContactManager(d)
	cm, _, _ = cm.Update(rune1('x'), DefaultKeys)
	if cm.mode != cmPickFile {
		t.Fatalf("expected folder picker mode, got %v", cm.mode)
	}
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)

	data, err := os.ReadFile(filepath.Join(downloads, "contacts.vcf"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "exported@example.com") {
		t.Fatalf("exported vcard missing contact:\n%s", string(data))
	}
}

func TestContactManagerEscExits(t *testing.T) {
	d := newContactTestDB(t)
	cm := NewContactManager(d)
	_, _, exit := cm.Update(tea.KeyMsg{Type: tea.KeyEsc}, DefaultKeys)
	if !exit {
		t.Fatal("esc should exit the contact manager")
	}
}

func TestContactManagerComposeSelectedContact(t *testing.T) {
	d := newContactTestDB(t)
	_, _ = d.AddContact("mary@example.com", "Mary", "manual")

	m := NewModel(d, config.DefaultConfig(), "dev", false)
	m.overlay = overlayContactManager
	m.contactManager = NewContactManager(d)

	next, _ := m.Update(rune1('c'))
	m = next.(Model)

	if m.overlay != overlayCompose {
		t.Fatalf("expected compose overlay, got %v", m.overlay)
	}
	if got := m.compose.toInput.Value(); got != "Mary <mary@example.com>" {
		t.Fatalf("expected selected contact in To, got %q", got)
	}
}

func TestContactManagerComposeMarkedContacts(t *testing.T) {
	d := newContactTestDB(t)
	_, _ = d.AddContact("alice@example.com", "Alice", "manual")
	_, _ = d.AddContact("bob@example.com", "Bob", "manual")
	_, _ = d.AddContact("carol@example.com", "Carol", "manual")

	m := NewModel(d, config.DefaultConfig(), "dev", false)
	m.overlay = overlayContactManager
	m.contactManager = NewContactManager(d)
	m.contactManager.toggleMarkAdvance()
	m.contactManager.toggleMarkAdvance()

	next, _ := m.Update(rune1('c'))
	m = next.(Model)

	if m.overlay != overlayCompose {
		t.Fatalf("expected compose overlay, got %v", m.overlay)
	}
	if got := m.compose.toInput.Value(); got != "Alice <alice@example.com>, Bob <bob@example.com>" {
		t.Fatalf("expected marked contacts in To, got %q", got)
	}
	if len(m.contactManager.marked) != 0 {
		t.Fatalf("expected marks to be cleared after composing, got %v", m.contactManager.marked)
	}
}
