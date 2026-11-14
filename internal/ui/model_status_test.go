package ui

import (
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
)

// A clear timer scheduled for an earlier status must not erase a newer one.
func TestStaleStatusClearKeepsNewerStatus(t *testing.T) {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.setStatus("first", false)
	first := m.statusGeneration
	m.setStatus("second", true)

	next, _ := m.Update(StatusClearMsg{Generation: first})
	m = next.(Model)
	if m.statusMsg != "second" || !m.statusErr {
		t.Fatalf("stale clear erased the newer status: %q err=%v", m.statusMsg, m.statusErr)
	}

	next, _ = m.Update(StatusClearMsg{Generation: m.statusGeneration})
	m = next.(Model)
	if m.statusMsg != "" {
		t.Fatalf("current clear did not clear: %q", m.statusMsg)
	}
}
