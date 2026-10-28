package ui

import (
	"errors"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/x/ansi"
)

func assertComposeBackdropMatchesFresh(t *testing.T, m Model) {
	t.Helper()
	cached := m.View()
	fresh := m
	fresh.composeBackdrop = nil
	if cached != fresh.View() {
		t.Fatal("cached backdrop changed the rendered screen")
	}
}

func TestComposeBackdropReusedDuringEditing(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 40, false)
	_ = m.View()
	original := m.composeBackdrop.panes
	for _, key := range []tea.KeyMsg{{Type: tea.KeyBackspace}, {Type: tea.KeyLeft}, {Type: tea.KeyRunes, Runes: []rune{'x'}}} {
		next, cmd := m.Update(key)
		m = next.(Model)
		if !m.composeBackdrop.valid || m.composeBackdrop.panes != original {
			t.Fatal("compose key invalidated unchanged panes")
		}
		assertComposeBackdropMatchesFresh(t, m)
		if cmd != nil {
			next, _ = m.Update(cmd())
			m = next.(Model)
		}
		if !m.composeBackdrop.valid {
			t.Fatal("successful autosave invalidated unchanged panes")
		}
	}
	// The status line is not in the cache and must still reflect current state.
	m.setStatus("fresh compose status", false)
	if !strings.Contains(ansi.Strip(m.View()), "fresh compose status") {
		t.Fatal("status line was cached")
	}
	assertComposeBackdropMatchesFresh(t, m)
}

func TestComposeBackdropRefreshesForBackgroundChanges(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 40, false)
	_ = m.View()
	old := m.composeBackdrop.panes
	next, _ := m.Update(MessagesLoadedMsg{MailboxID: 7, Messages: []db.Message{{ID: 999, MailboxID: 7, Subject: "newly synced mail"}}})
	m = next.(Model)
	if m.composeBackdrop.valid {
		t.Fatal("mail update kept a stale backdrop")
	}
	assertComposeBackdropMatchesFresh(t, m)
	if m.composeBackdrop.panes == old || !strings.Contains(ansi.Strip(m.composeBackdrop.panes), "newly synced mail") {
		t.Fatal("mail changes did not reach the backdrop")
	}
	next, _ = m.Update(tea.WindowSizeMsg{Width: 120, Height: 45})
	m = next.(Model)
	if m.composeBackdrop.valid {
		t.Fatal("resize kept a stale backdrop")
	}
	assertComposeBackdropMatchesFresh(t, m)
	next, _ = m.Update(DraftSavedMsg{Err: errors.New("synthetic failure")})
	m = next.(Model)
	if m.composeBackdrop.valid {
		t.Fatal("draft error did not invalidate backdrop")
	}
	assertComposeBackdropMatchesFresh(t, m)
}

func TestComposeBackdropDoesNotLeakAcrossOverlays(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 40, false)
	_ = m.View()
	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.overlay == overlayCompose {
		t.Fatal("compose did not close")
	}
	assertComposeBackdropMatchesFresh(t, m)
	// Even if the next action reopens compose, a key outside compose must clear
	// the prior backdrop before any new modal render can reuse it.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyEsc})
	m = next.(Model)
	if m.composeBackdrop.valid {
		t.Fatal("cached panes survived another overlay's event")
	}
}

func TestComposeBackdropIncomingMailPreservesDraft(t *testing.T) {
	preserveComposeEditorState(t)
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = database.Close() })
	accountID, err := database.AddAccount("", "Personal", "")
	if err != nil {
		t.Fatal(err)
	}
	mailboxID, err := database.UpsertMailbox(db.Mailbox{AccountID: accountID, Name: "INBOX"})
	if err != nil {
		t.Fatal(err)
	}
	m, existing := newMailboxListModel(mailboxID, 1)
	m.db = database
	m.height = 60 // Keep the first inbox row visible above the centered modal.
	m.cfg.Display.Notifications = false
	existing[0].UID = 1
	if err := database.UpsertMessage(existing[0]); err != nil {
		t.Fatal(err)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'c'}})
	m = next.(Model)
	if m.overlay != overlayCompose {
		t.Fatal("compose shortcut did not open the modal")
	}
	m.compose.toInput.SetValue("recipient@example.com")
	m.compose.subjectInput.SetValue("Unsent draft")
	m.compose.bodyInput.SetValue("Keep this paragraph.\n\n> Keep this quotation too.")
	draft := m.compose.toDraftRecord()
	_ = m.View()
	if !m.composeBackdrop.valid || m.composeBackdrop.panes == "" {
		t.Fatal("compose did not populate the background cache")
	}

	// A completed synchronization has already persisted incoming mail before
	// delivering MailboxSyncedMsg. Exercise that same local completion path.
	incoming := db.Message{MailboxID: mailboxID, UID: 2, Subject: "New arrival", Date: time.Unix(2000, 0)}
	if err := database.UpsertMessage(incoming); err != nil {
		t.Fatal(err)
	}
	next, cmd := m.Update(MailboxSyncedMsg{MailboxID: mailboxID, NewCount: 1})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("sync completion did not request a reload")
	}
	// Bubble Tea renders between sync completion and the asynchronous reload.
	_ = m.View()
	batch, ok := cmd().(tea.BatchMsg)
	if !ok {
		t.Fatal("sync completion did not return its reload commands")
	}
	reloaded := false
	for _, command := range batch {
		// Consume the mailbox reload; unrelated sync follow-up results are
		// outside this focused regression. All commands read the temporary DB.
		if loaded, ok := command().(MessagesLoadedMsg); ok {
			if loaded.Err != nil {
				t.Fatal(loaded.Err)
			}
			next, _ = m.Update(loaded)
			m = next.(Model)
			reloaded = true
			break
		}
	}
	if !reloaded {
		t.Fatal("sync completion did not reload the inbox")
	}
	if view := ansi.Strip(m.View()); !strings.Contains(view, incoming.Subject) {
		t.Fatal("next rendered inbox omitted the incoming message")
	}
	if m.overlay != overlayCompose {
		t.Fatal("incoming mail closed the composer")
	}
	if !reflect.DeepEqual(m.compose.toDraftRecord(), draft) {
		t.Fatal("incoming mail changed the draft contents")
	}
}

func TestDraftAutosaveRefreshesDraftsListBehindCompose(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 40, false)
	m.mailboxes[0].Name = "Drafts"
	_ = m.View()

	next, cmd := m.Update(DraftSavedMsg{DraftID: 1})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("autosave in the Drafts folder did not reload the list")
	}
	loaded, ok := cmd().(DraftsLoadedMsg)
	if !ok {
		t.Fatalf("expected DraftsLoadedMsg, got %T", cmd())
	}
	next, _ = m.Update(loaded)
	m = next.(Model)
	if m.composeBackdrop.valid {
		t.Fatal("reloaded drafts kept a stale backdrop")
	}
	assertComposeBackdropMatchesFresh(t, m)
}

func TestDraftAutosaveOutsideDraftsDoesNotReload(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(100, 40, false)
	_ = m.View()
	_, cmd := m.Update(DraftSavedMsg{DraftID: 1})
	if cmd != nil {
		t.Fatal("autosave outside Drafts should not reload the list")
	}
}
