package ui

import (
	"context"
	"errors"
	"github.com/allisonhere/tidemail/internal/smtp"
	"github.com/emersion/go-imap/v2/imapserver"
	"github.com/emersion/go-imap/v2/imapserver/imapmemserver"
	"net"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	"github.com/allisonhere/tidemail/internal/filter"
	imapClient "github.com/allisonhere/tidemail/internal/imap"
	tea "github.com/charmbracelet/bubbletea"
)

func TestDraftActionsNeverTouchHiddenMessages(t *testing.T) {
	for _, k := range []string{"a", "m", "x", "*", "A", " "} {
		t.Run(k, func(t *testing.T) {
			m := draftsPreviewModel(t)
			m.messageCursor = 1
			m.messages = []db.Message{{ID: 100, UID: 1}, {ID: 101, UID: 2}}
			m.filteredMessages = m.messages
			next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(k)})
			m = next.(Model)
			if m.messageCursor != 1 || len(m.selectedMessages) != 0 || m.overlay != overlayNone || len(m.pendingReads) != 0 {
				t.Fatalf("draft action changed hidden message state: key %q", k)
			}
			if !strings.Contains(m.statusMsg, "unavailable for drafts") {
				t.Fatalf("missing explanation: %q", m.statusMsg)
			}
		})
	}
}

func draftSafetyModel(t *testing.T) Model {
	t.Helper()
	database, err := db.OpenPath(t.TempDir() + "/mail.db")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { database.Close() })
	cfg := config.DefaultConfig()
	cfg.Accounts = []config.AccountConfig{{ID: "draft-safety", Name: "Test", User: "me@example.com"}}
	m := NewModel(database, cfg, "test", false)
	m.compose = NewCompose(cfg.Accounts[0], cfg.Accounts, nil)
	m.overlay = overlayCompose
	return m
}

func TestDraftSaveSerializesEditsAndIgnoresStaleAcknowledgment(t *testing.T) {
	m := draftSafetyModel(t)
	m.compose.bodyInput.SetValue("h")
	first := m.saveDraftCmd()
	m.compose.bodyInput.SetValue("he")
	if cmd := m.saveDraftCmd(); cmd != nil {
		t.Fatal("second save started before first completed")
	}
	ack := first().(DraftSavedMsg)
	next, second := m.Update(ack)
	m = next.(Model)
	if second == nil || !m.compose.dirty || !m.compose.saving {
		t.Fatal("latest edit was not queued")
	}
	next, staleCmd := m.Update(ack)
	m = next.(Model)
	if staleCmd != nil || !m.compose.saving {
		t.Fatal("stale acknowledgement affected pending save")
	}
	next, _ = m.Update(second())
	m = next.(Model)
	drafts, err := m.db.ListDrafts("draft-safety", "Test", "me@example.com")
	if err != nil || len(drafts) != 1 || drafts[0].BodyText != "he" || m.compose.dirty {
		t.Fatalf("drafts=%+v err=%v dirty=%v", drafts, err, m.compose.dirty)
	}
	old := ack
	m.compose = NewCompose(config.AccountConfig{}, nil, nil)
	m.compose.bodyInput.SetValue("new session")
	next, _ = m.Update(old)
	if next.(Model).compose.draftID != 0 {
		t.Fatal("old save assigned ID to new compose")
	}
}

func TestFailedCloseSavePreservesCompose(t *testing.T) {
	m := draftSafetyModel(t)
	m.compose.bodyInput.SetValue("precious text")
	if err := m.db.Close(); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.closeComposeSavingDraft()
	m = next.(Model)
	if m.overlay != overlayCompose || m.compose.bodyInput.Value() != "precious text" {
		t.Fatal("closed before durable save")
	}
	next, _ = m.Update(cmd())
	m = next.(Model)
	if m.overlay != overlayCompose || m.compose.bodyInput.Value() != "precious text" || !m.compose.dirty || m.compose.busy {
		t.Fatal("failed save lost editable text")
	}
	if !strings.Contains(m.statusMsg, "draft save failed") {
		t.Fatal(m.compose.statusMsg)
	}
}

func TestDiscardWaitsForDraftInsert(t *testing.T) {
	m := draftSafetyModel(t)
	m.compose.bodyInput.SetValue("discard")
	cmd := m.saveDraftCmd()
	next, _ := m.discardComposeDraft()
	m = next.(Model)
	if m.overlay != overlayCompose {
		t.Fatal("discard raced unfinished insert")
	}
	next, cleanup := m.Update(cmd())
	m = next.(Model)
	if m.overlay != overlayNone {
		t.Fatal("discard did not finish")
	}
	for _, child := range cleanup().(tea.BatchMsg) {
		// The other child is a status timer; running it is harmless in this test.
		child()
	}
	drafts, err := m.db.ListDrafts("draft-safety", "Test", "me@example.com")
	if err != nil || len(drafts) != 0 {
		t.Fatalf("orphan drafts: %+v %v", drafts, err)
	}
}

func TestFailedFilterDeletePreservesCacheAndDoesNotTombstone(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	disconnected := imapClient.New(config.AccountConfig{})
	acted, err := applyFilterAction(context.Background(), f.database, disconnected, f.source, f.messages[0], filter.Action{Type: filter.ActionDelete})
	if err == nil || acted {
		t.Fatal("disconnected delete succeeded")
	}
	if _, err := f.database.GetMessage(f.messages[0].ID); err != nil {
		t.Fatal("failed delete removed cache", err)
	}
	deleted, err := f.database.MessageDeletedLocally(f.source.ID, f.messages[0].UID, f.messages[0].MessageID)
	if err != nil || deleted {
		t.Fatalf("failed delete left tombstone: %v %v", deleted, err)
	}
}

func TestDetectedLinksRejectTerminalControls(t *testing.T) {
	for _, raw := range []string{"https://evil.example/\x1b]52;c;ZXZpbA==\a", "https://evil.example/\x7f", "https://evil.example/\u009b31m", "https://evil.example/\n"} {
		if got := cleanDetectedURL(raw); got != "" {
			t.Fatalf("accepted unsafe URL %q", got)
		}
	}
	if got := cleanDetectedURL("https://example.com/path"); got != "https://example.com/path" {
		t.Fatal(got)
	}
}

func TestDraftSaveErrorDoesNotQueueSend(t *testing.T) {
	m := draftSafetyModel(t)
	m.compose.bodyInput.SetValue("mail")
	_ = m.saveDraftCmd()
	next, _ := m.Update(SendQueuedMsg{})
	m = next.(Model)
	next, _ = m.Update(DraftSavedMsg{Session: m.compose.saveSession, Revision: m.compose.savingRevision, Err: errors.New("disk full")})
	m = next.(Model)
	if m.compose.sendAfterSave != nil || m.compose.busy || m.overlay != overlayCompose {
		t.Fatal("failed draft save queued send or locked compose")
	}
}

func TestMailboxMarkAllReadRemoteFailurePreservesLocalState(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	m.cfg.Accounts[0].IMAPHost = "127.0.0.1"
	m.cfg.Accounts[0].IMAPPort = 0
	result := m.markMailboxReadCmd(f.source.ID)().(MailboxReadUpdatedMsg)
	if result.Err == nil || len(result.MailboxIDs) != 0 {
		t.Fatal("failed server operation reported local success")
	}
	msg, err := m.db.GetMessage(f.messages[0].ID)
	if err != nil || msg.Read {
		t.Fatalf("failure changed read state: %v %v", msg, err)
	}
}

func TestMailboxMarkAllReadLocalAccountPersistsCount(t *testing.T) {
	f := newUndoTestFixture(t, 1)
	m := f.model
	if err := m.db.SetMailboxUnreadCount(f.source.ID, 1); err != nil {
		t.Fatal(err)
	}
	result := m.markMailboxReadCmd(f.source.ID)().(MailboxReadUpdatedMsg)
	if result.Err != nil || len(result.MailboxIDs) != 1 {
		t.Fatalf("mark all: %+v", result)
	}
	mb, err := m.db.GetMailbox(f.source.ID)
	if err != nil || mb.UnreadCount != 0 {
		t.Fatalf("stale count: %+v %v", mb, err)
	}
}

func TestHTMLAndTextLinkExtractionRejectControls(t *testing.T) {
	if links := extractActionableLinks("visit https://evil.example/\x1b]52;c;ZXZpbA==\a", ""); len(links) != 0 {
		t.Fatalf("unsafe prose links %q", links)
	}
	if links := extractActionableLinksFromHTML(`<a href="https://evil.example/&#27;]52;c;ZXZpbA==&#7;">click</a>`, ""); len(links) != 0 {
		t.Fatalf("unsafe HTML links %q", links)
	}
}

func TestRemoteMovesEvictSourceDespiteDestinationUIDCollision(t *testing.T) {
	for _, path := range []string{"filter", "undo commit"} {
		t.Run(path, func(t *testing.T) {
			f := newUndoTestFixture(t, 1)
			backend := imapmemserver.New()
			user := imapmemserver.NewUser("test", "password")
			backend.AddUser(user)
			for _, name := range []string{"INBOX", "Archive"} {
				if err := user.Create(name, nil); err != nil {
					t.Fatal(err)
				}
			}
			server := imapserver.New(&imapserver.Options{InsecureAuth: true, NewSession: func(*imapserver.Conn) (imapserver.Session, *imapserver.GreetingData, error) {
				return backend.NewSession(), nil, nil
			}})
			listener, err := net.Listen("tcp", "127.0.0.1:0")
			if err != nil {
				t.Fatal(err)
			}
			go func() { _ = server.Serve(listener) }()
			defer server.Close()
			defer listener.Close()
			cfg := config.AccountConfig{Name: "Test", IMAPHost: "127.0.0.1", IMAPPort: listener.Addr().(*net.TCPAddr).Port, User: "test", Password: "password"}
			client := imapClient.New(cfg)
			ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
			defer cancel()
			if err := client.Connect(ctx); err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			for _, name := range []string{"INBOX", "Archive"} {
				if err := client.AppendWithFlags(ctx, name, []byte("Subject: test\r\n\r\nbody\r\n"), time.Now()); err != nil {
					t.Fatal(err)
				}
			}
			if err := f.database.UpsertMessage(db.Message{MailboxID: f.target.ID, UID: 1, Subject: "other"}); err != nil {
				t.Fatal(err)
			}
			if path == "filter" {
				acted, err := moveMessage(ctx, f.database, client, f.source, f.messages[0], f.target)
				if err != nil || !acted {
					t.Fatalf("filter move: %v %v", acted, err)
				}
			} else {
				result := executeDestructiveAction(pendingDestructiveAction{Kind: destructiveMove, Entries: []pendingDestructiveEntry{{Message: f.messages[0], Source: f.source, Target: f.target, Account: cfg}}}, f.database, f.model.sessions)
				defer f.model.sessions.Close()
				if result.Err != nil || len(result.Succeeded) != 1 {
					t.Fatalf("move result: %+v", result)
				}
			}
			source, err := f.database.ListMessages(f.source.ID)
			if err != nil || len(source) != 0 {
				t.Fatalf("source survived: %v %v", source, err)
			}
			dest, err := f.database.ListMessages(f.target.ID)
			if err != nil || len(dest) != 1 || dest[0].Subject != "other" {
				t.Fatalf("colliding target damaged: %v %v", dest, err)
			}
			state, _, err := client.ServerState(ctx, "Archive")
			if err != nil || len(state) != 2 {
				t.Fatalf("server move did not complete: %v %v", state, err)
			}
		})
	}
}

func TestSendingWaitsForDraftInsertAndUndoUsesSameRow(t *testing.T) {
	m := draftSafetyModel(t)
	m.cfg.Display.SendDelaySeconds = 30
	m.compose.toInput.SetValue("to@example.com")
	m.compose.bodyInput.SetValue("mail")
	save := m.saveDraftCmd()
	next, _ := m.Update(SendQueuedMsg{Account: m.cfg.Accounts[0], Msg: smtp.OutgoingMessage{To: []string{"to@example.com"}, Body: "mail"}})
	m = next.(Model)
	if len(m.pendingSends) != 0 || m.overlay != overlayCompose {
		t.Fatal("send raced draft insert")
	}
	next, _ = m.Update(save())
	m = next.(Model)
	if len(m.pendingSends) != 1 || m.overlay != overlayNone || m.pendingSends[0].DraftID == 0 {
		t.Fatal("send did not use saved draft")
	}
	if !m.undoLatestPendingSend() {
		t.Fatal("could not undo send")
	}
	if m.compose.bodyInput.Value() != "mail" || m.compose.saving || m.compose.busy {
		t.Fatal("undo restored pending save state")
	}
	drafts, err := m.db.ListDrafts("draft-safety", "Test", "me@example.com")
	if err != nil || len(drafts) != 1 || drafts[0].ID != m.compose.draftID {
		t.Fatalf("duplicate drafts after send undo: %v %v", drafts, err)
	}
}

func armDraftSaveResult(m *Model, msg DraftSavedMsg) DraftSavedMsg {
	m.compose.saveSession = draftSaveSessions.Add(1)
	m.compose.saveRevision = 1
	m.compose.savingRevision = 1
	m.compose.saving = true
	msg.Session, msg.Revision = m.compose.saveSession, 1
	return msg
}
