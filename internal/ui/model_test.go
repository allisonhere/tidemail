package ui

import (
	"errors"
	"fmt"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestSaveConfigSurfacesError(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	orig := configSave
	configSave = func(config.Config) error { return fmt.Errorf("disk full") }
	defer func() { configSave = orig }()

	m.saveConfig()
	if !m.statusErr {
		t.Fatal("expected the status line to be flagged as an error")
	}
	if !strings.Contains(m.statusMsg, "disk full") {
		t.Fatalf("expected the save error surfaced on the status line, got %q", m.statusMsg)
	}
}

func TestSaveConfigSuccessNoError(t *testing.T) {
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	orig := configSave
	configSave = func(config.Config) error { return nil }
	defer func() { configSave = orig }()

	m.saveConfig()
	if m.statusErr {
		t.Fatalf("did not expect an error status on success, got %q", m.statusMsg)
	}
}

func TestDeleteKeyWaitsForDeleteResultBeforeRemovingMessage(t *testing.T) {
	msg := db.Message{ID: 42, MailboxID: 7, UID: 99, Subject: "Keep until confirmed"}
	m := Model{
		keys:             DefaultKeys,
		focused:          paneMessages,
		messages:         []db.Message{msg},
		filteredMessages: []db.Message{msg},
		selectedMessages: make(map[int64]bool),
	}

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)

	if cmd == nil {
		t.Fatal("expected delete key to start a delete command")
	}
	if len(m.messages) != 1 || len(m.filteredMessages) != 1 {
		t.Fatalf("expected message to stay visible until delete succeeds, got %d/%d", len(m.messages), len(m.filteredMessages))
	}

	next, _ = m.Update(MessageDeletedMsg{MessageID: msg.ID, MailboxID: msg.MailboxID})
	m = next.(Model)

	if len(m.messages) != 0 || len(m.filteredMessages) != 0 {
		t.Fatalf("expected successful delete result to remove message, got %d/%d", len(m.messages), len(m.filteredMessages))
	}
}

func TestMessageDeletedMsgRemovesLocallyDeletedMessageDespiteRemoteError(t *testing.T) {
	msg := db.Message{ID: 1, MailboxID: 2, Subject: "Delete me"}
	m := Model{
		messages:         []db.Message{msg},
		filteredMessages: []db.Message{msg},
		messageCursor:    0,
		selectedMessages: make(map[int64]bool),
	}

	next, _ := m.Update(MessageDeletedMsg{
		MessageID:    msg.ID,
		MailboxID:    msg.MailboxID,
		LocalDeleted: true,
		Err:          errors.New("remote unavailable"),
	})
	m = next.(Model)

	if len(m.messages) != 0 || len(m.filteredMessages) != 0 {
		t.Fatalf("expected local delete result to remove message, got %d/%d", len(m.messages), len(m.filteredMessages))
	}
}

func TestSpaceSelectAdvanceKeepsCursorVisible(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	m.focused = paneMessages

	visible := m.articleRowsVisible()
	if visible < 2 {
		t.Fatalf("expected at least 2 visible message rows, got %d", visible)
	}
	msgs := make([]db.Message, visible+2)
	for i := range msgs {
		msgs[i] = db.Message{ID: int64(i + 1), Subject: fmt.Sprintf("Message %d", i+1)}
	}
	m.messages = msgs
	m.filteredMessages = msgs
	m.messageCursor = visible - 1
	m.listOffset = 0

	next, _ = m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)

	if m.messageCursor != visible {
		t.Fatalf("expected cursor to advance to %d, got %d", visible, m.messageCursor)
	}
	if m.listOffset != 1 {
		t.Fatalf("expected list offset to scroll to 1, got %d", m.listOffset)
	}
}

func TestValidateAccountForConnect(t *testing.T) {
	base := config.AccountConfig{Name: "Acct", IMAPHost: "imap.x", User: "u", IMAPPort: 993, SMTPPort: 587}
	if got := validateAccountForConnect(base); got != "" {
		t.Fatalf("valid config rejected: %q", got)
	}
	cases := []struct {
		name string
		mut  func(*config.AccountConfig)
	}{
		{"missing name", func(c *config.AccountConfig) { c.Name = "" }},
		{"missing imap host", func(c *config.AccountConfig) { c.IMAPHost = "" }},
		{"missing user", func(c *config.AccountConfig) { c.User = "" }},
		{"imap port too high", func(c *config.AccountConfig) { c.IMAPPort = 70000 }},
		{"smtp port zero", func(c *config.AccountConfig) { c.SMTPPort = 0 }},
		{"imap port negative", func(c *config.AccountConfig) { c.IMAPPort = -1 }},
	}
	for _, tc := range cases {
		cfg := base
		tc.mut(&cfg)
		if got := validateAccountForConnect(cfg); got == "" {
			t.Errorf("%s: expected a validation error, got none", tc.name)
		}
	}
}
