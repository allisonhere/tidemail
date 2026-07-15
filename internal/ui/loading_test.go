package ui

import (
	"strings"
	"testing"

	"github.com/allisonhere/tide/internal/config"
	tea "github.com/charmbracelet/bubbletea"
)

// The model starts in a loading state so the message pane can show a spinner
// while accounts/messages load asynchronously, and clears it once messages land.
func TestInitialLoadingShowsSpinnerThenClears(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	if !m.initialLoading {
		t.Fatal("expected initialLoading true right after NewModel")
	}
	if !m.spinnerActive() {
		t.Fatal("expected spinnerActive true while initial loading")
	}

	// Give the pane dimensions, then confirm the empty list shows the loading text.
	next, _ := m.Update(tea.WindowSizeMsg{Width: 100, Height: 30})
	m = next.(Model)
	if out := m.renderMessagesPane(); !strings.Contains(out, "Loading messages") {
		t.Fatalf("expected loading text in message pane while loading; got:\n%s", out)
	}

	// The first message load clears the loading state.
	next, _ = m.Update(MessagesLoadedMsg{MailboxID: 0, Messages: nil})
	m = next.(Model)
	if m.initialLoading {
		t.Fatal("expected initialLoading cleared after MessagesLoadedMsg")
	}
	if m.spinnerActive() {
		t.Fatal("expected spinnerActive false once loading is done")
	}
	if out := m.renderMessagesPane(); strings.Contains(out, "Loading messages") {
		t.Fatalf("did not expect loading text after load completed; got:\n%s", out)
	}
}
