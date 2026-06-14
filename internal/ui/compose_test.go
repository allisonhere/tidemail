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

	view := c.View(60, 13, BuildStyles(CatppuccinMocha, "compact"))
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
	if c.focusedField != composeFieldBCC {
		t.Fatalf("tab should accept CC suggestion and advance to BCC, got focus %v", c.focusedField)
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

func TestComposeBodyNeverEscapesWidth(t *testing.T) {
	// The editor wraps with go-runewidth while lipgloss measures with x/ansi;
	// for emoji/flags they disagree. Compose must frame (clip+pad), not re-wrap,
	// the editor output — otherwise overflow escapes to the app's left edge.
	styles := BuildStyles(BuiltinThemes[0], "compact")
	contents := []string{
		strings.Repeat("🇺🇸", 40), // regional-indicator flags (worst offender)
		strings.Repeat("👍", 60),  // emoji
		strings.Repeat("x", 300), // long unbroken ASCII
		strings.Repeat("hello world ", 40),
	}
	for _, w := range []int{30, 40, 60, 80} {
		for _, content := range contents {
			c := NewCompose(config.AccountConfig{}, nil, nil)
			c.focusedField = composeFieldBody
			c.bodyInput.Focus()
			c.bodyInput.SetValue(content)
			for i, line := range strings.Split(c.View(w, 24, styles), "\n") {
				if got := ansi.StringWidth(line); got != w {
					t.Fatalf("width=%d content=%.8q: line %d has display width %d, want %d",
						w, content, i, got, w)
				}
			}
		}
	}
}

func TestComposeAttachMovedOffCtrlA(t *testing.T) {
	// ctrl+a in the body selects all and does NOT open the picker.
	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hi there")}, DefaultKeys)
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlA}, DefaultKeys)
	if c.picker.active {
		t.Fatal("ctrl+a must not open the attach picker anymore")
	}
	if got := c.bodyInput.SelectedText(); got != "hi there" {
		t.Fatalf("ctrl+a should select all, got %q", got)
	}

	// alt+f opens the attach picker.
	c2 := NewCompose(config.AccountConfig{}, nil, nil)
	c2.focusedField = composeFieldBody
	c2, _, _ = c2.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("f"), Alt: true}, DefaultKeys)
	if !c2.picker.active {
		t.Fatal("alt+f should open the attach picker")
	}
}

func TestComposeBodyCopyCutToClipboard(t *testing.T) {
	var copied string
	defer stubClipboardWrite(t, &copied)()

	c := NewCompose(config.AccountConfig{}, nil, nil)
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()

	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune("hello")}, DefaultKeys)
	// ctrl+a selects all in the body (it no longer triggers attach).
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlA}, DefaultKeys)
	if got := c.bodyInput.SelectedText(); got != "hello" {
		t.Fatalf("ctrl+a should select all body text, got %q", got)
	}

	// Copy: clipboard receives the selection; the body is unchanged.
	var cmd tea.Cmd
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlC}, DefaultKeys)
	if cmd == nil {
		t.Fatal("ctrl+c should return a clipboard write command")
	}
	cmd()
	if copied != "hello" {
		t.Fatalf("copy should put the selection on the clipboard, got %q", copied)
	}
	if got := c.bodyInput.Value(); got != "hello" {
		t.Fatalf("copy must not modify the body, got %q", got)
	}

	// Cut: clipboard receives the selection; the body is emptied.
	copied = ""
	c, cmd, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlX}, DefaultKeys)
	if cmd == nil {
		t.Fatal("ctrl+x should return a clipboard write command")
	}
	cmd()
	if copied != "hello" {
		t.Fatalf("cut should put the selection on the clipboard, got %q", copied)
	}
	if got := c.bodyInput.Value(); got != "" {
		t.Fatalf("cut should remove the selection from the body, got %q", got)
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

func TestCtrlPPastesClipboardIntoComposeBody(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.focusedField = composeFieldBody
	m.compose.bodyInput.Focus()

	restore := stubClipboardRead(t, "pasted text")
	defer restore()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyCtrlP})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected ctrl+p in compose to read clipboard")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)

	if got := m.compose.bodyInput.Value(); got != "pasted text" {
		t.Fatalf("expected clipboard text pasted into compose body, got %q", got)
	}
}

func stubClipboardRead(t *testing.T, text string) func() {
	t.Helper()
	orig := clipboardReadCmd
	clipboardReadCmd = func() tea.Cmd {
		return func() tea.Msg {
			return ClipboardReadMsg{Text: text}
		}
	}
	return func() { clipboardReadCmd = orig }
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

func TestDraftCloseConfirmDiscardsWithLowercaseD(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.overlay = overlayDraftCloseConfirm
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.bodyInput.SetValue("Body")

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)

	if m.overlay != overlayNone {
		t.Fatalf("expected compose closed after discarding draft, got overlay %v", m.overlay)
	}
	if cmd != nil {
		t.Fatalf("expected discard without existing draft to need no command, got %#v", cmd)
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
