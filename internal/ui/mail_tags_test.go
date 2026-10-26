package ui

import (
	"context"
	"fmt"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	"github.com/allisonhere/tidemail/internal/mailtag"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/emersion/go-imap/v2"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
)

func mailTagTestModel(t *testing.T) (Model, *db.DB, db.Message) {
	t.Helper()
	m, database, messages := newAnnotationDBModel(t, t.TempDir(), 1)
	t.Cleanup(m.sessions.Close)
	message := messages[0]
	if err := database.ApplyServerFlags(message.MailboxID, map[uint32][]string{message.UID: {"$label1"}}); err != nil {
		t.Fatal(err)
	}
	message, _ = database.GetMessage(message.ID)
	m.messages[0] = message
	m.reloadMailTags()
	m.applyFilter()
	return m, database, message
}

func selectCategory(t *testing.T, m Model, category string) Model {
	t.Helper()
	for m.classification.field != classificationCategory {
		m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyDown})
	}
	for i, choice := range m.classification.choices {
		if choice == category {
			m.classification.choice = i
			return m
		}
	}
	t.Fatalf("missing category %s", category)
	return m
}

func TestUnifiedTagEditorAndLocalMapping(t *testing.T) {
	m, database, message := mailTagTestModel(t)
	m, _ = press(t, m, "e")
	if m.overlay != overlayClassification || !strings.Contains(m.View(), "Needs reply") {
		t.Fatal("e must open classification directly")
	}
	m = selectCategory(t, m, "important")
	if view := m.View(); !strings.Contains(view, "[x] Important") || strings.Contains(view, "$label1") || strings.Contains(view, "tab switches") {
		t.Fatalf("wrong editor: %s", view)
	}
	personal := 0
	for _, choice := range m.classification.choices {
		if choice == "personal" {
			personal++
		}
	}
	if personal != 1 {
		t.Fatalf("Personal choices: %d", personal)
	}
	next, _ := m.openMailTagSettings()
	m = next.(Model)
	m, _ = press(t, m, "n")
	if m.mailTags.editor.form {
		t.Fatal("keyword creation still available")
	}
	m, _ = press(t, m, "e")
	m.mailTags.editor.input.SetValue("Critical")
	m, _ = press(t, m, "enter")
	definitions, err := database.ListMailTags()
	if err != nil || len(definitions) != 1 || definitions[0].Name != "Critical" {
		t.Fatalf("rename: %+v / %v", definitions, err)
	}
	current, _ := database.GetMessage(message.ID)
	if !mailtag.Has(current.Flags, "$label1") {
		t.Fatal("rename changed assignment")
	}
	m, _ = press(t, m, "c")
	m.colorPicker.setRGB(18, 52, 86)
	m, _ = press(t, m, "enter")
	definitions, _ = database.ListMailTags()
	if definitions[0].Color != "#123456" || m.overlay != overlayMailTags {
		t.Fatal("color editor failed")
	}
}

func TestDiscoveredTagsAndAccountIsolation(t *testing.T) {
	m, database, message := mailTagTestModel(t)
	otherID, err := database.AddAccount("", "Other", "")
	if err != nil {
		t.Fatal(err)
	}
	m.accounts = append(m.accounts, db.Account{ID: otherID, Name: "Other"})
	if err := database.SaveMailTag(mailtag.Definition{AccountID: otherID, Keyword: "Project-X", Name: "Client Work"}); err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, "S")
	m.settings.setActiveSection(ssDisplay)
	m.settings.setFocusedPane(settingsPaneDetail)
	m.settings.setFocusedField(sfMailTags)
	m, _ = press(t, m, "enter")
	m, _ = pressKey(m, tea.KeyMsg{Type: tea.KeyRight})
	if m.mailTags.editor.accountID != otherID || !strings.Contains(strings.ToLower(m.View()), "client work") {
		t.Fatal("account selector failed")
	}
	m, _ = press(t, m, "esc")
	if m.overlay != overlaySettings {
		t.Fatal("settings should remain open")
	}
	next, _ := m.openClassificationEditorFor(message.ID)
	m = next.(Model)
	m = selectCategory(t, m, "personal")
	for _, choice := range m.classification.choices {
		if choice == "tag:Project-X" {
			t.Fatal("other account leaked into editor")
		}
	}
	mailbox := m.mailboxByID(message.MailboxID)
	if err := database.SaveMailTag(mailtag.Definition{AccountID: mailbox.AccountID, Keyword: "Project-X", Name: "Client Work"}); err != nil {
		t.Fatal(err)
	}
	next, _ = m.openClassificationEditorFor(message.ID)
	m = selectCategory(t, next.(Model), "tag:Project-X")
	if !strings.Contains(strings.ToLower(m.View()), "client work") {
		t.Fatal("discovered tag name missing")
	}
}

func TestMailTagPendingAndStaleEditorResults(t *testing.T) {
	m, database, message := mailTagTestModel(t)
	m, _ = press(t, m, "e")
	m = selectCategory(t, m, "important")
	m, cmd := press(t, m, " ")
	if cmd == nil || !m.mailTags.busy {
		t.Fatal("space must start a tag operation")
	}
	operation := m.mailTags.operation
	m, duplicate := press(t, m, " ")
	if duplicate != nil || m.mailTags.operation != operation {
		t.Fatal("duplicate pending operation started")
	}
	result := cmd().(mailTagChangedMsg) // fixture has no connected IMAP account
	if result.err == nil {
		t.Fatal("offline change must fail")
	}
	current, _ := database.GetMessage(message.ID)
	if !mailtag.Has(current.Flags, "$label1") {
		t.Fatal("failed write modified local assignment")
	}
	// Reopening settings while the request runs must not close or dirty it.
	next, _ := m.openMailTagSettings()
	m = next.(Model)
	next, _ = m.handleMailTagChanged(result)
	m = next.(Model)
	if m.mailTags.busy || m.overlay != overlayMailTags || m.mailTags.editor.error != "" {
		t.Fatal("old result changed the new editor")
	}
	m.mailTags.busy = true
	m.mailTags.operation++
	next, _ = m.handleMailTagChanged(result)
	m = next.(Model)
	if !m.mailTags.busy {
		t.Fatal("stale acknowledgment cleared a newer pending operation")
	}
}

func TestMailTagsRowsOverflowAndStatusFlags(t *testing.T) {
	m, _, message := mailTagTestModel(t)
	message.Flags = append(message.Flags, `\Seen`, "$junk", "$Forwarded", "Project-X")
	tags := m.mailTagsForMessage(message)
	if len(tags) != 2 {
		t.Fatalf("status flags rendered: %+v", tags)
	}
	m.messages[0] = message
	m.width = 180
	m.applyFilter()
	if view := m.renderMessagesPane(); !strings.Contains(view, "IMPORTANT") {
		t.Fatalf("mail tag missing from row: %s", view)
	}
	tags = append([]annotationTag{{kind: tagReply}}, tags...)
	for i := 0; i < 8; i++ {
		tags = append(tags, annotationTag{kind: tagMail, category: fmt.Sprintf("project-%d", i)})
	}
	chips := layoutTags(tags, "pills", "square", false, 12)
	if chipsWidth(chips) > 12 || !strings.Contains(chipsText(chips), "+") || len(chips) > 1 && chips[0].colorKey != tagKeyReply {
		t.Fatalf("overflow or priority failed: %+v", chips)
	}
	for width := 1; width <= 20; width++ {
		chips := layoutTags(tags, "plain", "square", false, width)
		if chipsWidth(chips) > width {
			t.Fatalf("width %d overflowed", width)
		}
	}
}

func TestMailTagLoadedRowsCannotRestoreOldFlags(t *testing.T) {
	m, database, message := mailTagTestModel(t)
	m, _ = press(t, m, "e")
	m.mailTags.operation = 1
	if err := database.ApplyServerFlags(message.MailboxID, map[uint32][]string{message.UID: {"$label2"}}); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(MessagesLoadedMsg{MailboxID: message.MailboxID, Messages: []db.Message{message}})
	m = next.(Model)
	if mailtag.Has(m.messages[0].Flags, "$label1") || !mailtag.Has(m.messages[0].Flags, "$label2") {
		t.Fatal("stale list restored old flags")
	}
	if !mailtag.Has(m.mailTags.editor.message.Flags, "$label2") {
		t.Fatal("open editor did not refresh with sync")
	}
}

func TestMailTagUIAndSyncRoundTrip(t *testing.T) {
	backend := imapmemserver.New()
	user := imapmemserver.NewUser("test", "password")
	backend.AddUser(user)
	if err := user.Create("INBOX", nil); err != nil {
		t.Fatal(err)
	}
	server := imapserver.New(&imapserver.Options{InsecureAuth: true, NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
		return backend.NewSession(), nil, nil
	}})
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { _ = server.Close(); _ = listener.Close() })
	m, database, message := mailTagTestModel(t)
	cfg := config.AccountConfig{Name: "Personal", IMAPHost: "127.0.0.1", IMAPPort: listener.Addr().(*net.TCPAddr).Port, User: "test", Password: "password"}
	m.cfg.Accounts = []config.AccountConfig{cfg}
	observer := imapClient.New(cfg)
	if err := observer.Connect(context.Background()); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = observer.Close() })
	// The server message is old enough to fall outside the next SINCE fetch.
	raw := []byte("From: a@example.com\r\nTo: b@example.com\r\nSubject: old mail\r\nDate: Thu, 01 Jan 1970 00:16:40 +0000\r\n\r\nbody\r\n")
	if err := observer.AppendWithFlags(context.Background(), "INBOX", raw, time.Unix(1000, 0), imap.FlagSeen, imap.Flag("$label1")); err != nil {
		t.Fatal(err)
	}
	state, validity, err := observer.ServerState(context.Background(), "INBOX")
	if err != nil || len(state) != 1 || state[0].UID != message.UID {
		t.Fatalf("fixture state: %+v / %v", state, err)
	}
	if err := database.SetMailboxUIDValidity(message.MailboxID, validity); err != nil {
		t.Fatal(err)
	}
	m, _ = press(t, m, "e")
	m = selectCategory(t, m, "important")
	m, cmd := press(t, m, " ")
	if cmd == nil {
		t.Fatal("no tag command from space")
	}
	result := cmd().(mailTagChangedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	next, _ := m.Update(result)
	m = next.(Model)
	current, _ := database.GetMessage(message.ID)
	if mailtag.Has(current.Flags, "$label1") || mailtag.Has(m.mailTags.editor.message.Flags, "$label1") {
		t.Fatal("UI did not confirm tag removal")
	}
	state, _, err = observer.ServerState(context.Background(), "INBOX")
	if err != nil || mailtag.Has(state[0].Flags, "$label1") {
		t.Fatalf("other client sees stale tag: %+v / %v", state, err)
	}
	// Category selection adds its tag without removing other server assignments.
	if _, err := observer.SetKeyword(context.Background(), "INBOX", message.UID, validity, "Project-X", true); err != nil {
		t.Fatal(err)
	}
	m = selectCategory(t, m, "personal")
	m, cmd = press(t, m, "enter")
	result = cmd().(mailTagChangedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	next, _ = m.Update(result)
	m = next.(Model)
	current, _ = database.GetMessage(message.ID)
	if !mailtag.Has(current.Flags, "$label3") || !mailtag.Has(current.Flags, "Project-X") {
		t.Fatalf("category lost tags: %v", current.Flags)
	}
	if m.effectiveClassification(message.ID).Category != "personal" {
		t.Fatal("category correction missing")
	}
	merged := m.mergeMessageTags([]annotationTag{{kind: tagCategory, category: "personal"}}, current)
	if len(merged) != 2 {
		t.Fatalf("duplicate category displayed: %+v", merged)
	}
	m = selectCategory(t, m, "work")
	m, cmd = press(t, m, "enter")
	result = cmd().(mailTagChangedMsg)
	if result.err != nil {
		t.Fatal(result.err)
	}
	next, _ = m.Update(result)
	m = next.(Model)
	current, _ = database.GetMessage(message.ID)
	if !mailtag.Has(current.Flags, "$label3") || !mailtag.Has(current.Flags, "$label2") || !mailtag.Has(current.Flags, "Project-X") {
		t.Fatalf("reclassification erased tags: %v", current.Flags)
	}
	// Another client assigns a tag; the old cached message must refresh on sync.
	if _, err := observer.SetKeyword(context.Background(), "INBOX", message.UID, validity, "$label2", true); err != nil {
		t.Fatal(err)
	}
	if err := database.SetMailboxLastSynced(message.MailboxID, time.Now()); err != nil {
		t.Fatal(err)
	}
	resultSync := m.syncMailboxCmd(message.MailboxID, false)().(MailboxSyncedMsg)
	if resultSync.Err != nil {
		t.Fatal(resultSync.Err)
	}
	current, _ = database.GetMessage(message.ID)
	if !mailtag.Has(current.Flags, "$label2") {
		t.Fatal("normal sync did not refresh tags on old mail")
	}
	if _, err := observer.SetKeyword(context.Background(), "INBOX", message.UID, validity, "$label2", false); err != nil {
		t.Fatal(err)
	}
	m.syncing[message.MailboxID] = false
	resultSync = m.syncMailboxCmd(message.MailboxID, false)().(MailboxSyncedMsg)
	if resultSync.Err != nil {
		t.Fatal(resultSync.Err)
	}
	current, _ = database.GetMessage(message.ID)
	if mailtag.Has(current.Flags, "$label2") {
		t.Fatal("normal sync did not remove an external tag")
	}
}

func TestMailTagStaleEmptySearchCannotHideNewMatches(t *testing.T) {
	m, _, message := mailTagTestModel(t)
	m.searchQuery = "#important"
	m.searchInput.SetValue(m.searchQuery)
	m.mailTags.searchVersion = 1
	next, cmd := m.Update(MessagesLoadedMsg{Search: true, Query: m.searchQuery, MailTagVersion: 0})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("stale empty search must be repeated")
	}
	result := cmd().(MessagesLoadedMsg)
	if result.MailTagVersion != 1 || len(result.Messages) != 1 || result.Messages[0].ID != message.ID {
		t.Fatalf("fresh search = %+v", result)
	}
}

func TestCompatibleCategoryFailureDoesNotSaveLocalCorrection(t *testing.T) {
	m, database, message := mailTagTestModel(t)
	m, _ = press(t, m, "e")
	m = selectCategory(t, m, "personal")
	_, cmd := press(t, m, "enter")
	result := cmd().(mailTagChangedMsg)
	if result.err == nil {
		t.Fatal("offline operation succeeded")
	}
	overrides, err := database.GetClassificationOverrides(message.ID)
	if err != nil || len(overrides) != 0 {
		t.Fatalf("failed write changed category: %v / %v", overrides, err)
	}
}

func TestCategoryEditorBoundsAndExternalCustomTag(t *testing.T) {
	m, database, message := mailTagTestModel(t)
	m, _ = press(t, m, "e")
	m = selectCategory(t, m, "personal")
	if err := database.ApplyServerFlags(message.MailboxID, map[uint32][]string{message.UID: {"$label1", "Project-X"}}); err != nil {
		t.Fatal(err)
	}
	current, _ := database.GetMessage(message.ID)
	m.reloadMailTags()
	m.refreshMailTagTarget([]db.Message{current})
	if m.classification.choices[m.classification.choice] != "personal" {
		t.Fatal("sync moved the user's selection")
	}
	m = selectCategory(t, m, "tag:Project-X")
	if !strings.Contains(m.View(), "[x] Project-X") {
		t.Fatal("external custom tag missing from open editor")
	}
	m.mailTags.busy = true
	m.mailTags.editor.error = "A tag change is still pending."
	for _, height := range []int{24, 30, 40} {
		m.width, m.height = 80, height
		view := m.renderClassificationOverlay()
		if lipgloss.Height(view) > height || lipgloss.Width(view) > m.width {
			t.Fatalf("editor exceeds 80x%d: %dx%d", height, lipgloss.Width(view), lipgloss.Height(view))
		}
		if !strings.Contains(view, "Project-X") {
			t.Fatal("selected choice scrolled out of view")
		}
	}
}
