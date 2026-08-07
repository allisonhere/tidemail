package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func TestBuildMessageThreadsGroupsRepliesByHeaders(t *testing.T) {
	root := db.Message{ID: 1, MessageID: "<root@example.com>", Subject: "Plan", Date: time.Unix(100, 0), From: "Ada <ada@example.com>", Read: true}
	reply := db.Message{ID: 2, MessageID: "<reply@example.com>", InReplyTo: "<root@example.com>", Subject: "Re: Plan", Date: time.Unix(200, 0), From: "Bob <bob@example.com>", Read: false}

	threads := buildMessageThreads([]db.Message{reply, root})

	if len(threads) != 1 {
		t.Fatalf("expected one thread, got %+v", threads)
	}
	if threads[0].Representative.ID != reply.ID {
		t.Fatalf("expected newest reply as representative, got %+v", threads[0].Representative)
	}
	if threads[0].Count != 2 || threads[0].UnreadCount != 1 {
		t.Fatalf("expected count/unread 2/1, got %d/%d", threads[0].Count, threads[0].UnreadCount)
	}
	if got := []int64{threads[0].Messages[0].ID, threads[0].Messages[1].ID}; got[0] != root.ID || got[1] != reply.ID {
		t.Fatalf("expected chronological messages root, reply; got %+v", got)
	}
}

func TestBuildMessageThreadsGroupsReferencesChainWithMissingRoot(t *testing.T) {
	first := db.Message{ID: 1, MessageID: "<a@example.com>", References: "<missing@example.com>", Date: time.Unix(100, 0)}
	second := db.Message{ID: 2, MessageID: "<b@example.com>", References: "<missing@example.com> <a@example.com>", Date: time.Unix(200, 0)}

	threads := buildMessageThreads([]db.Message{second, first})

	if len(threads) != 1 {
		t.Fatalf("expected references chain grouped, got %+v", threads)
	}
	if threads[0].Representative.ID != second.ID {
		t.Fatalf("expected newest message representative, got %+v", threads[0].Representative)
	}
}

func TestBuildMessageThreadsLeavesHeaderlessMessagesSeparate(t *testing.T) {
	threads := buildMessageThreads([]db.Message{
		{ID: 1, Subject: "Same", Date: time.Unix(100, 0)},
		{ID: 2, Subject: "Same", Date: time.Unix(200, 0)},
	})

	if len(threads) != 2 {
		t.Fatalf("expected headerless messages to stay separate, got %+v", threads)
	}
}

func TestCurrentRowMessagesReturnsWholeThreadWhenThreaded(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ThreadedConversations = true
	m := NewModel(nil, cfg, "dev", false)
	m.filteredMessages = []db.Message{
		{ID: 2, MessageID: "<reply@example.com>", InReplyTo: "<root@example.com>", Date: time.Unix(200, 0)},
		{ID: 1, MessageID: "<root@example.com>", Date: time.Unix(100, 0)},
	}
	m.rebuildMessageThreads()

	msgs := m.currentRowMessages()

	if len(msgs) != 2 || msgs[0].ID != 1 || msgs[1].ID != 2 {
		t.Fatalf("expected chronological thread messages, got %+v", msgs)
	}
	if rep := m.currentRowMessage(); rep == nil || rep.ID != 2 {
		t.Fatalf("expected newest representative, got %+v", rep)
	}
}

func TestCurrentRowMessagesStaysFlatWhenThreadingDisabled(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.filteredMessages = []db.Message{
		{ID: 2, MessageID: "<reply@example.com>", InReplyTo: "<root@example.com>", Date: time.Unix(200, 0)},
		{ID: 1, MessageID: "<root@example.com>", Date: time.Unix(100, 0)},
	}

	msgs := m.currentRowMessages()

	if len(msgs) != 1 || msgs[0].ID != 2 {
		t.Fatalf("expected flat current message only, got %+v", msgs)
	}
}

func TestThreadedMessageListAndContentRenderConversation(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ThreadedConversations = true
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100
	m.height = 24
	m.focused = paneMessages
	m.filteredMessages = []db.Message{
		{ID: 2, MessageID: "<reply@example.com>", InReplyTo: "<root@example.com>", Subject: "Re: Plan", From: "Bob <bob@example.com>", Date: time.Unix(200, 0), BodyText: "second"},
		{ID: 1, MessageID: "<root@example.com>", Subject: "Plan", From: "Ada <ada@example.com>", Date: time.Unix(100, 0), BodyText: "first", Read: true},
	}
	m.rebuildMessageThreads()

	list := m.renderMessagesPane()
	if !strings.Contains(list, "Re: Plan (2)") {
		t.Fatalf("expected thread count in message list, got %q", list)
	}

	content := m.renderThreadContent(m.messageThreads[0])
	first := strings.Index(content, "first")
	second := strings.Index(content, "second")
	if first < 0 || second < 0 || first > second {
		t.Fatalf("expected chronological thread content, got %q", content)
	}
	if !strings.Contains(content, "Ada") || !strings.Contains(content, "Bob") {
		t.Fatalf("expected per-message sender separators, got %q", content)
	}
}

func TestThreadedMarkReadMarksWholeCurrentThreadRead(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Display.ThreadedConversations = true
	m := NewModel(database, cfg, "dev", false)
	m.focused = paneMessages
	m.filteredMessages = []db.Message{
		{ID: 2, MessageID: "<reply@example.com>", InReplyTo: "<root@example.com>", Date: time.Unix(200, 0)},
		{ID: 1, MessageID: "<root@example.com>", Date: time.Unix(100, 0), Read: true},
	}
	m.rebuildMessageThreads()

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'x'}})
	m = next.(Model)
	msg := cmd()
	batch, ok := msg.(tea.BatchMsg)
	if !ok || len(batch) != 2 {
		t.Fatalf("expected batch mark-read commands for both thread messages, got %#v", msg)
	}
	for _, c := range batch {
		got := c().(MessageReadUpdatedMsg)
		if !got.Read {
			t.Fatalf("expected thread message %d marked read, got %+v", got.MessageID, got)
		}
	}
}
