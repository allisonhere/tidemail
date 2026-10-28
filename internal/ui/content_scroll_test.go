package ui

import (
	"fmt"
	"strings"
	"testing"

	"github.com/allisonhere/tidemail/internal/config"
	"github.com/charmbracelet/bubbles/viewport"
)

// scrollModel builds a reader whose content is a two-line header, a blank line,
// then body lines longer than the viewport.
func scrollModel(body int) Model {
	m := NewModel(nil, config.DefaultConfig(), "dev", false)
	m.width, m.height = 100, 30
	lines := []string{"Subject line", "From: someone", ""}
	for i := 0; i < body; i++ {
		lines = append(lines, fmt.Sprintf("body line %d", i))
	}
	m.contentLines = lines
	m.contentLineCount = len(lines)
	m.contentFocusable = focusableFromLines(lines)
	m.viewport = viewport.New(80, m.contentBodyHeight())
	m.viewport.SetContent(strings.Join(lines, "\n"))
	return m
}

func TestScrollingUpRevealsMessageHeader(t *testing.T) {
	m := scrollModel(80)
	last := m.contentLineCount - 1
	m.contentFocusLine = last
	m.viewport.GotoBottom()
	if m.viewport.YOffset == 0 {
		t.Fatal("test needs content taller than the viewport")
	}
	for i := 0; i < last+5; i++ {
		m.moveContentFocusLine(-1)
	}
	if m.contentFocusLine != 3 {
		t.Fatalf("focus line = %d, want the first body line (3)", m.contentFocusLine)
	}
	if m.viewport.YOffset != 0 {
		t.Fatalf("YOffset = %d, want 0 so the header is visible", m.viewport.YOffset)
	}
}

func TestEnsureFocusVisibleShowsHeaderForFirstBodyLine(t *testing.T) {
	m := scrollModel(80)
	m.viewport.SetYOffset(40)
	m.contentFocusLine = 3
	m.ensureContentFocusVisible()
	if m.viewport.YOffset != 0 {
		t.Fatalf("YOffset = %d, want 0", m.viewport.YOffset)
	}
}

func TestEnsureFocusVisibleStillTracksBodyLines(t *testing.T) {
	m := scrollModel(80)
	m.viewport.SetYOffset(40)
	m.contentFocusLine = 20
	m.ensureContentFocusVisible()
	if m.viewport.YOffset != 20 {
		t.Fatalf("YOffset = %d, want 20 (focus line scrolled to the top edge)", m.viewport.YOffset)
	}
}

func TestHasFocusableBefore(t *testing.T) {
	f := []bool{false, false, false, true, true}
	if hasFocusableBefore(f, 3) {
		t.Fatal("first focusable line has nothing focusable before it")
	}
	if !hasFocusableBefore(f, 4) {
		t.Fatal("line 4 has line 3 before it")
	}
	if hasFocusableBefore(nil, 2) {
		t.Fatal("empty focusable set")
	}
}
