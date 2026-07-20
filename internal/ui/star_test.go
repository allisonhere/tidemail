package ui

import (
	"testing"
	"time"

	"github.com/allisonhere/tide/internal/config"
	"github.com/allisonhere/tide/internal/db"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
)

func TestStarredRowBackgroundIsSubtleTint(t *testing.T) {
	theme := CatppuccinMocha
	got := starredRowBackground(theme)
	if got == theme.Bg {
		t.Fatal("expected starred row background to differ from the base background")
	}
	if want := mixColors(theme.Bg, starColor(theme), 0.12); got != want {
		t.Fatalf("starred row background = %q, want %q", got, want)
	}

	base := lipgloss.NewStyle().Background(theme.Bg)
	selected := lipgloss.NewStyle().Background(theme.Selected)
	if got := applyMessageRowState(base, selected, true, false, theme).GetBackground(); got != starredRowBackground(theme) {
		t.Fatalf("starred row did not receive tint: %q", got)
	}
	if got := applyMessageRowState(base, selected, true, true, theme).GetBackground(); got != theme.Selected {
		t.Fatalf("cursor background must override starred tint: %q", got)
	}
}

// Pressing "*" on the focused message issues a star command and, once its
// result is folded back in, flips the message's Starred state.
func TestStarKeyTogglesStarOnMessage(t *testing.T) {
	t.Setenv("XDG_DATA_HOME", t.TempDir())
	database, err := db.Open()
	if err != nil {
		t.Fatal(err)
	}
	defer database.Close()

	m := NewModel(database, config.DefaultConfig(), "dev", false)
	m.focused = paneMessages
	msgs := []db.Message{{ID: 1, Subject: "Star me", Date: time.Unix(100, 0)}}
	m.messages = msgs
	m.filteredMessages = msgs

	next, cmd := m.Update(tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune{'*'}})
	m = next.(Model)
	if cmd == nil {
		t.Fatal("expected a star command from '*'")
	}
	got, ok := cmd().(MessageStarredUpdatedMsg)
	if !ok {
		t.Fatalf("expected MessageStarredUpdatedMsg, got %#v", cmd())
	}
	if !got.Starred {
		t.Fatalf("expected star command to set Starred=true, got %+v", got)
	}

	next2, _ := m.Update(got)
	m = next2.(Model)
	if !m.messages[0].Starred {
		t.Fatalf("expected in-memory message to be starred after update")
	}
}

// The starred-first sort (key "t") floats starred messages to the top while
// preserving the relative order of everything else, and never mutates the
// underlying m.messages slice.
func TestStarredFirstSortFloatsStarredToTop(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.messages = []db.Message{
		{ID: 1, Subject: "A", Date: time.Unix(300, 0)},
		{ID: 2, Subject: "B starred", Date: time.Unix(200, 0), Starred: true},
		{ID: 3, Subject: "C", Date: time.Unix(100, 0)},
	}

	m.starredFirst = true
	m.applyFilter()
	if len(m.filteredMessages) != 3 {
		t.Fatalf("expected 3 messages, got %d", len(m.filteredMessages))
	}
	if m.filteredMessages[0].ID != 2 {
		t.Fatalf("expected starred message (ID 2) first, got ID %d", m.filteredMessages[0].ID)
	}
	if m.filteredMessages[1].ID != 1 || m.filteredMessages[2].ID != 3 {
		t.Fatalf("expected non-starred order preserved (1 then 3), got %d then %d",
			m.filteredMessages[1].ID, m.filteredMessages[2].ID)
	}
	// Sorting must not reorder the shared source slice.
	if m.messages[0].ID != 1 {
		t.Fatalf("expected m.messages untouched by sort, got first ID %d", m.messages[0].ID)
	}

	m.starredFirst = false
	m.applyFilter()
	if m.filteredMessages[0].ID != 1 {
		t.Fatalf("expected original order restored when starred-first off, got ID %d", m.filteredMessages[0].ID)
	}
}

// In threaded mode the starred-first sort must float threads containing a
// starred message to the top, even though buildMessageThreads orders by date.
func TestStarredFirstSortFloatsStarredThreadInThreadedMode(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.ThreadedConversations = true
	m := NewModel(nil, cfg, "dev", false)
	m.messages = []db.Message{
		// Newest thread, not starred.
		{ID: 1, MessageID: "<a@x>", Subject: "Newest", Date: time.Unix(300, 0)},
		// Older thread, starred — should float to the top when starred-first is on.
		{ID: 2, MessageID: "<b@x>", Subject: "Older starred", Date: time.Unix(100, 0), Starred: true},
	}

	m.starredFirst = true
	m.applyFilter()
	if len(m.messageThreads) != 2 {
		t.Fatalf("expected 2 threads, got %d", len(m.messageThreads))
	}
	if m.messageThreads[0].Representative.ID != 2 {
		t.Fatalf("expected starred thread (ID 2) first, got ID %d", m.messageThreads[0].Representative.ID)
	}

	m.starredFirst = false
	m.applyFilter()
	if m.messageThreads[0].Representative.ID != 1 {
		t.Fatalf("expected date order (newest ID 1) first when starred-first off, got ID %d", m.messageThreads[0].Representative.ID)
	}
}

// The Display.StarredFirst config value round-trips through the Settings overlay
// and seeds the model's runtime sort at startup.
func TestStarredFirstConfigPersistence(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.Display.StarredFirst = true

	s := newSettings(cfg, settingsUpdateState{})
	if !s.starredFirst {
		t.Fatal("expected settings to load starred-first from config")
	}
	s.starredFirst = false
	if next := s.ApplyTo(cfg); next.Display.StarredFirst {
		t.Fatal("expected ApplyTo to persist disabled starred-first")
	}

	m := NewModel(nil, cfg, "dev", false)
	if !m.starredFirst {
		t.Fatal("expected model to seed starredFirst from config at startup")
	}
}
