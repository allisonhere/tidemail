package editor

import (
	"strings"
	"testing"

	"github.com/charmbracelet/lipgloss"
)

func TestViewLineCount(t *testing.T) {
	m := New(80, 5)
	m.InsertString("line one\nline two\nline three\nline four\nline five\nline six")
	
	sel := lipgloss.NewStyle().Reverse(true)
	view := m.View(sel)
	lines := strings.Split(view, "\n")
	
	t.Logf("total lines in model: %d, height: %d", m.TotalLines(), m.height)
	t.Logf("view returned %d lines", len(lines))
	for i, l := range lines {
		t.Logf("  [%d]: %q", i, l)
	}
	
	if len(lines) != 5 {
		t.Errorf("expected 5 visible lines, got %d", len(lines))
	}
}
