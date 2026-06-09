package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func composeFieldLine(view, label string) string {
	stripped := ansi.Strip(view)
	for _, line := range strings.Split(stripped, "\n") {
		if strings.Contains(line, label) {
			return line
		}
	}
	return ""
}

func TestComposeActionsStayVisibleInShortView(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c.bodyInput.SetValue(strings.Repeat("this is a long compose line that wraps inside the body editor ", 20))

	view := c.View(60, 12, BuildStyles(CatppuccinMocha, "compact"))
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "SEND") {
		t.Fatalf("expected compose actions to stay visible in short view, got %q", stripped)
	}
}

func TestComposeActionsStayVisibleInOverlay(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 80, Height: 18})
	m = next.(Model)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()
	m.compose.bodyInput.SetValue(strings.Repeat("this is a long compose line that wraps inside the body editor ", 40))

	view := m.View()
	stripped := ansi.Strip(view)

	if !strings.Contains(stripped, "SEND") {
		t.Fatalf("expected compose actions to stay visible in overlay, got %q", stripped)
	}
}

func TestComposeActionsWrapInNarrowView(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, nil)
	view := c.View(32, 16, BuildStyles(CatppuccinMocha, "compact"))
	stripped := ansi.Strip(view)

	for _, want := range []string{"SEND", "GRAMMAR"} {
		if !strings.Contains(stripped, want) {
			t.Fatalf("expected compose action %q to stay visible in narrow view, got %q", want, stripped)
		}
	}
}

func TestComposeTabAdvancesAfterAcceptingRecipientSuggestion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Alice <alice@example.com>"})

	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("ali"), Paste: true}, DefaultKeys)
	if cmd != nil {
		cmd()
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if got := c.toInput.Value(); got != "alice <alice@example.com>" {
		t.Fatalf("expected tab to accept To suggestion, got %q", got)
	}
	if c.focusedField != composeFieldCC {
		t.Fatalf("tab should accept To suggestion and advance to CC, got focus %v", c.focusedField)
	}
	toLine := composeFieldLine(c.View(90, 24, BuildStyles(CatppuccinMocha, "compact")), "To")
	if strings.Contains(toLine, " >") {
		t.Fatalf("expected To row not to show focus marker after advancing to CC, got %q", toLine)
	}
}

func TestComposeTabAdvancesAfterAcceptingCCSuggestion(t *testing.T) {
	c := NewCompose(config.AccountConfig{}, nil, []string{"Carol <carol@example.com>"})
	c.advanceField(1)

	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("car"), Paste: true}, DefaultKeys)
	if cmd != nil {
		cmd()
	}

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyTab}, DefaultKeys)
	if got := c.ccInput.Value(); got != "carol <carol@example.com>" {
		t.Fatalf("expected tab to accept CC suggestion, got %q", got)
	}
	if c.focusedField != composeFieldSubject {
		t.Fatalf("tab should accept CC suggestion and advance to Subject, got focus %v", c.focusedField)
	}
}

func TestReplyTypingStartsAboveQuotedMessage(t *testing.T) {
	c := NewReply(db.Message{
		From:      "alice@example.com",
		Subject:   "Plans",
		MessageID: "<plans@example.com>",
		BodyText:  "quoted line",
	}, config.AccountConfig{}, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}, DefaultKeys)

	got := c.bodyInput.Value()
	if !strings.HasPrefix(got, "H\n\nOn alice@example.com wrote:\n> quoted line") {
		t.Fatalf("expected typed reply text above quoted original, got %q", got)
	}
}

func TestForwardTypingStartsAboveQuotedMessage(t *testing.T) {
	c := NewForward(db.Message{
		From:      "alice@example.com",
		Subject:   "Plans",
		MessageID: "<plans@example.com>",
		BodyText:  "forwarded line",
	}, config.AccountConfig{}, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("H")}, DefaultKeys)

	got := c.bodyInput.Value()
	if !strings.HasPrefix(got, "H\n\n---------- Forwarded message ----------\nFrom: alice@example.com") {
		t.Fatalf("expected typed forward text above quoted original, got %q", got)
	}
	if !strings.Contains(got, "> forwarded line") {
		t.Fatalf("expected forwarded body to remain quoted, got %q", got)
	}
}

func TestComposeFromDraftRestoresFieldsAndAttachments(t *testing.T) {
	draft := db.Draft{
		ID:           7,
		AccountName:  "Personal",
		AccountUser:  "allie@example.com",
		AccountIndex: 1,
		To:           "bob@example.com",
		CC:           "carol@example.com",
		Subject:      "Saved subject",
		BodyText:     "Saved body",
		InReplyTo:    "<orig@example.com>",
		References:   "<orig@example.com>",
		Attachments:  []db.DraftAttachment{{Filename: "notes.txt", Path: "/tmp/notes.txt", Data: []byte("notes")}},
	}
	accounts := []config.AccountConfig{{Name: "Other", User: "other@example.com"}, {Name: "Personal", User: "allie@example.com"}}

	c := NewComposeFromDraft(draft, accounts, nil)

	if c.draftID != draft.ID || c.toInput.Value() != draft.To || c.ccInput.Value() != draft.CC || c.subjectInput.Value() != draft.Subject || c.bodyInput.Value() != draft.BodyText {
		t.Fatalf("draft fields not restored: %+v", c)
	}
	if c.inReplyTo != draft.InReplyTo || c.references != draft.References || c.accountIndex != 1 {
		t.Fatalf("draft metadata not restored: %+v", c)
	}
	if len(c.attachments) != 1 || c.attachments[0].Name != "notes.txt" || string(c.attachments[0].Data) != "notes" {
		t.Fatalf("draft attachments not restored: %+v", c.attachments)
	}
}

func TestComposeEscapeWithContentOpensDraftCloseConfirm(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{Name: "Personal", User: "allie@example.com"}, []config.AccountConfig{{Name: "Personal", User: "allie@example.com"}}, nil)
	m.compose.subjectInput.SetValue("Keep me")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)

	if cmd != nil {
		t.Fatal("escape should open confirmation modal before saving")
	}
	if m.overlay != overlayDraftCloseConfirm {
		t.Fatalf("expected draft close confirmation overlay, got %v", m.overlay)
	}
}

func TestDraftCloseConfirmSavesDraft(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayDraftCloseConfirm
	m.compose = NewCompose(config.AccountConfig{Name: "Personal", User: "allie@example.com"}, []config.AccountConfig{{Name: "Personal", User: "allie@example.com"}}, nil)
	m.compose.toInput.SetValue("bob@example.com")
	m.compose.subjectInput.SetValue("Keep me")
	m.compose.bodyInput.SetValue("Body")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'y'}})
	m = next.(Model)
	if m.overlay != overlayNone {
		t.Fatalf("expected compose closed after saving draft, got overlay %v", m.overlay)
	}
	if cmd == nil {
		t.Fatal("expected save draft command")
	}
	msg, ok := cmd().(DraftSavedMsg)
	if !ok || msg.Err != nil {
		t.Fatalf("expected successful DraftSavedMsg, got %#v", msg)
	}
	drafts, err := database.ListDrafts("Personal", "allie@example.com")
	if err != nil {
		t.Fatal(err)
	}
	if len(drafts) != 1 || drafts[0].Subject != "Keep me" || drafts[0].BodyText != "Body" {
		t.Fatalf("expected saved draft, got %+v", drafts)
	}
}

func TestEnterOnDraftRowReopensCompose(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.focused = paneMessages
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: 2, AccountID: 1, Name: "Drafts"}}
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: 2, accountID: 1}}
	m.drafts = []db.Draft{{ID: 9, AccountName: "Personal", Subject: "Reopen me", BodyText: "draft body"}}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyEnter})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("reopening local draft should not need a command")
	}
	if m.overlay != overlayCompose {
		t.Fatalf("expected compose overlay, got %v", m.overlay)
	}
	if m.compose.draftID != 9 || m.compose.subjectInput.Value() != "Reopen me" || m.compose.bodyInput.Value() != "draft body" {
		t.Fatalf("expected reopened draft in compose, got %+v", m.compose)
	}
}

func TestComposeEditAutosavesDraft(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{Name: "Personal", User: "allie@example.com"}, []config.AccountConfig{{Name: "Personal", User: "allie@example.com"}}, nil)
	m.compose.focusedField = composeFieldSubject
	m.compose.toInput.Blur()
	m.compose.subjectInput.Focus()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("Draft"), Paste: true})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected edit to autosave draft")
	}
	msg, ok := cmd().(DraftSavedMsg)
	if !ok || msg.Err != nil || msg.DraftID == 0 {
		t.Fatalf("expected successful DraftSavedMsg, got %#v", msg)
	}
}
