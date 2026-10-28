package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/mailapp"
	tea "github.com/charmbracelet/bubbletea"
)

func TestComposeSignatureInitialPlacement(t *testing.T) {
	a := config.AccountConfig{User: "me@example.com", Signature: "My signature"}
	original := db.Message{From: "other@example.com", To: a.User, Subject: "Topic", MessageID: "<original@example.com>", BodyText: "Original text"}
	for _, tc := range []struct {
		name   string
		c      ComposeModel
		quoted bool
	}{
		{"new", NewCompose(a, nil, nil), false}, {"reply", NewReply(original, a, nil, nil), true},
		{"reply-all", NewReplyAll(original, a, nil, nil), true}, {"forward", NewForward(original, a, nil, nil), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			body := tc.c.bodyInput.Value()
			if strings.Count(body, mailapp.SignatureMarker) != 1 || !strings.HasPrefix(body, "\n\n⁂\n") {
				t.Fatal(body)
			}
			if tc.c.bodyInput.ed.CursorIndex() != 0 {
				t.Fatal("caret not at writing area")
			}
			if tc.quoted && strings.Index(body, "⁂") > strings.Index(body, "> Original text") {
				t.Fatal("marker after quote")
			}
		})
	}
}

func TestComposeSignatureDeleteMoveUndoAndDraft(t *testing.T) {
	accounts := composeSenderAccounts()
	c := NewCompose(accounts[0], accounts, nil)
	c.toInput.SetValue("reader@example.com")
	c.setInitialBody("Hello", "")
	c.focusedField = composeFieldBody
	c.bodyInput.Focus()
	// End is after the newline. Backspace twice removes newline then the rune.
	for _, key := range []tea.KeyType{tea.KeyCtrlEnd, tea.KeyBackspace, tea.KeyBackspace} {
		c, _, _ = c.Update(tea.KeyMsg{Type: key}, DefaultKeys)
	}
	if strings.Contains(c.bodyInput.Value(), "⁂") {
		t.Fatal("delete failed")
	}
	if !c.bodyInput.ed.Undo() || strings.Count(c.bodyInput.Value(), "⁂") != 1 || !c.bodyInput.ed.Redo() {
		t.Fatal("ordinary history failed")
	}
	_, cmd, _ := c.send()
	if cmd == nil || strings.Contains(cmd().(SendQueuedMsg).Msg.Body, "sig") {
		t.Fatal("deleted marker still signed")
	}
	c.busy = false
	c.bodyInput.SetValue("Top\n\n⁂\n\nBottom")
	before := c.bodyInput.Value()
	c, _, _ = c.Update(tea.KeyMsg{Type: tea.KeyCtrlU}, DefaultKeys)
	if c.selectedAccount().User != accounts[1].User || c.bodyInput.Value() != before {
		t.Fatal("sender switch changed marker")
	}
	m := newSendTestModel(t, 5)
	id, err := m.db.SaveDraft(c.toDraftRecord())
	if err != nil {
		t.Fatal(err)
	}
	saved, err := m.db.GetDraft(id)
	if err != nil {
		t.Fatal(err)
	}
	restored := NewComposeFromDraft(saved, accounts, nil)
	if restored.bodyInput.Value() != before {
		t.Fatal("draft marker changed")
	}
	_, cmd, _ = restored.send()
	if cmd == nil {
		t.Fatal(restored.statusMsg)
	}
	msg := cmd().(SendQueuedMsg).Msg
	if !strings.Contains(msg.Body, "Work sig") || strings.Contains(msg.Body, "Personal sig") || strings.Index(msg.Body, "Work sig") > strings.Index(msg.Body, "Bottom") {
		t.Fatalf("wrong signature/location: %q", msg.Body)
	}
	restored.busy = false
	restored.bodyInput.SetValue("Legacy draft body")
	_, cmd, _ = restored.send()
	if cmd == nil || cmd().(SendQueuedMsg).Msg.Body != "Legacy draft body" {
		t.Fatal("legacy draft appended")
	}
}

func TestComposeSignatureOnlyClosesWithoutDraft(t *testing.T) {
	m := newSendTestModel(t, 5)
	m.overlay = overlayCompose
	m.compose = NewCompose(config.AccountConfig{Signature: "Configured"}, nil, nil)
	if m.compose.hasContent() {
		t.Fatal("generated marker counts as authored content")
	}
	// Moving through the body must not autosave a marker-only draft.
	m.compose.focusedField = composeFieldBody
	next, cmd := m.handleCompose(tea.KeyMsg{Type: tea.KeyRight})
	m = next.(Model)
	if cmd != nil {
		t.Fatal("navigation saved a marker-only draft")
	}
	next, cmd = m.handleCompose(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay != overlayNone || cmd != nil {
		t.Fatal("empty composer prompted or saved")
	}
	var count int
	if err := m.db.QueryRow("SELECT count(*) FROM drafts").Scan(&count); err != nil || count != 0 {
		t.Fatalf("unwanted draft %d %v", count, err)
	}
}

func TestComposeSignatureScheduleUndoAndRestore(t *testing.T) {
	m := newSendTestModel(t, 5)
	a := config.AccountConfig{ID: "sender", User: "sender@example.com", Signature: "Signature at queue time"}
	m.cfg.Accounts = []config.AccountConfig{a}
	m.compose = NewReply(db.Message{From: "reader@example.com", BodyText: "Quoted original"}, a, m.cfg.Accounts, nil)
	m.overlay = overlayCompose
	original := m.compose.bodyInput.Value()
	next, cmd := m.queueScheduledSend(time.Now().Add(time.Hour))
	m = next.(Model)
	if cmd == nil {
		t.Fatal(m.compose.statusMsg)
	}
	queued := cmd().(SendQueuedMsg)
	if strings.Contains(queued.Msg.Body, "⁂") || !strings.Contains(queued.Msg.Body, a.Signature) {
		t.Fatal("unprepared scheduled body")
	}
	m, _ = m.handleSendQueued(queued)
	item, err := m.db.GetOutbox(int64(m.pendingSends[0].ID))
	if err != nil {
		t.Fatal(err)
	}
	saved, err := m.db.EditOutbox(item.ID)
	if err != nil {
		t.Fatal(err)
	}
	restored := m.composeRestored(saved, item.MessageJSON)
	if restored.bodyInput.Value() != original {
		t.Fatal("cancel-to-edit lost marker")
	}
	// In-session send undo uses the compose snapshot rather than prepared text.
	m.pendingSends = nil
	m.compose = restored
	m.overlay = overlayCompose
	queued.ScheduledAt = time.Time{}
	m, _ = m.handleSendQueued(queued)
	if !m.undoLatestPendingSend() || m.compose.bodyInput.Value() != original {
		t.Fatal("send undo lost marker")
	}
}

func TestComposeSignaturePGPIntentUsesPreparedBody(t *testing.T) {
	c := NewCompose(config.AccountConfig{User: "sender@example.com", Signature: "Signed identity"}, nil, nil)
	c.toInput.SetValue("reader@example.com")
	c.pgpSign = true
	c.pgpEncrypt = true
	c.pgpAvailable = true
	_, cmd, _ := c.send()
	if cmd == nil {
		t.Fatal(c.statusMsg)
	}
	queued := cmd().(SendQueuedMsg)
	if !queued.Sign || !queued.Encrypt || strings.Contains(queued.Msg.Body+queued.Msg.HTMLBody, "⁂") || !strings.Contains(queued.Msg.Body, "Signed identity") {
		t.Fatal("PGP received unresolved body")
	}
	c.busy = false
	c.bodyInput.SetValue("bad ⁂ placement")
	if _, cmd, _ := c.send(); cmd != nil {
		t.Fatal("invalid marker reached PGP")
	}
}

func TestUnsubscribeComposeHasSignatureMarker(t *testing.T) {
	m := mailtoTestModel(t, 1)
	source := m.messages[0]
	source.Headers = "List-Unsubscribe\n<mailto:unsubscribe@example.com>\n"
	next, _ := m.handleUnsubscribe(source)
	m = next.(Model)
	if m.overlay != overlayCompose || strings.Count(m.compose.bodyInput.Value(), "⁂") != 1 || m.compose.selectedAccount().ID != m.cfg.Accounts[1].ID {
		t.Fatal("unsubscribe lost marker/source")
	}
}
