package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

// The render caches must never change what the reader sees — only how often it
// is computed. These tests pin the invalidation rules, because a stale hit
// would show the wrong message body.

func cacheTestModel(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	t.Cleanup(func() { database.Close() })

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	return next.(Model)
}

func cacheTestMessage(id int64, body string) db.Message {
	return db.Message{
		ID: id, MailboxID: 1, UID: uint32(id),
		MessageID: fmt.Sprintf("<c%d@example.com>", id),
		From:      "sender@example.com",
		Subject:   "Subject",
		BodyHTML:  "<html><body><p>" + body + "</p></body></html>",
	}
}

func TestBodyCacheHitMatchesFreshRender(t *testing.T) {
	m := cacheTestModel(t)
	msg := cacheTestMessage(1, "hello world")
	width := m.contentBodyWidth()

	first := m.renderMessageForDisplay(msg, width)
	if m.bodyCache.len() != 1 {
		t.Fatalf("cache holds %d entries after one render, want 1", m.bodyCache.len())
	}
	second := m.renderMessageForDisplay(msg, width)

	if first.body != second.body {
		t.Fatalf("cached render differs from the first:\n%q\nvs\n%q", second.body, first.body)
	}
	if strings.Join(first.links, "|") != strings.Join(second.links, "|") {
		t.Fatalf("cached links = %v, want %v", second.links, first.links)
	}
}

func TestBodyCacheKeyedOnRenderInputs(t *testing.T) {
	msg := cacheTestMessage(1, "hello world")

	t.Run("width", func(t *testing.T) {
		m := cacheTestModel(t)
		m.renderMessageForDisplay(msg, 60)
		m.renderMessageForDisplay(msg, 90)
		if got := m.bodyCache.len(); got != 2 {
			t.Fatalf("cache holds %d entries for two widths, want 2", got)
		}
	})

	t.Run("filterLinks", func(t *testing.T) {
		m := cacheTestModel(t)
		width := m.contentBodyWidth()
		m.cfg.Display.FilterLinks = false
		m.renderMessageForDisplay(msg, width)
		m.cfg.Display.FilterLinks = true
		m.renderMessageForDisplay(msg, width)
		if got := m.bodyCache.len(); got != 2 {
			t.Fatalf("cache holds %d entries for two link settings, want 2", got)
		}
	})

	t.Run("changed body", func(t *testing.T) {
		m := cacheTestModel(t)
		width := m.contentBodyWidth()
		first := m.renderMessageForDisplay(msg, width)

		// Same ID, re-fetched with different content.
		changed := cacheTestMessage(1, "completely different text here")
		second := m.renderMessageForDisplay(changed, width)

		if strings.Contains(second.body, "hello world") {
			t.Fatalf("a re-fetched message served the old cached body: %q", second.body)
		}
		if first.body == second.body {
			t.Fatal("changed body produced an identical render — the fingerprint is not working")
		}
	})
}

func TestBodyCacheStaysBounded(t *testing.T) {
	m := cacheTestModel(t)
	width := m.contentBodyWidth()
	for i := 0; i < defaultBodyCacheLimit+20; i++ {
		m.renderMessageForDisplay(cacheTestMessage(int64(i+1), fmt.Sprintf("body %d", i)), width)
	}
	if got := m.bodyCache.len(); got != defaultBodyCacheLimit {
		t.Fatalf("cache holds %d entries, want it capped at %d", got, defaultBodyCacheLimit)
	}
}

func TestUnsavedMessageIsNotCached(t *testing.T) {
	m := cacheTestModel(t)
	// ID 0 means "not stored yet" — there is no stable identity to key on.
	m.renderMessageForDisplay(cacheTestMessage(0, "preview"), m.contentBodyWidth())
	if got := m.bodyCache.len(); got != 0 {
		t.Fatalf("cache holds %d entries for an unsaved message, want 0", got)
	}
}

func TestSetStylesDropsTheRenderCaches(t *testing.T) {
	m := cacheTestModel(t)
	m.setViewportMessage(cacheTestMessage(1, "hello"))
	if m.bodyCache.len() == 0 || m.viewportCache.len() == 0 {
		t.Fatalf("expected both caches populated, got body=%d viewport=%d", m.bodyCache.len(), m.viewportCache.len())
	}

	// A theme can change colours while keeping its name, so the key cannot
	// catch it — the swap must clear.
	m.setStyles(BuildStyles(BuiltinThemes[1], "compact", "square"))

	if m.bodyCache.len() != 0 || m.viewportCache.len() != 0 {
		t.Fatalf("caches survived a style change: body=%d viewport=%d", m.bodyCache.len(), m.viewportCache.len())
	}
}

func TestViewportCacheHitMatchesFreshRender(t *testing.T) {
	m := cacheTestModel(t)
	msg := cacheTestMessage(1, "hello world")

	m.setViewportMessage(msg)
	firstLines := append([]string(nil), m.contentLines...)
	firstCount := m.contentLineCount
	firstFocusable := append([]bool(nil), m.contentFocusable...)

	// Move away and back: the second visit is the cache hit.
	m.setViewportMessage(cacheTestMessage(2, "another message"))
	m.setViewportMessage(msg)

	if m.contentLineCount != firstCount {
		t.Fatalf("line count = %d on revisit, want %d", m.contentLineCount, firstCount)
	}
	if strings.Join(m.contentLines, "\n") != strings.Join(firstLines, "\n") {
		t.Fatal("revisiting a message produced different display lines")
	}
	if len(m.contentFocusable) != len(firstFocusable) {
		t.Fatalf("focusable lines = %d on revisit, want %d", len(m.contentFocusable), len(firstFocusable))
	}
	for i := range firstFocusable {
		if m.contentFocusable[i] != firstFocusable[i] {
			t.Fatalf("focusable line %d changed on revisit", i)
		}
	}
	if m.contentMessageID != msg.ID {
		t.Fatalf("contentMessageID = %d, want %d", m.contentMessageID, msg.ID)
	}
}

func TestViewportCacheSeparatesMessagesAndThreads(t *testing.T) {
	m := cacheTestModel(t)
	msg := cacheTestMessage(1, "hello world")

	m.setViewportMessage(msg)
	single := strings.Join(m.contentLines, "\n")

	thread := messageThread{
		Key:            "1",
		Representative: msg,
		Messages:       []db.Message{msg, cacheTestMessage(2, "a reply in the same thread")},
		Count:          2,
	}
	m.setViewportThread(thread)
	threaded := strings.Join(m.contentLines, "\n")

	if single == threaded {
		t.Fatal("a thread rendered identically to its first message — the keys collide")
	}
	if !strings.Contains(threaded, "reply in the same thread") {
		t.Fatalf("thread content is missing the second message:\n%s", threaded)
	}
}

func TestFocusableFromLinesMatchesFullStrip(t *testing.T) {
	m := cacheTestModel(t)
	content := m.renderMessageContent(cacheTestMessage(1, "one two three"))

	lines, _ := contentDisplayLines(content)
	shared := focusableFromLines(lines)
	full := messageFocusableLines(content)

	if len(shared) != len(full) {
		t.Fatalf("shared pass gave %d lines, full strip gave %d", len(shared), len(full))
	}
	for i := range full {
		if shared[i] != full[i] {
			t.Fatalf("line %d: shared pass = %v, full strip = %v", i, shared[i], full[i])
		}
	}
}

func TestFocusPaneKeepsContentWithoutRerendering(t *testing.T) {
	m := cacheTestModel(t)
	msg := cacheTestMessage(1, "hello world")
	m.messages = []db.Message{msg}
	m.filteredMessages = m.messages
	m.messageCursor = 0
	m.focused = paneMessages
	m.setViewportForCurrentRow()

	before := strings.Join(m.contentLines, "\n")
	beforeID := m.contentMessageID

	next, _ := m.focusPane(paneContent)
	m = next.(Model)
	if m.focused != paneContent {
		t.Fatalf("focused = %v, want the content pane", m.focused)
	}
	next, _ = m.focusPane(paneMessages)
	m = next.(Model)

	// Tab must not disturb what the reading pane is showing — it used to
	// re-render the whole thread just to update the focus rail.
	if m.contentMessageID != beforeID {
		t.Fatalf("contentMessageID = %d after Tab, want %d", m.contentMessageID, beforeID)
	}
	if got := strings.Join(m.contentLines, "\n"); got != before {
		t.Fatal("Tab changed the reading pane content")
	}
}
