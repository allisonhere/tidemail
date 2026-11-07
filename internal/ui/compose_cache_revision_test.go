package ui

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func TestComposeCacheBackgroundResults(t *testing.T) {
	preserveComposeEditorState(t)
	for _, name := range []string{"same-id-mail", "star", "annotations", "unread-count", "viewport", "threads", "draft-count", "sync", "status"} {
		t.Run(name, func(t *testing.T) {
			m := composeEventModel(120, 50, false)
			m.sizeCompose()
			stats := &renderCacheStats{}
			m.composeBackdrop.paneStage.stats = stats
			_ = m.View()
			revision := m.composeDisplayRevision
			var msg tea.Msg
			switch name {
			case "same-id-mail":
				messages := append([]db.Message(nil), m.messages...)
				messages[0].Subject = "asynchronous replacement"
				msg = MessagesLoadedMsg{MailboxID: 7, Messages: messages}
			case "star":
				msg = MessageStarredUpdatedMsg{MessageID: m.messages[0].ID, Starred: true}
			case "annotations":
				msg = pluginAnnotationsRefreshedMsg{MessageID: m.messages[0].ID, Annotations: []db.PluginAnnotation{{PluginID: "test", Key: "urgent", Value: "true"}}}
			case "unread-count":
				m.adjustMailboxUnreadCount(7, 3)
			case "viewport":
				m.setViewportMessage(db.Message{ID: m.messages[0].ID, Subject: "new reader", BodyText: "new reader content"})
			case "threads":
				m.cfg.Display.ThreadedConversations = true
				m.rebuildMessageThreads()
			case "draft-count":
				m.refreshDraftCounts()
			case "sync":
				msg = MailboxSyncedMsg{MailboxID: 7, SyncedAt: time.Now()}
			case "status":
				m.setStatus("background status only", false)
			}
			if msg != nil {
				next, _ := m.Update(msg)
				m = next.(Model)
			}
			if m.overlay != overlayCompose {
				t.Fatal("background result closed composer")
			}
			assertComposeBackdropMatchesFresh(t, m)
			if name == "status" {
				if stats.Misses != 1 || m.composeDisplayRevision != revision {
					t.Fatal("status invalidated pane data")
				}
			} else if stats.Misses != 2 || m.composeDisplayRevision == revision {
				t.Fatalf("background revision not consumed: misses=%d revision=%d -> %d", stats.Misses, revision, m.composeDisplayRevision)
			}
		})
	}
}

func TestComposeCacheMouseKeepsDisplayRevision(t *testing.T) {
	preserveComposeEditorState(t)
	m := composeEventModel(160, 50, false)
	m.cfg.Display.MouseSupport = true
	m.sizeCompose()
	m.compose.bodyInput.ed.ScrollRows(-10000)
	stats := &renderCacheStats{}
	m.composeBackdrop.paneStage.stats = stats
	_ = m.View()
	revision := m.composeDisplayRevision
	x, y, _, _, ok := m.composeBodyRect()
	if !ok {
		t.Fatal("body not visible")
	}
	for _, msg := range []tea.MouseMsg{
		{X: x + 1, Y: y, Button: tea.MouseButtonLeft, Action: tea.MouseActionPress},
		{X: x + 5, Y: y + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionMotion},
		{X: x + 5, Y: y + 2, Button: tea.MouseButtonLeft, Action: tea.MouseActionRelease},
	} {
		next, _ := m.Update(msg)
		m = next.(Model)
		_ = m.View()
	}
	if m.composeDisplayRevision != revision || stats.Misses != 1 || m.compose.bodyInput.SelectedText() == "" {
		t.Fatal("mouse interaction touched background revision or lost selection")
	}
}

func TestComposeCacheLabelDeadlines(t *testing.T) {
	now := time.Date(2026, 11, 7, 12, 0, 0, 0, time.UTC)
	for _, c := range []struct{ age, until time.Duration }{{30 * time.Second, 30 * time.Second}, {90 * time.Second, 30 * time.Second}, {65 * time.Minute, 55 * time.Minute}, {26 * time.Hour, 22 * time.Hour}, {8 * 24 * time.Hour, 6 * 24 * time.Hour}} {
		got := nextRelativeLabelChange(now.Add(-c.age), now)
		if !got.Equal(now.Add(c.until)) {
			t.Fatalf("age=%s deadline=%s", c.age, got)
		}
	}
	if !nextRelativeLabelChange(now.Add(-29*24*time.Hour), now).IsZero() {
		t.Fatal("stable date expires")
	}
	preserveComposeEditorState(t)
	m := composeEventModel(120, 50, false)
	m.sizeCompose()
	m.messages[0].Date = time.Now().Add(-90 * time.Second)
	m.applyFilter()
	stats := &renderCacheStats{}
	m.composeBackdrop.paneStage.stats = stats
	_ = m.View()
	deadline := m.composeBackdrop.labelsExpire
	if deadline.IsZero() || !deadline.After(time.Now()) {
		t.Fatal("missing relative label deadline")
	}
	_ = m.View()
	if stats.Misses != 1 {
		t.Fatal("unexpired date invalidated panes")
	}
	m.composeBackdrop.labelsExpire = time.Now().Add(-time.Nanosecond)
	assertComposeBackdropMatchesFresh(t, m)
	if stats.Misses != 2 {
		t.Fatal("expired label did not refresh panes")
	}
}

// The loaded collection/body size varies; the measured hit validation never
// serializes or visits those collections. Full event-path checks use Foot.
func BenchmarkComposeRevisionKeyScaling(b *testing.B) {
	preserveComposeEditorState(b)
	for _, n := range []int{20, 200, 1000} {
		for _, size := range []int{1024, 32768} {
			b.Run(fmt.Sprintf("messages=%d/body=%d", n, size), func(b *testing.B) {
				m, msgs := newMailboxListModel(7, n)
				body := strings.Repeat("x", size)
				for i := range msgs {
					msgs[i].BodyText = body
					msgs[i].BodyHTML = body
				}
				m.messages = msgs
				m.applyFilter()
				_ = m.composePaneKey()
				b.ReportAllocs()
				b.ResetTimer()
				for b.Loop() {
					_ = m.composePaneKey()
				}
			})
		}
	}
}

func TestComposeCacheReadCompletion(t *testing.T) {
	preserveComposeEditorState(t)
	m, _, _ := readPendingModel(t)
	m.compose = NewCompose(m.cfg.Accounts[0], m.cfg.Accounts, nil)
	m.overlay = overlayCompose
	m.sizeCompose()
	msg := m.messages[0]
	m.pendingReads = map[int64]pendingRead{msg.ID: {Token: 9, Read: true, MailboxID: msg.MailboxID}}
	_ = m.View()
	if err := m.db.MarkRead(msg.ID, true); err != nil {
		t.Fatal(err)
	}
	next, _ := m.Update(readRemoteCompletedMsg{Token: 9, IDs: []int64{msg.ID}, Success: []MessageReadBatchEntry{{MessageID: msg.ID, MailboxID: msg.MailboxID, Read: true}}})
	m = next.(Model)
	if m.overlay != overlayCompose || len(m.pendingReads) != 0 || !m.messages[0].Read {
		t.Fatal("read completion did not update open compose background")
	}
	assertComposeBackdropMatchesFresh(t, m)
}
