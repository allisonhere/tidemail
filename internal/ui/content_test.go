package ui

import (
	"strings"
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	"github.com/charmbracelet/x/ansi"
)

func TestRenderMessageContentAddsBlankLineAfterMessageID(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width = 100
	m.contentShowHeaders = true

	view := ansi.Strip(m.renderMessageContent(db.Message{
		Subject:   "Hello",
		From:      "alice@example.com",
		To:        "bob@example.com",
		Date:      time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		MessageID: "<message-id@example.com>",
		BodyText:  "Body starts here.",
	}))

	messageIDLine := -1
	lines := strings.Split(view, "\n")
	for i, line := range lines {
		if strings.Contains(line, "Message-ID:") {
			messageIDLine = i
			break
		}
	}
	if messageIDLine < 0 {
		t.Fatalf("expected Message-ID header in rendered content, got %q", view)
	}
	if messageIDLine+1 >= len(lines) || strings.TrimSpace(lines[messageIDLine+1]) != "" {
		t.Fatalf("expected blank line after Message-ID, got next line %q in %q", lines[messageIDLine+1], view)
	}
}

func TestRenderMessageTitleSpansPaneWhenReadingWidthIsCapped(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ReadingWidth = 32
	m := NewModel(nil, cfg, "dev", false)
	m.width = 100

	view := m.renderMessageContent(db.Message{
		Subject:  "A message with a title bar",
		Date:     time.Date(2026, 6, 1, 10, 0, 0, 0, time.UTC),
		BodyText: "Body starts here.",
	})
	firstLine := strings.Split(view, "\n")[0]

	if got, want := ansi.StringWidth(firstLine), m.articlesPaneWidth(); got != want {
		t.Fatalf("expected title line to span content pane width %d, got %d in %q", want, got, ansi.Strip(firstLine))
	}
}
