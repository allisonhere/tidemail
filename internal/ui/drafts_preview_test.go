package ui

import (
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// draftsPreviewModel is a model sitting on a Drafts mailbox with two drafts.
func draftsPreviewModel(t *testing.T) Model {
	t.Helper()
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m.focused = paneMessages
	m.accounts = []db.Account{{ID: 1, Name: "Personal"}}
	m.mailboxes = []db.Mailbox{{ID: 2, AccountID: 1, Name: "Drafts", Flags: []string{"\\Drafts"}}}
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: 2, accountID: 1}}
	m.drafts = []db.Draft{
		{ID: 9, AccountName: "Personal", To: "bob@example.com", Subject: "First draft",
			BodyText: "body of the first draft", UpdatedAt: time.Unix(1710000000, 0)},
		{ID: 10, AccountName: "Personal", To: "carol@example.com", Subject: "Second draft",
			BodyText: "body of the second draft", UpdatedAt: time.Unix(1710000100, 0)},
	}
	if !m.selectedDraftsMailbox() {
		t.Fatal("test setup: expected the Drafts mailbox to be selected")
	}
	return m
}

func contentText(m Model) string {
	return strings.Join(m.contentLines, "\n")
}

// Moving through the drafts list must show the draft under the cursor, the way
// moving through any other mailbox shows the message under the cursor.
func TestScrollingDraftsShowsTheDraftUnderTheCursor(t *testing.T) {
	m := draftsPreviewModel(t)
	m.setViewportForCurrentRow()

	body := contentText(m)
	if !strings.Contains(body, "First draft") || !strings.Contains(body, "body of the first draft") {
		t.Fatalf("expected the first draft in the content pane, got:\n%s", body)
	}

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyDown})
	m = next.(Model)
	if m.messageCursor != 1 {
		t.Fatalf("expected the cursor to move to the second draft, got %d", m.messageCursor)
	}

	body = contentText(m)
	if !strings.Contains(body, "Second draft") || !strings.Contains(body, "body of the second draft") {
		t.Fatalf("expected the second draft after scrolling down, got:\n%s", body)
	}
	if strings.Contains(body, "body of the first draft") {
		t.Fatalf("the first draft is still showing after moving down:\n%s", body)
	}

	// And back up again.
	next, _ = m.Update(tea.KeyMsg{Type: tea.KeyUp})
	m = next.(Model)
	body = contentText(m)
	if !strings.Contains(body, "body of the first draft") {
		t.Fatalf("expected the first draft after scrolling back up, got:\n%s", body)
	}
}

// The recipient is what distinguishes one unsent draft from another.
func TestDraftPreviewShowsRecipients(t *testing.T) {
	m := draftsPreviewModel(t)
	m.drafts[0].CC = "dave@example.com"
	m.setViewportForCurrentRow()

	body := contentText(m)
	for _, want := range []string{"bob@example.com", "dave@example.com", "Draft"} {
		if !strings.Contains(body, want) {
			t.Errorf("expected %q in the draft preview, got:\n%s", want, body)
		}
	}
}

// A draft with nothing in it must still render, not leave a blank pane that
// looks like a failure to load.
func TestEmptyDraftStillRenders(t *testing.T) {
	m := draftsPreviewModel(t)
	m.drafts = []db.Draft{{ID: 11, AccountName: "Personal"}}
	m.messageCursor = 0
	m.setViewportForCurrentRow()

	body := contentText(m)
	if !strings.Contains(body, "(no subject)") {
		t.Errorf("expected a placeholder subject, got:\n%s", body)
	}
	if !strings.Contains(body, "no recipient yet") {
		t.Errorf("expected a placeholder recipient, got:\n%s", body)
	}
}

// contentMessageID is resolved against filteredMessages; putting a draft ID
// there would make an unrelated message look like the one on screen.
func TestDraftPreviewDoesNotSetContentMessageID(t *testing.T) {
	m := draftsPreviewModel(t)
	m.filteredMessages = []db.Message{{ID: 9, Subject: "Unrelated message"}}
	m.setViewportForCurrentRow()

	if m.contentMessageID != 0 {
		t.Fatalf("contentMessageID must stay 0 while a draft is shown, got %d", m.contentMessageID)
	}
	if m.contentDraftID != 9 {
		t.Fatalf("expected contentDraftID 9, got %d", m.contentDraftID)
	}
	if got := m.currentContentMessage(); got != nil {
		t.Fatalf("a draft must not resolve to a message, got %q", got.Subject)
	}
}

// Leaving the drafts mailbox must not leave the pane resolving back to a draft
// the next time it re-renders, such as on a resize.
func TestLeavingDraftsClearsTheDraftPreview(t *testing.T) {
	m := draftsPreviewModel(t)
	m.setViewportForCurrentRow()
	if m.contentDraftID == 0 {
		t.Fatal("test setup: expected a draft to be showing")
	}

	// Move to an ordinary mailbox holding one message.
	m.mailboxes = append(m.mailboxes, db.Mailbox{ID: 3, AccountID: 1, Name: "Inbox"})
	m.sidebarRows = []sidebarRow{{kind: rowKindMailbox, mailboxID: 3, accountID: 1}}
	m.filteredMessages = []db.Message{{ID: 42, Subject: "A real message", BodyText: "real body"}}
	m.messageCursor = 0
	m.setViewportForCurrentRow()

	if m.contentDraftID != 0 {
		t.Fatalf("contentDraftID must reset when a message is shown, got %d", m.contentDraftID)
	}
	if m.contentMessageID != 42 {
		t.Fatalf("expected contentMessageID 42, got %d", m.contentMessageID)
	}

	// A resize must keep showing the message, not fall back to the draft.
	m.refreshContentAfterPaneResize()
	body := contentText(m)
	if !strings.Contains(body, "A real message") {
		t.Fatalf("expected the message after a resize, got:\n%s", body)
	}
	if strings.Contains(body, "First draft") {
		t.Fatalf("the draft came back after a resize:\n%s", body)
	}
}

// A runaway draft body (one this size took seconds to wrap on the UI thread)
// must render only its first part in the preview, and say so.
func TestHugeDraftPreviewIsCapped(t *testing.T) {
	m := draftsPreviewModel(t)
	m.drafts[0].BodyText = strings.Repeat("yoooooooooooooooo", 256<<10) // 4.25 MB, one line
	m.setViewportForCurrentRow()

	body := contentText(m)
	if !strings.Contains(body, "preview shows the first 64.0 KB of 4.2 MB") {
		t.Fatalf("expected a truncation note, got tail:\n%s", body[max(0, len(body)-400):])
	}
	if got, limit := len(strings.Join(m.contentLines, "")), 2*draftPreviewLimit; got > limit {
		t.Fatalf("preview rendered %d bytes of text, want at most about %d", got, limit)
	}
}

func TestDraftPreviewTextCutsOnARuneBoundary(t *testing.T) {
	body := strings.Repeat("a", draftPreviewLimit-1) + "é" + strings.Repeat("b", 10)
	got, truncated := draftPreviewText(body)
	if !truncated || !utf8.ValidString(got) || len(got) != draftPreviewLimit-1 {
		t.Fatalf("truncated=%v valid=%v len=%d", truncated, utf8.ValidString(got), len(got))
	}
	if got, truncated := draftPreviewText("short"); truncated || got != "short" {
		t.Fatalf("short body changed: %q %v", got, truncated)
	}
}

// Reloading the Drafts list re-shows the same draft; that must reuse the
// rendered preview, while a saved edit must not show the stale one.
func TestDraftPreviewIsCachedUntilTheDraftChanges(t *testing.T) {
	m := draftsPreviewModel(t)
	m.setViewportForCurrentRow()
	m.setViewportForCurrentRow()
	if n := m.viewportCache.len(); n != 1 {
		t.Fatalf("expected one cached preview after re-showing the same draft, got %d", n)
	}

	m.drafts[0].BodyText = "edited body"
	m.drafts[0].UpdatedAt = m.drafts[0].UpdatedAt.Add(time.Minute)
	m.setViewportForCurrentRow()
	if body := contentText(m); !strings.Contains(body, "edited body") {
		t.Fatalf("expected the edited draft, got:\n%s", body)
	}
}
