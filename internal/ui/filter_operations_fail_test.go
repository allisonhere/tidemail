package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/allisonhere/tidemail/internal/db"
	tea "github.com/charmbracelet/bubbletea"
)

func filterManagerWithClosedDB(t *testing.T) Model {
	t.Helper()
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	t.Setenv("XDG_CONFIG_HOME", t.TempDir())

	database, err := db.Open()
	if err != nil {
		t.Fatalf("open DB: %v", err)
	}
	firstID, err := database.UpsertRule(db.RuleRecord{Priority: 1, Enabled: true, Name: "first", JSON: "{}"})
	if err != nil {
		t.Fatal(err)
	}
	secondID, err := database.UpsertRule(db.RuleRecord{Priority: 2, Enabled: true, Name: "second", JSON: "{}"})
	if err != nil {
		t.Fatal(err)
	}

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.overlay = overlayFilterManager
	m.filterManager = filterManager{
		mode: fmList,
		rules: []db.RuleRecord{
			{ID: firstID, Priority: 1, Enabled: true, Name: "first", JSON: "{}"},
			{ID: secondID, Priority: 2, Enabled: true, Name: "second", JSON: "{}"},
		},
	}
	if err := database.Close(); err != nil {
		t.Fatal(err)
	}
	return m
}

func TestFilterToggleFailureIsSurfaced(t *testing.T) {
	m := filterManagerWithClosedDB(t)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeySpace})
	m = next.(Model)

	if !strings.Contains(m.filterManager.status, "update failed") {
		t.Fatalf("expected update failure status, got %q", m.filterManager.status)
	}
}

func TestFilterDeleteFailureIsSurfaced(t *testing.T) {
	m := filterManagerWithClosedDB(t)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'d'}})
	m = next.(Model)

	if !strings.Contains(m.filterManager.status, "delete failed") {
		t.Fatalf("expected delete failure status, got %q", m.filterManager.status)
	}
}

func TestFilterReorderFailureIsSurfacedAndCursorStaysPut(t *testing.T) {
	m := filterManagerWithClosedDB(t)

	next, _ := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'J'}})
	m = next.(Model)

	if !strings.Contains(m.filterManager.status, "reorder failed") {
		t.Fatalf("expected reorder failure status, got %q", m.filterManager.status)
	}
	if m.filterManager.cursor != 0 {
		t.Fatalf("expected cursor to remain on original rule, got %d", m.filterManager.cursor)
	}
}
