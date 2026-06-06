package ui

import (
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
)

func TestRenderArticleRowWithSenderShowsBothColumns(t *testing.T) {
	row := renderArticleRowWithSender("● ", "Jane Doe", "Lunch tomorrow?", "2h", 50, 12)
	if !strings.Contains(row, "Jane Doe") {
		t.Fatalf("expected sender in row: %q", row)
	}
	if !strings.Contains(row, "Lunch tomorrow?") {
		t.Fatalf("expected subject in row: %q", row)
	}
}

func TestMessageListSenderToggle(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("Open DB: %v", err)
	}
	defer database.Close()

	cfg := config.DefaultConfig()
	cfg.Display.ShowSender = true
	m := NewModel(database, cfg, "dev", false)
	next, _ := m.Update(tea.WindowSizeMsg{Width: 120, Height: 40})
	m = next.(Model)
	m.filteredMessages = []db.Message{
		{ID: 1, From: "Jane Doe <jane@example.com>", Subject: "Lunch tomorrow?"},
	}

	if !strings.Contains(m.renderMessagesPane(), "Jane Doe") {
		t.Fatalf("expected sender shown when ShowSender is on")
	}

	m.cfg.Display.ShowSender = false
	if strings.Contains(m.renderMessagesPane(), "Jane Doe") {
		t.Fatalf("expected sender hidden when ShowSender is off")
	}
}
