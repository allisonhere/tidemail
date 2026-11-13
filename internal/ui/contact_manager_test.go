package ui

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
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

	view := ansi.Strip(cm.View(60, 8, BuildStyles(CatppuccinMocha, "compact", "square")))
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
	top := ansi.Strip(cm.View(w, h, BuildStyles(CatppuccinMocha, "compact", "square")))
	last := cm.contacts[len(cm.contacts)-1].Addr
	if strings.Contains(top, last) {
		t.Fatalf("last contact should not be visible before scrolling:\n%s", top)
	}

	for i := 0; i < len(cm.contacts)-1; i++ {
		cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyDown}, DefaultKeys)
	}
	bottom := ansi.Strip(cm.View(w, h, BuildStyles(CatppuccinMocha, "compact", "square")))
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

func TestContactManagerMarksStayWithContactsAfterReload(t *testing.T) {
	d := newContactTestDB(t)
	for _, name := range []string{"Bob", "Carol", "Zed"} {
		if _, err := d.AddContact(strings.ToLower(name)+"@example.com", name, "manual"); err != nil {
			t.Fatal(err)
		}
	}
	cm := NewContactManager(d)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeySpace}, DefaultKeys)
	if _, err := d.AddContact("amy@example.com", "Amy", "manual"); err != nil {
		t.Fatal(err)
	}
	cm.reload()
	marked := cm.targetContacts()
	if len(marked) != 2 || marked[0].Addr != "bob@example.com" || marked[1].Addr != "carol@example.com" {
		t.Fatalf("marks moved after reload: %+v", marked)
	}
	cm, _, _ = cm.Update(rune1('d'), DefaultKeys)
	cm, _, _ = cm.Update(rune1('y'), DefaultKeys)
	remaining, err := d.ListContacts()
	if err != nil || len(remaining) != 2 || remaining[0].Addr != "amy@example.com" || remaining[1].Addr != "zed@example.com" {
		t.Fatalf("deleted wrong contacts: %+v, %v", remaining, err)
	}
}

func TestContactManagerPickerAddsSeenAddresses(t *testing.T) {
	d := newContactTestDB(t)
	accountID, _ := d.AddAccount("", "Acct", "")
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

func entryNames(cm ContactManager) []string {
	var names []string
	for _, e := range cm.filePicker.entries {
		names = append(names, e.name)
	}
	return names
}

func hasName(names []string, want string) bool {
	for _, n := range names {
		if n == want {
			return true
		}
	}
	return false
}

// Hidden files and folders (a vdirsyncer store lives in ~/.contacts) can be
// shown with the same "." key the attachment picker uses.
func TestContactImportPickerDotTogglesHiddenFiles(t *testing.T) {
	d := newContactTestDB(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(home, ".contacts"), 0o755); err != nil {
		t.Fatal(err)
	}

	cm := NewContactManager(d)
	cm, _, _ = cm.Update(rune1('i'), DefaultKeys)
	if hasName(entryNames(cm), ".contacts") {
		t.Fatal("hidden folder listed before pressing .")
	}
	cm, _, _ = cm.Update(rune1('.'), DefaultKeys)
	if !hasName(entryNames(cm), ".contacts") {
		t.Fatalf("hidden folder not listed after pressing .: %v", entryNames(cm))
	}
	cm, _, _ = cm.Update(rune1('.'), DefaultKeys)
	if hasName(entryNames(cm), ".contacts") {
		t.Fatal("a second . should hide it again")
	}
}

// Import offers a folder row first, and it reads every .vcf file there.
func TestContactImportPickerImportsAWholeFolder(t *testing.T) {
	d := newContactTestDB(t)
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(home, "book")
	if err := os.Mkdir(dir, 0o755); err != nil {
		t.Fatal(err)
	}
	card := func(fn, email string) string {
		s := "BEGIN:VCARD\r\nVERSION:3.0\r\nFN:" + fn + "\r\n"
		if email != "" {
			s += "EMAIL:" + email + "\r\n"
		}
		return s + "END:VCARD\r\n"
	}
	for name, body := range map[string]string{
		"a.vcf": card("Ann", "ann@example.com"), "b.vcf": card("Bob", "bob@example.com"), "c.vcf": card("Cy", ""),
	} {
		if err := os.WriteFile(filepath.Join(dir, name), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}

	cm := NewContactManager(d)
	cm, _, _ = cm.Update(rune1('i'), DefaultKeys)
	if len(cm.filePicker.entries) == 0 || cm.filePicker.entries[0].name != importFolderRow {
		t.Fatalf("import picker should start with the folder row, got %v", entryNames(cm))
	}
	cm, _, _ = cm.Update(rune1('b'), DefaultKeys) // jump to the "book" folder
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys)
	if cm.filePicker.currentDir != dir {
		t.Fatalf("did not open the folder, in %q", cm.filePicker.currentDir)
	}
	cm, _, _ = cm.Update(tea.KeyMsg{Type: tea.KeyEnter}, DefaultKeys) // first row: import this folder

	addrs, _ := d.ContactAddresses()
	if len(addrs) != 2 {
		t.Fatalf("imported %v, want ann and bob", addrs)
	}
	for _, want := range []string{"imported 2 contacts from 3 files", "skipped 1 cards with no email address"} {
		if !strings.Contains(cm.statusMsg, want) {
			t.Fatalf("status %q missing %q", cm.statusMsg, want)
		}
	}
	if cm.isErr {
		t.Fatal("a partly successful import should not be an error")
	}
}

func TestImportSummaryWording(t *testing.T) {
	msg, isErr := importSummary(db.VCardImport{Added: 3}, 0, 0, "")
	if msg != "imported 3 contacts" || isErr {
		t.Fatalf("plain import: %q err=%v", msg, isErr)
	}
	msg, isErr = importSummary(db.VCardImport{NoEmail: 2}, 0, 0, "")
	if !isErr || !strings.Contains(msg, "skipped 2 cards with no email address") {
		t.Fatalf("nothing saved should be an error that says why: %q err=%v", msg, isErr)
	}
	msg, _ = importSummary(db.VCardImport{Added: 1}, 4, 1, "bad.vcf: boom")
	if !strings.Contains(msg, "from 4 files") || !strings.Contains(msg, "1 files failed (bad.vcf: boom)") {
		t.Fatalf("folder summary: %q", msg)
	}
}
